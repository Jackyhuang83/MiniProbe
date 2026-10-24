package main

import (
 "crypto"
 "crypto/rand"
 "crypto/rsa"
 "crypto/sha256"
 "encoding/base64"
 "encoding/json"
 "fmt"
 "math/big"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
)

func accessTestConfig()subscriptionAccessConfig {
 return subscriptionAccessConfig{Enabled:true,Hostname:"subs.example.com",Team:"my-team",Audience:"0123456789abcdef0123456789abcdef0123456789abcdef",Email:"owner@example.com"}
}
func accessTestToken(t *testing.T,key *rsa.PrivateKey,c subscriptionAccessConfig,overrides map[string]any)string{
 t.Helper()
 header:=map[string]any{"alg":"RS256","typ":"JWT","kid":"test-key"}
 now:=time.Now().Unix()
 claims:=map[string]any{
  "iss":"https://"+c.Team+".cloudflareaccess.com",
  "aud":[]string{c.Audience},
  "email":c.Email,"iat":now-3,"nbf":now-3,"exp":now+1800,
 }
 for k,v:=range overrides{claims[k]=v}
 head,_:=json.Marshal(header)
 payload,_:=json.Marshal(claims)
 encoded:=base64.RawURLEncoding.EncodeToString(head)+"."+base64.RawURLEncoding.EncodeToString(payload)
 digest:=sha256.Sum256([]byte(encoded))
 sig,err:=rsa.SignPKCS1v15(rand.Reader,key,crypto.SHA256,digest[:]);if err!=nil{t.Fatal(err)}
 return encoded+"."+base64.RawURLEncoding.EncodeToString(sig)
}
func accessTestRequest(method,path,host,token,body string)*http.Request{
 r:=httptest.NewRequest(method,"https://"+host+path,strings.NewReader(body))
 r.Header.Set("Cf-Access-Jwt-Assertion",token)
 return r
}
func TestSubscriptionAccessConfigValidation(t *testing.T){
 good:=accessTestConfig()
 if err:=validateSubscriptionAccess(good);err!=nil{t.Fatal(err)}
 for _,tc:=range []struct{name string;mutate func(*subscriptionAccessConfig)}{
  {"team URL injection",func(c *subscriptionAccessConfig){c.Team="my-team.example.com"}},
  {"host without domain",func(c *subscriptionAccessConfig){c.Hostname="localhost"}},
  {"Cloudflare team hostname",func(c *subscriptionAccessConfig){c.Hostname="my-team.cloudflareaccess.com"}},
  {"bad audience",func(c *subscriptionAccessConfig){c.Audience="bad"}},
  {"bad email",func(c *subscriptionAccessConfig){c.Email="root@example.com\r\nBcc:bad@example.com"}},
  {"disabled",func(c *subscriptionAccessConfig){c.Enabled=false}},
 } {
  c:=good;tc.mutate(&c)
  if validateSubscriptionAccess(c)==nil{t.Errorf("invalid Access config accepted: %s",tc.name)}
 }
}
func TestSubscriptionAccessRS256OwnerIsolationAndCSRF(t *testing.T){
 s:=subscriptionTestServer(t)
 key,err:=rsa.GenerateKey(rand.Reader,2048);if err!=nil{t.Fatal(err)}
 cfg:=accessTestConfig()
 s.subscriptionAccess=cfg
 s.subscriptionAccessKeys=map[string]*rsa.PublicKey{"test-key":&key.PublicKey}
 s.subscriptionAccessKeysUntil=time.Now().Add(time.Hour)
 token:=accessTestToken(t,key,cfg,nil)
 r:=accessTestRequest("GET","/api/v1/subscriptions/session",cfg.Hostname,token,"")
 if !s.subscriptionAuthorized(r){t.Fatal("valid signed Access JWT rejected")}
 w:=httptest.NewRecorder();s.subscriptionSession(w,r)
 var result map[string]any
 if err=json.Unmarshal(w.Body.Bytes(),&result);err!=nil{t.Fatal(err)}
 if result["authenticated"]!=true||result["access_mode"]!=true||result["secure"]!=true||result["csrf"]==""{
  t.Fatalf("Access session did not unlock private view: %v",result)
 }
 r=accessTestRequest("GET","/api/v1/subscriptions",cfg.Hostname,token,"")
 w=httptest.NewRecorder();s.subscriptionRead(w,r)
 if w.Code!=200{t.Fatalf("signed Access JWT could not read private ledger: %d",w.Code)}
 cases:=[]struct{name string;request *http.Request}{
  {"missing assertion",accessTestRequest("GET","/api/v1/subscriptions",cfg.Hostname,"","")},
  {"forged identity header",accessTestRequest("GET","/api/v1/subscriptions",cfg.Hostname,"","")},
  {"wrong audience",accessTestRequest("GET","/api/v1/subscriptions",cfg.Hostname,accessTestToken(t,key,cfg,map[string]any{"aud":[]string{"wrong-aud"}}),"")},
  {"wrong issuer",accessTestRequest("GET","/api/v1/subscriptions",cfg.Hostname,accessTestToken(t,key,cfg,map[string]any{"iss":"https://evil.cloudflareaccess.com"}),"")},
  {"wrong email",accessTestRequest("GET","/api/v1/subscriptions",cfg.Hostname,accessTestToken(t,key,cfg,map[string]any{"email":"guest@example.com"}),"")},
  {"expired",accessTestRequest("GET","/api/v1/subscriptions",cfg.Hostname,accessTestToken(t,key,cfg,map[string]any{"exp":time.Now().Unix()-1}),"")},
  {"future issued",accessTestRequest("GET","/api/v1/subscriptions",cfg.Hostname,accessTestToken(t,key,cfg,map[string]any{"iat":time.Now().Unix()+999}),"")},
  {"main hostname",accessTestRequest("GET","/api/v1/subscriptions","monitor.example.com",token,"")},
  {"HTTP",httptest.NewRequest("GET","http://"+cfg.Hostname+"/api/v1/subscriptions",nil)},
 }
 for _,tc:=range cases{
  if tc.name=="forged identity header"{tc.request.Header.Set("Cf-Access-Authenticated-User-Email",cfg.Email)}
  if tc.name=="HTTP"{tc.request.Header.Set("Cf-Access-Jwt-Assertion",token)}
  w=httptest.NewRecorder();s.subscriptionRead(w,tc.request)
  if w.Code!=401{t.Errorf("%s: private ledger exposed (HTTP %d)",tc.name,w.Code)}
 }
 // Tampered signature must be rejected even if all decoded claims look right.
 tampered:=token[:len(token)-1]+"A"
 if tampered==token{tampered=token[:len(token)-1]+"B"}
 if s.subscriptionAuthorized(accessTestRequest("GET","/api/v1/subscriptions",cfg.Hostname,tampered,"")){t.Fatal("tampered JWT accepted")}
 // A JWT validated on the Access hostname can manage a subscription only
 // when the origin and CSRF token match that exact JWT.
 payload:=`{"name":"Test Cloudflare Auth","category":"software","amount_minor":100,"currency":"USD","cycle":"monthly","due_date":"2027-01-31","status":"active","time_zone":"Asia/Tokyo"}`
 write:=accessTestRequest("POST","/api/v1/subscriptions/manage",cfg.Hostname,token,payload)
 write.Header.Set("X-MiniProbe-Request","1")
 write.Header.Set("Origin","https://"+cfg.Hostname)
 write.Header.Set("Content-Type","application/json")
 write.Header.Set(subscriptionCSRFHeader,s.subscriptionCSRFToken(write))
 w=httptest.NewRecorder();s.subscriptionWebManage(w,write)
 if w.Code!=200{t.Fatalf("valid Cloudflare Access write rejected %d %s",w.Code,w.Body.String())}
 for _,name:=range []string{"no CSRF","wrong CSRF","cross-origin"}{
  wr:=accessTestRequest("POST","/api/v1/subscriptions/manage",cfg.Hostname,token,payload)
  wr.Header.Set("X-MiniProbe-Request","1")
  wr.Header.Set("Origin","https://"+cfg.Hostname)
  wr.Header.Set("Content-Type","application/json")
  wr.Header.Set(subscriptionCSRFHeader,s.subscriptionCSRFToken(wr))
  if name=="no CSRF"{wr.Header.Del(subscriptionCSRFHeader)}
  if name=="wrong CSRF"{wr.Header.Set(subscriptionCSRFHeader,"invalid")}
  if name=="cross-origin"{wr.Header.Set("Origin","https://evil.example.com")}
  w=httptest.NewRecorder();s.subscriptionWebManage(w,wr)
  if w.Code!=403{t.Errorf("%s write allowed with %d",name,w.Code)}
 }
 // The original username/password path is not a fallback on Access hostname.
 login:=accessTestRequest("POST","/api/v1/subscriptions/login",cfg.Hostname,token,`{"password":"test-password"}`)
 w=httptest.NewRecorder();s.subscriptionLogin(w,login)
 if w.Code!=403{t.Fatalf("Access host permitted password fallback: %d",w.Code)}
 // No user access data on ordinary hostname without independent password.
 main:=accessTestRequest("GET","/api/v1/subscriptions/session","monitor.example.com",token,"")
 w=httptest.NewRecorder();s.subscriptionSession(w,main)
 if strings.Contains(w.Body.String(),`"authenticated":true`){t.Fatal("JWT reused across hostnames")}
}
func TestSubscriptionAccessJWKFiltering(t *testing.T){
 key,err:=rsa.GenerateKey(rand.Reader,2048);if err!=nil{t.Fatal(err)}
 public:=&key.PublicKey
 body,_:=json.Marshal(map[string]any{"keys":[]map[string]any{
  {"kid":"test-key","kty":"RSA","alg":"RS256","n":base64.RawURLEncoding.EncodeToString(public.N.Bytes()),"e":base64.RawURLEncoding.EncodeToString(big.NewInt(int64(public.E)).Bytes())},
 }})
 keys,err:=accessJSONPublicKeys(body)
 if err!=nil||len(keys)!=1{t.Fatalf("JWK parse failed: %v",err)}
 if keys["test-key"].E!=public.E||keys["test-key"].N.Cmp(public.N)!=0{t.Fatal("parsed JWK key mismatch")}
 _,err=accessJSONPublicKeys([]byte(`{"keys":[{"kid":"x","kty":"oct","alg":"HS256","n":"AA","e":"AQAB"}]}`))
 if err==nil{t.Fatal("HMAC JWK accepted as RSA")}
 _=fmt.Sprintf
}
