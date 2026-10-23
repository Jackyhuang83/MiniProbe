package main
import(
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)
func TestSubscriptionDEV3BackupRestoreAndPrivacy(t *testing.T){
 s:=subscriptionTestServer(t)
 dev2ManualRecord(s,"sub-original","2027-01-20")
 w:=httptest.NewRecorder()
 s.localSubscriptionBackup(w,httptest.NewRequest(http.MethodPost,"http://unix/v1/subscriptions/backup",strings.NewReader("{}")))
 if w.Code!=200{t.Fatalf("backup failed %d %s",w.Code,w.Body.String())}
 var saved struct{Name string `json:"name"`}
 if err:=json.Unmarshal(w.Body.Bytes(),&saved);err!=nil{t.Fatal(err)}
 info,err:=os.Stat(filepath.Join(s.subscriptionBackupDir(),saved.Name))
 if err!=nil||info.Mode().Perm()!=0600{t.Fatal("backup not private",err)}
 dir,err:=os.Stat(s.subscriptionBackupDir())
 if err!=nil||dir.Mode().Perm()!=0700{t.Fatal("backup directory not root-only",err)}
 s.subscriptionMu.Lock()
 next:=s.subscriptions;next.Items=nil
 err=s.saveSubscriptionsLocked(next)
 s.subscriptionMu.Unlock()
 if err!=nil{t.Fatal(err)}
 w=dev2WriteJSON(t,s,s.localSubscriptionRestore,"POST","/v1/subscriptions/restore",map[string]string{"name":saved.Name,"confirm":"RESTORE"})
 if w.Code!=200{t.Fatalf("restore failed %d %s",w.Code,w.Body.String())}
 if len(s.subscriptions.Items)!=1||s.subscriptions.Items[0].ID!="sub-original"{t.Fatal("restore content mismatch")}
 files,err:=os.ReadDir(s.subscriptionBackupDir());if err!=nil{t.Fatal(err)}
 if len(files)<2{t.Fatal("pre-restore safety backup not created")}
 w=dev2WriteJSON(t,s,s.localSubscriptionRestore,"POST","/v1/subscriptions/restore",map[string]string{"name":"../../etc/passwd","confirm":"RESTORE"})
 if w.Code!=400{t.Fatal("path traversal accepted")}
 w=dev2WriteJSON(t,s,s.localSubscriptionRestore,"POST","/v1/subscriptions/restore",map[string]string{"name":saved.Name,"confirm":"NO"})
 if w.Code!=400{t.Fatal("restore without confirmation accepted")}
}
