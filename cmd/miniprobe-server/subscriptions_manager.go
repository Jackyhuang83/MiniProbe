package main

import (
 "bufio"
 "fmt"
 "net/http"
 "strconv"
 "strings"
)

func getLocalSubscriptions(c *localClient)([]subscriptionView,error) {
 var out struct {Items []subscriptionView `json:"items"`}
 err:=c.request(http.MethodGet,"/v1/subscriptions",nil,&out)
 return out.Items,err
}
func subscriptionCategoryName(cat string)string{
 switch cat{
 case "vps":return "VPS"
 case "video":return "影视/IPTV"
 case "ai":return "AI"
 case "domain":return "域名/云服务"
 case "software":return "软件/App"
 default:return "其他"
 }
}
func subscriptionCycleName(cycle string)string{
 switch cycle{
 case "monthly":return "月付"
 case "quarterly":return "季付"
 case "yearly":return "年付"
 case "custom":return "自定义"
 default:return "长期"
 }
}
func printSubscriptions(items []subscriptionView) {
 if len(items)==0 {fmt.Println("尚无订阅。");return}
 for i,item:=range items {
  due:=item.DueDate;if due==""{due="长期/未设置"}
  amount:="未设置"
  if item.Currency!=""{amount=formatCurrencyMinor(item.AmountMinor,item.Currency)+" "+item.Currency}
  mark:=""
  if item.Source=="vps"{mark=" [VPS 自动关联 · 只读]"}
  fmt.Printf("%3d. %s (%s) · %s · %s · %s · %s%s\n",i+1,item.Name,subscriptionCategoryName(item.Category),amount,subscriptionCycleName(item.Cycle),due,item.Status,mark)
 }
}
func pickManualSubscription(r *bufio.Reader,items []subscriptionView)*subscriptionRecord{
 printSubscriptions(items)
 v:=prompt(r,"选择手动订阅编号（回车取消）: ")
 n,err:=strconv.Atoi(v)
 if err!=nil||n<1||n>len(items){fmt.Println("已取消或编号无效。");return nil}
 item:=items[n-1]
 if item.Source!="manual"{fmt.Println("VPS 数据来自节点设置，请到「修改节点」操作。");return nil}
 rec:=item.subscriptionRecord
 return &rec
}
func promptSubDefault(r *bufio.Reader,label,old string)string{
 if old!=""{label+=fmt.Sprintf(" [%s]",old)}
 v:=prompt(r,label+": ")
 if v==""{return old}
 return v
}
func promptSubscriptionFields(r *bufio.Reader,old *subscriptionRecord)(subscriptionRecord,error){
 rec:=subscriptionRecord{Category:"other",Currency:"USD",Cycle:"monthly",TimeZone:"Asia/Tokyo",Status:"active"}
 if old!=nil{rec=*old}
 rec.Name=promptSubDefault(r,"服务名称",rec.Name)
 fmt.Println("分类：1 影视/IPTV | 2 AI | 3 域名/云服务 | 4 软件/App | 5 其他（VPS 自动关联）")
 cats:=map[string]string{"1":"video","2":"ai","3":"domain","4":"software","5":"other"}
 selection:=prompt(r,"选择分类（回车保留）: ")
 if selection!=""{
  val,ok:=cats[selection];if !ok{return rec,fmt.Errorf("分类编号无效")}
  rec.Category=val
 }
 oldCurrency:=rec.Currency
 rec.Currency=strings.ToUpper(promptSubDefault(r,"币种 ISO 代码",rec.Currency))
 defaultAmount:=formatCurrencyMinor(rec.AmountMinor,oldCurrency)
 if rec.Currency!=oldCurrency {defaultAmount=""}
 amount:=promptSubDefault(r,"费用"+map[bool]string{true:"（币种变化后必须重新输入）",false:""}[rec.Currency!=oldCurrency],defaultAmount)
 minor,err:=parseCurrencyMinor(amount,rec.Currency)
 if err!=nil{return rec,err}
 rec.AmountMinor=minor
 fmt.Println("周期：1 月付 | 2 季付 | 3 年付 | 4 自定义天数 | 5 长期")
 cycles:=map[string]string{"1":"monthly","2":"quarterly","3":"yearly","4":"custom","5":"long_term"}
 selection=prompt(r,"选择周期（回车保留）: ")
 if selection!=""{
  v,ok:=cycles[selection];if !ok{return rec,fmt.Errorf("周期编号无效")}
  rec.Cycle=v
 }
 if rec.Cycle=="custom"{
  days:=promptSubDefault(r,"周期天数",strconv.Itoa(rec.CustomDays))
  rec.CustomDays,err=strconv.Atoi(days);if err!=nil{return rec,err}
 }
 if rec.Cycle!="long_term"{
  rec.DueDate=promptSubDefault(r,"到期日期 YYYY-MM-DD",rec.DueDate)
 }else{rec.DueDate=""}
 rec.TimeZone=promptSubDefault(r,"IANA 时区",rec.TimeZone)
 ar:="n";if rec.AutoRenew{ar="y"}
 v:=strings.ToLower(promptSubDefault(r,"是否自动续费 y/n",ar))
 if v!="y"&&v!="n"{return rec,fmt.Errorf("自动续费只接受 y/n")}
 rec.AutoRenew=v=="y"
 fmt.Println("状态：1 有效 | 2 暂停 | 3 已取消 | 4 已到期")
 states:=map[string]string{"1":"active","2":"paused","3":"cancelled","4":"expired"}
 selection=prompt(r,"选择状态（回车保留）: ")
 if selection!=""{
  v,ok:=states[selection];if !ok{return rec,fmt.Errorf("状态编号无效")}
  rec.Status=v
 }
 rec.Notes=promptSubDefault(r,"备注（不能填写密码/Token/Cookie）",rec.Notes)
 tags:=promptSubDefault(r,"标签（英文逗号分隔）",strings.Join(rec.Tags,","))
 if tags==""{rec.Tags=nil}else{rec.Tags=strings.Split(tags,",")}
 return validateSubscription(rec)
}
func manageSubscriptions(r *bufio.Reader,c *localClient){
 for{
  fmt.Println("\n========== 订阅管理中心 · DEV2 ==========")
  fmt.Println(" 1. 查看全部订阅（含自动关联 VPS）")
  fmt.Println(" 2. 新增订阅")
  fmt.Println(" 3. 编辑订阅")
  fmt.Println(" 4. 暂停 / 取消 / 恢复 / 标记到期")
  fmt.Println(" 5. 删除订阅（需确认）")
  fmt.Println(" 6. 设置/更换私有网页访问密码")
  fmt.Println(" 7. 查看 / 记录续费（含 VPS 记录）")
  fmt.Println(" 8. Telegram 到期提醒（默认关闭）")
  fmt.Println(" 9. 单条订阅提醒设置")
  fmt.Println(" 0. 返回主菜单")
  switch prompt(r,"请选择: "){
  case "1":
   items,err:=getLocalSubscriptions(c)
   if err!=nil{fmt.Println("读取失败:",err)}else{printSubscriptions(items)}
  case "2":
   rec,err:=promptSubscriptionFields(r,nil)
   if err!=nil{fmt.Println("输入无效:",err);continue}
   if err=c.request(http.MethodPost,"/v1/subscriptions",rec,nil);err!=nil{fmt.Println("保存失败:",err)}else{fmt.Println("已添加。")}
  case "3":
   items,err:=getLocalSubscriptions(c)
   if err!=nil{fmt.Println("读取失败:",err);continue}
   old:=pickManualSubscription(r,items);if old==nil{continue}
   rec,err:=promptSubscriptionFields(r,old)
   if err!=nil{fmt.Println("输入无效:",err);continue}
   if err=c.request(http.MethodPut,"/v1/subscriptions",rec,nil);err!=nil{fmt.Println("更新失败:",err)}else{fmt.Println("已更新。")}
  case "4":
   items,err:=getLocalSubscriptions(c)
   if err!=nil{fmt.Println("读取失败:",err);continue}
   rec:=pickManualSubscription(r,items);if rec==nil{continue}
   states:=map[string]string{"1":"active","2":"paused","3":"cancelled","4":"expired"}
   fmt.Println("1 恢复有效 | 2 暂停 | 3 取消 | 4 标记到期")
   state,ok:=states[prompt(r,"选择状态: ")];if !ok{fmt.Println("无效选项。");continue}
   rec.Status=state
   if err=c.request(http.MethodPut,"/v1/subscriptions",rec,nil);err!=nil{fmt.Println("保存失败:",err)}else{fmt.Println("状态已更新。")}
  case "5":
   items,err:=getLocalSubscriptions(c)
   if err!=nil{fmt.Println("读取失败:",err);continue}
   rec:=pickManualSubscription(r,items);if rec==nil{continue}
   if prompt(r,fmt.Sprintf("永久删除「%s」，输入 DELETE 确认: ",rec.Name))!="DELETE"{fmt.Println("已取消。");continue}
   if err=c.request(http.MethodDelete,"/v1/subscriptions?id="+rec.ID,nil,nil);err!=nil{fmt.Println("删除失败:",err)}else{fmt.Println("已删除。")}
  case "6":
   fmt.Println("密码仅用于订阅私有只读网页；与 Dashboard 密码完全独立。")
   p:=promptPassword(r,"新密码（至少 12 字符）: ")
   if len(p)<12||len(p)>128{fmt.Println("密码长度不符合要求。");continue}
   p2:=promptPassword(r,"再次输入新密码: ")
   if p!=p2{fmt.Println("两次输入不一致。");continue}
   if err:=c.request(http.MethodPut,"/v1/subscriptions/password",map[string]string{"password":p},nil);err!=nil{fmt.Println("修改失败:",err)}else{fmt.Println("密码已设置；旧订阅网页会话即刻失效。")}
  case "7":manageSubscriptionRenewals(r,c)
  case "8":manageSubscriptionReminders(r,c)
  case "9":manageSubscriptionOverride(r,c)
  case "0":return
  default:fmt.Println("无效选项。")
  }
 }
}


