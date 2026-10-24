package main

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
)

func dev4OwnerRequest(s *server, method, path, payload string) *http.Request {
 r:=httptest.NewRequest(method,"https://probe.example"+path,strings.NewReader(payload))
 r.AddCookie(&http.Cookie{Name:subscriptionCookie,Value:s.newSubscriptionSession(time.Now().Add(time.Hour))})
 r.Header.Set("Origin","https://probe.example")
 r.Header.Set("Content-Type","application/json")
 r.Header.Set("X-MiniProbe-Request","1")
 r.Header.Set(subscriptionCSRFHeader,s.subscriptionCSRFToken(r))
 return r
}

func TestSubscriptionDev4RejectsUnsafeRequests(t *testing.T) {
 s:=subscriptionTestServer(t)
 s.subscriptions.OwnerPassword=hashPassword("dev4 test owner password")
 body:=`{"name":"Secret","category":"ai","currency":"USD","amount_minor":1250,"cycle":"yearly","due_date":"2027-10-01","status":"active"}`
 for _,method:=range []string{http.MethodPost,http.MethodPut,http.MethodDelete} {
  r:=httptest.NewRequest(method,"https://probe.example/api/v1/subscriptions/manage",strings.NewReader(body))
  r.Header.Set("Origin","https://probe.example")
  r.Header.Set("Content-Type","application/json")
  r.Header.Set("X-MiniProbe-Request","1")
  w:=httptest.NewRecorder()
  s.subscriptionWebManage(w,r)
  if w.Code!=401{t.Fatalf("unauthenticated %s accepted: %d",method,w.Code)}
 }
 // Dashboard owner and subscription owner are different identities.
 dashboard:=httptest.NewRequest(http.MethodPost,"https://probe.example/api/v1/subscriptions/manage",strings.NewReader(body))
 dashboard.AddCookie(&http.Cookie{Name:cookieName,Value:s.newDashboardSessionToken(time.Now().Add(time.Hour))})
 w:=httptest.NewRecorder()
 s.subscriptionWebManage(w,dashboard)
 if w.Code!=401{t.Fatal("dashboard-only session gained subscription write privilege")}

 cases:=[]struct{name string; mutate func(*http.Request)}{
  {"missing origin",func(r *http.Request){r.Header.Del("Origin")}},
  {"cross origin",func(r *http.Request){r.Header.Set("Origin","https://other.example")}},
  {"http origin",func(r *http.Request){r.Header.Set("Origin","http://probe.example")}},
  {"missing ajax header",func(r *http.Request){r.Header.Del("X-MiniProbe-Request")}},
  {"missing csrf",func(r *http.Request){r.Header.Del(subscriptionCSRFHeader)}},
  {"forged csrf",func(r *http.Request){r.Header.Set(subscriptionCSRFHeader,"forged")}},
  {"wrong content type",func(r *http.Request){r.Header.Set("Content-Type","text/plain")}},
  {"http with spoofed forwarded header",func(r *http.Request){r.TLS=nil;r.RemoteAddr="198.51.100.2:1234";r.Header.Set("X-Forwarded-Proto","https")}},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   r:=dev4OwnerRequest(s,http.MethodPost,"/api/v1/subscriptions/manage",body)
   tc.mutate(r)
   w:=httptest.NewRecorder();s.subscriptionWebManage(w,r)
   if w.Code==200||w.Code==201{t.Fatalf("unsafe write accepted: %s",tc.name)}
  })
 }
 r:=dev4OwnerRequest(s,http.MethodGet,"/api/v1/subscriptions/manage","")
 w=httptest.NewRecorder();s.subscriptionWebManage(w,r)
 if w.Code!=405{t.Fatalf("manage GET should be disabled, got %d",w.Code)}
 r=dev4OwnerRequest(s,http.MethodPut,"/api/v1/subscriptions/renewals","{}")
 w=httptest.NewRecorder();s.subscriptionWebRenewal(w,r)
 if w.Code!=405{t.Fatalf("renewal PUT should be disabled, got %d",w.Code)}
 if len(s.subscriptions.Items)!=0{t.Fatal("blocked attempts modified private ledger")}
}

