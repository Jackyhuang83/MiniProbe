package main

// Optional subscription owner email recovery. Mail secrets never enter the
// public API, subscription ledger, or Telegram. This file is kept separate
// from the subscription ledger so a normal ledger export excludes SMTP keys.
import (
 "crypto/tls"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "net/mail"
 "net/smtp"
 "os"
 "path/filepath"
 "regexp"
 "strings"
 "time"
)

type subscriptionMailConfig struct {
 Enabled bool `json:"enabled"`
 Host string `json:"host"`
 Port int `json:"port"`
 Username string `json:"username"`
 Password string `json:"password"`
 From string `json:"from"`
 To string `json:"to"`
}

var mailHostPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,251}[A-Za-z0-9]$`)

func (s *server) subscriptionMailPath() string {
 return filepath.Join(filepath.Dir(s.dataFile),"subscriptions-email.json")
}
func cleanSubscriptionMailAddress(v string)(string,error){
 if v==""||len(v)>254||strings.ContainsAny(v,"\r\n<>") {return "",errors.New("邮箱格式无效")}
 a,err:=mail.ParseAddress(v)
 if err!=nil||a.Address!=v||!strings.Contains(v,"@"){return "",errors.New("邮箱格式无效")}
 return v,nil
}
func validateSubscriptionMail(cfg subscriptionMailConfig) error {
 if !cfg.Enabled{return errors.New("邮件恢复必须显式启用")}
 if len(cfg.Host)>253||!mailHostPattern.MatchString(cfg.Host)||strings.Contains(cfg.Host,"..") {return errors.New("SMTP 主机名无效")}
 if cfg.Port!=465&&cfg.Port!=587{return errors.New("SMTP 只允许 TLS 465 或 STARTTLS 587")}
 if cfg.Username==""||len(cfg.Username)>254||strings.ContainsAny(cfg.Username,"\r\n") {return errors.New("SMTP 账号无效")}
 if cfg.Password==""||len(cfg.Password)>256 {return errors.New("SMTP 授权码无效")}
 if _,err:=cleanSubscriptionMailAddress(cfg.From);err!=nil{return errors.New("发件地址无效")}
 if _,err:=cleanSubscriptionMailAddress(cfg.To);err!=nil{return errors.New("收件地址无效")}
 return nil
}
func (s *server) loadSubscriptionMail() error {
 info,err:=os.Lstat(s.subscriptionMailPath())
 if errors.Is(err,os.ErrNotExist){return nil}
 if err!=nil{return err}
 if !info.Mode().IsRegular()||info.Mode().Perm()&0077!=0||info.Size()>4096{
  return errors.New("subscriptions-email.json 必须为 0600 普通文件且小于 4KiB")
 }
 b,err:=os.ReadFile(s.subscriptionMailPath());if err!=nil{return err}
 var cfg subscriptionMailConfig
 if err=json.Unmarshal(b,&cfg);err!=nil{return errors.New("订阅邮箱配置文件无效")}
 if err=validateSubscriptionMail(cfg);err!=nil{return err}
 s.subscriptionEmailMu.Lock();s.subscriptionEmailConfig=cfg;s.subscriptionEmailMu.Unlock()
 return nil
}
func (s *server) saveSubscriptionMail(cfg subscriptionMailConfig)error {
 b,err:=json.MarshalIndent(cfg,"","  ");if err!=nil{return err}
 dir:=filepath.Dir(s.subscriptionMailPath())
 if err:=os.MkdirAll(dir,0750);err!=nil{return err}
 file,err:=os.CreateTemp(dir,".subscriptions-email-*.tmp");if err!=nil{return err}
 temp:=file.Name();defer os.Remove(temp)
 if err=file.Chmod(0600);err==nil{_,err=file.Write(b)}
 if err==nil{err=file.Sync()}
 if closeErr:=file.Close();err==nil{err=closeErr}
 if err!=nil{return err}
 if err:=os.Rename(temp,s.subscriptionMailPath());err!=nil{return err}
 s.subscriptionEmailMu.Lock()
 s.subscriptionEmailConfig=cfg
 s.subscriptionEmailChallenges=nil // credentials rotation revokes outstanding codes
 s.subscriptionEmailMu.Unlock()
 return nil
}
func maskSubscriptionEmail(v string)string {
 parts:=strings.SplitN(v,"@",2)
 if len(parts)!=2{return "已配置"}
 prefix:="*"
 if len(parts[0])>0{prefix=parts[0][:1]+"***"}
 return prefix+"@"+parts[1]
}
func (s *server) subscriptionMailStatus() map[string]any {
 s.subscriptionEmailMu.Lock()
 cfg:=s.subscriptionEmailConfig
 s.subscriptionEmailMu.Unlock()
 status:=map[string]any{"enabled":cfg.Enabled}
 if cfg.Enabled {status["recipient"]=maskSubscriptionEmail(cfg.To);status["smtp_host"]=cfg.Host;status["smtp_port"]=cfg.Port}
 return status
}
func (s *server) localSubscriptionMail(w http.ResponseWriter,r *http.Request){
 noStore(w)
 switch r.Method {
 case http.MethodGet:
  writeJSON(w,200,s.subscriptionMailStatus())
 case http.MethodPut:
  var cfg subscriptionMailConfig
  if err:=decodeJSON(w,r,&cfg);err!=nil{http.Error(w,"JSON 无效",400);return}
  if err:=validateSubscriptionMail(cfg);err!=nil{http.Error(w,err.Error(),400);return}
  // Only enable recovery once SMTP can deliver to the pre-registered mailbox.
  if err:=s.sendSubscriptionMail(cfg,"MiniProbe SMTP test\n\nEmail recovery can now be enabled. No login code is in this message.\n");err!=nil{
   http.Error(w,"SMTP 测试邮件发送失败；原配置保持不变",502);return
  }
  if err:=s.saveSubscriptionMail(cfg);err!=nil{http.Error(w,"本地邮箱配置保存失败",500);return}
  writeJSON(w,200,s.subscriptionMailStatus())
 case http.MethodDelete:
  var in struct{Confirm string `json:"confirm"`}
  if err:=decodeJSON(w,r,&in);err!=nil||in.Confirm!="DISABLE"{http.Error(w,"请确认 DISABLE",400);return}
  s.subscriptionEmailMu.Lock()
  // No recovery credential remains after disabling. Remove persisted file.
  err:=os.Remove(s.subscriptionMailPath())
  if errors.Is(err,os.ErrNotExist){err=nil}
  if err==nil{s.subscriptionEmailConfig=subscriptionMailConfig{};s.subscriptionEmailChallenges=nil}
  s.subscriptionEmailMu.Unlock()
  if err!=nil{http.Error(w,"无法清除邮箱恢复配置",500);return}
  writeJSON(w,200,map[string]bool{"ok":true})
 default:http.Error(w,"method not allowed",405)
 }
}

// Standard library SMTP transport; never allow cleartext credentials or
// fallback to plaintext if STARTTLS cannot be negotiated.
func sendSubscriptionSMTP(cfg subscriptionMailConfig, body string)error{
 if err:=validateSubscriptionMail(cfg);err!=nil{return err}
 address:=net.JoinHostPort(cfg.Host,fmt.Sprint(cfg.Port))
 dialer:=&net.Dialer{Timeout:7*time.Second}
 tlsCfg:=&tls.Config{ServerName:cfg.Host,MinVersion:tls.VersionTLS12}
 var conn net.Conn
 var err error
 if cfg.Port==465{
  conn,err=tls.DialWithDialer(dialer,"tcp",address,tlsCfg)
 }else{
  conn,err=dialer.Dial("tcp",address)
 }
 if err!=nil{return errors.New("SMTP 连接失败")}
 defer conn.Close()
 _=conn.SetDeadline(time.Now().Add(8*time.Second))
 client,err:=smtp.NewClient(conn,cfg.Host)
 if err!=nil{return errors.New("SMTP 会话建立失败")}
 defer client.Close()
 if cfg.Port==587{
  ok,_:=client.Extension("STARTTLS")
  if !ok{return errors.New("SMTP 不支持要求的 STARTTLS")}
  if err=client.StartTLS(tlsCfg);err!=nil{return errors.New("SMTP TLS 握手失败")}
 }
 if err=client.Auth(smtp.PlainAuth("",cfg.Username,cfg.Password,cfg.Host));err!=nil{return errors.New("SMTP 授权失败")}
 if err=client.Mail(cfg.From);err!=nil{return errors.New("SMTP 发件地址被拒绝")}
 if err=client.Rcpt(cfg.To);err!=nil{return errors.New("SMTP 收件地址被拒绝")}
 writer,err:=client.Data();if err!=nil{return errors.New("SMTP 邮件写入失败")}
 // Header values have been validated against CRLF injection.
 msg:=fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: MiniProbe email verification\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n",cfg.From,cfg.To,strings.ReplaceAll(body,"\n","\r\n"))
 if _,err=io.WriteString(writer,msg);err!=nil{_ = writer.Close();return errors.New("SMTP 发送失败")}
 if err=writer.Close();err!=nil{return errors.New("SMTP 邮件发送未确认")}
 return nil
}

func (s *server) sendSubscriptionMail(cfg subscriptionMailConfig, body string)error{
 if s.subscriptionEmailSend!=nil{return s.subscriptionEmailSend(cfg,body)}
 return sendSubscriptionSMTP(cfg,body)
}
