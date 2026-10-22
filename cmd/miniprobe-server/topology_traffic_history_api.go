package main

import (
    "bytes"
    "encoding/json"
    "net/http"
    "os"
    "path/filepath"
    "sort"
    "strconv"
    "time"
)

type topologyHistoryDay struct {
    Date string `json:"date"`
    Links []topologyHistoryLink `json:"links"`
}
type topologyHistoryLink struct {
    ID string `json:"id"`
    UplinkBytes uint64 `json:"uplink_bytes"`
    DownlinkBytes uint64 `json:"downlink_bytes"`
    Incomplete bool `json:"incomplete"`
}
func (s *server) topologyTrafficHistoryAPI(w http.ResponseWriter,r *http.Request){
    if r.Method!=http.MethodGet {http.Error(w,"method not allowed",http.StatusMethodNotAllowed);return}
    n,_:=strconv.Atoi(r.URL.Query().Get("days"))
    if n!=7 && n!=30 && n!=90 {n=7}
    s.mu.RLock()
    cfg:=s.db.Topology
    permitted:=map[string]bool{}
    if cfg.Enabled {
        for _,l:=range cfg.Links {
            if l.TrafficSource=="xray" {permitted[topologyEdgeID(l.From,l.To)]=true}
        }
    }
    s.mu.RUnlock()
    out:=make([]topologyHistoryDay,0,n)
    dir:=s.topologyTrafficHistoryDir
    now:=time.Now().UTC()
    for i:=n-1;i>=0;i-- {
        date:=now.AddDate(0,0,-i).Format("2006-01-02")
        path:=filepath.Join(dir,date+".ndjson")
        info,err:=os.Lstat(path)
        if err!=nil||!info.Mode().IsRegular()||info.Size()>8<<20 {continue}
        b,err:=os.ReadFile(path);if err!=nil{continue}
        records:=map[string]topologyTrafficCheckpoint{}
        for _,line:=range bytes.Split(b,[]byte{10}) {
            var v topologyTrafficCheckpoint
            if json.Unmarshal(line,&v)!=nil || v.Day!=date || v.Revision!=cfg.Revision || !permitted[v.LinkID]{continue}
            if old,ok:=records[v.LinkID];!ok || v.At>=old.At {records[v.LinkID]=v}
        }
        if len(records)==0 {continue} // missing data is not zero bytes
        day:=topologyHistoryDay{Date:date,Links:make([]topologyHistoryLink,0,len(records))}
        for id,v:=range records {
            day.Links=append(day.Links,topologyHistoryLink{
                ID:id,UplinkBytes:v.Uplink,DownlinkBytes:v.Downlink,Incomplete:v.Incomplete,
            })
        }
        sort.Slice(day.Links,func(i,j int)bool{return day.Links[i].ID<day.Links[j].ID})
        out=append(out,day)
    }
    noStore(w)
    writeJSON(w,http.StatusOK,map[string]any{
        "days":out,"requested_days":n,"time_basis":"UTC",
        "accounting":"per-hop-outbound-counter; multi-hop traffic may be double-counted",
    })
}
