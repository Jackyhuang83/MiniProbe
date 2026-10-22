package main

import (
    "context"
    "crypto/ed25519"
    "crypto/rand"
    "encoding/base64"
    "encoding/json"
    "errors"
    "net"
    "testing"

    "github.com/Jackyhuang83/MiniProbe/internal/common"
)

func topologySignedTestPolicy(t *testing.T,p common.TopologyProbePolicy,private ed25519.PrivateKey) *common.SignedTopologyProbePolicy {
    t.Helper()
    data,err:=json.Marshal(p)
    if err!=nil {t.Fatal(err)}
    return &common.SignedTopologyProbePolicy{Policy:p,Signature:base64.RawURLEncoding.EncodeToString(ed25519.Sign(private,data))}
}
func TestTopologyRejectsUnsafeTargets(t *testing.T) {
    valid:=[]string{"203.0.113.10","2001:db8::1","10.10.0.5","fd00::1"}
    invalid:=[]string{"localhost","127.0.0.1","::1","0.0.0.0","::","169.254.169.254","100.100.100.200","168.63.129.16","fd00:ec2::254","fe80::1","224.0.0.1","ff02::1",""}
    for _,ip:=range valid {if !validTopologyProbeIP(ip) {t.Errorf("valid target rejected: %s",ip)}}
    for _,ip:=range invalid {if validTopologyProbeIP(ip) {t.Errorf("unsafe target accepted: %s",ip)}}
}
func TestTopologyPolicySignedAndBounded(t *testing.T) {
    pub,priv,err:=ed25519.GenerateKey(rand.Reader)
    if err!=nil {t.Fatal(err)}
    target:=common.TopologyProbeTarget{LinkID:"a~b",To:"b",Address:"203.0.113.10",Port:443}
    p:=common.TopologyProbePolicy{Version:2,NodeID:"a",Enabled:true,IntervalSeconds:60,Targets:[]common.TopologyProbeTarget{target}}
    r:=&topologyProbeRunner{}
    if err:=r.apply("a",pub,topologySignedTestPolicy(t,p,priv));err!=nil {t.Fatal(err)}
    if err:=r.apply("a",pub,topologySignedTestPolicy(t,p,priv));err!=nil {t.Fatal("same policy must be idempotent",err)}
    tampered:=topologySignedTestPolicy(t,p,priv)
    tampered.Policy.Targets[0].Address="192.0.2.20"
    if err:=r.apply("a",pub,tampered);err==nil {t.Fatal("tampered signed policy accepted")}
    wrong:=topologySignedTestPolicy(t,p,priv)
    if err:=r.apply("b",pub,wrong);err==nil {t.Fatal("wrong node accepted")}
    replay:=p;replay.Version=1
    if err:=r.apply("a",pub,topologySignedTestPolicy(t,replay,priv));err==nil {t.Fatal("replay accepted")}
    local:=p;local.Version=3;local.Targets=[]common.TopologyProbeTarget{{LinkID:"a~b",To:"b",Address:"127.0.0.1",Port:80}}
    if err:=r.apply("a",pub,topologySignedTestPolicy(t,local,priv));err==nil {t.Fatal("loopback accepted")}
    overcrowded:=p;overcrowded.Version=3
    overcrowded.Targets=make([]common.TopologyProbeTarget,5)
    if err:=r.apply("a",pub,topologySignedTestPolicy(t,overcrowded,priv));err==nil {t.Fatal("too many targets accepted")}
    disabled:=common.TopologyProbePolicy{Version:4,NodeID:"a",IntervalSeconds:60}
    if err:=r.apply("a",pub,topologySignedTestPolicy(t,disabled,priv));err!=nil {t.Fatal("disable failed",err)}
    if got:=len(r.snapshot());got!=0 {t.Fatalf("disable retained measurements: %d",got)}
}
func TestTopologyTCPResultReportsHandshakeOnly(t *testing.T) {
    target:=common.TopologyProbeTarget{LinkID:"a~b",To:"b",Address:"203.0.113.2",Port:443}
    count:=0
    sample:=probeTopologyTCPWithDial(9,target,func(_ context.Context,network,address string)(net.Conn,error){
        count++
        if network!="tcp4" || address!="203.0.113.2:443" {t.Fatalf("unexpected dial %s %s",network,address)}
        a,b:=net.Pipe();_ = b.Close()
        return a,nil
    })
    if count!=3 || sample.Attempts!=3 || sample.Succeeded!=3 || sample.Revision!=9 || sample.LinkID!="a~b" {
        t.Fatalf("bad handshake sample: %+v count=%d",sample,count)
    }
    if sample.MaxMS<sample.MinMS {t.Fatalf("bad RTT range: %+v",sample)}
    lost:=probeTopologyTCPWithDial(9,target,func(_ context.Context,_,_ string)(net.Conn,error){return nil,errors.New("timeout")})
    if lost.Succeeded!=0 || lost.MinMS!=0 || lost.MaxMS!=0 || lost.AvgMS!=0 {t.Fatalf("failure fabricated latency: %+v",lost)}
    ipv6:=target;ipv6.Address="2001:db8::10"
    probeTopologyTCPWithDial(9,ipv6,func(_ context.Context,network,address string)(net.Conn,error){
        if network!="tcp6" || address!="[2001:db8::10]:443" {t.Fatalf("wrong IPv6 dial: %s %s",network,address)}
        return nil,errors.New("unreachable")
    })
}
