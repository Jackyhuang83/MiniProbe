package main

// MiniProbe subscription ledger. It is deliberately independent of miniprobe.json:
// older Server versions cannot silently drop private subscriptions during rollback.
import (
 "crypto/hmac"
 "crypto/sha256"
 "encoding/base64"
 "encoding/binary"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "math"
 "net/http"
 "os"
 "path/filepath"
 "regexp"
 "sort"
 "strconv"
 "strings"
 "time"
 "unicode/utf8"
)

const (
 subscriptionFileLimit = 1 << 20
 subscriptionMaxRecords = 128
 subscriptionCookie = "miniprobe_sub_owner"
 subscriptionSessionTTL = 24 * time.Hour
)
var subscriptionISODate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
var subscriptionCurrency = regexp.MustCompile(`^[A-Z]{3}$`)

type subscriptionRecord struct {
 ID string `json:"id"`
 Name string `json:"name"`
 Category string `json:"category"`
 AmountMinor int64 `json:"amount_minor"`
 Currency string `json:"currency"`
 Cycle string `json:"cycle"`
 CustomDays int `json:"custom_days,omitempty"`
 DueDate string `json:"due_date,omitempty"`
 TimeZone string `json:"time_zone"`
 AutoRenew bool `json:"auto_renew"`
 Status string `json:"status"`
 Tags []string `json:"tags,omitempty"`
 Notes string `json:"notes,omitempty"`
 CreatedAt time.Time `json:"created_at"`
 UpdatedAt time.Time `json:"updated_at"`
}
type subscriptionStore struct {
 Version int `json:"version"`
 OwnerPassword passwordConfig `json:"owner_password"`
 Items []subscriptionRecord `json:"items"`
 Renewals []subscriptionRenewalEvent `json:"renewals,omitempty"`
 RemindersEnabled bool `json:"reminders_enabled,omitempty"`
 ReminderDays []int `json:"reminder_days"`
 ReminderOverrides map[string][]int `json:"reminder_overrides,omitempty"`
 Receipts []subscriptionReminderReceipt `json:"reminder_receipts,omitempty"`
}
type subscriptionView struct {
 subscriptionRecord
 Source string `json:"source"`
 NodeID string `json:"node_id,omitempty"`
}