type localSubscriptionOverview struct {
 Items []subscriptionView `json:"items"`
 Renewals []subscriptionRenewalEvent `json:"renewals"`
 Reminders subscriptionReminderConfig `json:"reminders"`
}
func getSubscriptionOverview(c *localClient)(localSubscriptionOverview,error) {
 var out localSubscriptionOverview
 err:=c.request(http.MethodGet,"/v1/subscriptions",nil,&out)
 return out,err
}
func pickSubscription(r *bufio.Reader,items []subscriptionView)(subscriptionView,bool){
 printSubscriptions(items)
 idx,err:=strconv.Atoi(prompt(r,"订阅编号（回车取消）: "))
 if err!=nil||idx<1||idx>len(items){fmt.Println("已取消。");return subscriptionView{},false}
 return items[idx-1],true
}
func parseReminderMenuDays(input string)([]int,error){
 input=strings.ReplaceAll(input,"，",",")
 var days []int
 if strings.TrimSpace(input)==""{return []int{},nil}
 for _,raw:=range strings.Split(input,","){
  token:=strings.TrimSpace(raw)
  if token==""{return nil,fmt.Errorf("提醒天数不能留空")}
  n,err:=strconv.Atoi(token);if err!=nil{return nil,fmt.Errorf("提醒天数须为整数")}
  days=append(days,n)
 }
 if err:=validateReminderDays(days);err!=nil{return nil,err}
 return days,nil
}
func reminderDaysText(days []int)string{
 var vals []string
 for _,day:=range days{if day==-1{vals=append(vals,"逾期一次")}else{vals=append(vals,fmt.Sprintf("提前 %d 天",day))}}
 if len(vals)==0{return "不提醒"}
 return strings.Join(vals,"、")
}
func manageSubscriptionRenewals(r *bufio.Reader,c *localClient){
 overview,err:=getSubscriptionOverview(c);if err!=nil{fmt.Println("读取失败:",err);return}
 fmt.Println("\n========== 续费历史 ==========")
 if len(overview.Renewals)==0{fmt.Println("尚无续费记录。")}else{
  max:=len(overview.Renewals);if max>15{max=15}
  for _,event:=range overview.Renewals[:max]{
   name:=event.SubscriptionID
   for _,item:=range overview.Items{if item.ID==event.SubscriptionID{name=item.Name;break}}
   fmt.Printf("%s · %s · %s %s",event.Date,name,formatCurrencyMinor(event.AmountMinor,event.Currency),event.Currency)
   if event.NewDueDate!=""{fmt.Printf(" · 新到期日 %s",event.NewDueDate)}
   fmt.Println()
  }
  if len(overview.Renewals)>max{fmt.Printf("显示最近 %d/%d 条（完整历史保存在私有账本）。\n",max,len(overview.Renewals))}
 }
 if strings.ToUpper(prompt(r,"输入 ADD 新增一条续费记录（回车返回）: "))!="ADD"{return}
 item,ok:=pickSubscription(r,overview.Items);if !ok{return}
 now:=time.Now().In(time.FixedZone("JST",9*3600)).Format("2006-01-02")
 date:=promptSubDefault(r,"实际续费日期 YYYY-MM-DD",now)
 if !validSubscriptionDate(date){fmt.Println("日期无效");return}
 currency:=item.Currency;if currency==""{currency="USD"}
 cur:=strings.ToUpper(promptSubDefault(r,"实际支付币种",currency))
 amount:=promptSubDefault(r,"实际支付金额",formatCurrencyMinor(item.AmountMinor,currency))
 if cur!=currency{
  amount=prompt(r,"币种变化，请重新输入实际支付金额: ")
 }
 minor,err:=parseCurrencyMinor(amount,cur)
 if err!=nil{fmt.Println("金额无效:",err);return}
 due:=""
 if item.Source=="manual"&&item.Cycle!="long_term"{
  fmt.Printf("当前到期日：%s\n",item.DueDate)
  due=prompt(r,"续费后新到期日 YYYY-MM-DD（留空不修改）: ")
  if due!=""&&!validSubscriptionDate(due){fmt.Println("新到期日无效");return}
 }else if item.Source=="vps"{fmt.Println("VPS 的到期日需到「修改节点」调整，续费历史不会修改原节点。")}
 notes:=prompt(r,"备注（可留空，勿填密码/Token）: ")
 if strings.ToUpper(prompt(r,"确认追加不可编辑的续费记录？输入 ADD: "))!="ADD"{fmt.Println("已取消");return}
 payload:=subscriptionRenewalInput{SubscriptionID:item.ID,Date:date,AmountMinor:minor,Currency:cur,NewDueDate:due,Notes:notes}
 if err=c.request(http.MethodPost,"/v1/subscriptions/renewals",payload,nil);err!=nil{fmt.Println("保存失败:",err)}else{fmt.Println("续费记录已保存。")}
}
func manageSubscriptionReminders(r *bufio.Reader,c *localClient){
 overview,err:=getSubscriptionOverview(c);if err!=nil{fmt.Println("读取失败:",err);return}
 cfg:=overview.Reminders
 fmt.Printf("\n========== Telegram 订阅提醒 ==========\n当前：%s\n提醒阶段：%s\n",map[bool]string{true:"启用",false:"关闭"}[cfg.Enabled],reminderDaysText(cfg.Days))
 fmt.Println("提醒复用现有 Telegram 设置，但订阅提醒默认关闭，不会自动扣款。")
 fmt.Println("1. 启用提醒")
 fmt.Println("2. 关闭提醒")
 fmt.Println("3. 修改提醒天数（例：30,7,3,1,0,-1，-1 表示逾期一次）")
 choice:=prompt(r,"请选择（回车返回）: ")
 switch choice{
 case "1","2":
  if promptSelection:=prompt(r,"确认修改？输入 YES: ");promptSelection!="YES"{fmt.Println("已取消");return}
  fresh,e:=getSubscriptionOverview(c);if e!=nil{fmt.Println("读取失败:",e);return}
  next:=subscriptionReminderConfig{Enabled:choice=="1",Days:fresh.Reminders.Days}
  if err:=c.request(http.MethodPut,"/v1/subscriptions/reminders",next,nil);err!=nil{fmt.Println("保存失败:",err)}else{fmt.Println("已保存。")}
 case "3":
  raw:=prompt(r,"提醒天数（逗号分隔，-1=逾期；输入 - 表示不提醒）: ")
  if raw=="-"{raw=""}
  days,e:=parseReminderMenuDays(raw);if e!=nil{fmt.Println("输入无效:",e);return}
  if err:=c.request(http.MethodPut,"/v1/subscriptions/reminders",subscriptionReminderConfig{Enabled:cfg.Enabled,Days:days},nil);err!=nil{fmt.Println("保存失败:",err)}else{fmt.Println("提醒阶段已更新。")}
 default:fmt.Println("已返回。")
 }
}
func manageSubscriptionOverride(r *bufio.Reader,c *localClient){
 overview,err:=getSubscriptionOverview(c);if err!=nil{fmt.Println("读取失败:",err);return}
 item,ok:=pickSubscription(r,overview.Items);if !ok{return}
 fmt.Printf("\n订阅：%s\n全局阶段：%s\n",item.Name,reminderDaysText(overview.Reminders.Days))
 fmt.Println("1. 继承全局提醒")
 fmt.Println("2. 关闭本条订阅的提醒")
 fmt.Println("3. 使用单独的提醒阶段")
 choice:=prompt(r,"请选择: ")
 payload:=map[string]any{"subscription_id":item.ID}
 switch choice{
 case "1":payload["mode"]="inherit"
 case "2":payload["mode"]="off"
 case "3":
  days,err:=parseReminderMenuDays(prompt(r,"提醒阶段（例 7,1,0,-1）: "))
  if err!=nil||len(days)==0{fmt.Println("自定义阶段无效");return}
  payload["mode"]="custom";payload["days"]=days
 default:fmt.Println("已取消");return
 }
 if err:=c.request(http.MethodPut,"/v1/subscriptions/reminder-override",payload,nil);err!=nil{fmt.Println("保存失败:",err)}else{fmt.Println("本条订阅提醒设置已保存。")}
}
