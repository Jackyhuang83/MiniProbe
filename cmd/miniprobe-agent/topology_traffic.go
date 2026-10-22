package main

import (
    "bytes"
    "context"
    "crypto/ed25519"
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "os/exec"
    "regexp"
    "strconv"
    "strings"
    "sync"
    "syscall"
    "time"

    "github.com/Jackyhuang83/MiniProbe/internal/common"
)

const (
    topologyTrafficCapability = "topology-traffic-xray-v1"
    topologyTrafficInterval = time.Minute
    topologyTrafficMaxTargets = 4
    topologyTrafficOutputLimit = 64 << 10
)
var topologyOutboundTag = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,48}$`)

type topologyTrafficRunner struct {
    mu sync.RWMutex
    policy common.TopologyTrafficPolicy
    results []common.TopologyTrafficResult
    lastRun time.Time
}
func newTopologyTrafficRunner() *topologyTrafficRunner {
    r:=&topologyTrafficRunner{}
    go r.loop()
    return r
}
func (r *topologyTrafficRunner) loop() {
    tick:=time.NewTicker(2*time.Second)
    defer tick.Stop()
    for range tick.C {
        r.mu.Lock()
        p:=r.policy
        if !p.Enabled || len(p.Targets)==0 || (!r.lastRun.IsZero() && time.Since(r.lastRun)<topologyTrafficInterval) {
            r.mu.Unlock()
            continue
        }
        r.lastRun=time.Now()
        r.mu.Unlock()

        results:=make([]common.TopologyTrafficResult,0,len(p.Targets))
        for _,t:=range p.Targets {
            ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second)
            up,down,err:=readXrayOutboundCounter(ctx,t)
            cancel()
            if err!=nil {continue} // missing stats means NOT COLLECTED, never zero
            results=append(results,common.TopologyTrafficResult{
                LinkID:t.LinkID,Revision:p.Version,At:time.Now().UTC(),
                Source:"xray-outbound-tag",UplinkBytes:up,DownlinkBytes:down,
            })
        }
        r.mu.Lock()
        if r.policy.Version==p.Version && r.policy.Enabled {r.results=results}
        r.mu.Unlock()
    }
}
func (r *topologyTrafficRunner) snapshot() []common.TopologyTrafficResult {
    r.mu.RLock()
    defer r.mu.RUnlock()
    return append([]common.TopologyTrafficResult(nil),r.results...)
}
func (r *topologyTrafficRunner) apply(nodeID string,key ed25519.PublicKey,signed *common.SignedTopologyTrafficPolicy) error {
    if signed==nil {return nil}
    p:=signed.Policy
    if p.NodeID!=nodeID || p.Version<=0 || p.IntervalSeconds!=60 || len(p.Targets)>topologyTrafficMaxTargets {
        return errors.New("invalid traffic policy bounds")
    }
    raw,err:=base64.RawURLEncoding.DecodeString(signed.Signature)
    if err!=nil || len(raw)!=ed25519.SignatureSize {return errors.New("invalid traffic policy signature encoding")}
    payload,_:=json.Marshal(p)
    if !ed25519.Verify(key,payload,raw) {return errors.New("invalid traffic policy signature")}
    keys:=map[string]bool{}
    for _,t:=range p.Targets {
        if t.LinkID=="" || t.Port==0 || !topologyOutboundTag.MatchString(t.Tag) || keys[t.LinkID] {
            return errors.New("invalid outbound counter mapping")
        }
        keys[t.LinkID]=true
    }
    r.mu.Lock()
    defer r.mu.Unlock()
    if p.Version<r.policy.Version {return errors.New("replayed traffic policy")}
    if p.Version==r.policy.Version {
        old,_:=json.Marshal(r.policy)
        if !bytes.Equal(old,payload) {return errors.New("conflicting traffic policy revision")}
        return nil
    }
    r.policy=p
    r.results=nil
    r.lastRun=time.Time{}
    return nil
}

// Fixed read-only Xray CLI; never executes a command received from Server.
// Refuses symlinks, non-root-owned executables, and writable program files,
// because the Agent may run as root. No shell, no user-provided paths.
func trustedXrayExecutable() string {
    for _,path:=range []string{"/usr/local/bin/xray","/usr/bin/xray"} {
        st,err:=os.Lstat(path)
        if err!=nil || !st.Mode().IsRegular() || st.Mode().Perm()&0022!=0 || st.Mode().Perm()&0111==0 {continue}
        raw,ok:=st.Sys().(*syscall.Stat_t)
        if ok && raw.Uid==0 {return path}
    }
    return ""
}
type cappedWriter struct {
    bytes.Buffer
    limit int
}
func (w *cappedWriter) Write(b []byte)(int,error){
    if w.Len()+len(b)>w.limit {return 0,errors.New("stats output exceeds limit")}
    return w.Buffer.Write(b)
}
func readXrayOutboundCounter(ctx context.Context,target common.TopologyTrafficTarget)(uint64,uint64,error){
    if !topologyOutboundTag.MatchString(target.Tag) || target.Port==0 {return 0,0,errors.New("invalid outbound mapping")}
    binary:=trustedXrayExecutable()
    if binary=="" {return 0,0,errors.New("trusted Xray CLI unavailable")}
    pattern:="outbound>>>"+target.Tag+">>>traffic>>>"
    endpoint:="127.0.0.1:"+strconv.Itoa(int(target.Port))
    cmd:=exec.CommandContext(ctx,binary,"api","statsquery","--server="+endpoint,"-pattern",pattern)
    cmd.Stderr=&cappedWriter{limit:1024}
    output:=&cappedWriter{limit:topologyTrafficOutputLimit}
    cmd.Stdout=output
    if err:=cmd.Run();err!=nil{return 0,0,fmt.Errorf("read-only Xray statistics query failed: %w",err)}
    return parseXrayOutboundStats(output.Bytes(),target.Tag)
}

// Xray emits int64 protobuf values as JSON strings. Accept numeric JSON too.
// Stats are exact-tag matched, not totals or unrelated outbound/user counters.
// Counters are never reset by MiniProbe.
func parseXrayOutboundStats(b []byte,tag string)(uint64,uint64,error) {
    if !topologyOutboundTag.MatchString(tag) || len(b)>topologyTrafficOutputLimit {return 0,0,errors.New("invalid stats response")}
    var resp struct {
        Stat []struct {Name string `json:"name"`;Value json.Number `json:"value"`} `json:"stat"`
    }
    dec:=json.NewDecoder(bytes.NewReader(b))
    dec.UseNumber()
    if err:=dec.Decode(&resp);err!=nil{return 0,0,err}
    var up,down uint64
    hasUp,hasDown:=false,false
    for _,item:=range resp.Stat {
        switch item.Name {
        case "outbound>>>"+tag+">>>traffic>>>uplink":
            if hasUp {return 0,0,errors.New("duplicated outbound uplink counter")}
            n,err:=strconv.ParseUint(strings.TrimSpace(item.Value.String()),10,63)
            if err!=nil{return 0,0,err}
            up=n;hasUp=true
        case "outbound>>>"+tag+">>>traffic>>>downlink":
            if hasDown {return 0,0,errors.New("duplicated outbound downlink counter")}
            n,err:=strconv.ParseUint(strings.TrimSpace(item.Value.String()),10,63)
            if err!=nil{return 0,0,err}
            down=n;hasDown=true
        }
    }
    if !hasUp && !hasDown {return 0,0,errors.New("no outbound counters; collector disabled or tag unused")}
    return up,down,nil
}