func validSubscriptionDate(v string) bool {
 if !subscriptionISODate.MatchString(v) { return false }
 t,err:=time.Parse("2006-01-02",v)
 return err==nil && t.Format("2006-01-02")==v
}
func currencyDecimals(currency string) int {
 switch currency {
 case "JPY","KRW","VND","CLP","ISK","PYG","RWF","UGX","XAF","XOF": return 0
 case "BHD","KWD","OMR","JOD","TND": return 3
 default: return 2
 }
}
func subscriptionMinor(amount float64,currency string) int64 {
 if math.IsNaN(amount)||math.IsInf(amount,0)||amount<0||amount>1e9 {return 0}
 return int64(math.Round(amount*math.Pow10(currencyDecimals(currency))))
}
func validateSubscription(rec subscriptionRecord) (subscriptionRecord,error) {
 rec.Name=strings.TrimSpace(rec.Name)
 rec.Currency=strings.ToUpper(strings.TrimSpace(rec.Currency))
 rec.Category=strings.TrimSpace(rec.Category)
 rec.Cycle=strings.TrimSpace(rec.Cycle)
 rec.Status=strings.TrimSpace(rec.Status)
 rec.DueDate=strings.TrimSpace(rec.DueDate)
 rec.TimeZone=strings.TrimSpace(rec.TimeZone)
 rec.Notes=strings.TrimSpace(rec.Notes)
 if !utf8.ValidString(rec.Name)||len(rec.Name)==0||len(rec.Name)>120 {return rec,errors.New("服务名称需要 1–120 字节")}
 switch rec.Category {
 case "video","ai","domain","software","other":
 default:return rec,errors.New("分类无效；VPS 请使用已有节点自动关联")
 }
 if !subscriptionCurrency.MatchString(rec.Currency) {return rec,errors.New("币种必须为三个大写字母")}
 if rec.AmountMinor<0||rec.AmountMinor>1e12 {return rec,errors.New("费用超过限制")}
 switch rec.Cycle {
 case "monthly","quarterly","yearly":
  rec.CustomDays=0
 case "custom":
  if rec.CustomDays<1||rec.CustomDays>3650 {return rec,errors.New("自定义周期只能为 1–3650 天")}
 case "long_term":
  rec.DueDate=""
  rec.CustomDays=0
 default: return rec,errors.New("扣费周期无效")
 }
 if rec.Cycle!="long_term"&&!validSubscriptionDate(rec.DueDate) {return rec,errors.New("请输入 YYYY-MM-DD 格式的到期日期")}
 if rec.TimeZone=="" {rec.TimeZone="Asia/Tokyo"}
 if len(rec.TimeZone)>64 {return rec,errors.New("时区名称过长")}
 if _,err:=time.LoadLocation(rec.TimeZone);err!=nil {return rec,errors.New("时区必须是 IANA 时区")}
 switch rec.Status {
 case "active","paused","cancelled","expired":
 default: return rec,errors.New("订阅状态无效")
 }
 if !utf8.ValidString(rec.Notes)||len(rec.Notes)>1000 {return rec,errors.New("备注过长")}
 if len(rec.Tags)>8 {return rec,errors.New("最多 8 个标签")}
 clean:=make([]string,0,len(rec.Tags))
 for _,tag:=range rec.Tags {
  tag=strings.TrimSpace(tag)
  if !utf8.ValidString(tag)||len(tag)>30{return rec,errors.New("标签过长")}
  if tag!="" {clean=append(clean,tag)}
 }
 rec.Tags=clean
 return rec,nil
}
func (s *server) subscriptionPath() string {return filepath.Join(filepath.Dir(s.dataFile),"subscriptions.json")}

