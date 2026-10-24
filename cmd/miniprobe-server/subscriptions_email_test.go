package main

import (
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "os"
 "regexp"
 "strings"
 "testing"
 "time"
)

func subscriptionMailFixture()subscriptionMailConfig{
 return subscriptionMailConfig{Enabled:true,Host:"smtp.example.com",Port:465,Username:"sender@example.com",Password:"smtp-app-password",From:"sender@example.com",To:"recipient@example.com"}
}
func subscriptionOTPRequest(method,route,body string)*http.Request{
 r:=httptest.NewRequest(method,"https://probe.example/api/v1/subscriptions"+route,strings.NewReader(body))
 r.Header.Set("Origin","https://probe.example")
 r.Header.Set("X-MiniProbe-Request","1")
 r.Header.Set("Content-Type","application/json")
 return r
}
func TestSubscriptionEmailConfigLocalOnlyAndPrivate(t *testing.T){
 s:=subscriptionTestServer(t)
 cfg:=subscriptionMailFixture()
 s.subscriptionEmailSend=func(c subscriptionMailConfig,body string)error{
  if c.Password!=cfg.Password||!strings.Contains(body,"SMTP test"){t.Fatal("SMTP test payload mismatch")}
  return nil
 }
 payload,_:=json.Marshal(cfg)
 r:=httptest.NewRequest(http.MethodPut,"http://unix/v1/subscriptions/email",strings.NewReader(string(payload)))
 w:=httptest.NewRecorder();s.localSubscriptionMail(w,r)
 if w.Code!=200{t.Fatalf("cannot enroll SMTP: %d %s",w.Code,w.Body.String())}
 st,err:=os.Stat(s.subscriptionMailPath())
 if err!=nil||st.Mode().Perm()!=0600{t.Fatalf("SMTP secret file must be 0600: %v %v",st,err)}
 response:=w.Body.String()
 if strings.Contains(response,cfg.Password)||strings.Contains(response,cfg.To){t.Fatal("SMTP credentials or full recipient disclosed")}
 second:=subscriptionTestServer(t);second.dataFile=s.dataFile
 if err:=second.loadSubscriptionMail();err!=nil{t.Fatal(err)}
 if second.subscriptionEmailConfig.Password!=cfg.Password{t.Fatal("SMTP secret did not persist")}
 old:=s.subscriptionEmailConfig
 s.subscriptionEmailSend=func(subscriptionMailConfig,string)error{return errors.New("SMTP unreachable")}
 w=httptest.NewRecorder();s.localSubscriptionMail(w,httptest.NewRequest(http.MethodPut,"http://unix/v1/subscriptions/email",strings.NewReader(string(payload))))
 if w.Code!=502||s.subscriptionEmailConfig!=old{t.Fatal("SMTP test failure overwrote old config")}
 w=httptest.NewRecorder();s.localSubscriptionMail(w,httptest.NewRequest(http.MethodDelete,"http://unix/v1/subscriptions/email",strings.NewReader(`{"confirm":"DISABLE"}`)))
 if w.Code!=200{t.Fatalf("disable failed %d",w.Code)}
 if _,err:=os.Stat(s.subscriptionMailPath());!os.IsNotExist(err){t.Fatalf("SMTP secret persisted after disable: %v",err)}
}
func TestSubscriptionEmailConfigValidation(t *testing.T){
 cfg:=subscriptionMailFixture()
 for _,tc:=range []struct{label string;cfg subscriptionMailConfig}{
  {"plaintext 25",func()subscriptionMailConfig{x:=cfg;x.Port=25;return x}()},
  {"smtp header injection",func()subscriptionMailConfig{x:=cfg;x.To="victim@example.com\r\nBcc:other@example.com";return x}()},
  {"bad host",func()subscriptionMailConfig{x:=cfg;x.Host="smtp..example.com";return x}()},
  {"empty authorization",func()subscriptionMailConfig{x:=cfg;x.Password="";return x}()},
 }{if validateSubscriptionMail(tc.cfg)==nil{t.Errorf("invalid config accepted: %s",tc.label)}}
}
func TestSubscriptionEmailRecoveryCodeLifecycleAndReset(t *testing.T){
 s:=subscriptionTestServer(t)
 s.subscriptions.OwnerPassword=hashPassword("preexisting secure owner password")
 s.subscriptionEmailConfig=subscriptionMailFixture()
 var emailBody string
 s.subscriptionEmailSend=func(_ subscriptionMailConfig,body string)error{emailBody=body;return nil}
 req:=subscriptionOTPRequest("POST","/email/request",`{}`)
 w:=httptest.NewRecorder();s.subscriptionEmailStart(w,req)
 if w.Code!=200{t.Fatalf("OTP start %d %s",w.Code,w.Body.String())}
 var challenge *http.Cookie
 for _,cookie:=range w.Result().Cookies(){if cookie.Name==subscriptionEmailChallengeCookie{challenge=cookie}}
 if challenge==nil||!challenge.HttpOnly||!challenge.Secure||challenge.SameSite!=http.SameSiteStrictMode{t.Fatal("OTP cookie missing security flags")}
 code:=regexp.MustCompile(`code: ([0-9]{8})`).FindStringSubmatch(emailBody)
 if len(code)!=2{t.Fatal("8 digit code not received by stub SMTP")}
 for _,sensitive:=range []string{code[1],s.subscriptionEmailConfig.To,s.subscriptionEmailConfig.Password}{
  if strings.Contains(w.Body.String(),sensitive){t.Fatal("public request leaked secret")}
 }
 reRequest:=subscriptionOTPRequest("POST","/email/request",`{}`)
 w=httptest.NewRecorder();s.subscriptionEmailStart(w,reRequest)
 if w.Code!=429{t.Fatalf("no cooldown after first email: %d",w.Code)}
 // Wrong code cannot be verified; the correct code still works for this browser.
 bad:=subscriptionOTPRequest("POST","/email/verify",`{"code":"00000000"}`)
 bad.AddCookie(challenge)
 if code[1]=="00000000"{t.Skip("rare randomly generated zero code")}
 w=httptest.NewRecorder();s.subscriptionEmailVerify(w,bad)
 if w.Code!=401{t.Fatalf("wrong code accepted %d",w.Code)}
 good:=subscriptionOTPRequest("POST","/email/verify",`{"code":"`+code[1]+`"}`)
 good.AddCookie(challenge)
 w=httptest.NewRecorder();s.subscriptionEmailVerify(w,good)
 if w.Code!=200{t.Fatalf("good code failed %d %s",w.Code,w.Body.String())}
 var authCookie,resetCookie *http.Cookie
 for _,cookie:=range w.Result().Cookies(){
  if cookie.Name==subscriptionCookie {authCookie=cookie}
  if cookie.Name==subscriptionEmailResetCookie {resetCookie=cookie}
 }
 if authCookie==nil||resetCookie==nil||!resetCookie.HttpOnly||!resetCookie.Secure{t.Fatal("email login did not issue safe owner and reset sessions")}
 w=httptest.NewRecorder();s.subscriptionEmailVerify(w,good)
 if w.Code!=401{t.Fatal("OTP was accepted more than once")}
 session:=httptest.NewRequest("GET","https://probe.example/api/v1/subscriptions/session",nil)
 session.AddCookie(authCookie);session.AddCookie(resetCookie)
 w=httptest.NewRecorder();s.subscriptionSession(w,session)
 var state map[string]any
 if err:=json.Unmarshal(w.Body.Bytes(),&state);err!=nil{t.Fatal(err)}
 if state["authenticated"]!=true||state["email_reset_allowed"]!=true{t.Fatalf("email login lacks reset authority: %v",state)}
 reset:=subscriptionOTPRequest("PUT","/email/reset-password",`{"password":"a changed safe subscription password"}`)
 reset.AddCookie(authCookie);reset.AddCookie(resetCookie)
 reset.Header.Set(subscriptionCSRFHeader,s.subscriptionCSRFToken(reset))
 w=httptest.NewRecorder();s.subscriptionEmailResetPassword(w,reset)
 if w.Code!=200{t.Fatalf("password reset failed %d %s",w.Code,w.Body.String())}
 if !verifyPassword("a changed safe subscription password",s.subscriptions.OwnerPassword){t.Fatal("new owner password did not persist")}
 if s.hasSubscriptionSession(session){t.Fatal("old password sessions remained valid after reset")}
 w=httptest.NewRecorder();s.subscriptionEmailResetPassword(w,reset)
 if w.Code==200{t.Fatal("reset token reusable after resetting password")}
}
func TestSubscriptionEmailRecoveryRejectsUnsafeAndExpired(t *testing.T){
 s:=subscriptionTestServer(t)
 s.subscriptions.OwnerPassword=hashPassword("original password used for owner")
 s.subscriptionEmailConfig=subscriptionMailFixture()
 s.subscriptionEmailSend=func(subscriptionMailConfig,string)error{return nil}
 for _,tc:=range []struct{name string;req *http.Request}{
  {"HTTP request",httptest.NewRequest("POST","http://probe.example/api/v1/subscriptions/email/request",strings.NewReader(`{}`))},
  {"cross origin",func()*http.Request{r:=subscriptionOTPRequest("POST","/email/request",`{}`);r.Header.Set("Origin","https://other.example");return r}()},
  {"missing origin",func()*http.Request{r:=subscriptionOTPRequest("POST","/email/request",`{}`);r.Header.Del("Origin");return r}()},
 }{
  w:=httptest.NewRecorder();s.subscriptionEmailStart(w,tc.req)
  if w.Code==200{t.Fatalf("unsafe email request accepted: %s",tc.name)}
 }
 r:=subscriptionOTPRequest("POST","/email/request",`{}`)
 w:=httptest.NewRecorder();s.subscriptionEmailStart(w,r)
 if w.Code!=200{t.Fatal("test challenge not issued")}
 cookie:=w.Result().Cookies()[0]
 s.subscriptionEmailMu.Lock()
 challenge:=s.subscriptionEmailChallenges[cookie.Value]
 challenge.Until=time.Now().Add(-time.Second)
 s.subscriptionEmailChallenges[cookie.Value]=challenge
 s.subscriptionEmailMu.Unlock()
 r=subscriptionOTPRequest("POST","/email/verify",`{"code":"11111111"}`)
 r.AddCookie(cookie)
 w=httptest.NewRecorder();s.subscriptionEmailVerify(w,r)
 if w.Code!=401{t.Fatalf("expired OTP accepted: %d",w.Code)}
 r=subscriptionOTPRequest("PUT","/email/reset-password",`{"password":"replaced password!"}`)
 w=httptest.NewRecorder();s.subscriptionEmailResetPassword(w,r)
 if w.Code==200{t.Fatal("unauthorized password reset accepted")}
}
