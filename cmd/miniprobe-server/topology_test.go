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

func topologyTestServer() *server {
    now:=time.Now()
    return &server{ db:database{
        Settings:settings{DashboardMode:dashboardPublic},
        Nodes:map[string]nodeConfig{
            "a":{ID:"a",DisplayName:"入口A"},
            "b":{ID:"b",DisplayName:"出口B"},
        },
        States:map[string]common.NodeView{
            "a":{LastSeen:now,Report:common.Report{AgentVersion:"legacy", Info:common.StaticInfo{OS:"Debian",IPv4:[]string{"192.0.2.1"}}}},
            "b":{LastSeen:now,Report:common.Report{AgentVersion:"legacy"}},
        },
    } }
}
func TestTopologyValidation(t *testing.T) {
    s:=topologyTestServer()
    valid:=topologyConfig{Enabled:true,Nodes:[]topologyNodeConfig{{ID:"a",Role:"entry"},{ID:"b",Role:"exit"}},Links:[]topologyLinkConfig{{From:"a",To:"b"}}}
    if _,err:=validateTopology(valid,s.db.Nodes);err!=nil { t.Fatal(err) }
    cases:=[]struct{name string;modify func(*topologyConfig)}{
        {"unknown",func(x *topologyConfig){x.Nodes[1].ID="unknown"}},
        {"duplicate node",func(x *topologyConfig){x.Nodes[1].ID="a"}},
        {"bad role",func(x *topologyConfig){x.Nodes[0].Role="admin"}},
        {"orphan link",func(x *topologyConfig){x.Links[0].To="ghost"}},
        {"self link",func(x *topologyConfig){x.Links[0].To="a"}},
        {"duplicate link",func(x *topologyConfig){x.Links=append(x.Links,x.Links[0])}},
        {"control text",func(x *topologyConfig){x.Nodes[0].Group="bad\nnewline"}},
    }
    for _,tc:=range cases {
        t.Run(tc.name,func(t *testing.T){
            in:=topologyConfig{Enabled:valid.Enabled,Nodes:append([]topologyNodeConfig(nil),valid.Nodes...),Links:append([]topologyLinkConfig(nil),valid.Links...)}
            tc.modify(&in)
            if _,err:=validateTopology(in,s.db.Nodes);err==nil {t.Fatal("expected validation error")}
        })
    }
}
func TestTopologyNeverInventsLinkMeasurements(t *testing.T) {
    s:=topologyTestServer()
    s.db.Topology=topologyConfig{Enabled:true,Nodes:[]topologyNodeConfig{{ID:"a",Role:"entry"},{ID:"b",Role:"exit"}},Links:[]topologyLinkConfig{{From:"a",To:"b"}}}
    w:=httptest.NewRecorder()
    s.topologyAPI(w,httptest.NewRequest(http.MethodGet,"/api/v1/topology",nil))
    if w.Code!=http.StatusOK {t.Fatal(w.Code,w.Body.String())}
    var result struct {
        Summary topologySummary `json:"summary"`
        Links []topologyLinkView `json:"links"`
    }
    if err:=json.Unmarshal(w.Body.Bytes(),&result);err!=nil {t.Fatal(err)}
    if result.Summary.HealthyLinks!=nil || result.Summary.TrafficTodayBytes!=nil || result.Summary.AverageRTTMS!=nil {t.Fatalf("must not synthesize counters: %+v",result.Summary)}
    if len(result.Links)!=1 || result.Links[0].Reachability!="not_tested" || result.Links[0].TrafficStatus!="not_collected" {t.Fatalf("bad semantics: %+v",result.Links)}
    if strings.Contains(w.Body.String(),"192.0.2.1") {t.Fatal("topology leaked IP address")}
    if w.Header().Get("Cache-Control")!="no-store" {t.Fatal("API must be no-store")}
}
func TestTopologyOfflineEndpointIsNotMeasuredFailure(t *testing.T) {
    s:=topologyTestServer()
    state:=s.db.States["b"];state.LastSeen=time.Now().Add(-time.Hour);s.db.States["b"]=state
    s.db.Topology=topologyConfig{Enabled:true,Nodes:[]topologyNodeConfig{{ID:"a",Role:"entry"},{ID:"b",Role:"exit"}},Links:[]topologyLinkConfig{{From:"a",To:"b"}}}
    w:=httptest.NewRecorder()
    s.topologyAPI(w,httptest.NewRequest("GET","/api/v1/topology",nil))
    var result struct {Links []topologyLinkView `json:"links"`}
    if err:=json.Unmarshal(w.Body.Bytes(),&result);err!=nil{t.Fatal(err)}
    if len(result.Links)!=1 || result.Links[0].EndpointStatus!="endpoint_offline" || result.Links[0].Reachability!="not_tested" {t.Fatalf("offline ≠ measured link outage: %+v",result.Links)}
}
func TestTopologyDisabledReturnsNoDetails(t *testing.T) {
    s:=topologyTestServer()
    s.db.Topology=topologyConfig{Enabled:false,Nodes:[]topologyNodeConfig{{ID:"a",Role:"entry"}}}
    w:=httptest.NewRecorder()
    s.topologyAPI(w,httptest.NewRequest("GET","/api/v1/topology",nil))
    if strings.Contains(w.Body.String(),"入口A") {t.Fatal("disabled topology leaked configured nodes")}
}
