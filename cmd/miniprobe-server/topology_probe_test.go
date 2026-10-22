package main

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/Jackyhuang83/MiniProbe/internal/common"
)

func makeDev2Server() *server {
    s:=topologyTestServer()
    s.secret=[]byte("0123456789abcdef0123456789abcdef")
    s.db.Topology=topologyConfig{
        Enabled:true,Revision:2,
        Nodes:[]topologyNodeConfig{{ID:"a",Role:"entry"},{ID:"b",Role:"exit"}},
        Links:[]topologyLinkConfig{{From:"a",To:"b",ProbeAddress:"203.0.113.10",ProbePort:443}},
    }
    a:=s.db.States["a"];a.Capabilities=[]string{topologyProbeCapability};s.db.States["a"]=a
    return s
}
func TestTopologyProbeConfigValidationAndIsolation(t *testing.T) {
    s:=makeDev2Server()
    good:=s.db.Topology
    if _,err:=validateTopology(good,s.db.Nodes);err!=nil {t.Fatal(err)}
    for _,bad:=range []string{"127.0.0.1","169.254.169.254","100.100.100.200","168.63.129.16","fd00:ec2::254","localhost","fe80::1",""} {
        c:=good;c.Links=append([]topologyLinkConfig(nil),good.Links...)
        c.Links[0].ProbeAddress=bad
        if _,err:=validateTopology(c,s.db.Nodes);err==nil {t.Errorf("unsafe probe config accepted: %s",bad)}
    }
    c:=good;c.Links=append([]topologyLinkConfig(nil),good.Links...)
    c.Links[0].ProbeAddress="fd00::2"
    if _,err:=validateTopology(c,s.db.Nodes);err!=nil {t.Fatalf("ULA target should be allowed: %v",err)}
    w:=httptest.NewRecorder()
    s.topologyAPI(w,httptest.NewRequest(http.MethodGet,"/api/v1/topology",nil))
    if strings.Contains(w.Body.String(),"203.0.113.10") || strings.Contains(w.Body.String(),"\"probe_port\"") {
        t.Fatal("read-only browser endpoint exposed probing IP:port")
    }
}
func TestTopologySignedOnlyForCapableAgent(t *testing.T) {
    s:=makeDev2Server()
    cfg:=s.db.Nodes["a"]
    p:=s.signedTopologyProbePolicyLocked(cfg,common.Report{NodeID:"a",Capabilities:[]string{topologyProbeCapability}})
    if p==nil || !p.Policy.Enabled || p.Policy.Version!=2 || len(p.Policy.Targets)!=1 || p.Policy.Targets[0].Address!="203.0.113.10" {
        t.Fatalf("wrong probe policy: %+v",p)
    }
    if s.signedTopologyProbePolicyLocked(cfg,common.Report{NodeID:"a"})!=nil {
        t.Fatal("legacy Agent must not receive topology probe policy")
    }
    other:=s.signedTopologyProbePolicyLocked(s.db.Nodes["b"],common.Report{NodeID:"b",Capabilities:[]string{topologyProbeCapability}})
    if other==nil || len(other.Policy.Targets)!=0 {t.Fatal("destination agent should not probe source")}
}
func TestTopologySampleDedupAndOutageThreshold(t *testing.T) {
    s:=makeDev2Server()
    first:=time.Now().UTC().Add(-90*time.Second)
    sample:=common.TopologyProbeResult{
        LinkID:"a~b",To:"b",Revision:2,Attempts:3,Succeeded:3,
        ProbeAt:first,MinMS:20,MaxMS:30,AvgMS:25,
    }
    s.ingestTopologyProbes("a",[]common.TopologyProbeResult{sample},[]string{topologyProbeCapability})
    s.ingestTopologyProbes("a",[]common.TopologyProbeResult{sample},[]string{topologyProbeCapability})
    if len(s.topologyStats["a~b"].Samples)!=1 {t.Fatal("repeat report counted twice")}
    for i:=0;i<3;i++ {
        lost:=common.TopologyProbeResult{LinkID:"a~b",To:"b",Revision:2,Attempts:3,ProbeAt:first.Add(time.Duration(i+1)*20*time.Second)}
        s.ingestTopologyProbes("a",[]common.TopologyProbeResult{lost},[]string{topologyProbeCapability})
    }
    got:=s.topologyMeasurement("a~b",2,time.Now())
    if got.NetworkStatus!="interrupted" || got.Reachability!="unreachable" || got.LossPct==nil || *got.LossPct!=75 || got.LastSuccess.IsZero() || got.LastFailure.IsZero() {
        t.Fatalf("expected 3 consecutive failures, last known success and 75pct loss; got %+v",got)
    }
    invalid:=common.TopologyProbeResult{LinkID:"a~b",To:"b",Revision:999,Attempts:3,ProbeAt:time.Now(),Succeeded:3,MinMS:5,MaxMS:10,AvgMS:7}
    s.ingestTopologyProbes("a",[]common.TopologyProbeResult{invalid},[]string{topologyProbeCapability})
    if len(s.topologyStats["a~b"].Samples)!=4 {t.Fatal("wrong revision accepted")}
    s.resetTopologyStats()
    if got:=s.topologyMeasurement("a~b",2,time.Now());got.HasData {t.Fatal("reset must invalidate old statistics")}
}
func TestTopologyNetworkHealthNotProxyHealth(t *testing.T) {
    s:=makeDev2Server()
    sample:=common.TopologyProbeResult{LinkID:"a~b",To:"b",Revision:2,Attempts:3,Succeeded:3,MinMS:10,MaxMS:13,AvgMS:11,ProbeAt:time.Now().UTC()}
    s.ingestTopologyProbes("a",[]common.TopologyProbeResult{sample},[]string{topologyProbeCapability})
    w:=httptest.NewRecorder()
    s.topologyAPI(w,httptest.NewRequest("GET","/api/v1/topology",nil))
    var out topologySnapshot
    if err:=json.Unmarshal(w.Body.Bytes(),&out);err!=nil {t.Fatal(err)}
    if len(out.Links)!=1 || out.Links[0].NetworkStatus!="healthy" || out.Links[0].Reachability!="reachable" || out.Links[0].ProxyHealth!="not_tested" || out.Links[0].TrafficStatus!="not_collected" {
        t.Fatalf("proxy health or NIC traffic was improperly inferred: %+v",out.Links)
    }
    if out.Summary.HealthyLinks==nil || *out.Summary.HealthyLinks!=1 || out.Summary.AverageRTTMS==nil {t.Fatal("missing real network KPI")}
}
