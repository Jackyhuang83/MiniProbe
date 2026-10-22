package main

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/Jackyhuang83/MiniProbe/internal/common"
)
func testTrafficServer() *server {
    s:=makeDev2Server()
    cfg:=s.db.Topology
    cfg.Links=[]topologyLinkConfig{{From:"a",To:"b",TrafficSource:"xray",TrafficTag:"hk-out",TrafficPort:10085}}
    s.db.Topology=cfg
    node:=s.db.States["a"]
    node.Capabilities=append(node.Capabilities,topologyTrafficCapability)
    s.db.States["a"]=node
    return s
}
func TestTopologyTrafficValidationAndOneTagPerLink(t *testing.T) {
    s:=testTrafficServer()
    if _,err:=validateTopology(s.db.Topology,s.db.Nodes);err!=nil {t.Fatal(err)}
    p:=s.signedTopologyTrafficPolicyLocked(s.db.Nodes["a"],common.Report{NodeID:"a",Capabilities:[]string{topologyTrafficCapability}})
    if p==nil || len(p.Policy.Targets)!=1 || p.Policy.Targets[0].Tag!="hk-out" || p.Policy.Targets[0].Port!=10085 {t.Fatalf("wrong signed mapping: %+v",p)}
    if s.signedTopologyTrafficPolicyLocked(s.db.Nodes["a"],common.Report{NodeID:"a"})!=nil {t.Fatal("legacy Agent received collector policy")}
    bad:=s.db.Topology
    bad.Links=append([]topologyLinkConfig(nil),s.db.Topology.Links...)
    bad.Links[0].TrafficTag="../secrets"
    if _,err:=validateTopology(bad,s.db.Nodes);err==nil {t.Fatal("unsafe label accepted")}
    second:=bad.Links[0]
    second.TrafficTag="hk-out"
    bad.Links[0].TrafficTag="hk-out"
    second.To="c"
    bad.Nodes=append(bad.Nodes,topologyNodeConfig{ID:"c",Role:"exit"})
    known:=map[string]nodeConfig{"a":s.db.Nodes["a"],"b":s.db.Nodes["b"],"c":{ID:"c"}}
    bad.Links=append(bad.Links,second)
    if _,err:=validateTopology(bad,known);err==nil {t.Fatal("duplicate outbound tag double counted")}
    w:=httptest.NewRecorder()
    s.topologyAPI(w,httptest.NewRequest(http.MethodGet,"/api/v1/topology",nil))
    var obj map[string]any
    if err:=json.Unmarshal(w.Body.Bytes(),&obj);err!=nil {t.Fatal(err)}
    if string(w.Body.Bytes())=="" {t.Fatal("empty response")}
    if string(w.Body.Bytes())!="" && containsTrafficSecret(w.Body.String(),"hk-out") {t.Fatal("outbound tag exposed to browser")}
}
func containsTrafficSecret(s,token string)bool{
    for i:=0;i+len(token)<=len(s);i++ {if s[i:i+len(token)]==token{return true}}
    return false
}
func TestTopologyTrafficBaselineGapResetAndRealtime(t *testing.T){
    s:=testTrafficServer()
    now:=time.Now().UTC().Truncate(time.Second)
    if now.Hour()==0 && now.Minute()<5 {t.Skip("test cannot cross UTC day boundary")}
    caps:=[]string{topologyTrafficCapability}
    send:=func(at time.Time,up,down uint64){
        s.ingestTopologyTraffic("a",[]common.TopologyTrafficResult{{
            LinkID:"a~b",Revision:2,Source:"xray-outbound-tag",At:at,
            UplinkBytes:up,DownlinkBytes:down,
        }},caps)
    }
    send(now.Add(-2*time.Minute),1000,2000)
    if m:=s.topologyTrafficMeasurement("a~b",2,now);m.Status!="baseline"||m.TodayUplink!=nil {t.Fatalf("first counter treated as traffic: %+v",m)}
    send(now.Add(-time.Minute),3000,6000)
    m:=s.topologyTrafficMeasurement("a~b",2,now)
    if m.Status!="collected" || m.TodayUplink==nil || *m.TodayUplink!=2000 || *m.TodayDownlink!=4000 || m.RateUplink==nil {t.Fatalf("wrong delta: %+v",m)}
    // Replayed HTTP reports must not count traffic twice.
    send(now.Add(-time.Minute),3000,6000)
    if len(s.topologyTraffic)!=1 || s.topologyTraffic["a~b"].TodayUplink!=2000 {t.Fatal("duplicate report added traffic")}
    send(now,100,200)
    if !s.topologyTraffic["a~b"].Incomplete || s.topologyTraffic["a~b"].TodayUplink!=2000 {t.Fatal("Xray restart counted as wraparound traffic")}
    s.resetTopologyTraffic()
    if got:=s.topologyTrafficMeasurement("a~b",2,time.Now());got.Status!="pending" {t.Fatalf("disable/revision did not clear counters: %+v",got)}
}
func TestTopologyTrafficNeverUsesHostInterfaceCounters(t *testing.T){
    s:=testTrafficServer()
    v:=s.db.States["a"];v.Metrics.NetTxTotal=1_000_000_000_000;v.Metrics.NetRxTotal=2_000_000_000_000;s.db.States["a"]=v
    w:=httptest.NewRecorder()
    s.topologyAPI(w,httptest.NewRequest(http.MethodGet,"/api/v1/topology",nil))
    var out topologySnapshot
    if err:=json.Unmarshal(w.Body.Bytes(),&out);err!=nil{t.Fatal(err)}
    if out.Summary.TrafficTodayBytes!=nil || len(out.Links)!=1 || out.Links[0].TrafficStatus!="pending"{
        t.Fatalf("NIC substituted for real link data: %+v",out)
    }
}
func TestTopologyHistoryCheckpointWithoutCredentialData(t *testing.T){
    s:=testTrafficServer()
    dir:=t.TempDir()
    s.dataFile=filepath.Join(dir,"miniprobe.json")
    s.downloadsDir=dir
    s.topologyTrafficHistoryDir=filepath.Join(dir,"topology-traffic")
    now:=time.Now().UTC()
    s.topologyTraffic=map[string]*topologyTrafficState{
        "a~b":{Revision:2,Source:"xray-outbound-tag",Day:now.Format("2006-01-02"),LastAt:now,
            LastUplink:12345,LastDownlink:67890,TodayUplink:400,TodayDownlink:800,HasDelta:true},
    }
    if err:=s.flushTopologyTrafficHistory(now);err!=nil {t.Fatal(err)}
    b,err:=os.ReadFile(filepath.Join(s.topologyTrafficHistoryDir,now.Format("2006-01-02")+".ndjson"))
    if err!=nil{t.Fatal(err)}
    if containsTrafficSecret(string(b),"hk-out") || containsTrafficSecret(string(b),"10085"){t.Fatal("history leaked tag or port")}
    s.topologyTraffic=nil
    if err:=s.restoreTopologyTrafficHistory(now);err!=nil{t.Fatal(err)}
    got:=s.topologyTraffic["a~b"]
    if got==nil || got.LastUplink!=12345 || got.TodayDownlink!=800 || !got.Incomplete {t.Fatalf("restore lost baseline or gap flag: %+v",got)}
}
