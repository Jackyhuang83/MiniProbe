package main

// One-time mailbox recovery is a separate, optional owner authentication path.
// Request never accepts an address: mail can only go to the locally enrolled
// recipient, preventing the public endpoint from becoming an email relay.
import (
 "crypto/hmac"
 "crypto/sha256"
 "encoding/base64"
 "encoding/binary"
 "encoding/hex"
 "fmt"
 "net/http"
 "regexp"
 "time"
)

const (
 subscriptionEmailChallengeCookie = "miniprobe_sub_email_challenge"
 subscriptionEmailResetCookie = "miniprobe_sub_email_reset"
 subscriptionEmailTTL = 10*time.Minute
)
var subscriptionEightDigits=regexp.MustCompile(`^[0-9]{8}$`)
type subscriptionEmailChallenge struct {
 Digest []byte
 Until time.Time
 Attempts int
 PasswordHash string
}
func (s *server) subscriptionEmailDigest(id,code string)[]byte{
 mac:=hmac.New(sha256.New,s.secret)
 mac.Write([]byte("subscription-email-one-time-v1\x00"))
 mac.Write([]byte(id))
 mac.Write([]byte{0})
 mac.Write([]byte(code))
 return mac.Sum(nil)
}
func (s *server) emailRecoveryAvailable()bool{
 s.subscriptionEmailMu.Lock();enabled:=s.subscriptionEmailConfig.Enabled;s.subscriptionEmailMu.Unlock()
 return enabled
}
func (s *server) subscriptionEmailStart(w http.ResponseWriter,r *http.Request){
 noStore(w)
 if s.subscriptionAccessEnabled(r){http.Error(w,"此域名使用 Cloudflare Access 邮箱登录",403);return}
 if r.Method!=http.MethodPost{http.Error(w,"method not allowed",405);return}
 if !requestIsTLS(r)||!subscriptionWebOriginOK(r)||r.Header.Get("Content-Type")!="application/json"{
  http.Error(w,"forbidden",403);return
 }
 // Invalid or unconfigured email accounts receive no mail. Never accept a
 // recipient from HTTP, and never display the registered address publicly.
 s.subscriptionMu.RLock();hash:=s.subscriptions.OwnerPassword.Hash;s.subscriptionMu.RUnlock()
 s.subscriptionEmailMu.Lock()
 cfg:=s.subscriptionEmailConfig
 now:=time.Now()
 if !cfg.Enabled||hash==""{s.subscriptionEmailMu.Unlock();writeJSON(w,200,map[string]bool{"ok":true});return}
 kept:=s.subscriptionEmailSends[:0]
 for _,sent:=range s.subscriptionEmailSends{if now.Sub(sent)<time.Hour{kept=append(kept,sent)}}
 s.subscriptionEmailSends=kept
 if (len(kept)>0&&now.Sub(kept[len(kept)-1])<time.Minute)||len(kept)>=6{
  s.subscriptionEmailMu.Unlock()
  http.Error(w,"发送频率过高，请稍后再试",429);return
 }
 if s.subscriptionEmailChallenges==nil{s.subscriptionEmailChallenges=make(map[string]subscriptionEmailChallenge)}
 for id,ch:=range s.subscriptionEmailChallenges{if now.After(ch.Until){delete(s.subscriptionEmailChallenges,id)}}
 id:=hex.EncodeToString(randomBytes(16))
 code:=fmt.Sprintf("%08d",binary.BigEndian.Uint64(randomBytes(8))%100000000)
 s.subscriptionEmailChallenges[id]=subscriptionEmailChallenge{Digest:s.subscriptionEmailDigest(id,code),Until:now.Add(subscriptionEmailTTL),PasswordHash:hash}
 s.subscriptionEmailSends=append(s.subscriptionEmailSends,now)
 s.subscriptionEmailMu.Unlock()

 body:=fmt.Sprintf("Your MiniProbe subscription sign-in code: %s\n\nExpires in 10 minutes and can be used once. If you did not request this code, ignore this message.\n",code)
 if err:=s.sendSubscriptionMail(cfg,body);err!=nil{
  s.subscriptionEmailMu.Lock();delete(s.subscriptionEmailChallenges,id);s.subscriptionEmailMu.Unlock()
  // No host, username, recipient or SMTP secret is exposed in errors/logs.
  http.Error(w,"邮件发送失败，请通过本地 MiniProbe 菜单检查 SMTP 设置",503);return
 }
 http.SetCookie(w,&http.Cookie{Name:subscriptionEmailChallengeCookie,Value:id,
  Path:"/api/v1/subscriptions",Expires:now.Add(subscriptionEmailTTL),
  MaxAge:int(subscriptionEmailTTL.Seconds()),Secure:true,HttpOnly:true,SameSite:http.SameSiteStrictMode})
 writeJSON(w,200,map[string]bool{"ok":true})
}

