package main

import (
    "crypto/ed25519"
    "encoding/base64"
    "encoding/json"
    "math"
    "net"
    "sort"
    "time"

    "github.com/Jackyhuang83/MiniProbe/internal/common"
)

const (
    topologyProbeCapability = "topology-tcp-probe-v1"
    topologyMaxTargetsPerAgent = 4
    topologyMaxSamples = 20 // around 20 minutes at one sample / 60s
    topologyFreshness = 3*time.Minute
)

type topologyLinkStats struct {
    Revision int64
    LastProbe time.Time
    LastSuccess time.Time
    LastFailure time.Time
    Samples []common.TopologyProbeResult
    ConsecutiveFailures int
}

func (s *server) signedTopologyProbePolicyLocked(cfg nodeConfig, rep common.Report) *common.SignedTopologyProbePolicy {
    if !hasCapability(rep.Capabilities,topologyProbeCapability) {return nil}
    tc:=s.db.Topology
    version:=tc.Revision
    if version<=0 {version=1}
    p:=common.TopologyProbePolicy{
        Version:version,NodeID:cfg.ID,Enabled:tc.Enabled,IntervalSeconds:60,
        Targets:make([]common.TopologyProbeTarget,0,topologyMaxTargetsPerAgent),
    }
    if tc.Enabled {
        for _,l:=range tc.Links {
            if l.From!=cfg.ID || l.ProbeAddress=="" || l.ProbePort==0 {continue}
            // The local SSH-only configuration API validates these fields;
            // recheck before emitting any signed Agent-side network target.
            if !validTopologyProbeIP(l.ProbeAddress) {continue}
            p.Targets=append(p.Targets,common.TopologyProbeTarget{
                LinkID:topologyEdgeID(l.From,l.To),To:l.To,
                Address:l.ProbeAddress,Port:l.ProbePort,
            })
            if len(p.Targets)>=topologyMaxTargetsPerAgent {break}
        }
    }
    payload,_:=json.Marshal(p)
    priv:=ed25519.NewKeyFromSeed(s.secret[:ed25519.SeedSize])
    return &common.SignedTopologyProbePolicy{
        Policy:p,Signature:base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv,payload)),
    }
}

// Kept identical in Server and Agent to avoid accepting unsafe targets even
// if persisted configuration is hand-edited. Only literal IPs; no DNS.
func validTopologyProbeIP(raw string) bool {
    ip:=net.ParseIP(raw)
    return ip!=nil && ip.IsGlobalUnicast() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}
func topologyEdgeID(from,to string) string {return from+"~"+to}

// Agent tokens authenticate reports. Reported link IDs and revisions still
// require authorization against the SERVER-owned topology; only bounded,
// plausible measurements can change status. No raw target IP is saved.
func (s *server) ingestTopologyProbes(source string, samples []common.TopologyProbeResult, caps []string) {
    if !hasCapability(caps,topologyProbeCapability) || len(samples)==0 || len(samples)>topologyMaxTargetsPerAgent {return}
    s.mu.RLock()
    cfg:=s.db.Topology
    if !cfg.Enabled {s.mu.RUnlock();return}
    allowed:=map[string]string{}
    for _,l:=range cfg.Links {
        if l.From==source && l.ProbeAddress!="" && l.ProbePort>0 {
            allowed[topologyEdgeID(l.From,l.To)]=l.To
        }
    }
    s.mu.RUnlock()

    now:=time.Now().UTC()
    s.topologyMu.Lock()
    defer s.topologyMu.Unlock()
    if s.topologyStats==nil {s.topologyStats=map[string]*topologyLinkStats{}}
    for _,sample:=range samples {
        if allowed[sample.LinkID]!=sample.To || sample.To=="" || sample.Revision!=cfg.Revision ||
            sample.ProbeAt.IsZero() || sample.ProbeAt.After(now.Add(2*time.Minute)) ||
            now.Sub(sample.ProbeAt)>5*time.Minute ||
            sample.Attempts!=3 || sample.Succeeded<0 || sample.Succeeded>sample.Attempts ||
            !topologySampleValid(sample) {continue}
        st:=s.topologyStats[sample.LinkID]
        if st==nil || st.Revision!=cfg.Revision {
            st=&topologyLinkStats{Revision:cfg.Revision}
            s.topologyStats[sample.LinkID]=st
        }
        if !sample.ProbeAt.After(st.LastProbe) {continue} // repeated 2s reports are NOT new probes
        st.LastProbe=sample.ProbeAt
        if sample.Succeeded>0 {
            st.ConsecutiveFailures=0
            st.LastSuccess=now
        } else {
            st.ConsecutiveFailures++
        }
        if sample.Succeeded<sample.Attempts {st.LastFailure=now}
        st.Samples=append(st.Samples,sample)
        if len(st.Samples)>topologyMaxSamples {st.Samples=st.Samples[len(st.Samples)-topologyMaxSamples:]}
    }
}
func topologySampleValid(p common.TopologyProbeResult) bool {
    vals:=[]float64{p.MinMS,p.MaxMS,p.AvgMS}
    for _,v:=range vals {if math.IsNaN(v)||math.IsInf(v,0)||v<0||v>6000 {return false}}
    if p.Succeeded==0 {return p.MinMS==0&&p.MaxMS==0&&p.AvgMS==0}
    return p.MinMS<=p.AvgMS && p.AvgMS<=p.MaxMS
}