func (s *server) loadSubscriptions() error {
 path:=s.subscriptionPath()
 info,err:=os.Lstat(path)
 if errors.Is(err,os.ErrNotExist) {
  s.subscriptions=subscriptionStore{Version:1,Items:[]subscriptionRecord{}}
  return nil
 }
 if err!=nil{return err}
 if !info.Mode().IsRegular()||info.Mode().Perm()&0077!=0{return errors.New("subscriptions.json 必须为权限 0600 的普通文件")}
 if info.Size()>subscriptionFileLimit{return errors.New("subscriptions.json 超过 1 MiB 限制")}
 b,err:=os.ReadFile(path);if err!=nil{return err}
 var store subscriptionStore
 if err:=json.Unmarshal(b,&store);err!=nil{return fmt.Errorf("订阅文件解析失败: %w",err)}
 if store.Version!=1&&store.Version!=2{return errors.New("订阅文件版本不兼容；拒绝覆盖")}
 // Read v1 safely; the first DEV2 write converts it to v2. Older Server
 // versions must then fail closed instead of stripping unknown history fields.
 if len(store.Items)>subscriptionMaxRecords{return errors.New("订阅数量超过限制")}
 ids:=map[string]bool{}
 for _,item:=range store.Items {
  if item.ID==""||ids[item.ID] {return errors.New("订阅 ID 缺失或重复")}
  ids[item.ID]=true
  if _,err:=validateSubscription(item);err!=nil{return fmt.Errorf("订阅文件无效: %w",err)}
 }
 if err:=validateSubscriptionDev2(store);err!=nil{return fmt.Errorf("订阅 DEV2 数据无效: %w",err)}
 if store.Items==nil{store.Items=[]subscriptionRecord{}}
 s.subscriptions=store
 return nil
}
func (s *server) saveSubscriptionsLocked(store subscriptionStore) error {
 store.Version=2
 if err:=validateSubscriptionDev2(store);err!=nil{return err}
 b,err:=json.MarshalIndent(store,"","  ");if err!=nil{return err}
 if len(b)>subscriptionFileLimit{return errors.New("订阅存储超过 1 MiB 限制")}
 path:=s.subscriptionPath()
 if err:=os.MkdirAll(filepath.Dir(path),0750);err!=nil{return err}
 temp,err:=os.CreateTemp(filepath.Dir(path),".subscriptions-*.tmp")
 if err!=nil{return err}
 tmp:=temp.Name()
 defer os.Remove(tmp)
 if err=temp.Chmod(0600);err==nil{_,err=temp.Write(b)}
 if err==nil{err=temp.Sync()}
 if closeErr:=temp.Close();err==nil{err=closeErr}
 if err!=nil{return err}
 if err:=os.Rename(tmp,path);err!=nil{return err}
 s.subscriptions=store
 return nil
}
func (s *server) subscriptionSnapshot() []subscriptionView {
 s.subscriptionMu.RLock()
 out:=make([]subscriptionView,0,len(s.subscriptions.Items)+16)
 for _,rec:=range s.subscriptions.Items {
  copyRec:=rec
  copyRec.Tags=append([]string(nil),rec.Tags...)
  out=append(out,subscriptionView{subscriptionRecord:copyRec,Source:"manual"})
 }
 s.subscriptionMu.RUnlock()
 s.mu.RLock()
 for id,node:=range s.db.Nodes {
  currency:=strings.ToUpper(strings.TrimSpace(node.Currency))
  if !subscriptionCurrency.MatchString(currency){currency=""}
  due:=node.ExpireAt
  cycle:="monthly"
  if due==longTermExpireDate {cycle="long_term";due=""}
  if !validSubscriptionDate(due){due=""}
  name:=strings.TrimSpace(node.DisplayName)
  if name==""{name=id}
  out=append(out,subscriptionView{subscriptionRecord:subscriptionRecord{
   ID:"vps:"+id,Name:name,Category:"vps",AmountMinor:subscriptionMinor(node.MonthlyPrice,currency),
   Currency:currency,Cycle:cycle,DueDate:due,TimeZone:"Asia/Tokyo",
   Status:"active",CreatedAt:node.CreatedAt,
  },Source:"vps",NodeID:id})
 }
 s.mu.RUnlock()
 sort.Slice(out,func(i,j int)bool{
  a,b:=out[i].DueDate,out[j].DueDate
  if a==""{a="9999-12-31"};if b==""{b="9999-12-31"}
  if a!=b{return a<b}
  return out[i].Name<out[j].Name
 })
 return out
}
func (s *server) localSubscriptions(w http.ResponseWriter,r *http.Request) {
 noStore(w)
 switch r.Method {
 case http.MethodGet:
  writeJSON(w,http.StatusOK,s.subscriptionOwnerData())
 case http.MethodPost,http.MethodPut:
  var rec subscriptionRecord
  if err:=decodeJSON(w,r,&rec);err!=nil{http.Error(w,"JSON 无效",400);return}
  rec,err:=validateSubscription(rec)
  if err!=nil{http.Error(w,err.Error(),400);return}
  now:=time.Now().UTC()
  s.subscriptionMu.Lock();defer s.subscriptionMu.Unlock()
  next:=s.subscriptions
  next.Items=append([]subscriptionRecord(nil),s.subscriptions.Items...)
  if r.Method==http.MethodPost{
   if len(next.Items)>=subscriptionMaxRecords{http.Error(w,"订阅已达到 128 条上限",400);return}
   rec.ID="sub-"+hex.EncodeToString(randomBytes(8))
   rec.CreatedAt=now;rec.UpdatedAt=now
   next.Items=append(next.Items,rec)
  }else{
   if !strings.HasPrefix(rec.ID,"sub-"){http.Error(w,"只能编辑手动订阅",400);return}
   found:=false
   for i:=range next.Items{
    if next.Items[i].ID==rec.ID{
     rec.CreatedAt=next.Items[i].CreatedAt;rec.UpdatedAt=now
     next.Items[i]=rec;found=true;break
    }
   }
   if !found{http.Error(w,"订阅不存在",404);return}
  }
  if err:=s.saveSubscriptionsLocked(next);err!=nil{http.Error(w,"写入订阅失败",500);return}
  writeJSON(w,http.StatusOK,rec)
 case http.MethodDelete:
  id:=r.URL.Query().Get("id")
  if !strings.HasPrefix(id,"sub-"){http.Error(w,"只能删除手动订阅",400);return}
  s.subscriptionMu.Lock();defer s.subscriptionMu.Unlock()
  next:=s.subscriptions
  next.Items=make([]subscriptionRecord,0,len(s.subscriptions.Items))
  for _,rec:=range s.subscriptions.Items{if rec.ID!=id{next.Items=append(next.Items,rec)}}
  if len(next.Items)==len(s.subscriptions.Items){http.Error(w,"订阅不存在",404);return}
  if err:=s.saveSubscriptionsLocked(next);err!=nil{http.Error(w,"写入订阅失败",500);return}
  writeJSON(w,200,map[string]bool{"ok":true})
 default:http.Error(w,"method not allowed",405)
 }
}
func (s *server) localSubscriptionPassword(w http.ResponseWriter,r *http.Request) {
 noStore(w)
 if r.Method!=http.MethodPut{http.Error(w,"method not allowed",405);return}
 var in struct{Password string `json:"password"`}
 if err:=decodeJSON(w,r,&in);err!=nil{http.Error(w,"bad json",400);return}
 if len(in.Password)<12||len(in.Password)>128{http.Error(w,"独立访问密码必须为 12–128 字符",400);return}
 s.subscriptionMu.Lock();defer s.subscriptionMu.Unlock()
 next:=s.subscriptions
 next.OwnerPassword=hashPassword(in.Password)
 if err:=s.saveSubscriptionsLocked(next);err!=nil{http.Error(w,"save failed",500);return}
 writeJSON(w,200,map[string]bool{"ok":true})
}
func (s *server) subscriptionMAC(payload []byte) []byte {
 s.subscriptionMu.RLock()
 hash:=s.subscriptions.OwnerPassword.Hash
 s.subscriptionMu.RUnlock()
 mac:=hmac.New(sha256.New,s.secret)
 mac.Write([]byte("miniprobe-subscriptions-owner-v1\x00"))
 mac.Write([]byte(hash))
 mac.Write([]byte{0})
 mac.Write(payload)
 return mac.Sum(nil)
}
func (s *server) newSubscriptionSession(exp time.Time) string {
 payload:=make([]byte,24)
 binary.BigEndian.PutUint64(payload[:8],uint64(exp.Unix()))
 copy(payload[8:],randomBytes(16))
 return base64.RawURLEncoding.EncodeToString(append(payload,s.subscriptionMAC(payload)...))
}
func (s *server) hasSubscriptionSession(r *http.Request) bool {
 c,err:=r.Cookie(subscriptionCookie);if err!=nil{return false}
 b,err:=base64.RawURLEncoding.DecodeString(c.Value)
 if err!=nil||len(b)!=24+sha256.Size{return false}
 expiry:=int64(binary.BigEndian.Uint64(b[:8]))
 now:=time.Now()
 if expiry<=now.Unix()||expiry>now.Add(subscriptionSessionTTL+time.Minute).Unix(){return false}
 s.subscriptionMu.RLock()
 enabled:=s.subscriptions.OwnerPassword.Hash!=""
 s.subscriptionMu.RUnlock()
 return enabled&&hmac.Equal(b[24:],s.subscriptionMAC(b[:24]))
}
func (s *server) subscriptionSession(w http.ResponseWriter,r *http.Request) {
 noStore(w)
 if r.Method!=http.MethodGet{http.Error(w,"method not allowed",405);return}
 s.subscriptionMu.RLock();enabled:=s.subscriptions.OwnerPassword.Hash!=""
 s.subscriptionMu.RUnlock()
 secure:=requestIsTLS(r)
 authenticated:=secure&&s.hasSubscriptionSession(r)
 state:=map[string]any{"enabled":enabled,"authenticated":authenticated,"secure":secure}
 if authenticated {state["csrf"]=s.subscriptionCSRFToken(r)}
 writeJSON(w,200,state)
}
func (s *server) subscriptionLogin(w http.ResponseWriter,r *http.Request) {
 noStore(w)
 if r.Method!=http.MethodPost{http.Error(w,"method not allowed",405);return}
 if !requestIsTLS(r){http.Error(w,"订阅私有访问要求 HTTPS",403);return}
 if !validSameOriginMutation(r){http.Error(w,"forbidden",403);return}
 ip:=remoteIP(r.RemoteAddr)
 if !s.loginAllowed("subscriptions:"+ip){http.Error(w,"too many attempts",429);return}
 var in struct{Password string `json:"password"`}
 if err:=decodeJSON(w,r,&in);err!=nil{http.Error(w,"bad json",400);return}
 s.subscriptionMu.RLock();pc:=s.subscriptions.OwnerPassword;s.subscriptionMu.RUnlock()
 if pc.Hash==""||!verifyPassword(in.Password,pc){
  s.noteLoginFailure("subscriptions:"+ip)
  http.Error(w,"密码无效",401);return
 }
 s.clearLoginFailures("subscriptions:"+ip)
 exp:=time.Now().Add(subscriptionSessionTTL)
 http.SetCookie(w,&http.Cookie{Name:subscriptionCookie,Value:s.newSubscriptionSession(exp),
  Path:"/api/v1/subscriptions",Expires:exp,MaxAge:int(subscriptionSessionTTL.Seconds()),
  HttpOnly:true,Secure:true,SameSite:http.SameSiteStrictMode})
 writeJSON(w,200,map[string]any{"ok":true})
}
func (s *server) subscriptionLogout(w http.ResponseWriter,r *http.Request) {
 noStore(w)
 if r.Method!=http.MethodPost{http.Error(w,"method not allowed",405);return}
 if !requestIsTLS(r)||!validSameOriginMutation(r){http.Error(w,"forbidden",403);return}
 http.SetCookie(w,&http.Cookie{Name:subscriptionCookie,Path:"/api/v1/subscriptions",MaxAge:-1,HttpOnly:true,Secure:true,SameSite:http.SameSiteStrictMode})
 writeJSON(w,200,map[string]bool{"ok":true})
}
func (s *server) subscriptionRead(w http.ResponseWriter,r *http.Request) {
 noStore(w)
 if r.Method!=http.MethodGet{http.Error(w,"read-only",405);return}
 if !requestIsTLS(r)||!s.hasSubscriptionSession(r){http.Error(w,"unauthorized",401);return}
 data:=s.subscriptionOwnerData()
 data["server_date"]=time.Now().UTC().Format("2006-01-02")
 writeJSON(w,200,data)
}

