package main

import (
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "os"
 "strings"
 "testing"
 "time"
)

func dev2WriteJSON(t *testing.T,s *server,handler func(http.ResponseWriter,*http.Request),verb,path string,payload any) *httptest.ResponseRecorder{
 t.Helper()
 data,err:=json.Marshal(payload);if err!=nil{t.Fatal(err)}
 w:=httptest.NewRecorder()
 handler(w,httptest.NewRequest(verb,"http://unix"+path,strings.NewReader(string(data))))
 return w
}
func dev2ManualRecord(s *server,id,due string){
 rec:=sampleSubscription()
 rec.ID=id
 rec.DueDate=due
 s.subscriptions.Items=[]subscriptionRecord{rec}
}
func TestSubscriptionDEV2RenewalAppendOnlyAndVPSUnmodified(t *testing.T){
 s:=subscriptionTestServer(t)
 dev2ManualRecord(s,"sub-manual","2027-02-28")
 s.db.Nodes["node-1"]=nodeConfig{ID:"node-1",DisplayName:"Test VPS",ExpireAt:"2027-04-30",MonthlyPrice:9.9,Currency:"USD"}
 in:=subscriptionRenewalInput{SubscriptionID:"sub-manual",Date:"2027-02-28",AmountMinor:1999,Currency:"USD",NewDueDate:"2027-03-31",Notes:"手动续期"}
 w:=dev2WriteJSON(t,s,s.localSubscriptionRenewals,"POST","/v1/subscriptions/renewals",in)
 if w.Code!=201{t.Fatalf("renewal failed %d: %s",w.Code,w.Body.String())}
 if len(s.subscriptions.Renewals)!=1||s.subscriptions.Items[0].DueDate!="2027-03-31"{t.Fatal("history and due date not saved atomically")}
 event:=s.subscriptions.Renewals[0]
 if event.PreviousDueDate!="2027-02-28"||event.NewDueDate!="2027-03-31"||event.AmountMinor!=1999{t.Fatalf("bad immutable renewal: %+v",event)}
 newer:=in;newer.NewDueDate="2027-04-30";newer.Date="2027-03-31"
 w=dev2WriteJSON(t,s,s.localSubscriptionRenewals,"POST","/v1/subscriptions/renewals",newer)
 if w.Code!=201||len(s.subscriptions.Renewals)!=2||s.subscriptions.Renewals[0]!=event{t.Fatal("past event was overwritten")}
 vps:=subscriptionRenewalInput{SubscriptionID:"vps:node-1",Date:"2027-04-30",AmountMinor:990,Currency:"USD",NewDueDate:"2028-04-30"}
 w=dev2WriteJSON(t,s,s.localSubscriptionRenewals,"POST","/v1/subscriptions/renewals",vps)
 if w.Code!=400{t.Fatal("VPS due date mutation must be forbidden")}
 vps.NewDueDate=""
 w=dev2WriteJSON(t,s,s.localSubscriptionRenewals,"POST","/v1/subscriptions/renewals",vps)
 if w.Code!=201{t.Fatalf("VPS history failed %d: %s",w.Code,w.Body.String())}
 if s.db.Nodes["node-1"].ExpireAt!="2027-04-30"{t.Fatal("VPS data was written by subscription history")}
 other:=subscriptionTestServer(t);other.dataFile=s.dataFile
 if err:=other.loadSubscriptions();err!=nil{t.Fatal(err)}
 if len(other.subscriptions.Renewals)!=3||other.subscriptions.Renewals[0].ID!=event.ID{t.Fatal("persisted immutable history missing")}
 if info,err:=os.Stat(s.subscriptionPath());err!=nil||info.Mode().Perm()!=0600{t.Fatal("private file permissions lost",err)}
}
func TestSubscriptionDEV2ReminderCalendarDaysAndStages(t *testing.T){
 at:=time.Date(2026,10,23,16,30,0,0,time.UTC)
 tokyo,ok:=subscriptionCalendarDays(at,"2026-10-25","Asia/Tokyo")
 if !ok||tokyo!=1{t.Fatalf("Tokyo days=%d ok=%t",tokyo,ok)}
 la,ok:=subscriptionCalendarDays(at,"2026-10-25","America/Los_Angeles")
 if !ok||la!=2{t.Fatalf("LA days=%d ok=%t",la,ok)}
 dst:=time.Date(2027,3,13,12,0,0,0,time.UTC)
 delta,ok:=subscriptionCalendarDays(dst,"2027-03-15","America/Los_Angeles")
 if !ok||delta!=2{t.Fatalf("DST changed calendar-day semantics %d",delta)}
 for _,tc:=range []struct{remaining,want int;valid bool}{
  {31,0,false},{30,30,true},{8,30,true},{7,7,true},{4,7,true},{3,3,true},
  {2,3,true},{1,1,true},{0,0,true},{-1,-1,true},{-9,-1,true},{-31,0,false},
 }{
  got,found:=selectSubscriptionReminderStage(tc.remaining,defaultSubscriptionReminders)
  if got!=tc.want||found!=tc.valid{t.Fatalf("stage %d: got %d/%v expected %d/%v",tc.remaining,got,found,tc.want,tc.valid)}
 }
 if err:=validateReminderDays([]int{7,7});err==nil{t.Fatal("duplicate stage accepted")}
 if err:=validateReminderDays([]int{-2});err==nil{t.Fatal("invalid negative stage accepted")}
}
func TestSubscriptionDEV2ReminderSuccessDedupAndRestart(t *testing.T){
 s:=subscriptionTestServer(t)
 dev2ManualRecord(s,"sub-1","2026-10-30")
 s.subscriptions.RemindersEnabled=true
 s.db.Settings.Telegram=telegramConfig{Enabled:true,BotToken:"test",ChatID:"123"}
 now:=time.Date(2026,10,23,13,0,0,0,time.UTC)
 calls:=0
 send:=func(msg string)error{calls++;if !strings.Contains(msg,"Chat service"){t.Fatal("name missing in notification")};return nil}
 if err:=s.processSubscriptionReminders(now,send);err!=nil{t.Fatal(err)}
 if err:=s.processSubscriptionReminders(now.Add(time.Minute),send);err!=nil{t.Fatal(err)}
 if calls!=1||len(s.subscriptions.Receipts)!=1||s.subscriptions.Receipts[0].DeliveredAt.IsZero(){t.Fatal("duplicate Telegram notification",calls)}
 other:=subscriptionTestServer(t);other.dataFile=s.dataFile
 other.db.Settings.Telegram=s.db.Settings.Telegram
 if err:=other.loadSubscriptions();err!=nil{t.Fatal(err)}
 if err:=other.processSubscriptionReminders(now.Add(time.Hour),send);err!=nil{t.Fatal(err)}
 if calls!=1{t.Fatal("restart lost durable dedup")}
 // New due date creates a distinct idempotency key.
 other.subscriptionMu.Lock()
 next:=other.subscriptions
 next.Items=append([]subscriptionRecord(nil),next.Items...)
 next.Items[0].DueDate="2026-11-20"
 err:=other.saveSubscriptionsLocked(next)
 other.subscriptionMu.Unlock()
 if err!=nil{t.Fatal(err)}
 if err:=other.processSubscriptionReminders(now,send);err!=nil{t.Fatal(err)}
 if calls!=2{t.Fatal("new due date not notified")}
}
func TestSubscriptionDEV2ReminderRetryNoSpam(t *testing.T){
 s:=subscriptionTestServer(t)
 dev2ManualRecord(s,"sub-retry","2026-10-30")
 s.subscriptions.RemindersEnabled=true
 s.db.Settings.Telegram=telegramConfig{Enabled:true,BotToken:"test",ChatID:"123"}
 now:=time.Date(2026,10,23,10,0,0,0,time.UTC)
 calls:=0
 send:=func(string)error{calls++;if calls==1{return errors.New("network failure")};return nil}
 if err:=s.processSubscriptionReminders(now,send);err!=nil{t.Fatal(err)}
 if err:=s.processSubscriptionReminders(now.Add(30*time.Minute),send);err!=nil{t.Fatal(err)}
 if calls!=1{t.Fatal("retry sent too early")}
 if err:=s.processSubscriptionReminders(now.Add(time.Hour),send);err!=nil{t.Fatal(err)}
 if err:=s.processSubscriptionReminders(now.Add(4*time.Hour),send);err!=nil{t.Fatal(err)}
 if calls!=2||s.subscriptions.Receipts[0].Attempts!=2||s.subscriptions.Receipts[0].DeliveredAt.IsZero(){t.Fatal("retry or dedup failed")}
}
func TestSubscriptionDEV2RemindersOptInAndIndividualOff(t *testing.T){
 s:=subscriptionTestServer(t)
 dev2ManualRecord(s,"sub-off","2026-10-30")
 s.db.Settings.Telegram=telegramConfig{Enabled:true,BotToken:"test",ChatID:"123"}
 now:=time.Date(2026,10,23,10,0,0,0,time.UTC)
 calls:=0
 send:=func(string)error{calls++;return nil}
 if err:=s.processSubscriptionReminders(now,send);err!=nil{t.Fatal(err)}
 if calls!=0{t.Fatal("reminders must default off")}
 settings:=subscriptionReminderConfig{Enabled:true,Days:[]int{7,1,0,-1}}
 w:=dev2WriteJSON(t,s,s.localSubscriptionReminders,"PUT","/v1/subscriptions/reminders",settings)
 if w.Code!=200{t.Fatalf("settings failed %d %s",w.Code,w.Body.String())}
 w=dev2WriteJSON(t,s,s.localSubscriptionOverride,"PUT","/v1/subscriptions/reminder-override",map[string]any{"subscription_id":"sub-off","mode":"off"})
 if w.Code!=200{t.Fatalf("override failed %d %s",w.Code,w.Body.String())}
 if err:=s.processSubscriptionReminders(now,send);err!=nil{t.Fatal(err)}
 if calls!=0{t.Fatal("per-item opt-out ignored")}
 w=dev2WriteJSON(t,s,s.localSubscriptionOverride,"PUT","/v1/subscriptions/reminder-override",map[string]any{"subscription_id":"sub-off","mode":"inherit"})
 if w.Code!=200{t.Fatalf("inherit failed %d",w.Code)}
 if err:=s.processSubscriptionReminders(now,send);err!=nil{t.Fatal(err)}
 if calls!=1{t.Fatal("re-enabling inherited reminder failed")}
 s.subscriptionMu.Lock();s.subscriptions.Items[0].Status="paused";s.subscriptions.Items[0].DueDate="2026-10-24";s.subscriptionMu.Unlock()
 if err:=s.processSubscriptionReminders(now,send);err!=nil{t.Fatal(err)}
 if calls!=1{t.Fatal("paused item notified")}
}
func TestSubscriptionDEV2OwnerDataNotPublic(t *testing.T){
 s:=subscriptionTestServer(t)
 dev2ManualRecord(s,"sub-secret","2026-10-30")
 s.subscriptions.Renewals=[]subscriptionRenewalEvent{{ID:"renew-1",SubscriptionID:"sub-secret",Date:"2026-10-21",Currency:"USD",Notes:"Private renewal detail"}}
 r:=httptest.NewRequest("GET","https://test.example/api/v1/subscriptions",nil)
 w:=httptest.NewRecorder()
 s.subscriptionRead(w,r)
 if w.Code!=401||strings.Contains(w.Body.String(),"Private renewal detail"){t.Fatal("public request exposed renewal data")}
 s.subscriptions.OwnerPassword=hashPassword("a very strong private password")
 r=httptest.NewRequest("GET","https://test.example/api/v1/subscriptions",nil)
 r.AddCookie(&http.Cookie{Name:subscriptionCookie,Value:s.newSubscriptionSession(time.Now().Add(time.Hour))})
 w=httptest.NewRecorder();s.subscriptionRead(w,r)
 if w.Code!=200||!strings.Contains(w.Body.String(),"Private renewal detail"){t.Fatal("owner cannot inspect renewal history")}
 w=httptest.NewRecorder()
 s.subscriptionRead(w,httptest.NewRequest("POST","https://test.example/api/v1/subscriptions",strings.NewReader("{}")))
 if w.Code!=405{t.Fatal("public write endpoint opened")}
}

