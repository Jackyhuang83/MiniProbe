package main

// DEV4: narrowly scoped HTTPS owner mutations. Never expose the local Unix
// management mux, password setter, backup/restore or node configuration.
import (
 "crypto/hmac"
 "crypto/sha256"
 "encoding/base64"
 "net/http"
 "mime"
 "net/url"
 "strings"
)

const subscriptionCSRFHeader = "X-MiniProbe-Subscription-CSRF"

// The token is independently MACed and bound to one signed HttpOnly session.
// It is sent only after a successful HTTPS session check, never in a cookie.
func (s *server) subscriptionCSRFToken(r *http.Request) string {
 if s.subscriptionAccessEnabled(r){return s.subscriptionAccessCSRF(r)}
 c,err:=r.Cookie(subscriptionCookie)
 if err!=nil{return ""}
 mac:=hmac.New(sha256.New,s.secret)
 mac.Write([]byte("miniprobe-subscription-csrf-v1\x00"))
 mac.Write([]byte(c.Value))
 return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func subscriptionWebOriginOK(r *http.Request) bool {
 if r.Header.Get("X-MiniProbe-Request")!="1" {return false}
 raw:=r.Header.Get("Origin")
 if raw==""||raw=="null" {return false}
 origin,err:=url.Parse(raw)
 if err!=nil||origin.Scheme!="https"||origin.User!=nil||
    origin.Host==""||origin.Opaque!=""||origin.Path!=""||
    origin.RawQuery!=""||origin.Fragment!="" {return false}
 return strings.EqualFold(origin.Host,r.Host)
}

func (s *server) requireSubscriptionWebWrite(w http.ResponseWriter,r *http.Request) bool {
 noStore(w)
 if !requestIsTLS(r) || !s.subscriptionAuthorized(r) {
  http.Error(w,"subscription owner session required",http.StatusUnauthorized)
  return false
 }
 mediaType,_,err:=mime.ParseMediaType(r.Header.Get("Content-Type"))
 if err!=nil||mediaType!="application/json"||!subscriptionWebOriginOK(r) {
  http.Error(w,"subscription write origin or content type forbidden",http.StatusForbidden)
  return false
 }
 got:=r.Header.Get(subscriptionCSRFHeader)
 want:=s.subscriptionCSRFToken(r)
 if want==""||!hmac.Equal([]byte(got),[]byte(want)) {
  http.Error(w,"subscription write token invalid",http.StatusForbidden)
  return false
 }
 return true
}

// A single restricted route delegates to existing validated transaction
// handlers: POST=create, PUT=edit, DELETE=manual-only deletion. GET forbidden.
func (s *server) subscriptionWebManage(w http.ResponseWriter,r *http.Request) {
 if r.Method!=http.MethodPost&&r.Method!=http.MethodPut&&r.Method!=http.MethodDelete {
  noStore(w)
  http.Error(w,"method not allowed",http.StatusMethodNotAllowed)
  return
 }
 if !s.requireSubscriptionWebWrite(w,r){return}
 s.localSubscriptions(w,r)
}

// Renewal history is append-only; node-linked VPS due dates remain managed by
// node configuration and cannot be silently changed from the web.
func (s *server) subscriptionWebRenewal(w http.ResponseWriter,r *http.Request) {
 if r.Method!=http.MethodPost {
  noStore(w)
  http.Error(w,"method not allowed",http.StatusMethodNotAllowed)
  return
 }
 if !s.requireSubscriptionWebWrite(w,r){return}
 s.localSubscriptionRenewals(w,r)
}
