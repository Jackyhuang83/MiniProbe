package main

// DEV3 backup operations are only exposed through the existing root-only Unix socket.
// Backup files never enter the public web tree.
import (
 "encoding/json"
 "errors"
 "fmt"
 "net/http"
 "os"
 "path/filepath"
 "strings"
 "time"
)

func (s *server) subscriptionBackupDir() string {
 return filepath.Join(filepath.Dir(s.dataFile),"subscription-backups")
}
func (s *server) localSubscriptionBackup(w http.ResponseWriter,r *http.Request){
 noStore(w)
 if r.Method!=http.MethodPost{http.Error(w,"method not allowed",405);return}
 dir:=s.subscriptionBackupDir()
 if err:=os.MkdirAll(dir,0700);err!=nil{http.Error(w,"backup directory error",500);return}
 if err:=os.Chmod(dir,0700);err!=nil{http.Error(w,"backup permissions error",500);return}
 s.subscriptionMu.Lock();defer s.subscriptionMu.Unlock()
 if err:=validateSubscriptionDev2(s.subscriptions);err!=nil{http.Error(w,"ledger invalid",500);return}
 data,err:=json.MarshalIndent(s.subscriptions,"","  ")
 if err!=nil||len(data)>subscriptionFileLimit{http.Error(w,"backup exceeds limit",500);return}
 name:="subscriptions-"+time.Now().UTC().Format("20060102T150405")+".json"
 path:=filepath.Join(dir,name)
 file,err:=os.OpenFile(path,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600)
 if err!=nil{http.Error(w,"backup already exists or cannot create",409);return}
 _,writeErr:=file.Write(data)
 if writeErr==nil{writeErr=file.Sync()}
 closeErr:=file.Close()
 if writeErr==nil{writeErr=closeErr}
 if writeErr!=nil{_ = os.Remove(path);http.Error(w,"backup write failed",500);return}
 writeJSON(w,200,map[string]any{"name":name,"bytes":len(data)})
}
func (s *server) localSubscriptionBackups(w http.ResponseWriter,r *http.Request){
 noStore(w)
 if r.Method!=http.MethodGet{http.Error(w,"method not allowed",405);return}
 dir:=s.subscriptionBackupDir()
 entries,err:=os.ReadDir(dir)
 if errors.Is(err,os.ErrNotExist){writeJSON(w,200,map[string]any{"items":[]string{}});return}
 if err!=nil{http.Error(w,"cannot list backups",500);return}
 names:=[]string{}
 for _,entry:=range entries{
  if entry.Type().IsRegular()&&strings.HasPrefix(entry.Name(),"subscriptions-")&&strings.HasSuffix(entry.Name(),".json"){names=append(names,entry.Name())}
 }
 writeJSON(w,200,map[string]any{"items":names})
}
func (s *server) localSubscriptionRestore(w http.ResponseWriter,r *http.Request){
 noStore(w)
 if r.Method!=http.MethodPost{http.Error(w,"method not allowed",405);return}
 var in struct{Name string `json:"name"`;Confirm string `json:"confirm"`}
 if err:=decodeJSON(w,r,&in);err!=nil{http.Error(w,"bad request",400);return}
 if in.Confirm!="RESTORE"||filepath.Base(in.Name)!=in.Name||!strings.HasPrefix(in.Name,"subscriptions-")||!strings.HasSuffix(in.Name,".json")||strings.ContainsAny(in.Name,"/\"){
  http.Error(w,"invalid backup name or confirmation",400);return
 }
 path:=filepath.Join(s.subscriptionBackupDir(),in.Name)
 info,err:=os.Lstat(path)
 if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0||info.Size()>subscriptionFileLimit{
  http.Error(w,"backup missing or unsafe",400);return
 }
 data,err:=os.ReadFile(path)
 if err!=nil{http.Error(w,"cannot read backup",500);return}
 // Reuse the full ledger validation path, not just a JSON decode.
 var store subscriptionStore
 if err:=json.Unmarshal(data,&store);err!=nil||store.Version!=2||len(store.Items)>subscriptionMaxRecords{
  http.Error(w,"invalid backup format",400);return
 }
 ids:=map[string]bool{}
 for _,item:=range store.Items{
  if item.ID==""||ids[item.ID]{http.Error(w,"duplicate subscription ID",400);return}
  ids[item.ID]=true
  if _,err:=validateSubscription(item);err!=nil{http.Error(w,"invalid subscription record",400);return}
 }
 if err:=validateSubscriptionDev2(store);err!=nil{http.Error(w,"invalid renewal/reminder data",400);return}
 s.subscriptionMu.Lock();defer s.subscriptionMu.Unlock()
 // Refuse restore unless a safety copy of the current ledger was made.
 current,err:=json.MarshalIndent(s.subscriptions,"","  ")
 if err!=nil||len(current)>subscriptionFileLimit{http.Error(w,"cannot serialize current ledger",500);return}
 safetyName:="subscriptions-pre-restore-"+time.Now().UTC().Format("20060102T150405.000000000")+".json"
 safetyPath:=filepath.Join(s.subscriptionBackupDir(),safetyName)
 safety,err:=os.OpenFile(safetyPath,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600)
 if err!=nil{http.Error(w,"cannot create pre-restore backup",500);return}
 _,err=safety.Write(current)
 if err==nil{err=safety.Sync()}
 closeErr:=safety.Close();if err==nil{err=closeErr}
 if err!=nil{_ = os.Remove(safetyPath);http.Error(w,"pre-restore backup failed",500);return}
 if err:=s.saveSubscriptionsLocked(store);err!=nil{http.Error(w,fmt.Sprintf("restore failed: %v",err),500);return}
 writeJSON(w,200,map[string]bool{"ok":true})
}