// parseCurrencyMinor accepts exact decimal user input; floating point is never
// used for prices entered through the SSH menu.
func parseCurrencyMinor(raw,currency string)(int64,error){
 raw=strings.TrimSpace(raw)
 d:=currencyDecimals(currency)
 parts:=strings.Split(raw,".")
 if len(parts)>2||len(parts[0])==0||len(parts[0])>10{return 0,errors.New("金额格式无效")}
 if len(parts)==2&&len(parts[1])>d{return 0,errors.New("小数位数超过币种精度")}
 for _,c:=range strings.ReplaceAll(raw,".",""){if c<'0'||c>'9'{return 0,errors.New("请输入非负十进制金额")}}
 whole,err:=strconv.ParseInt(parts[0],10,64);if err!=nil{return 0,err}
 minor:=whole*int64(math.Pow10(d))
 if len(parts)==2&&parts[1]!=""{
  fractional,err:=strconv.ParseInt(parts[1],10,64);if err!=nil{return 0,err}
  minor+=fractional*int64(math.Pow10(d-len(parts[1])))
 }
 if minor>1e12 {return 0,errors.New("费用超过限制")}
 return minor,nil
}
func formatCurrencyMinor(amount int64,currency string)string{
 d:=currencyDecimals(currency);div:=int64(math.Pow10(d))
 if d==0{return strconv.FormatInt(amount,10)}
 return fmt.Sprintf("%d.%0*d",amount/div,d,amount%div)
}
