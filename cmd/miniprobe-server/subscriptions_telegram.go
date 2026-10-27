package main

// Telegram subscription reminder buttons. All callback operations are read-only,
// are restricted to the owner's configured private Telegram chat, and never
// execute payments or expose the private subscription API on a new route.
import (
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "regexp"
 "strconv"
 "strings"
 "time"
)

type telegramInlineButton struct {
 Text string `json:"text"`
 URL string `json:"url,omitempty"`
 CallbackData string `json:"callback_data,omitempty"`
}
type telegramInlineMarkup struct {
 InlineKeyboard [][]telegramInlineButton `json:"inline_keyboard"`
}
type telegramUpdate struct {
 UpdateID int64 `json:"update_id"`
 CallbackQuery struct {
  ID string `json:"id"`
  From struct { ID int64 `json:"id"` } `json:"from"`
  Message struct {
   MessageID int64 `json:"message_id"`
   Chat struct {ID int64 `json:"id"`} `json:"chat"`
  } `json:"message"`
  Data string `json:"data"`
 } `json:"callback_query"`
}
var telegramSubscriptionIDRE=regexp.MustCompile(`^(?:sub-[0-9a-f]{16}|vps:node-[0-9a-f]{10})$`)

func telegramSubscriptionCallback(action,id string) string {
 if !telegramSubscriptionIDRE.MatchString(id) {return ""}
 if action!="refresh"&&action!="list" {return ""}
 // Telegram limits callback_data to 64 UTF-8 bytes.
 value:="mp1:"+action+":"+id
 if len(value)>64{return ""}
 return value
}
func parseTelegramSubscriptionCallback(value string)(action,id string,ok bool){
 parts:=strings.SplitN(value,":",3)
 if len(parts)!=3||parts[0]!="mp1"||!telegramSubscriptionIDRE.MatchString(parts[2]){return "","",false}
 if parts[1]!="refresh"&&parts[1]!="list"{return "","",false}
 return parts[1],parts[2],true
}
func configuredPrivateTelegramChat(chatID string)int64{
 id,err:=strconv.ParseInt(strings.TrimSpace(chatID),10,64)
 if err!=nil||id<=0{return 0}
 return id
}
// The subscription page is owner-authenticated, regardless of whether the
// hostname is behind Cloudflare Access or the standard private owner login.
func subscriptionTelegramURL(access subscriptionAccessConfig,publicURL string)string{
 if access.Enabled&&validateSubscriptionAccess(access)==nil {
  return "https://"+access.Hostname+"/subscriptions.html"
 }
 u,err:=url.Parse(publicURL)
 if err!=nil||u.Scheme!="https"||u.Hostname()==""||u.User!=nil||u.RawQuery!=""||u.Fragment!="" {return ""}
 // Never trust arbitrary URL paths, userinfo, or Telegram callback input.
 if strings.ContainsAny(u.Host,"\r\n\\") {return ""}
 return "https://"+u.Host+"/subscriptions.html"
}
func subscriptionTelegramKeyboard(item subscriptionView,access subscriptionAccessConfig,publicURL string,interactive bool)[][]telegramInlineButton{
 url:=subscriptionTelegramURL(access,publicURL)
 var out [][]telegramInlineButton
 if url!="" {
  out=append(out,[]telegramInlineButton{{Text:"💳 续费 / 查看订阅",URL:url}})
 }
 if interactive {
  refresh:=telegramSubscriptionCallback("refresh",item.ID)
  list:=telegramSubscriptionCallback("list",item.ID)
  if refresh!=""&&list!="" {
   out=append(out,[]telegramInlineButton{
    {Text:"🔄 刷新状态",CallbackData:refresh},
    {Text:"≪ 订阅列表",CallbackData:list},
   })
  }
 }
 return out
}
func subscriptionTelegramMessage(item subscriptionView,remaining int)string {
 name:=strings.NewReplacer("\n"," ","\r"," ","\t"," ").Replace(item.Name)
 name=strings.TrimSpace(name)
 if name=="" {name="未命名订阅"}
 if len([]rune(name))>100 {name=string([]rune(name)[:100])+"…"}
 kind:=map[string]string{"vps":"VPS","video":"影视 / IPTV","ai":"AI 服务","domain":"域名 / 云服务","software":"软件 / App","other":"其他订阅"}[item.Category]
 if kind==""{kind="其他订阅"}
 billing:=map[string]string{"monthly":"月","quarterly":"季","yearly":"年","custom":"自定义周期"}[item.Cycle]
 if item.Cycle=="custom"&&item.CustomDays>0 {billing=fmt.Sprintf("%d 天",item.CustomDays)}
 if billing==""{billing="周期未设置"}
 cost:="未设置"
 if item.Currency!=""&&item.AmountMinor>=0 {
  cost=fmt.Sprintf("%s %s / %s",formatCurrencyMinor(item.AmountMinor,item.Currency),item.Currency,billing)
 }
 due:="未设置"
 if item.DueDate!="" {due=item.DueDate}
 remain:=fmt.Sprintf("剩余 %d 天",remaining)
 if remaining==0 {remain="今天到期"}
 if remaining<0 {remain=fmt.Sprintf("逾期 %d 天",-remaining)}
 return fmt.Sprintf("📅 MiniProbe · 订阅续费提醒\n\n📦 产品：%s\n📂 类型：%s\n💰 续费：%s\n🗓 到期：%s\n⏳ 状态：%s\n\n🔒 仅供提醒；不会自动扣款或修改服务商订单。",name,kind,cost,due,remain)
}
func subscriptionTelegramListMessage(items []subscriptionView,now time.Time)string{
 lines:=[]string{"📋 MiniProbe · 私人订阅列表",""}
 shown:=0
 for _,item:=range items {
  if item.Status!="active"||item.DueDate==""||item.Cycle=="long_term"{continue}
  remaining,ok:=subscriptionCalendarDays(now,item.DueDate,item.TimeZone)
  if !ok{continue}
  name:=strings.NewReplacer("\n"," ","\r"," ","\t"," ").Replace(item.Name)
  name=strings.TrimSpace(name)
  if len([]rune(name))>48{name=string([]rune(name)[:48])+"…"}
  state:=fmt.Sprintf("%d 天",remaining)
  if remaining==0{state="今天"}
  if remaining<0{state=fmt.Sprintf("逾期 %d 天",-remaining)}
  lines=append(lines,fmt.Sprintf("%d. %s · %s · %s",shown+1,name,item.DueDate,state))
  shown++
  if shown>=10{break}
 }
 if shown==0{lines=append(lines,"没有设置到期日的有效订阅。")}
 lines=append(lines,"","🔒 只读列表。修改或续费请在 HTTPS 私人订阅中心完成。")
 return strings.Join(lines,"\n")
}
// Telegram transport is deliberately fixed to the official Bot API. No
// user-supplied URL or payment endpoint is accepted here.
func telegramBotPOST(token,method string,fields url.Values,output any)error{
 if token==""{return errors.New("Telegram token missing")}
 target:="https://api.telegram.org/bot"+token+"/"+method
 request,err:=http.NewRequest(http.MethodPost,target,strings.NewReader(fields.Encode()))
 if err!=nil{return err}
 request.Header.Set("Content-Type","application/x-www-form-urlencoded")
 client:=&http.Client{Timeout:17*time.Second}
 response,err:=client.Do(request)
 if err!=nil{return errors.New("Telegram API connection failed")}
 defer response.Body.Close()
 if response.StatusCode/100!=2{return fmt.Errorf("Telegram API HTTP %d",response.StatusCode)}
 if output!=nil {
  body,err:=io.ReadAll(io.LimitReader(response.Body,1<<17))
  if err!=nil{return errors.New("Telegram response read failed")}
  if err:=json.Unmarshal(body,output);err!=nil{return errors.New("Telegram response invalid")}
 }
 return nil
}
func telegramMarkupJSON(rows [][]telegramInlineButton)string{
 if len(rows)==0{return ""}
 value,err:=json.Marshal(telegramInlineMarkup{InlineKeyboard:rows})
 if err!=nil{return ""}
 return string(value)
}
func (s *server) sendSubscriptionTelegramReminder(item subscriptionView,remaining int)error{
 s.mu.RLock()
 config:=s.db.Settings.Telegram
 publicURL:=s.db.Settings.PublicURL
 s.mu.RUnlock()
 s.subscriptionAccessMu.RLock()
 access:=s.subscriptionAccess
 s.subscriptionAccessMu.RUnlock()
 if !config.Enabled||config.BotToken==""||config.ChatID==""{return errors.New("Telegram notifications disabled")}
 msg:=subscriptionTelegramMessage(item,remaining)
 fields:=url.Values{"chat_id":{config.ChatID},"text":{msg},"disable_web_page_preview":{"true"}}
 rows:=subscriptionTelegramKeyboard(item,access,publicURL,configuredPrivateTelegramChat(config.ChatID)!=0)
 if markup:=telegramMarkupJSON(rows);markup!=""{fields.Set("reply_markup",markup)}
 return telegramBotPOST(config.BotToken,"sendMessage",fields,nil)
}
func subscriptionTelegramCallbackAuthorized(u telegramUpdate,ownerChat int64)bool{
 return ownerChat>0&&u.CallbackQuery.ID!=""&&u.CallbackQuery.From.ID==ownerChat&&u.CallbackQuery.Message.Chat.ID==ownerChat&&u.CallbackQuery.Message.MessageID>0
}
func (s *server) handleSubscriptionTelegramCallback(u telegramUpdate,token string,ownerChat int64){
 if u.CallbackQuery.ID==""{return}
 answer:=url.Values{"callback_query_id":{u.CallbackQuery.ID}}
 respond:=func(message string) {
  if message!=""{answer.Set("text",message)}
  _=telegramBotPOST(token,"answerCallbackQuery",answer,nil)
 }
 if !subscriptionTelegramCallbackAuthorized(u,ownerChat){respond("仅私人 Telegram 账号可操作");return}
 action,id,ok:=parseTelegramSubscriptionCallback(u.CallbackQuery.Data)
 if !ok{respond("操作无效");return}
 s.subscriptionMu.RLock()
 enabled:=s.subscriptions.RemindersEnabled
 s.subscriptionMu.RUnlock()
 if !enabled{respond("订阅提醒已关闭");return}
 items:=s.subscriptionSnapshot()
 item,found:=subscriptionViewForID(items,id)
 if !found {respond("订阅已删除或不存在");return}
 s.mu.RLock()
 publicURL:=s.db.Settings.PublicURL
 s.mu.RUnlock()
 s.subscriptionAccessMu.RLock()
 access:=s.subscriptionAccess
 s.subscriptionAccessMu.RUnlock()
 var msg string
 if action=="refresh" {
  if item.Status!="active"||item.DueDate==""||item.Cycle=="long_term"{respond("记录无有效到期日");return}
  remaining,ok:=subscriptionCalendarDays(time.Now(),item.DueDate,item.TimeZone)
  if !ok{respond("到期日期无效");return}
  msg=subscriptionTelegramMessage(item,remaining)
 }else {
  msg=subscriptionTelegramListMessage(items,time.Now())
 }
 buttons:=subscriptionTelegramKeyboard(item,access,publicURL,true)
 fields:=url.Values{"chat_id":{strconv.FormatInt(ownerChat,10)},
  "message_id":{strconv.FormatInt(u.CallbackQuery.Message.MessageID,10)},
  "text":{msg},"disable_web_page_preview":{"true"}}
 if markup:=telegramMarkupJSON(buttons);markup!=""{fields.Set("reply_markup",markup)}
 if err:=telegramBotPOST(token,"editMessageText",fields,nil);err!=nil{
  // A no-op edit is normal if the message was already current.
  respond("状态无变化或暂时无法更新")
  return
 }
 respond("已从 MiniProbe Server 更新")
}
func (s *server) subscriptionTelegramCallbackLoop(){
 // This bot feature is optional. Only a private numeric Chat ID can use
 // callback buttons; public/group chats never receive working callbacks.
 var offset int64
 previousToken:=""
 for {
  s.mu.RLock()
  cfg:=s.db.Settings.Telegram
  s.mu.RUnlock()
  s.subscriptionMu.RLock()
  reminders:=s.subscriptions.RemindersEnabled
  s.subscriptionMu.RUnlock()
  ownerChat:=configuredPrivateTelegramChat(cfg.ChatID)
  if !cfg.Enabled||!reminders||cfg.BotToken==""||ownerChat==0{
   time.Sleep(15*time.Second)
   continue
  }
  if cfg.BotToken!=previousToken{offset=0;previousToken=cfg.BotToken}
  fields:=url.Values{"timeout":{"8"},"limit":{"25"},"allowed_updates":{`["callback_query"]`}}
  if offset>0{fields.Set("offset",strconv.FormatInt(offset,10))}
  var result struct {
   OK bool `json:"ok"`
   Result []telegramUpdate `json:"result"`
  }
  if err:=telegramBotPOST(cfg.BotToken,"getUpdates",fields,&result);err!=nil||!result.OK{
   // Do not log bot credentials or private Telegram payloads. A webhook or
   // another getUpdates consumer can conflict; alerts still send normally.
   time.Sleep(20*time.Second)
   continue
  }
  for _,update:=range result.Result{
   if update.UpdateID>=offset{offset=update.UpdateID+1}
   if update.CallbackQuery.ID!=""{s.handleSubscriptionTelegramCallback(update,cfg.BotToken,ownerChat)}
  }
  time.Sleep(time.Second)
 }
}