func (s *server) emailResetTicket(session string,expiry time.Time)string{
 payload:=make([]byte,8)
 binary.BigEndian.PutUint64(payload,uint64(expiry.Unix()))
 mac:=hmac.New(sha256.New,s.secret)
 mac.Write([]byte("miniprobe-sub-email-reset-v1\x00"))
 mac.Write([]byte(session));mac.Write([]byte{0});mac.Write(payload)
 return base64.RawURLEncoding.EncodeToString(append(payload,mac.Sum(nil)...))
}
func (s *server) hasSubscriptionEmailReset(r *http.Request)bool{
 if !s.hasSubscriptionSession(r){return false}
 session,err:=r.Cookie(subscriptionCookie);if err!=nil{return false}
 ticket,err:=r.Cookie(subscriptionEmailResetCookie);if err!=nil{return false}
 b,err:=base64.RawURLEncoding.DecodeString(ticket.Value)
 if err!=nil||len(b)!=8+sha256.Size{return false}
 deadline:=int64(binary.BigEndian.Uint64(b[:8]))
 if deadline<=time.Now().Unix()||deadline>time.Now().Add(subscriptionEmailTTL+time.Minute).Unix(){return false}
 expected:=s.emailResetTicket(session.Value,time.Unix(deadline,0))
 return hmac.Equal([]byte(ticket.Value),[]byte(expected))
}
func (s *server) subscriptionEmailVerify(w http.ResponseWriter,r *http.Request){
 noStore(w)
 if s.subscriptionAccessEnabled(r){http.Error(w,"此域名使用 Cloudflare Access 邮箱登录",403);return}
 if r.Method!=http.MethodPost{http.Error(w,"method not allowed",405);return}
 if !requestIsTLS(r)||!subscriptionWebOriginOK(r)||r.Header.Get("Content-Type")!="application/json"{
  http.Error(w,"forbidden",403);return
 }
 ip:="sub-email-verify:"+remoteIP(r.RemoteAddr)
 if !s.loginAllowed(ip){http.Error(w,"验证次数过多",429);return}
 var in struct {Code string `json:"code"`}
 if err:=decodeJSON(w,r,&in);err!=nil||!subscriptionEightDigits.MatchString(in.Code){
  http.Error(w,"验证码无效",400);return
 }
 cookie,err:=r.Cookie(subscriptionEmailChallengeCookie)
 if err!=nil||len(cookie.Value)!=32{
  s.noteLoginFailure(ip);http.Error(w,"验证码无效或已过期",401);return
 }
 id:=cookie.Value
 s.subscriptionEmailMu.Lock()
 challenge,exists:=s.subscriptionEmailChallenges[id]
 if !exists||time.Now().After(challenge.Until)||challenge.Attempts>=5{
  delete(s.subscriptionEmailChallenges,id);s.subscriptionEmailMu.Unlock()
  s.noteLoginFailure(ip);http.Error(w,"验证码无效或已过期",401);return
 }
 challenge.Attempts++
 match:=hmac.Equal(challenge.Digest,s.subscriptionEmailDigest(id,in.Code))
 if match||challenge.Attempts>=5 {delete(s.subscriptionEmailChallenges,id)} else{s.subscriptionEmailChallenges[id]=challenge}
 s.subscriptionEmailMu.Unlock()
 s.subscriptionMu.RLock();hash:=s.subscriptions.OwnerPassword.Hash;s.subscriptionMu.RUnlock()
 if !match||hash==""||hash!=challenge.PasswordHash{
  s.noteLoginFailure(ip);http.Error(w,"验证码无效或已过期",401);return
 }
 s.clearLoginFailures(ip)
 now:=time.Now();exp:=now.Add(subscriptionSessionTTL)
 session:=s.newSubscriptionSession(exp)
 http.SetCookie(w,&http.Cookie{Name:subscriptionCookie,Value:session,
  Path:"/api/v1/subscriptions",Expires:exp,MaxAge:int(subscriptionSessionTTL.Seconds()),
  Secure:true,HttpOnly:true,SameSite:http.SameSiteStrictMode})
 resetExpiry:=now.Add(subscriptionEmailTTL)
 http.SetCookie(w,&http.Cookie{Name:subscriptionEmailResetCookie,Value:s.emailResetTicket(session,resetExpiry),
  Path:"/api/v1/subscriptions",Expires:resetExpiry,MaxAge:int(subscriptionEmailTTL.Seconds()),
  Secure:true,HttpOnly:true,SameSite:http.SameSiteStrictMode})
 http.SetCookie(w,&http.Cookie{Name:subscriptionEmailChallengeCookie,Path:"/api/v1/subscriptions",MaxAge:-1,Secure:true,HttpOnly:true,SameSite:http.SameSiteStrictMode})
 writeJSON(w,200,map[string]bool{"ok":true})
}

// Only an actively email-verified session may reset its password from the web.
// Ordinary password sessions cannot silently change credentials from a stale tab.
func (s *server) subscriptionEmailResetPassword(w http.ResponseWriter,r *http.Request){
 noStore(w)
 if r.Method!=http.MethodPut{http.Error(w,"method not allowed",405);return}
 if !s.requireSubscriptionWebWrite(w,r){return}
 if !s.hasSubscriptionEmailReset(r){http.Error(w,"邮箱验证已过期，请重新获取验证码",403);return}
 var in struct{Password string `json:"password"`}
 if err:=decodeJSON(w,r,&in);err!=nil{http.Error(w,"bad JSON",400);return}
 if len(in.Password)<12||len(in.Password)>128{http.Error(w,"新密码必须为 12–128 字符",400);return}
 s.subscriptionMu.Lock()
 next:=s.subscriptions
 next.OwnerPassword=hashPassword(in.Password)
 err:=s.saveSubscriptionsLocked(next)
 s.subscriptionMu.Unlock()
 if err!=nil{http.Error(w,"保存密码失败",500);return}
 for _,name:=range []string{subscriptionCookie,subscriptionEmailResetCookie}{
  http.SetCookie(w,&http.Cookie{Name:name,Path:"/api/v1/subscriptions",MaxAge:-1,
   Secure:true,HttpOnly:true,SameSite:http.SameSiteStrictMode})
 }
 writeJSON(w,200,map[string]bool{"ok":true})
}