type topologyLinkMeasurement struct {
    Reachability string
    NetworkStatus string
    RTTMS *float64
    MinMS *float64
    MaxMS *float64
    AvgMS *float64
    LossPct *float64
    LastSuccess time.Time
    LastFailure time.Time
    LastProbe time.Time
    HasData bool
}
func (s *server) topologyMeasurement(id string,revision int64,now time.Time) topologyLinkMeasurement {
    out:=topologyLinkMeasurement{Reachability:"not_tested",NetworkStatus:"pending"}
    s.topologyMu.Lock()
    defer s.topologyMu.Unlock()
    st:=s.topologyStats[id]
    if st==nil || st.Revision!=revision || len(st.Samples)==0 {return out}
    out.HasData=true
    out.LastProbe=st.LastProbe
    out.LastSuccess=st.LastSuccess
    out.LastFailure=st.LastFailure
    if now.Sub(st.LastProbe)>topologyFreshness {
        out.Reachability="stale"
        out.NetworkStatus="stale"
        return out
    }
    attempts,successes:=0,0
    minMS,maxMS,total:=0.0,0.0,0.0
    for _,p:=range st.Samples {
        attempts+=p.Attempts
        successes+=p.Succeeded
        if p.Succeeded==0 {continue}
        if minMS==0 || p.MinMS<minMS {minMS=p.MinMS}
        if p.MaxMS>maxMS {maxMS=p.MaxMS}
        total+=p.AvgMS*float64(p.Succeeded)
    }
    if attempts>0 {
        loss:=100*float64(attempts-successes)/float64(attempts)
        out.LossPct=&loss
    }
    last:=st.Samples[len(st.Samples)-1]
    if successes>0 {
        avg:=total/float64(successes)
        out.MinMS=&minMS;out.MaxMS=&maxMS;out.AvgMS=&avg
    }
    if last.Succeeded>0 {
        current:=last.AvgMS
        out.RTTMS=&current
        out.Reachability="reachable"
        if last.Succeeded==last.Attempts {
            out.NetworkStatus="healthy"
        } else {
            out.NetworkStatus="degraded"
        }
    } else {
        out.Reachability="unreachable"
        out.NetworkStatus="degraded"
        if st.ConsecutiveFailures>=3 {out.NetworkStatus="interrupted"}
    }
    return out
}
func (s *server) resetTopologyStats() {
    s.topologyMu.Lock()
    s.topologyStats=map[string]*topologyLinkStats{}
    s.topologyMu.Unlock()
}
func (s *server) topologyStatsIDs() []string {
    s.topologyMu.Lock()
    defer s.topologyMu.Unlock()
    keys:=make([]string,0,len(s.topologyStats))
    for k:=range s.topologyStats {keys=append(keys,k)}
    sort.Strings(keys)
    return keys
}