func TestSubscriptionDev4OwnerCRUDAndRenewal(t *testing.T) {
 s:=subscriptionTestServer(t)
 s.subscriptions.OwnerPassword=hashPassword("dev4 test owner password")
 s.db.Nodes["node-1"]=nodeConfig{ID:"node-1",DisplayName:"Sample VPS",MonthlyPrice:5,Currency:"USD",ExpireAt:"2027-11-02"}

 session:=dev4OwnerRequest(s,http.MethodGet,"/api/v1/subscriptions/session","")
 w:=httptest.NewRecorder();s.subscriptionSession(w,session)
 if w.Code!=200{t.Fatal("owner session check failed")}
 var state map[string]any
 if err:=json.Unmarshal(w.Body.Bytes(),&state);err!=nil{t.Fatal(err)}
 if state["csrf"]!=s.subscriptionCSRFToken(session){t.Fatalf("owner session has no correct CSRF token")}
 public:=httptest.NewRequest(http.MethodGet,"https://probe.example/api/v1/subscriptions/session",nil)
 w=httptest.NewRecorder();s.subscriptionSession(w,public)
 if strings.Contains(w.Body.String(),"csrf"){t.Fatal("CSRF token leaked before login")}

 payload:=`{"name":"TV Service","category":"video","amount_minor":1234,"currency":"USD","cycle":"monthly","due_date":"2027-11-30","time_zone":"Asia/Tokyo","status":"active","auto_renew":true}`
 r:=dev4OwnerRequest(s,http.MethodPost,"/api/v1/subscriptions/manage",payload)
 w=httptest.NewRecorder();s.subscriptionWebManage(w,r)
 if w.Code!=200{t.Fatalf("create failed %d %s",w.Code,w.Body.String())}
 var record subscriptionRecord
 if err:=json.Unmarshal(w.Body.Bytes(),&record);err!=nil{t.Fatal(err)}
 if !strings.HasPrefix(record.ID,"sub-"){t.Fatal("new item lacks manual ID")}
 record.Name="Renamed";record.AmountMinor=2399
 edit,_:=json.Marshal(record)
 r=dev4OwnerRequest(s,http.MethodPut,"/api/v1/subscriptions/manage",string(edit))
 w=httptest.NewRecorder();s.subscriptionWebManage(w,r)
 if w.Code!=200{t.Fatalf("edit failed %d %s",w.Code,w.Body.String())}

 // Web must never turn a monitored VPS into a manually editable or deletable item.
 record.ID="vps:node-1"
 edit,_=json.Marshal(record)
 r=dev4OwnerRequest(s,http.MethodPut,"/api/v1/subscriptions/manage",string(edit))
 w=httptest.NewRecorder();s.subscriptionWebManage(w,r)
 if w.Code!=400{t.Fatalf("VPS edit allowed: %d",w.Code)}
 r=dev4OwnerRequest(s,http.MethodDelete,"/api/v1/subscriptions/manage?id=vps:node-1","")
 w=httptest.NewRecorder();s.subscriptionWebManage(w,r)
 if w.Code!=400{t.Fatalf("VPS delete allowed: %d",w.Code)}

 // A forged or non-existent subscription ID cannot be renewed.
 r=dev4OwnerRequest(s,http.MethodPost,"/api/v1/subscriptions/renewals",`{"subscription_id":"missing","date":"2027-11-05","amount_minor":2399,"currency":"USD"}`)
 w=httptest.NewRecorder();s.subscriptionWebRenewal(w,r)
 if w.Code!=404{t.Fatalf("unknown renewal target accepted: %d",w.Code)}
 r=dev4OwnerRequest(s,http.MethodPost,"/api/v1/subscriptions/renewals",`{"subscription_id":"vps:node-1","date":"2027-11-05","amount_minor":500,"currency":"USD","new_due_date":"2028-01-01"}`)
 w=httptest.NewRecorder();s.subscriptionWebRenewal(w,r)
 if w.Code!=400{t.Fatalf("VPS due date changed by web: %d",w.Code)}
 manualID:=s.subscriptions.Items[0].ID
 body,_:=json.Marshal(subscriptionRenewalInput{SubscriptionID:manualID,Date:"2027-11-05",AmountMinor:2399,Currency:"USD",NewDueDate:"2027-12-31"})
 r=dev4OwnerRequest(s,http.MethodPost,"/api/v1/subscriptions/renewals",string(body))
 w=httptest.NewRecorder();s.subscriptionWebRenewal(w,r)
 if w.Code!=201{t.Fatalf("renewal failed %d %s",w.Code,w.Body.String())}
 if len(s.subscriptions.Renewals)!=1||s.subscriptions.Items[0].DueDate!="2027-12-31"{t.Fatal("renewal data not updated")}

 // Data is persisted, rather than being isolated to an in-memory web model.
 other:=subscriptionTestServer(t);other.dataFile=s.dataFile
 if err:=other.loadSubscriptions();err!=nil{t.Fatal(err)}
 if len(other.subscriptions.Renewals)!=1{t.Fatal("renewal did not persist")}

 r=dev4OwnerRequest(s,http.MethodDelete,"/api/v1/subscriptions/manage?id="+manualID,"")
 w=httptest.NewRecorder();s.subscriptionWebManage(w,r)
 if w.Code!=200{t.Fatalf("delete failed %d %s",w.Code,w.Body.String())}
 if len(s.subscriptions.Items)!=0||len(s.subscriptions.Renewals)!=1{t.Fatal("delete must preserve historical renewal records")}
}

// Rotating the private password must invalidate both write sessions and CSRF.
func TestSubscriptionDev4PasswordRotationRevokesWrite(t *testing.T) {
 s:=subscriptionTestServer(t);s.subscriptions.OwnerPassword=hashPassword("old owner password 2026")
 r:=dev4OwnerRequest(s,http.MethodPost,"/api/v1/subscriptions/manage",`{}`)
 s.subscriptions.OwnerPassword=hashPassword("new owner password 2026")
 w:=httptest.NewRecorder();s.subscriptionWebManage(w,r)
 if w.Code!=401{t.Fatalf("old owner session survived password rotation: %d",w.Code)}
}