func TestSubscriptionDEV2StoreV1MigrationAndFailClosed(t *testing.T){
 s:=subscriptionTestServer(t)
 rec:=sampleSubscription();rec.ID="sub-old"
 old:=subscriptionStore{Version:1,Items:[]subscriptionRecord{rec}}
 payload,err:=json.Marshal(old);if err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(s.subscriptionPath(),payload,0600);err!=nil{t.Fatal(err)}
 if err:=s.loadSubscriptions();err!=nil{t.Fatal("v1 must load safely",err)}
 if s.subscriptions.Version!=1{t.Fatal("v1 changed before writing")}
 s.subscriptionMu.Lock()
 err=s.saveSubscriptionsLocked(s.subscriptions)
 s.subscriptionMu.Unlock()
 if err!=nil{t.Fatal(err)}
 if s.subscriptions.Version!=2{t.Fatal("DEV2 write must upgrade store to v2")}
 v2,err:=os.ReadFile(s.subscriptionPath());if err!=nil{t.Fatal(err)}
 var stored subscriptionStore
 if err:=json.Unmarshal(v2,&stored);err!=nil{t.Fatal(err)}
 if stored.Version!=2{t.Fatal("persisted version is not 2")}
 next:=subscriptionTestServer(t);next.dataFile=s.dataFile
 if err:=next.loadSubscriptions();err!=nil{t.Fatal("v2 must load",err)}
 stored.Version=3
 bad,_:=json.Marshal(stored)
 if err:=os.WriteFile(s.subscriptionPath(),bad,0600);err!=nil{t.Fatal(err)}
 if err:=next.loadSubscriptions();err==nil{t.Fatal("unknown version must fail closed")}
}

