package main

import (
    "bytes"
    "crypto/ed25519"
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "time"

    "github.com/Jackyhuang83/MiniProbe/internal/common"
)

const (
    topologyTrafficCapability = "topology-traffic-xray-v1"
    topologyTrafficMaxTargets = 4
    topologyTrafficFreshness = 3*time.Minute
    topologyTrafficMaxGap = 4*time.Minute
    topologyTrafficHistoryInterval = 15*time.Minute
    topologyTrafficHistoryLimit = int64(32<<20)
    topologyTrafficRetentionDays = 90
)
type topologyTrafficState struct {
    Revision int64 `json:"revision"`
    Source string `json:"source"`
    Day string `json:"day"`
    LastAt time.Time `json:"last_at"`
    LastUplink uint64 `json:"last_uplink"`
    LastDownlink uint64 `json:"last_downlink"`
    TodayUplink uint64 `json:"today_uplink"`
    TodayDownlink uint64 `json:"today_downlink"`
    RateUplink float64 `json:"-"`
    RateDownlink float64 `json:"-"`
    HasDelta bool `json:"has_delta"`
    Incomplete bool `json:"incomplete"`
}
type topologyTrafficCheckpoint struct {
    LinkID string `json:"link_id"`
    At int64 `json:"at"`
    Revision int64 `json:"revision"`
    Day string `json:"day"`
    Uplink uint64 `json:"uplink"`
    Downlink uint64 `json:"downlink"`
    LastUplink uint64 `json:"last_uplink"`
    LastDownlink uint64 `json:"last_downlink"`
    LastSampleAt int64 `json:"last_sample_at"`
    HasDelta bool `json:"has_delta"`
    Incomplete bool `json:"incomplete"`
}
type topologyTrafficView struct {
    Status string `json:"status"`
    Source string `json:"source,omitempty"`
    TodayUplink *uint64 `json:"today_uplink_bytes"`
    TodayDownlink *uint64 `json:"today_downlink_bytes"`
    RateUplink *float64 `json:"uplink_bps"`
    RateDownlink *float64 `json:"downlink_bps"`
    Incomplete bool `json:"incomplete"`
    LastSample time.Time `json:"last_sample,omitempty"`
}
func topologyTrafficMappingValid(tag string,port uint16) bool {
    if port==0 || len(tag)<1 || len(tag)>48{return false}
    for _,c:=range tag {
        if c>='a'&&c<='z' || c>='A'&&c<='Z' || c>='0'&&c<='9' || c=='_' || c=='-' || c=='.'{continue}
        return false
    }
    return true
}
func (s *server) signedTopologyTrafficPolicyLocked(cfg nodeConfig,rep common.Report)*common.SignedTopologyTrafficPolicy{
    if !hasCapability(rep.Capabilities,topologyTrafficCapability){return nil}
    c:=s.db.Topology
    revision:=c.Revision
    if revision<=0 {revision=1}
    p:=common.TopologyTrafficPolicy{Version:revision,NodeID:cfg.ID,Enabled:c.Enabled,IntervalSeconds:60,
        Targets:make([]common.TopologyTrafficTarget,0,topologyTrafficMaxTargets)}
    if c.Enabled {
        for _,l:=range c.Links {
            if l.From!=cfg.ID || l.TrafficSource!="xray" || !topologyTrafficMappingValid(l.TrafficTag,l.TrafficPort){continue}
            p.Targets=append(p.Targets,common.TopologyTrafficTarget{LinkID:topologyEdgeID(l.From,l.To),Tag:l.TrafficTag,Port:l.TrafficPort})
            if len(p.Targets)>=topologyTrafficMaxTargets {break}
        }
    }
    b,_:=json.Marshal(p)
    priv:=ed25519.NewKeyFromSeed(s.secret[:ed25519.SeedSize])
    return &common.SignedTopologyTrafficPolicy{Policy:p,Signature:base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv,b))}
}
func (s *server) ingestTopologyTraffic(source string,samples []common.TopologyTrafficResult,caps []string){
    if !hasCapability(caps,topologyTrafficCapability) || len(samples)==0 || len(samples)>topologyTrafficMaxTargets {return}
    s.mu.RLock()
    cfg:=s.db.Topology
    if !cfg.Enabled {s.mu.RUnlock();return}
    allowed:=map[string]bool{}
    for _,l:=range cfg.Links {
        if l.From==source && l.TrafficSource=="xray" && topologyTrafficMappingValid(l.TrafficTag,l.TrafficPort){
            allowed[topologyEdgeID(l.From,l.To)]=true
        }
    }
    s.mu.RUnlock()

    now:=time.Now().UTC()
    s.topologyTrafficMu.Lock()
    defer s.topologyTrafficMu.Unlock()
    if s.topologyTraffic==nil {s.topologyTraffic=map[string]*topologyTrafficState{}}
    for _,sample:=range samples {
        if !allowed[sample.LinkID] || sample.Revision!=cfg.Revision ||
            sample.Source!="xray-outbound-tag" || sample.At.IsZero() ||
            sample.At.After(now.Add(30*time.Second)) || now.Sub(sample.At)>4*time.Minute {continue}
        st:=s.topologyTraffic[sample.LinkID]
        if st==nil || st.Revision!=cfg.Revision {
            st=&topologyTrafficState{Revision:cfg.Revision,Source:sample.Source}
            s.topologyTraffic[sample.LinkID]=st
        }
        if !sample.At.After(st.LastAt) {continue} // same data in subsequent 2s Agent reports is not a new event
        day:=sample.At.UTC().Format("2006-01-02")
        if st.Day!=day {
            st.Day=day
            st.TodayUplink=0
            st.TodayDownlink=0
            st.HasDelta=false
            st.Incomplete=false
        }
        if !st.LastAt.IsZero() {
            gap:=sample.At.Sub(st.LastAt)
            if sample.UplinkBytes<st.LastUplink || sample.DownlinkBytes<st.LastDownlink {
                st.Incomplete=true // Xray counters restarted; no unsigned wrap arithmetic
                st.RateUplink=0
                st.RateDownlink=0
            }else if gap<=topologyTrafficMaxGap && gap>0 {
                up:=sample.UplinkBytes-st.LastUplink
                down:=sample.DownlinkBytes-st.LastDownlink
                st.TodayUplink=saturatingTrafficSum(st.TodayUplink,up)
                st.TodayDownlink=saturatingTrafficSum(st.TodayDownlink,down)
                st.RateUplink=float64(up)*8/gap.Seconds()
                st.RateDownlink=float64(down)*8/gap.Seconds()
                st.HasDelta=true
            }else {
                st.Incomplete=true
                st.RateUplink=0
                st.RateDownlink=0
            }
        }
        st.LastAt=sample.At
        st.LastUplink=sample.UplinkBytes
        st.LastDownlink=sample.DownlinkBytes
    }
}
func (s *server) topologyTrafficMeasurement(id string,rev int64,now time.Time)topologyTrafficView{
    out:=topologyTrafficView{Status:"pending"}
    s.topologyTrafficMu.RLock()
    defer s.topologyTrafficMu.RUnlock()
    st:=s.topologyTraffic[id]
    if st==nil||st.Revision!=rev {return out}
    out.LastSample=st.LastAt
    out.Source=st.Source
    out.Incomplete=st.Incomplete
    if !st.HasDelta {out.Status="baseline";return out}
    up,down:=st.TodayUplink,st.TodayDownlink
    out.TodayUplink=&up
    out.TodayDownlink=&down
    if st.LastAt.UTC().Format("2006-01-02")!=now.UTC().Format("2006-01-02"){
        out.Status="stale";out.TodayUplink=nil;out.TodayDownlink=nil;return out
    }
    if now.Sub(st.LastAt)>topologyTrafficFreshness {out.Status="stale";return out}
    out.Status="collected"
    rateUp,rateDown:=st.RateUplink,st.RateDownlink
    out.RateUplink=&rateUp;out.RateDownlink=&rateDown
    return out
}
func (s *server) resetTopologyTraffic(){
    s.topologyTrafficMu.Lock()
    s.topologyTraffic=map[string]*topologyTrafficState{}
    s.topologyTrafficMu.Unlock()
}
// DEV3 traffic history writes are wholly detached from HTTP Agent ingestion.
// No source IP, proxy config, tag names or credentials are written to the log.
func (s *server) topologyTrafficHistoryLoop(){
    ticker:=time.NewTicker(topologyTrafficHistoryInterval)
    defer ticker.Stop()
    for range ticker.C {
        if err:=s.flushTopologyTrafficHistory(time.Now().UTC());err!=nil {
            fmt.Fprintf(os.Stderr,"MiniProbe topology traffic history: %v\n",err)
        }
    }
}
func (s *server) flushTopologyTrafficHistory(now time.Time)error{
    s.topologyTrafficMu.RLock()
    snapshots:=make([]topologyTrafficCheckpoint,0,len(s.topologyTraffic))
    for id,st:=range s.topologyTraffic {
        if !st.HasDelta || st.LastAt.IsZero() || st.Day!=now.Format("2006-01-02") {continue}
        snapshots=append(snapshots,topologyTrafficCheckpoint{
            LinkID:id,At:now.Unix(),Revision:st.Revision,Day:st.Day,
            Uplink:st.TodayUplink,Downlink:st.TodayDownlink,
            LastUplink:st.LastUplink,LastDownlink:st.LastDownlink,LastSampleAt:st.LastAt.Unix(),
            HasDelta:st.HasDelta,Incomplete:st.Incomplete,
        })
    }
    s.topologyTrafficMu.RUnlock()
    if len(snapshots)==0{return nil}
    sort.Slice(snapshots,func(i,j int)bool{return snapshots[i].LinkID<snapshots[j].LinkID})
    dir:=s.topologyTrafficHistoryDir
    if dir=="" {dir=filepath.Join(filepath.Dir(s.dataFile),"topology-traffic")}
    if err:=os.MkdirAll(dir,0750);err!=nil{return err}
    name:=filepath.Join(dir,now.Format("2006-01-02")+".ndjson")
    if st,err:=os.Lstat(name);err==nil && !st.Mode().IsRegular(){return errors.New("unsafe traffic history file")}
    var payload []byte
    for _,v:=range snapshots {
        b,err:=json.Marshal(v);if err!=nil{return err}
        payload=append(payload,b...);payload=append(payload,'\n')
    }
    // Preserve 2 GiB combined MiniProbe budget, with a small topology cap.
    used:=s.storageUsage()
    if used>=storageHardLimit || uint64(len(payload))>storageHardLimit-used {
        return errors.New("MiniProbe storage budget reached")
    }
    if err:=pruneTopologyTrafficFiles(dir,now,int64(len(payload)));err!=nil{return err}
    f,err:=os.OpenFile(name,os.O_APPEND|os.O_CREATE|os.O_WRONLY,0600)
    if err!=nil{return err}
    _,writeErr:=f.Write(payload)
    closeErr:=f.Close()
    if writeErr!=nil{return writeErr}
    return closeErr
}
func pruneTopologyTrafficFiles(dir string,now time.Time,incoming int64)error{
    type entry struct{path,date string;size int64}
    list:=[]entry{}
    total:=incoming
    names,err:=os.ReadDir(dir)
    if err!=nil{return err}
    cutoff:=now.AddDate(0,0,-topologyTrafficRetentionDays).Format("2006-01-02")
    for _,d:=range names {
        if !d.Type().IsRegular() || !strings.HasSuffix(d.Name(),".ndjson"){continue}
        day:=strings.TrimSuffix(d.Name(),".ndjson")
        if _,err:=time.Parse("2006-01-02",day);err!=nil{continue}
        path:=filepath.Join(dir,d.Name())
        if day<cutoff {if err:=os.Remove(path);err!=nil{return err};continue}
        info,err:=d.Info();if err!=nil{return err}
        total+=info.Size()
        list=append(list,entry{path:path,date:day,size:info.Size()})
    }
    sort.Slice(list,func(i,j int)bool{return list[i].date<list[j].date})
    for _,f:=range list {
        if total<=topologyTrafficHistoryLimit {break}
        if err:=os.Remove(f.path);err!=nil{return err}
        total-=f.size
    }
    if total>topologyTrafficHistoryLimit {return errors.New("topology history cap reached")}
    return nil
}
// Keep the most recent checkpoint per link, so process restart does not
// mistake lifetime Xray counters for new traffic. Counters are baselines only.
func (s *server) restoreTopologyTrafficHistory(now time.Time)error{
    path:=filepath.Join(s.topologyTrafficHistoryDir,now.UTC().Format("2006-01-02")+".ndjson")
    b,err:=os.ReadFile(path)
    if os.IsNotExist(err){return nil}
    if err!=nil{return err}
    if len(b)>8<<20{return errors.New("today topology checkpoint file exceeds read cap")}
    latest:=map[string]topologyTrafficCheckpoint{}
    for _,line:=range bytes.Split(b,[]byte{10}) {
        var rec topologyTrafficCheckpoint
        if json.Unmarshal(line,&rec)!=nil || rec.Day!=now.UTC().Format("2006-01-02"){continue}
        if prev,ok:=latest[rec.LinkID];!ok || rec.At>prev.At {latest[rec.LinkID]=rec}
    }
    s.topologyTrafficMu.Lock()
    defer s.topologyTrafficMu.Unlock()
    if s.topologyTraffic==nil{s.topologyTraffic=map[string]*topologyTrafficState{}}
    for id,v:=range latest {
        if !strings.Contains(id,"~"){continue}
        s.topologyTraffic[id]=&topologyTrafficState{
            Revision:v.Revision,Source:"xray-outbound-tag",Day:v.Day,
            LastAt:time.Unix(v.LastSampleAt,0).UTC(),LastUplink:v.LastUplink,LastDownlink:v.LastDownlink,
            TodayUplink:v.Uplink,TodayDownlink:v.Downlink,HasDelta:v.HasDelta,Incomplete:true,
        }
    }
    return nil
}
