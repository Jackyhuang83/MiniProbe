package main

// Cloudflare Access authenticates the owner only on a dedicated subscription
// hostname. Server-side RS256 verification is mandatory; unverified identity
// headers, the CF_Authorization cookie, and dashboard sessions are not trusted.
import (
 "crypto"
 "crypto/hmac"
 "crypto/rsa"
 "crypto/sha256"
 "encoding/base64"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "math/big"
 "net"
 "net/http"
 "net/mail"
 "net/url"
 "os"
 "path/filepath"
 "regexp"
 "strings"
 "time"
)

type subscriptionAccessConfig struct {
 Enabled bool `json:"enabled"`
 Hostname string `json:"hostname"`
 Team string `json:"team"`
 Audience string `json:"audience"`
 Email string `json:"email"`
}
var accessTeamRE=regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var accessHostRE=regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
var accessAudienceRE=regexp.MustCompile(`^[A-Za-z0-9_-]{16,256}$`)
var accessKidRE=regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func validateSubscriptionAccess(c subscriptionAccessConfig) error {
 if !c.Enabled{return errors.New("must explicitly enable Cloudflare Access")}
 if !accessTeamRE.MatchString(c.Team){return errors.New("Cloudflare Team 名称无效")}
 if !accessHostRE.MatchString(c.Hostname)||len(c.Hostname)>253||strings.HasSuffix(c.Hostname,".cloudflareaccess.com"){return errors.New("订阅专用域名无效")}
 if !accessAudienceRE.MatchString(c.Audience){return errors.New("Access 应用 AUD 无效")}
 if len(c.Email)>254||strings.ToLower(c.Email)!=c.Email||strings.TrimSpace(c.Email)!=c.Email||strings.ContainsAny(c.Email,"\r\n<>"){
  return errors.New("允许的邮箱地址无效")
 }
 a,err:=mail.ParseAddress(c.Email)
 if err!=nil||a.Address!=c.Email||!strings.Contains(c.Email,"@"){return errors.New("允许的邮箱地址无效")}
 return nil
}
func (s *server) subscriptionAccessPath()string{return filepath.Join(filepath.Dir(s.dataFile),"subscriptions-access.json")}
func (s *server) loadSubscriptionAccess()error{
 info,err:=os.Lstat(s.subscriptionAccessPath())
 if errors.Is(err,os.ErrNotExist){return nil}
 if err!=nil{return err}
 if !info.Mode().IsRegular()||info.Mode().Perm()&0077!=0||info.Size()>4096{return errors.New("订阅 Access 文件须为小于 4KiB 的 0600 普通文件")}
 b,err:=os.ReadFile(s.subscriptionAccessPath());if err!=nil{return err}
 var c subscriptionAccessConfig
 if err:=json.Unmarshal(b,&c);err!=nil{return err}
 if err:=validateSubscriptionAccess(c);err!=nil{return err}
 s.subscriptionAccessMu.Lock();s.subscriptionAccess=c;s.subscriptionAccessMu.Unlock()
 return nil
}
func (s *server) saveSubscriptionAccess(c subscriptionAccessConfig)error{
 b,err:=json.MarshalIndent(c,"","  ");if err!=nil{return err}
 dir:=filepath.Dir(s.subscriptionAccessPath())
 if err:=os.MkdirAll(dir,0750);err!=nil{return err}
 file,err:=os.CreateTemp(dir,".subscriptions-access-*.tmp");if err!=nil{return err}
 temp:=file.Name();defer os.Remove(temp)
 if err=file.Chmod(0600);err==nil{_,err=file.Write(b)}
 if err==nil{err=file.Sync()}
 if closeErr:=file.Close();err==nil{err=closeErr}
 if err!=nil{return err}
 if err=os.Rename(temp,s.subscriptionAccessPath());err!=nil{return err}
 s.subscriptionAccessMu.Lock()
 s.subscriptionAccess=c;s.subscriptionAccessKeys=nil;s.subscriptionAccessKeysUntil=time.Time{}
 s.subscriptionAccessMu.Unlock()
 return nil
}
func (s *server) localSubscriptionAccess(w http.ResponseWriter,r *http.Request){
 noStore(w)
 switch r.Method{
 case http.MethodGet:
  s.subscriptionAccessMu.RLock();cfg:=s.subscriptionAccess;s.subscriptionAccessMu.RUnlock()
  writeJSON(w,200,cfg)
 case http.MethodPut:
  var in struct{subscriptionAccessConfig;Confirm string `json:"confirm"`}
  if err:=decodeJSON(w,r,&in);err!=nil||in.Confirm!="ENABLE"{http.Error(w,"需要 ENABLE 确认",400);return}
  c:=in.subscriptionAccessConfig
  if err:=validateSubscriptionAccess(c);err!=nil{http.Error(w,err.Error(),400);return}
  s.mu.RLock();publicURL:=s.db.Settings.PublicURL;s.mu.RUnlock()
  if u,err:=url.Parse(publicURL);err==nil&&strings.EqualFold(u.Hostname(),c.Hostname){
   http.Error(w,"订阅域名必须与现有 Agent / Dashboard 主域名不同",409);return
  }
  // Do not persist an unverified configuration that could lock out the owner.
  // Connectivity/test occurs before any Access mode becomes active.
  if _,err:=s.fetchSubscriptionAccessKeys(c);err!=nil{
   http.Error(w,"Cloudflare Access 公钥获取失败，原配置保持不变",502);return
  }
  if err:=s.saveSubscriptionAccess(c);err!=nil{http.Error(w,"本地 Access 配置保存失败",500);return}
  writeJSON(w,200,map[string]bool{"ok":true})
 case http.MethodDelete:
  var in struct{Confirm string `json:"confirm"`}
  if err:=decodeJSON(w,r,&in);err!=nil||in.Confirm!="DISABLE"{http.Error(w,"需要 DISABLE 确认",400);return}
  s.subscriptionAccessMu.Lock()
  err:=os.Remove(s.subscriptionAccessPath());if errors.Is(err,os.ErrNotExist){err=nil}
  if err==nil{s.subscriptionAccess=subscriptionAccessConfig{};s.subscriptionAccessKeys=nil;s.subscriptionAccessKeysUntil=time.Time{}}
  s.subscriptionAccessMu.Unlock()
  if err!=nil{http.Error(w,"移除 Access 配置失败",500);return}
  writeJSON(w,200,map[string]bool{"ok":true})
 default:http.Error(w,"method not allowed",405)
 }
}
func (s *server) subscriptionAccessForHost(r *http.Request)(subscriptionAccessConfig,bool){
 s.subscriptionAccessMu.RLock();c:=s.subscriptionAccess;s.subscriptionAccessMu.RUnlock()
 // A dedicated hostname is mandatory. Main monitoring URL remains unchanged.
 host:=strings.ToLower(r.Host)
 return c,c.Enabled&&host==c.Hostname
}
type subscriptionAccessJWK struct {
 Kid string `json:"kid"`
 Kty string `json:"kty"`
 Alg string `json:"alg"`
 N string `json:"n"`
 E string `json:"e"`
}
func accessJSONPublicKeys(raw []byte)(map[string]*rsa.PublicKey,error){
 var response struct{Keys []subscriptionAccessJWK `json:"keys"`}
 if err:=json.Unmarshal(raw,&response);err!=nil{return nil,err}
 if len(response.Keys)==0||len(response.Keys)>12{return nil,errors.New("Cloudflare public key set empty or invalid")}
 keys:=make(map[string]*rsa.PublicKey)
 for _,k:=range response.Keys{
  if k.Kty!="RSA"||!accessKidRE.MatchString(k.Kid)||(k.Alg!=""&&k.Alg!="RS256"){continue}
  n,err1:=base64.RawURLEncoding.DecodeString(k.N)
  e,err2:=base64.RawURLEncoding.DecodeString(k.E)
  if err1!=nil||err2!=nil||len(n)<256||len(n)>1024||len(e)==0||len(e)>4{continue}
  modulus:=new(big.Int).SetBytes(n)
  exp:=new(big.Int).SetBytes(e)
  if !exp.IsInt64()||exp.Int64()<3||exp.Int64()>2147483647||exp.Int64()%2==0{continue}
  keys[k.Kid]=&rsa.PublicKey{N:modulus,E:int(exp.Int64())}
 }
 if len(keys)==0{return nil,errors.New("no valid RS256 public keys")}
 return keys,nil
}
func (s *server) fetchSubscriptionAccessKeys(c subscriptionAccessConfig)(map[string]*rsa.PublicKey,error){
 target:="https://"+c.Team+".cloudflareaccess.com/cdn-cgi/access/certs"
 client:=&http.Client{
  Timeout:6*time.Second,
  CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse},
  Transport:&http.Transport{Proxy:nil,DialContext:(&net.Dialer{Timeout:4*time.Second}).DialContext,
   TLSHandshakeTimeout:4*time.Second,DisableKeepAlives:true},
 }
 req,err:=http.NewRequest("GET",target,nil);if err!=nil{return nil,err}
 resp,err:=client.Do(req);if err!=nil{return nil,errors.New("Access 公钥端点不可用")}
 defer resp.Body.Close()
 if resp.StatusCode!=200{return nil,errors.New("Access 公钥服务器未返回 200")}
 body,err:=io.ReadAll(io.LimitReader(resp.Body,65537))
 if err!=nil||len(body)>65536{return nil,errors.New("Access 公钥数据过大或读取失败")}
 return accessJSONPublicKeys(body)
}
func (s *server) subscriptionAccessKey(c subscriptionAccessConfig,kid string)*rsa.PublicKey{
 now:=time.Now()
 s.subscriptionAccessMu.RLock()
 key:=s.subscriptionAccessKeys[kid]
 fresh:=now.Before(s.subscriptionAccessKeysUntil)
 s.subscriptionAccessMu.RUnlock()
 if fresh{return key}
 // Throttle unknown-kid refreshes to avoid external request flooding.
 s.subscriptionAccessFetchMu.Lock()
 defer s.subscriptionAccessFetchMu.Unlock()
 s.subscriptionAccessMu.RLock()
 key=s.subscriptionAccessKeys[kid];fresh=time.Now().Before(s.subscriptionAccessKeysUntil)
 s.subscriptionAccessMu.RUnlock()
 if fresh{return key}
 keys,err:=s.fetchSubscriptionAccessKeys(c)
 s.subscriptionAccessMu.Lock()
 defer s.subscriptionAccessMu.Unlock()
 if err==nil && s.subscriptionAccess==c{s.subscriptionAccessKeys=keys;s.subscriptionAccessKeysUntil=time.Now().Add(10*time.Minute)}
 if err!=nil{s.subscriptionAccessKeysUntil=time.Now().Add(time.Minute)}
 return s.subscriptionAccessKeys[kid]
}
func (s *server) subscriptionAccessValid(r *http.Request,c subscriptionAccessConfig)bool{
 if !requestIsTLS(r){return false}
 raw:=r.Header.Get("Cf-Access-Jwt-Assertion")
 if len(raw)<50||len(raw)>12000{return false}
 parts:=strings.Split(raw,".");if len(parts)!=3{return false}
 h,err1:=base64.RawURLEncoding.DecodeString(parts[0])
 p,err2:=base64.RawURLEncoding.DecodeString(parts[1])
 sig,err3:=base64.RawURLEncoding.DecodeString(parts[2])
 if err1!=nil||err2!=nil||err3!=nil||len(h)>2048||len(p)>8192{return false}
 var header struct{Alg string `json:"alg"`;Kid string `json:"kid"`;Typ string `json:"typ"`}
 if json.Unmarshal(h,&header)!=nil||header.Alg!="RS256"||!accessKidRE.MatchString(header.Kid){return false}
 key:=s.subscriptionAccessKey(c,header.Kid)
 if key==nil{return false}
 digest:=sha256.Sum256([]byte(parts[0]+"."+parts[1]))
 if rsa.VerifyPKCS1v15(key,crypto.SHA256,digest[:],sig)!=nil{return false}
 var claims struct {
  Iss string `json:"iss"`
  Aud json.RawMessage `json:"aud"`
  Email string `json:"email"`
  Exp int64 `json:"exp"`
  Nbf int64 `json:"nbf"`
  Iat int64 `json:"iat"`
 }
 if json.Unmarshal(p,&claims)!=nil{return false}
 if claims.Iss!="https://"+c.Team+".cloudflareaccess.com"||!strings.EqualFold(claims.Email,c.Email){return false}
 var audList []string
 if err:=json.Unmarshal(claims.Aud,&audList);err!=nil{
  var single string
  if json.Unmarshal(claims.Aud,&single)!=nil{return false}
  audList=[]string{single}
 }
 hasAudience:=false
 for _,aud:=range audList{if aud==c.Audience{hasAudience=true}}
 if !hasAudience{return false}
 now:=time.Now().Unix()
 if claims.Exp<=now||claims.Exp>now+24*3600*31||claims.Iat<=0||claims.Iat>now+60||claims.Nbf>now+60{return false}
 return true
}
func (s *server) subscriptionAuthorized(r *http.Request)bool{
 c,access:=s.subscriptionAccessForHost(r)
 if access{return s.subscriptionAccessValid(r,c)}
 return s.hasSubscriptionSession(r)
}
func (s *server) subscriptionAccessCSRF(r *http.Request)string{
 c,access:=s.subscriptionAccessForHost(r)
 if !access||!s.subscriptionAccessValid(r,c){return ""}
 mac:=hmac.New(sha256.New,s.secret)
 mac.Write([]byte("miniprobe-cf-access-csrf-v1\x00"))
 mac.Write([]byte(r.Header.Get("Cf-Access-Jwt-Assertion")))
 return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *server) subscriptionAccessEnabled(r *http.Request)bool{
 _,enabled:=s.subscriptionAccessForHost(r);return enabled
}
var _=fmt.Sprintf
