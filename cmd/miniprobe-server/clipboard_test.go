package main

import (
 "bytes"
 "crypto/tls"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
)

func newClipboardTestServer() *server {
 return &server{
  secret: []byte("clipboard-test-secret-not-used-in-production"),
  subscriptions: subscriptionStore{OwnerPassword: passwordConfig{Hash: "test-only-non-empty"}},
 }
}
func clipboardRequest(s *server, method, target, body string, authenticated bool) *http.Request {
 r := httptest.NewRequest(method, "https://example.test"+target, strings.NewReader(body))
 r.TLS = &tls.ConnectionState{}
 if authenticated {
  r.AddCookie(&http.Cookie{Name:subscriptionCookie, Value:s.newSubscriptionSession(time.Now().Add(time.Hour))})
  r.Header.Set("X-MiniProbe-Request", "1")
  r.Header.Set("X-MiniProbe-Subscription-CSRF", s.subscriptionCSRFToken(r))
 }
 return r
}
func clipboardResponse(s *server, req *http.Request) *httptest.ResponseRecorder {
 w := httptest.NewRecorder()
 s.clipboardAPI(w, req)
 return w
}
func TestClipboardRequiresSubscriptionAuthenticationAndHTTPS(t *testing.T) {
 s := newClipboardTestServer()
 for _, method := range []string{"GET", "POST", "DELETE"} {
  req := clipboardRequest(s, method, "/api/v1/subscriptions/clipboard", "", false)
  if got := clipboardResponse(s,req).Code; got != http.StatusUnauthorized {
   t.Errorf("%s unauthenticated status = %d, want 401", method, got)
  }
 }
 req := clipboardRequest(s,"GET","/api/v1/subscriptions/clipboard","",true)
 req.TLS = nil
 if got := clipboardResponse(s,req).Code; got != http.StatusUnauthorized {
  t.Errorf("HTTP request status = %d, want 401",got)
 }
}
func TestClipboardLifecycleAndTTL(t *testing.T) {
 s := newClipboardTestServer()
 endpoint := "/api/v1/subscriptions/clipboard"
 create := clipboardResponse(s,clipboardRequest(s,"POST",endpoint,`{"text":"hello\\n世界"}`,true))
 if create.Code != http.StatusCreated { t.Fatalf("create = %d: %s",create.Code,create.Body.String()) }
 var made clipboardItem
 if err:=json.Unmarshal(create.Body.Bytes(),&made);err!=nil {t.Fatal(err)}
 if made.ID == "" || made.Text != "hello\\n世界" {t.Fatalf("unexpected created item: %+v",made)}
 list := clipboardResponse(s,clipboardRequest(s,"GET",endpoint,"",true))
 if list.Code != http.StatusOK || !bytes.Contains(list.Body.Bytes(),[]byte("hello")) {
  t.Fatalf("list = %d: %s",list.Code,list.Body.String())
 }
 if h:=list.Header().Get("Cache-Control");h!="no-store" {t.Errorf("cache control = %q",h)}
 del := clipboardResponse(s,clipboardRequest(s,"DELETE",endpoint+"?id="+made.ID,"",true))
 if del.Code != http.StatusOK {t.Fatalf("delete = %d: %s",del.Code,del.Body.String())}
 if len(s.clipboard.items)!=0 {t.Fatalf("deleted item still present")}
 for i:=0;i<2;i++ {
  if got:=clipboardResponse(s,clipboardRequest(s,"POST",endpoint,`{"text":"keep"}`,true)).Code;got!=201 {t.Fatalf("create %d: %d",i,got)}
 }
 clear:=clipboardResponse(s,clipboardRequest(s,"DELETE",endpoint+"?all=1","",true))
 if clear.Code != 200 || len(s.clipboard.items)!=0 {t.Fatalf("clear = %d, remaining=%d",clear.Code,len(s.clipboard.items))}
 s.clipboard.items=[]clipboardItem{{ID:"expired",Text:"secret",ExpiresAt:time.Now().Add(-time.Minute)}}
 gone:=clipboardResponse(s,clipboardRequest(s,"GET",endpoint,"",true))
 if gone.Code != 200 || len(s.clipboard.items)!=0 || bytes.Contains(gone.Body.Bytes(),[]byte("secret")) {
  t.Fatalf("expired item not removed")
 }
}
func TestClipboardCSRFSizeAndLimit(t *testing.T) {
 s:=newClipboardTestServer()
 endpoint:="/api/v1/subscriptions/clipboard"
 missingCSRF:=clipboardRequest(s,"POST",endpoint,`{"text":"x"}`,true)
 missingCSRF.Header.Del("X-MiniProbe-Subscription-CSRF")
 if got:=clipboardResponse(s,missingCSRF).Code;got!=403 {t.Fatalf("missing CSRF: %d",got)}
 crossOrigin:=clipboardRequest(s,"POST",endpoint,`{"text":"x"}`,true)
 crossOrigin.Header.Set("Origin","https://attacker.example")
 if got:=clipboardResponse(s,crossOrigin).Code;got!=403 {t.Fatalf("cross origin: %d",got)}
 tooBig,_:=json.Marshal(map[string]string{"text":strings.Repeat("a",clipboardMaxTextBytes+1)})
 if got:=clipboardResponse(s,clipboardRequest(s,"POST",endpoint,string(tooBig),true)).Code;got!=400 {t.Fatalf("size: %d",got)}
 for i:=0;i<clipboardMaxItems+3;i++ {
  if got:=clipboardResponse(s,clipboardRequest(s,"POST",endpoint,`{"text":"x"}`,true)).Code;got!=201 {t.Fatalf("insert %d: %d",i,got)}
 }
 if len(s.clipboard.items)!=clipboardMaxItems {t.Fatalf("items=%d, want %d",len(s.clipboard.items),clipboardMaxItems)}
}
