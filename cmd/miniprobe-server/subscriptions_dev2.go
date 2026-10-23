package main

// DEV2 extends the independent owner-only subscription ledger. All mutations
// stay on the root-only Unix socket; the public HTTPS endpoint remains read-only.
import (
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "fmt"
 "log"
 "net/http"
 "sort"
 "strings"
 "time"
)

const (
 subscriptionMaxRenewals = 512
 subscriptionMaxReceipts = 1536
 subscriptionMaxOverrides = 128
)
var defaultSubscriptionReminders = []int{30,7,3,1,0,-1}

type subscriptionRenewalEvent struct {
 ID string `json:"id"`
 SubscriptionID string `json:"subscription_id"`
 Date string `json:"date"`
 AmountMinor int64 `json:"amount_minor"`
 Currency string `json:"currency"`
 PreviousDueDate string `json:"previous_due_date,omitempty"`
 NewDueDate string `json:"new_due_date,omitempty"`
 Notes string `json:"notes,omitempty"`
 CreatedAt time.Time `json:"created_at"`
}
type subscriptionReminderReceipt struct {
 Key string `json:"key"`
 Attempts int `json:"attempts"`
 LastAttempt time.Time `json:"last_attempt"`
 DeliveredAt time.Time `json:"delivered_at,omitempty"`
}
type subscriptionReminderConfig struct {
 Enabled bool `json:"enabled"`
 Days []int `json:"days"`
}
type subscriptionRenewalInput struct {
 SubscriptionID string `json:"subscription_id"`
 Date string `json:"date"`
 AmountMinor int64 `json:"amount_minor"`
 Currency string `json:"currency"`
 NewDueDate string `json:"new_due_date,omitempty"`
 Notes string `json:"notes,omitempty"`
}
func subscriptionReminderDays(days []int) []int {
 if days==nil{return append([]int(nil),defaultSubscriptionReminders...)}
 return append([]int{},days...)
}
func validateReminderDays(days []int) error {
 if len(days)>8{return errors.New("最多 8 个提醒阶段")}
 unique:=map[int]bool{}
 for _,day:=range days{
  if day!= -1 && (day<0||day>90){return errors.New("提醒天数仅支持 0–90 和 -1（逾期）")}
  if unique[day]{return errors.New("提醒天数不可重复")}
  unique[day]=true
 }
 return nil
}
func validateSubscriptionDev2(store subscriptionStore) error {
 if err:=validateReminderDays(subscriptionReminderDays(store.ReminderDays));err!=nil{return err}
 if len(store.Renewals)>subscriptionMaxRenewals || len(store.Receipts)>subscriptionMaxReceipts || len(store.ReminderOverrides)>subscriptionMaxOverrides {
  return errors.New("续费历史或提醒记录超过容量限制")
 }
 for id,days:=range store.ReminderOverrides {
  if id==""{return errors.New("提醒覆写 ID 不能为空")}
  if err:=validateReminderDays(days);err!=nil{return err}
 }
 ids:=map[string]bool{}
 for _,e:=range store.Renewals{
  if e.ID==""||ids[e.ID]||e.SubscriptionID==""||!validSubscriptionDate(e.Date)||!subscriptionCurrency.MatchString(e.Currency)||e.AmountMinor<0||e.AmountMinor>1e12||len(e.Notes)>240||(!e.NewDueDateIsValid()){
   return errors.New("历史续费记录无效")
  }
  ids[e.ID]=true
 }
 keys:=map[string]bool{}
 for _,r:=range store.Receipts{
  if len(r.Key)!=64||keys[r.Key]||r.Attempts<1||r.Attempts>5||r.LastAttempt.IsZero(){return errors.New("提醒回执记录无效")}
  keys[r.Key]=true
 }
 return nil
}
func (e subscriptionRenewalEvent) NewDueDateIsValid()bool{
 return (e.PreviousDueDate==""||validSubscriptionDate(e.PreviousDueDate))&&(e.NewDueDate==""||validSubscriptionDate(e.NewDueDate))
}
func (s *server) subscriptionOwnerData() map[string]any {
 s.subscriptionMu.RLock()
 renewals:=append([]subscriptionRenewalEvent(nil),s.subscriptions.Renewals...)
 cfg:=subscriptionReminderConfig{Enabled:s.subscriptions.RemindersEnabled,Days:subscriptionReminderDays(s.subscriptions.ReminderDays)}
 s.subscriptionMu.RUnlock()
 // Ordering is deterministic and does not alter stored immutable entries.
 sort.Slice(renewals,func(i,j int)bool{if renewals[i].Date==renewals[j].Date{return renewals[i].ID>renewals[j].ID};return renewals[i].Date>renewals[j].Date})
 return map[string]any{"items":s.subscriptionSnapshot(),"renewals":renewals,"reminders":cfg}
}
func subscriptionViewForID(items []subscriptionView,id string)(subscriptionView,bool){
 for _,item:=range items{if item.ID==id{return item,true}}
 return subscriptionView{},false
}
func (s *server) localSubscriptionRenewals(w http.ResponseWriter,r *http.Request) {
 noStore(w)
 if r.Method!=http.MethodPost{http.Error(w,"method not allowed",405);return}
 var in subscriptionRenewalInput
 if err:=decodeJSON(w,r,&in);err!=nil{http.Error(w,"bad json",400);return}
 in.SubscriptionID=strings.TrimSpace(in.SubscriptionID)
 in.Currency=strings.ToUpper(strings.TrimSpace(in.Currency))
 in.Notes=strings.TrimSpace(in.Notes)
 if !validSubscriptionDate(in.Date)||in.AmountMinor<0||in.AmountMinor>1e12||!subscriptionCurrency.MatchString(in.Currency)||len(in.Notes)>240||in.SubscriptionID==""{
  http.Error(w,"续费日期、金额、币种或备注无效",400);return
 }
 if in.NewDueDate!=""&&!validSubscriptionDate(in.NewDueDate){http.Error(w,"新到期日无效",400);return}
 item,ok:=subscriptionViewForID(s.subscriptionSnapshot(),in.SubscriptionID)
 if !ok{http.Error(w,"订阅不存在",404);return}
 if in.NewDueDate!=""&&(item.Source!="manual"||item.Cycle=="long_term"){
  http.Error(w,"VPS 到期日必须从节点菜单修改；长期订阅不支持设置到期日",400);return
 }
 now:=time.Now().UTC()
 event:=subscriptionRenewalEvent{ID:"renew-"+hex.EncodeToString(randomBytes(8)),SubscriptionID:in.SubscriptionID,Date:in.Date,
 AmountMinor:in.AmountMinor,Currency:in.Currency,PreviousDueDate:item.DueDate,NewDueDate:in.NewDueDate,Notes:in.Notes,CreatedAt:now}
 s.subscriptionMu.Lock();defer s.subscriptionMu.Unlock()
 next:=s.subscriptions
 if len(next.Renewals)>=subscriptionMaxRenewals{http.Error(w,"续费历史已达到 512 条上限",409);return}
 next.Renewals=append(append([]subscriptionRenewalEvent(nil),next.Renewals...),event)
 if in.NewDueDate!=""{
  next.Items=append([]subscriptionRecord(nil),next.Items...)
  found:=false
  for i:=range next.Items{
   if next.Items[i].ID!=in.SubscriptionID{continue}
   next.Items[i].DueDate=in.NewDueDate
   next.Items[i].Status="active"
   next.Items[i].UpdatedAt=now
   found=true
   break
  }
  if !found{http.Error(w,"订阅已被修改，请重试",409);return}
 }else if item.Source=="manual"{
  found:=false
  for _,rec:=range next.Items{if rec.ID==in.SubscriptionID{found=true;break}}
  if !found{http.Error(w,"订阅已被删除",409);return}
 }
 if err:=s.saveSubscriptionsLocked(next);err!=nil{http.Error(w,"保存失败",500);return}
 writeJSON(w,201,event)
}
func (s *server) localSubscriptionReminders(w http.ResponseWriter,r *http.Request){
 noStore(w)
 if r.Method!=http.MethodPut{http.Error(w,"method not allowed",405);return}
 var in subscriptionReminderConfig
 if err:=decodeJSON(w,r,&in);err!=nil{http.Error(w,"bad json",400);return}
 if err:=validateReminderDays(in.Days);err!=nil{http.Error(w,err.Error(),400);return}
 s.subscriptionMu.Lock();defer s.subscriptionMu.Unlock()
 next:=s.subscriptions
 next.RemindersEnabled=in.Enabled
 next.ReminderDays=append([]int{},in.Days...)
 if err:=s.saveSubscriptionsLocked(next);err!=nil{http.Error(w,"save failed",500);return}
 writeJSON(w,200,map[string]bool{"ok":true})
}
func (s *server) localSubscriptionOverride(w http.ResponseWriter,r *http.Request){
 noStore(w)
 if r.Method!=http.MethodPut{http.Error(w,"method not allowed",405);return}
 var in struct{SubscriptionID string `json:"subscription_id"`;Mode string `json:"mode"`;Days []int `json:"days"`}
 if err:=decodeJSON(w,r,&in);err!=nil{http.Error(w,"bad json",400);return}
 if _,ok:=subscriptionViewForID(s.subscriptionSnapshot(),in.SubscriptionID);!ok{http.Error(w,"订阅不存在",404);return}
 if in.Mode!="inherit"&&in.Mode!="off"&&in.Mode!="custom"{http.Error(w,"invalid reminder mode",400);return}
 if in.Mode=="custom"{
  if len(in.Days)==0{http.Error(w,"自定义提醒至少需要一个阶段",400);return}
  if err:=validateReminderDays(in.Days);err!=nil{http.Error(w,err.Error(),400);return}
 }
 s.subscriptionMu.Lock();defer s.subscriptionMu.Unlock()
 next:=s.subscriptions
 next.ReminderOverrides=make(map[string][]int,len(s.subscriptions.ReminderOverrides)+1)
 for id,days:=range s.subscriptions.ReminderOverrides{next.ReminderOverrides[id]=append([]int{},days...)}
 switch in.Mode{
 case "inherit":delete(next.ReminderOverrides,in.SubscriptionID)
 case "off":next.ReminderOverrides[in.SubscriptionID]=[]int{}
 case "custom":next.ReminderOverrides[in.SubscriptionID]=append([]int{},in.Days...)
 }
 if len(next.ReminderOverrides)>subscriptionMaxOverrides{http.Error(w,"单条提醒配置超过限制",409);return}
 if err:=s.saveSubscriptionsLocked(next);err!=nil{http.Error(w,"save failed",500);return}
 writeJSON(w,200,map[string]bool{"ok":true})
}
func subscriptionCalendarDays(now time.Time,due,zone string)(int,bool){
 if !validSubscriptionDate(due){return 0,false}
 loc,err:=time.LoadLocation(zone)
 if err!=nil{return 0,false}
 day:=now.In(loc)
 t,err:=time.Parse("2006-01-02",due);if err!=nil{return 0,false}
 // Use UTC civil dates rather than wall-clock intervals: DST does not
 // change the number of calendar days before expiry.
 todayUTC:=time.Date(day.Year(),day.Month(),day.Day(),0,0,0,0,time.UTC)
 targetUTC:=time.Date(t.Year(),t.Month(),t.Day(),0,0,0,0,time.UTC)
 return int(targetUTC.Sub(todayUTC).Hours()/24),true
}
func selectSubscriptionReminderStage(remaining int,days []int)(int,bool){
 if remaining<0{
  if remaining< -30{return 0,false}
  for _,day:=range days{if day==-1{return -1,true}}
  return 0,false
 }
 stage:=1000000
 for _,day:=range days{if day>=remaining&&day>=0&&day<stage{stage=day}}
 if stage==1000000{return 0,false}
 return stage,true
}
func subscriptionReceiptKey(id,due string,stage int)string{
 hash:=sha256.Sum256([]byte(id+" "+due+" "+fmt.Sprint(stage)))
 return hex.EncodeToString(hash[:])
}
func subscriptionReminderMessage(item subscriptionView,remaining int)string{
 name:=strings.NewReplacer("
"," ",""," ","	"," ").Replace(item.Name)
 status:=fmt.Sprintf("剩余 %d 天",remaining)
 if remaining==0{status="今天到期"}
 if remaining<0{status=fmt.Sprintf("逾期 %d 天",-remaining)}
 return fmt.Sprintf("📅 MiniProbe 订阅续费提醒

服务：%s
到期日：%s
状态：%s

请自行核对续费情况，本通知不会自动付款。",name,item.DueDate,status)
}
func retryDelay(attempts int)time.Duration{
 if attempts<1{return 0}
 // Hourly base with exponential spacing, capped at eight hours.
 hours:=1<<(attempts-1)
 if hours>8{hours=8}
 return time.Duration(hours)*time.Hour
}
func (s *server) subscriptionReminderLoop(){
 s.checkSubscriptionReminders(time.Now(),s.sendTelegram)
 ticker:=time.NewTicker(time.Hour)
 defer ticker.Stop()
 for now:=range ticker.C{s.checkSubscriptionReminders(now,s.sendTelegram)}
}
func (s *server) checkSubscriptionReminders(now time.Time,send func(string)error){
 if err:=s.processSubscriptionReminders(now,send);err!=nil{
  // Do not log subscription titles, costs or credentials.
  log.Printf("subscription reminders: check failed (private store or delivery)")
 }
}
func (s *server) processSubscriptionReminders(now time.Time,send func(string)error)error{
 s.subscriptionReminderRunMu.Lock();defer s.subscriptionReminderRunMu.Unlock()
 s.mu.RLock()
 tg:=s.db.Settings.Telegram
 s.mu.RUnlock()
 if !tg.Enabled||tg.BotToken==""||tg.ChatID==""{return nil}
 s.subscriptionMu.RLock()
 enabled:=s.subscriptions.RemindersEnabled
 globalDays:=subscriptionReminderDays(s.subscriptions.ReminderDays)
 overrides:=make(map[string][]int,len(s.subscriptions.ReminderOverrides))
 for id,v:=range s.subscriptions.ReminderOverrides{overrides[id]=append([]int{},v...)}
 s.subscriptionMu.RUnlock()
 if !enabled{return nil}
 items:=s.subscriptionSnapshot()
 for _,item:=range items{
  if item.Status!="active"||item.DueDate==""||item.Cycle=="long_term"{continue}
  remaining,ok:=subscriptionCalendarDays(now,item.DueDate,item.TimeZone)
  if !ok{continue}
  days:=globalDays
  if value,ok:=overrides[item.ID];ok{days=value}
  stage,ok:=selectSubscriptionReminderStage(remaining,days)
  if !ok{continue}
  key:=subscriptionReceiptKey(item.ID,item.DueDate,stage)
  s.subscriptionMu.Lock()
  next:=s.subscriptions
  index:=-1
  for i,receipt:=range next.Receipts{if receipt.Key==key{index=i;break}}
  if index>=0{
   receipt:=next.Receipts[index]
   if !receipt.DeliveredAt.IsZero()||receipt.Attempts>=5||now.Sub(receipt.LastAttempt)<retryDelay(receipt.Attempts){
    s.subscriptionMu.Unlock();continue
   }
  }
  next.Receipts=append([]subscriptionReminderReceipt(nil),next.Receipts...)
  if index==-1{
   // Old receipts past retention no longer describe a current renewal window.
   kept:=next.Receipts[:0]
   for _,r:=range next.Receipts{
    if r.LastAttempt.After(now.Add(-180*24*time.Hour)){kept=append(kept,r)}
   }
   next.Receipts=kept
   if len(next.Receipts)>=subscriptionMaxReceipts{s.subscriptionMu.Unlock();return errors.New("reminder receipts full")}
   index=len(next.Receipts)
   next.Receipts=append(next.Receipts,subscriptionReminderReceipt{Key:key})
  }
  next.Receipts[index].Attempts++
  next.Receipts[index].LastAttempt=now.UTC()
  if err:=s.saveSubscriptionsLocked(next);err!=nil{s.subscriptionMu.Unlock();return err}
  s.subscriptionMu.Unlock()
  if err:=send(subscriptionReminderMessage(item,remaining));err!=nil{continue}
  s.subscriptionMu.Lock()
  next=s.subscriptions
  next.Receipts=append([]subscriptionReminderReceipt(nil),next.Receipts...)
  for i:=range next.Receipts{
   if next.Receipts[i].Key==key{next.Receipts[i].DeliveredAt=now.UTC();break}
  }
  err:=s.saveSubscriptionsLocked(next)
  s.subscriptionMu.Unlock()
  if err!=nil{return err}
 }
 return nil
}
