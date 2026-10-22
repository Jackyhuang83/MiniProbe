package main

import (
    "context"
    "crypto/ed25519"
    "encoding/base64"
    "encoding/json"
    "errors"
    "net"
    "strconv"
    "sync"
    "time"

    "github.com/Jackyhuang83/MiniProbe/internal/common"
)

const (
    topologyProbeCapability = "topology-tcp-probe-v1"
    topologyMaxTargetsPerAgent = 4
    topologyProbeAttempts = 3
    topologyProbeTimeout = 1500 * time.Millisecond
    topologyProbeInterval = time.Minute
)

// topologyProbeRunner has its own goroutine so slow/unreachable destinations
// cannot delay the Agent's traffic accounting, existing probes, or reporting.
// It NEVER opens a listening socket or invokes an external command.
type topologyProbeRunner struct {
    mu sync.RWMutex
    policy common.TopologyProbePolicy
    results []common.TopologyProbeResult
    lastRun time.Time
}

func newTopologyProbeRunner() *topologyProbeRunner {
    r := &topologyProbeRunner{}
    go r.loop()
    return r
}
func (r *topologyProbeRunner) loop() {
    ticker := time.NewTicker(2*time.Second)
    defer ticker.Stop()
    for range ticker.C {
        r.mu.Lock()
        p := r.policy
        if !p.Enabled || len(p.Targets)==0 || (!r.lastRun.IsZero() && time.Since(r.lastRun)<topologyProbeInterval) {
            r.mu.Unlock()
            continue
        }
        r.lastRun=time.Now()
        r.mu.Unlock()
        samples:=runTopologyProbeBatch(p)
        r.mu.Lock()
        if r.policy.Version==p.Version && r.policy.Enabled { r.results=samples }
        r.mu.Unlock()
    }
}
func (r *topologyProbeRunner) snapshot() []common.TopologyProbeResult {
    r.mu.RLock()
    defer r.mu.RUnlock()
    return append([]common.TopologyProbeResult(nil),r.results...)
}
func (r *topologyProbeRunner) apply(nodeID string, key ed25519.PublicKey, signed *common.SignedTopologyProbePolicy) error {
    if signed == nil { return nil }
    p:=signed.Policy
    if p.NodeID!=nodeID { return errors.New("topology policy node mismatch") }
    if p.Version<=0 || p.IntervalSeconds!=60 || len(p.Targets)>topologyMaxTargetsPerAgent {
        return errors.New("invalid topology policy bounds")
    }
    sig,err:=base64.RawURLEncoding.DecodeString(signed.Signature)
    if err!=nil || len(sig)!=ed25519.SignatureSize {return errors.New("invalid topology policy signature encoding")}
    b,_:=json.Marshal(p)
    if !ed25519.Verify(key,b,sig) {return errors.New("invalid topology policy signature")}
    seen:=map[string]bool{}
    for _,t:=range p.Targets {
        if t.LinkID=="" || t.To=="" || seen[t.LinkID] || t.Port==0 || !validTopologyProbeIP(t.Address) {
            return errors.New("invalid topology probe target")
        }
        seen[t.LinkID]=true
    }
    r.mu.Lock()
    defer r.mu.Unlock()
    if p.Version<r.policy.Version {return errors.New("replayed topology policy version")}
    // A policy with the same revision but changed content should never happen;
    // refuse it rather than accepting a conflicting signed configuration.
    if p.Version==r.policy.Version {
        old,_:=json.Marshal(r.policy)
        if string(old)!=string(b) {return errors.New("conflicting topology policy revision")}
        return nil
    }
    r.policy=p
    r.results=nil
    r.lastRun=time.Time{} // fresh signed policy triggers a fresh measurement
    return nil
}

// Deny metadata/local-service access, broadcast, multicast, and link-local.
// Private IPv4 and IPv6 ULA are allowed for legitimate private VPS interlinks.
// Only literal IPs are accepted: no DNS rebinds or URL-based SSRF.
func validTopologyProbeIP(raw string) bool {
    ip:=net.ParseIP(raw)
    return ip!=nil && ip.IsGlobalUnicast() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}
func runTopologyProbeBatch(p common.TopologyProbePolicy) []common.TopologyProbeResult {
    out:=make([]common.TopologyProbeResult,len(p.Targets))
    sem:=make(chan struct{},2)
    var wg sync.WaitGroup
    for i,t:=range p.Targets {
        wg.Add(1)
        go func(i int,t common.TopologyProbeTarget) {
            defer wg.Done()
            sem<-struct{}{}
            defer func(){<-sem}()
            out[i]=probeTopologyTCP(p.Version,t)
        }(i,t)
    }
    wg.Wait()
    return out
}
func probeTopologyTCP(version int64,t common.TopologyProbeTarget) common.TopologyProbeResult {
    dialer:=net.Dialer{Timeout:topologyProbeTimeout,KeepAlive:-1}
    return probeTopologyTCPWithDial(version,t,dialer.DialContext)
}
func probeTopologyTCPWithDial(version int64,t common.TopologyProbeTarget,dial func(context.Context,string,string)(net.Conn,error)) common.TopologyProbeResult {
    result:=common.TopologyProbeResult{
        LinkID:t.LinkID,To:t.To,Revision:version,
        ProbeAt:time.Now().UTC(), Attempts:topologyProbeAttempts,
    }
    if !validTopologyProbeIP(t.Address) || t.Port==0 {return result}
    address:=net.JoinHostPort(t.Address,strconv.Itoa(int(t.Port)))
    network:="tcp6"
    if net.ParseIP(t.Address).To4()!=nil {network="tcp4"}
    total:=0.0
    for i:=0;i<topologyProbeAttempts;i++ {
        ctx,cancel:=context.WithTimeout(context.Background(),topologyProbeTimeout)
        start:=time.Now()
        conn,err:=dial(ctx,network,address)
        elapsed:=float64(time.Since(start).Microseconds())/1000
        cancel()
        if err!=nil {continue}
        _=conn.Close() // TCP handshake only; no proxy credentials or payload
        result.Succeeded++
        if result.Succeeded==1 || elapsed<result.MinMS {result.MinMS=elapsed}
        if elapsed>result.MaxMS {result.MaxMS=elapsed}
        total+=elapsed
    }
    if result.Succeeded>0 {result.AvgMS=total/float64(result.Succeeded)}
    result.ProbeAt=time.Now().UTC()
    return result
}