func TestSubscriptionDEV2EmptyReminderScheduleSurvivesRestart(t *testing.T) {
 s:=subscriptionTestServer(t)
 dev2ManualRecord(s,"sub-no-reminder","2026-10-30")
 cfg:=subscriptionReminderConfig{Enabled:true,Days:[]int{}}
 w:=dev2WriteJSON(t,s,s.localSubscriptionReminders,"PUT","/v1/subscriptions/reminders",cfg)
 if w.Code!=200{t.Fatalf("set disabled reminder phases: %d %s",w.Code,w.Body.String())}
 other:=subscriptionTestServer(t)
 other.dataFile=s.dataFile
 if err:=other.loadSubscriptions();err!=nil{t.Fatal(err)}
 if other.subscriptions.ReminderDays==nil||len(other.subscriptions.ReminderDays)!=0 {
  t.Fatalf("empty reminder schedule lost after reload: %+v",other.subscriptions.ReminderDays)
 }
 other.db.Settings.Telegram=telegramConfig{Enabled:true,BotToken:"test",ChatID:"123"}
 sent:=0
 now:=time.Date(2026,10,23,11,0,0,0,time.UTC)
 if err:=other.processSubscriptionReminders(now,func(string)error{sent++;return nil});err!=nil{t.Fatal(err)}
 if sent!=0{t.Fatal("empty schedule unexpectedly restored default alerts")}
}
