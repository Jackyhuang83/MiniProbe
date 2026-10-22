package main

import (
    "crypto/ed25519"
    "crypto/rand"
    "encoding/base64"
    "encoding/json"
    "strings"
    "testing"

    "github.com/Jackyhuang83/MiniProbe/internal/common"
)

func trafficSignedPolicy(t *testing.T,p common.TopologyTrafficPolicy,key ed25519.PrivateKey)*common.SignedTopologyTrafficPolicy{
    t.Helper()
    b,err:=json.Marshal(p);if err!=nil{t.Fatal(err)}
    return &common.SignedTopologyTrafficPolicy{Policy:p,Signature:base64.RawURLEncoding.EncodeToString(ed25519.Sign(key,b))}
}
func TestXrayReadOnlyCounterParser(t *testing.T){
    raw:=[]byte(`{"stat":[{"name":"outbound>>>hk-relay>>>traffic>>>uplink","value":"1099511627776"},{"name":"outbound>>>hk-relay>>>traffic>>>downlink","value":2048},{"name":"outbound>>>other>>>traffic>>>uplink","value":"999"}]}`)
    up,down,err:=parseXrayOutboundStats(raw,"hk-relay")
    if err!=nil||up!=1099511627776||down!=2048 {t.Fatalf("bad exact-tag parse up=%d down=%d err=%v",up,down,err)}
    for _,v:=range []string{
        `{"stat":[{"name":"user>>>a>>>traffic>>>uplink","value":"1"}]}`,
        `{"stat":[{"name":"outbound>>>hk-relay>>>traffic>>>uplink","value":"-1"}]}`,
        `{"stat":[{"name":"outbound>>>hk-relay>>>traffic>>>uplink","value":"3"},{"name":"outbound>>>hk-relay>>>traffic>>>uplink","value":"4"}]}`,
        `{"stat":[{"name":"outbound>>>hk-relay>>>traffic>>>uplink","value":"999999999999999999999"}]}`,
    } {
        if _,_,err:=parseXrayOutboundStats([]byte(v),"hk-relay");err==nil {t.Errorf("unsafe stats accepted: %s",v)}
    }
    if _,_,err:=parseXrayOutboundStats(raw,"hk-relay>>>evil");err==nil {t.Fatal("invalid tag accepted")}
}
func TestTopologyTrafficPolicySigningAndBounds(t *testing.T) {
    pub,priv,err:=ed25519.GenerateKey(rand.Reader)
    if err!=nil{t.Fatal(err)}
    p:=common.TopologyTrafficPolicy{Version:2,NodeID:"source",Enabled:true,IntervalSeconds:60,Targets:[]common.TopologyTrafficTarget{{LinkID:"source~dest",Tag:"relay-hk",Port:10085}}}
    r:=&topologyTrafficRunner{}
    if err:=r.apply("source",pub,trafficSignedPolicy(t,p,priv));err!=nil{t.Fatal(err)}
    modified:=trafficSignedPolicy(t,p,priv)
    modified.Policy.Targets[0].Tag="other"
    if err:=r.apply("source",pub,modified);err==nil{t.Fatal("unsigned alteration accepted")}
    if err:=r.apply("wrong",pub,trafficSignedPolicy(t,p,priv));err==nil{t.Fatal("wrong source accepted")}
    replay:=p;replay.Version=1
    if err:=r.apply("source",pub,trafficSignedPolicy(t,replay,priv));err==nil{t.Fatal("replay accepted")}
    bad:=p;bad.Version=3;bad.Targets=[]common.TopologyTrafficTarget{{LinkID:"a~b",Tag:"../../passwd",Port:10085}}
    if err:=r.apply("source",pub,trafficSignedPolicy(t,bad,priv));err==nil{t.Fatal("unsafe tag accepted")}
    disabled:=common.TopologyTrafficPolicy{Version:4,NodeID:"source",Enabled:false,IntervalSeconds:60}
    if err:=r.apply("source",pub,trafficSignedPolicy(t,disabled,priv));err!=nil{t.Fatal(err)}
    if len(r.snapshot())!=0{t.Fatal("disabled collector preserved results")}
}
func TestStatsWriterBound(t *testing.T){
    w:=&cappedWriter{limit:8}
    if n,err:=w.Write([]byte("1234"));err!=nil||n!=4{t.Fatal(n,err)}
    if _,err:=w.Write([]byte(strings.Repeat("a",9)));err==nil{t.Fatal("output overrun")}
}
