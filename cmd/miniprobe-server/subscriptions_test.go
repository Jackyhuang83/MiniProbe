package main

import (
 "crypto/tls"
 "encoding/json"
 "io"
 "io/fs"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func subscriptionTestServer(t *testing.T)*server{
 t.Helper()
 return &server{
  dataFile:filepath.Join(t.TempDir(),"miniprobe.json"),
  secret:[]byte("0123456789abcdef0123456789abcdef"),
  subscriptions:subscriptionStore{Version:1,Items:[]subscriptionRecord{}},
  db:database{Version:databaseVer,Secret:"MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY",Nodes:map[string]nodeConfig{},Settings:settings{DashboardMode:dashboardPublic}},
  failures:map[string]loginFailures{},
 }
}
func sampleSubscription() subscriptionRecord{
 return subscriptionRecord{Name:"Chat service",Category:"ai",AmountMinor:1999,Currency:"USD",Cycle:"monthly",DueDate:"2027-03-31",TimeZone:"Asia/Tokyo",Status:"active"}
}
func TestSubscriptionCRUDAndVPSOneSource(t *testing.T){
 s:=subscriptionTestServer(t)
 s.db.Nodes["node-jp"]=nodeConfig{ID:"node-jp",DisplayName:"JP-VPS",MonthlyPrice:5,Currency:"USD",ExpireAt:"2026-12-20"}
 post,_:=json.Marshal(sampleSubscription())
 w:=httptest.NewRecorder()
 s.localSubscriptions(w,httptest.NewRequest(http.MethodPost,"http://unix/v1/subscriptions",strings.NewReader(string(post))))
 if w.Code!=200{t.Fatalf("create: %d %s",w.Code,w.Body.String())}
 var created subscriptionRecord
 if err:=json.Unmarshal(w.Body.Bytes(),&created);err!=nil||!strings.HasPrefix(created.ID,"sub-"){t.Fatal("invalid id",err)}
 v:=s.subscriptionSnapshot()
 if len(v)!=2{t.Fatalf("want 2, got %d",len(v))}
 var vps subscriptionView
 for _,rec:=range v{if rec.Source=="vps"{vps=rec}}
 if vps.NodeID!="node-jp"||vps.AmountMinor!=500||vps.DueDate!="2026-12-20"||vps.Currency!="USD"{t.Fatalf("bad VPS binding: %+v",vps)}
 s.mu.Lock()
 n:=s.db.Nodes["node-jp"];n.MonthlyPrice=7.25;n.ExpireAt="2027-02-01";s.db.Nodes["node-jp"]=n
 s.mu.Unlock()
 after:=s.subscriptionSnapshot()
 for _,rec:=range after{if rec.Source=="vps"&&(rec.AmountMinor!=725||rec.DueDate!="2027-02-01"){t.Fatalf("VPS not live: %+v",rec)}}
 update:=created
 update.Name="Changed"
 b,_:=json.Marshal(update)
 w=httptest.NewRecorder()
 s.localSubscriptions(w,httptest.NewRequest(http.MethodPut,"http://unix/v1/subscriptions",strings.NewReader(string(b))))
 if w.Code!=200{t.Fatalf("update failed %d: %s",w.Code,w.Body.String())}
 w=httptest.NewRecorder()
 s.localSubscriptions(w,httptest.NewRequest(http.MethodDelete,"http://unix/v1/subscriptions?id="+created.ID,nil))
 if w.Code!=200{t.Fatalf("delete failed %d",w.Code)}
 if len(s.subscriptionSnapshot())!=1{t.Fatal("deleting manual entry must not delete VPS")}
 w=httptest.NewRecorder()
 s.localSubscriptions(w,httptest.NewRequest(http.MethodDelete,"http://unix/v1/subscriptions?id=vps:node-jp",nil))
 if w.Code!=400{t.Fatalf("VPS deletion should be forbidden: %d",w.Code)}
}
func TestSubscriptionStorageSeparateAndPrivate(t *testing.T){
 s:=subscriptionTestServer(t)
 store:=s.subscriptions;store.Items=[]subscriptionRecord{sampleSubscription()}
 s.subscriptionMu.Lock()
 err:=s.saveSubscriptionsLocked(store)
 s.subscriptionMu.Unlock()
 if err!=nil{t.Fatal(err)}
 info,err:=os.Stat(s.subscriptionPath());if err!=nil{t.Fatal(err)}
 if info.Mode().Perm()!=0600{t.Fatalf("private file perms: %o",info.Mode().Perm())}
 other:=subscriptionTestServer(t)
 other.dataFile=s.dataFile
 if err:=other.loadSubscriptions();err!=nil{t.Fatal(err)}
 if len(other.subscriptionSnapshot())!=1{t.Fatal("cannot reload stored subscriptions")}
 // The legacy miniprobe.json serializer must not overwrite the private ledger.
 if err:=s.save();err!=nil{t.Fatal(err)}
 if err:=other.loadSubscriptions();err!=nil||len(other.subscriptions.Items)!=1{t.Fatal("legacy database save damaged subscription ledger",err)}
 if err:=os.Chmod(s.subscriptionPath(),0644);err!=nil{t.Fatal(err)}
 if err:=other.loadSubscriptions();err==nil{t.Fatal("must reject publicly readable private ledger")}
}
func TestSubscriptionValidationCurrencyAndDates(t *testing.T){
 cases:=[]struct{input string;cur string;want int64}{{"5.99","USD",599},{"1200","JPY",1200},{"1.234","KWD",1234},{"0.1","EUR",10}}
 for _,tc:=range cases{
  got,err:=parseCurrencyMinor(tc.input,tc.cur)
  if err!=nil||got!=tc.want{t.Fatalf("%q/%s = %d err %v",tc.input,tc.cur,got,err)}
 }
 if _,err:=parseCurrencyMinor("2.123","USD");err==nil{t.Fatal("subcent USD must fail")}
 if _,err:=parseCurrencyMinor("-3","USD");err==nil{t.Fatal("negative cost must fail")}
 if validSubscriptionDate("2027-02-29"){t.Fatal("invalid leap date accepted")}
 if !validSubscriptionDate("2028-02-29"){t.Fatal("leap date rejected")}
 bad:=sampleSubscription();bad.Category="vps"
 if _,err:=validateSubscription(bad);err==nil{t.Fatal("duplicate manual VPS must be blocked")}
 bad=sampleSubscription();bad.TimeZone="Invalid/TZ"
 if _,err:=validateSubscription(bad);err==nil{t.Fatal("invalid IANA timezone accepted")}
 bad=sampleSubscription();bad.Cycle="custom";bad.CustomDays=0
 if _,err:=validateSubscription(bad);err==nil{t.Fatal("invalid custom period accepted")}
}
func TestSubscriptionReadAlwaysOwnerOnly(t *testing.T){
 for _,mode:=range []string{dashboardPublic,dashboardProtected,dashboardDisabled}{
  t.Run(mode,func(t *testing.T){
   s:=subscriptionTestServer(t)
   s.db.Settings.DashboardMode=mode
   rec:=sampleSubscription()
   s.subscriptions.Items=[]subscriptionRecord{rec}
   s.subscriptions.OwnerPassword=hashPassword("a strong subscription secret")
   request:=httptest.NewRequest(http.MethodGet,"https://monitor.test/api/v1/subscriptions",nil)
   w:=httptest.NewRecorder()
   s.subscriptionRead(w,request)
   if w.Code!=401{t.Fatalf("%s exposed private ledger: %d",mode,w.Code)}
   if strings.Contains(w.Body.String(),rec.Name){t.Fatal("response leaked private subscription")}
   // Dashboard trusted-device cookie does not authorize subscription API.
   request.AddCookie(&http.Cookie{Name:cookieName,Value:s.newDashboardSessionToken(time.Now().Add(time.Hour))})
   w=httptest.NewRecorder();s.subscriptionRead(w,request)
   if w.Code!=401{t.Fatalf("Dashboard cookie crossed privilege boundary: %d",w.Code)}
  })
 }
}
func TestSubscriptionOwnerLoginAndPasswordRevocation(t *testing.T){
 s:=subscriptionTestServer(t)
 s.subscriptions.OwnerPassword=hashPassword("password used only for subscriptions")
 r:=httptest.NewRequest(http.MethodPost,"https://monitor.test/api/v1/subscriptions/login",strings.NewReader(`{"password":"password used only for subscriptions"}`))
 r.Header.Set("X-MiniProbe-Request","1")
 r.Header.Set("Origin","https://monitor.test")
 w:=httptest.NewRecorder()
 s.subscriptionLogin(w,r)
 if w.Code!=200{t.Fatalf("login %d %s",w.Code,w.Body.String())}
 cookies:=w.Result().Cookies()
 if len(cookies)!=1||cookies[0].Name!=subscriptionCookie||!cookies[0].Secure||!cookies[0].HttpOnly {t.Fatalf("unsafe cookie: %+v",cookies)}
 read:=httptest.NewRequest(http.MethodGet,"https://monitor.test/api/v1/subscriptions",nil)
 read.AddCookie(cookies[0])
 w=httptest.NewRecorder();s.subscriptionRead(w,read)
 if w.Code!=200{t.Fatalf("authorized read %d",w.Code)}
 read.TLS=nil
 read.RemoteAddr="203.0.113.5:5555"
 read.Header.Set("X-Forwarded-Proto","https")
 w=httptest.NewRecorder();s.subscriptionRead(w,read)
 if w.Code!=401{t.Fatal("spoofed X-Forwarded-Proto bypassed secure access")}
 read.TLS=&tls.ConnectionState{}
 s.subscriptionMu.Lock();s.subscriptions.OwnerPassword=hashPassword("replacement private password");s.subscriptionMu.Unlock()
 w=httptest.NewRecorder();s.subscriptionRead(w,read)
 if w.Code!=401{t.Fatal("password change failed to revoke sessions")}
}
func TestSubscriptionLoginRequiresTLSAndOrigin(t *testing.T){
 s:=subscriptionTestServer(t)
 s.subscriptions.OwnerPassword=hashPassword("private only password")
 r:=httptest.NewRequest(http.MethodPost,"http://monitor.test/api/v1/subscriptions/login",strings.NewReader(`{"password":"private only password"}`))
 r.Header.Set("X-MiniProbe-Request","1")
 w:=httptest.NewRecorder();s.subscriptionLogin(w,r)
 if w.Code!=403{t.Fatal("password may not travel over HTTP")}
 r=httptest.NewRequest(http.MethodPost,"https://monitor.test/api/v1/subscriptions/login",strings.NewReader(`{"password":"private only password"}`))
 r.Header.Set("X-MiniProbe-Request","1");r.Header.Set("Origin","https://attacker.test")
 w=httptest.NewRecorder();s.subscriptionLogin(w,r)
 if w.Code!=403{t.Fatal("cross-origin login accepted")}
}
func TestSubscriptionUIAvailableWhenDashboardDisabled(t *testing.T){
 s:=subscriptionTestServer(t)
 s.db.Settings.DashboardMode=dashboardDisabled
 files,err:=fs.Sub(assets,"web");if err!=nil{t.Fatal(err)}
 handler:=s.dashboardAssets(http.FileServer(http.FS(files)))
 for _,path:=range []string{"/ui/"+serverVersion+"/subscriptions.html","/ui/"+serverVersion+"/subscriptions.js","/ui/"+serverVersion+"/subscriptions.css"}{
  w:=httptest.NewRecorder()
  handler.ServeHTTP(w,httptest.NewRequest(http.MethodGet,"http://monitor.test"+path,nil))
  if w.Code!=200{t.Fatalf("%s unavailable under disabled Dashboard: %d",path,w.Code)}
  if strings.Contains(w.Body.String(),"Chat service"){t.Fatal("HTML includes private data")}
 }
 w:=httptest.NewRecorder()
 handler.ServeHTTP(w,httptest.NewRequest(http.MethodGet,"http://monitor.test/ui/"+serverVersion+"/",nil))
 if w.Code!=404{t.Fatal("disabled Dashboard became public")}
 b,err:=io.ReadAll(w.Result().Body);if err!=nil{t.Fatal(err)}
 _=b
}
