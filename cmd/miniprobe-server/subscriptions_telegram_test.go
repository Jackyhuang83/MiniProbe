package main

import (
 "strings"
 "testing"
 "time"
)

func TestTelegramSubscriptionMessageContainsAccurateCycleAndCurrency(t *testing.T){
 cases:=[]struct{
  rec subscriptionView
  remaining int
  parts []string
 }{
  {subscriptionView{subscriptionRecord:subscriptionRecord{
   ID:"vps:node-0123456789",Name:"HK 迷你 VPS",Category:"vps",
   Currency:"USD",AmountMinor:500,Cycle:"yearly",DueDate:"2027-10-23",
  },Source:"vps"},7,[]string{"HK 迷你 VPS","VPS","5.00 USD / 年","2027-10-23","剩余 7 天","不会自动扣款"}},
  {subscriptionView{subscriptionRecord:subscriptionRecord{
   ID:"sub-0123456789abcdef",Name:"音乐订阅",Category:"software",
   Currency:"JPY",AmountMinor:2400,Cycle:"monthly",DueDate:"2027-10-23",
  },Source:"manual"},0,[]string{"2400 JPY / 月","今天到期"}},
  {subscriptionView{subscriptionRecord:subscriptionRecord{
   ID:"sub-0123456789abcdef",Name:"影视",Category:"video",
   Currency:"KWD",AmountMinor:1250,Cycle:"quarterly",DueDate:"2027-10-23",
  },Source:"manual"},-1,[]string{"1.250 KWD / 季","逾期 1 天"}},
 }
 for _,tc:=range cases{
  got:=subscriptionTelegramMessage(tc.rec,tc.remaining)
  for _,part:=range tc.parts{if !strings.Contains(got,part){t.Errorf("reminder %s missing %q: %s",tc.rec.ID,part,got)}}
 }
}
func TestTelegramSubscriptionURLPrefersCloudflareAccess(t *testing.T){
 access:=subscriptionAccessConfig{Enabled:true,Hostname:"subs.example.com",Team:"my-team",Audience:"0123456789abcdef0123456789abcdef0123456789abcdef",Email:"owner@example.com"}
 if got:=subscriptionTelegramURL(access,"https://probe.example.com");got!="https://subs.example.com/subscriptions.html"{t.Fatalf("Access hostname ignored: %q",got)}
 if got:=subscriptionTelegramURL(subscriptionAccessConfig{},"https://probe.example.com/path");got!="https://probe.example.com/subscriptions.html"{t.Fatalf("unexpected fallback: %q",got)}
 if got:=subscriptionTelegramURL(subscriptionAccessConfig{},"https://probe.example.com/path?secret=never");got!=""{t.Fatalf("URL with unexpected query must be rejected: %q",got)}
 for _,raw:=range []string{"http://probe.example.com","https://evil@example.com","javascript:alert(1)","https://user:pass@example.com","https://example.com\\@evil.invalid"}{
  if got:=subscriptionTelegramURL(subscriptionAccessConfig{},raw);got!=""{t.Errorf("unsafe public URL allowed %q -> %q",raw,got)}
 }
}
func TestTelegramInlineButtonsAndCallbacks(t *testing.T){
 item:=subscriptionView{subscriptionRecord:subscriptionRecord{ID:"sub-0123456789abcdef",Name:"家庭订阅"}}
 buttons:=subscriptionTelegramKeyboard(item,subscriptionAccessConfig{},"https://probe.example.com",true)
 if len(buttons)!=2||len(buttons[0])!=1||len(buttons[1])!=2 {t.Fatalf("expected full three-button menu: %+v",buttons)}
 if buttons[0][0].URL!="https://probe.example.com/subscriptions.html" {t.Fatal("URL must use protected subscriptions page")}
 if buttons[1][0].CallbackData!="mp1:refresh:sub-0123456789abcdef"{t.Fatalf("bad refresh callback: %+v",buttons)}
 if buttons[1][1].CallbackData!="mp1:list:sub-0123456789abcdef"{t.Fatalf("bad list callback: %+v",buttons)}
 if _,_,ok:=parseTelegramSubscriptionCallback("mp1:delete:sub-0123456789abcdef");ok{t.Fatal("write callback action accepted")}
 if _,_,ok:=parseTelegramSubscriptionCallback("mp1:refresh:../../secret");ok{t.Fatal("invalid callback ID accepted")}
 if _,_,ok:=parseTelegramSubscriptionCallback("mp1:list:sub-0123456789abcdef:extra");ok{t.Fatal("overlong ID accepted")}
 if v:=telegramSubscriptionCallback("refresh","vps:node-0123456789");v==""||len(v)>64{t.Fatalf("VPS callback invalid: %q",v)}
 if s:=telegramMarkupJSON(buttons);!strings.Contains(s,"inline_keyboard")||!strings.Contains(s,"callback_data"){t.Fatal("Telegram payload missing native inline buttons")}
 onlyWeb:=subscriptionTelegramKeyboard(item,subscriptionAccessConfig{},"https://probe.example.com",false)
 if len(onlyWeb)!=1||len(onlyWeb[0])!=1{t.Fatal("non-private chat must never get active callback controls")}
}
func TestTelegramCallbacksArePrivateChatOnly(t *testing.T){
 var event telegramUpdate
 event.CallbackQuery.ID="callback-1"
 event.CallbackQuery.From.ID=12345
 event.CallbackQuery.Message.Chat.ID=12345
 event.CallbackQuery.Message.MessageID=11
 event.CallbackQuery.Data="mp1:refresh:sub-0123456789abcdef"
 if !subscriptionTelegramCallbackAuthorized(event,12345){t.Fatal("owner's private callback rejected")}
 if subscriptionTelegramCallbackAuthorized(event,0){t.Fatal("non-private chats must never authorize callbacks")}
 event.CallbackQuery.From.ID=99999
 if subscriptionTelegramCallbackAuthorized(event,12345){t.Fatal("forwarded message from another user could read private list")}
 event.CallbackQuery.From.ID=12345
 event.CallbackQuery.Message.Chat.ID=-12345
 if subscriptionTelegramCallbackAuthorized(event,12345){t.Fatal("group chat callback could read private list")}
 event.CallbackQuery.Message.Chat.ID=12345
 event.CallbackQuery.Message.MessageID=0
 if subscriptionTelegramCallbackAuthorized(event,12345){t.Fatal("inline message without private chat context accepted")}
 if configuredPrivateTelegramChat("-100123456")!=0||configuredPrivateTelegramChat("not-a-number")!=0{t.Fatal("non-private TG chat IDs must be excluded")}
}
func TestTelegramSubscriptionListIsReadOnlyAndLimited(t *testing.T){
 items:=[]subscriptionView{
  {subscriptionRecord:subscriptionRecord{ID:"sub-1111111111111111",Name:"中文年付VPS",Category:"vps",Cycle:"yearly",DueDate:"2030-12-20",TimeZone:"Asia/Tokyo",Status:"active"}},
  {subscriptionRecord:subscriptionRecord{ID:"sub-2222222222222222",Name:"已停用",Cycle:"monthly",DueDate:"2030-11-20",TimeZone:"Asia/Tokyo",Status:"paused"}},
  {subscriptionRecord:subscriptionRecord{ID:"sub-3333333333333333",Name:"秘密 Token abc",Cycle:"long_term",Status:"active"}},
 }
 now:=time.Date(2030,12,10,4,0,0,0,time.UTC)
 message:=subscriptionTelegramListMessage(items,now)
 if !strings.Contains(message,"中文年付VPS")||!strings.Contains(message,"2030-12-20"){t.Fatalf("list missing active subscription: %q",message)}
 if strings.Contains(message,"已停用")||strings.Contains(message,"秘密 Token abc"){t.Fatal("inactive or no-due subscriptions included")}
 if !strings.Contains(message,"只读列表"){t.Fatal("list must be clearly read-only")}
}
