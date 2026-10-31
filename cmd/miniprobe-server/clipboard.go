package main

// Clipboard is deliberately ephemeral: no file, database entry or backup is made.
// A Server/container restart clears all entries. Neither this deletion nor GC
// constitutes secure memory erasure or deletion of external system snapshots.
import (
 "crypto/subtle"
 "encoding/hex"
 "net/http"
 "strings"
 "sync"
 "time"
)

const (
 clipboardTTL = 24 * time.Hour
 clipboardMaxItems = 50
 clipboardMaxTextBytes = 16 << 10
)

type clipboardItem struct {
 ID string `json:"id"`
 Text string `json:"text"`
 CreatedAt time.Time `json:"created_at"`
 ExpiresAt time.Time `json:"expires_at"`
}

// Separate from subscription data and all Agent/monitoring state.
type clipboardMemory struct {
 mu sync.Mutex
 items []clipboardItem
}

// Purge both stale entries and the old backing slice (to release references).
// Callers must hold the clipboard lock.
func (c *clipboardMemory) pruneLocked(now time.Time) {
 next := make([]clipboardItem, 0, len(c.items))
 for _, item := range c.items {
  if now.Before(item.ExpiresAt) {
   next = append(next, item)
  }
 }
 for i := range c.items { c.items[i] = clipboardItem{} }
 c.items = next
}

func (s *server) clipboardCleanupLoop() {
 ticker := time.NewTicker(time.Minute)
 defer ticker.Stop()
 for now := range ticker.C {
  s.clipboard.mu.Lock()
  s.clipboard.pruneLocked(now)
  s.clipboard.mu.Unlock()
 }
}

func (s *server) clipboardAPI(w http.ResponseWriter, r *http.Request) {
 noStore(w)
 if !requestIsTLS(r) || !s.subscriptionAuthorized(r) {
  http.Error(w, "unauthorized", http.StatusUnauthorized)
  return
 }
 if r.Method != http.MethodGet {
  expected := s.subscriptionCSRFToken(r)
  supplied := r.Header.Get("X-MiniProbe-Subscription-CSRF")
  if !validSameOriginMutation(r) || expected == "" ||
   subtle.ConstantTimeCompare([]byte(supplied), []byte(expected)) != 1 {
   http.Error(w, "forbidden", http.StatusForbidden)
   return
  }
 }

 switch r.Method {
 case http.MethodGet:
  s.clipboard.mu.Lock()
  s.clipboard.pruneLocked(time.Now())
  items := append([]clipboardItem{}, s.clipboard.items...)
  s.clipboard.mu.Unlock()
  writeJSON(w, http.StatusOK, map[string]any{"items": items, "ttl_hours": 24, "limit": clipboardMaxItems})

 case http.MethodPost:
  var in struct { Text string `json:"text"` }
  if err := decodeJSON(w, r, &in); err != nil {
   http.Error(w, "invalid JSON", http.StatusBadRequest)
   return
  }
  if strings.TrimSpace(in.Text) == "" || len(in.Text) > clipboardMaxTextBytes {
   http.Error(w, "text must be 1 to 16384 bytes", http.StatusBadRequest)
   return
  }
  now := time.Now().UTC()
  item := clipboardItem{
   ID: hex.EncodeToString(randomBytes(16)),
   Text: in.Text,
   CreatedAt: now,
   ExpiresAt: now.Add(clipboardTTL),
  }
  s.clipboard.mu.Lock()
  s.clipboard.pruneLocked(now)
  s.clipboard.items = append([]clipboardItem{item}, s.clipboard.items...)
  if len(s.clipboard.items) > clipboardMaxItems {
   for i := clipboardMaxItems; i < len(s.clipboard.items); i++ {
    s.clipboard.items[i] = clipboardItem{}
   }
   s.clipboard.items = s.clipboard.items[:clipboardMaxItems:clipboardMaxItems]
  }
  s.clipboard.mu.Unlock()
  writeJSON(w, http.StatusCreated, item)

 case http.MethodDelete:
  id := r.URL.Query().Get("id")
  clearAll := r.URL.Query().Get("all") == "1"
  if (id == "" && !clearAll) || (id != "" && clearAll) {
   http.Error(w, "specify id or all=1", http.StatusBadRequest)
   return
  }
  s.clipboard.mu.Lock()
  s.clipboard.pruneLocked(time.Now())
  if clearAll {
   for i := range s.clipboard.items { s.clipboard.items[i] = clipboardItem{} }
   s.clipboard.items = nil
  } else {
   next := make([]clipboardItem, 0, len(s.clipboard.items))
   for _, item := range s.clipboard.items {
    if item.ID != id { next = append(next, item) }
   }
   if len(next) == len(s.clipboard.items) {
    s.clipboard.mu.Unlock()
    http.Error(w, "not found", http.StatusNotFound)
    return
   }
   for i := range s.clipboard.items { s.clipboard.items[i] = clipboardItem{} }
   s.clipboard.items = next
  }
  s.clipboard.mu.Unlock()
  writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

 default:
  w.Header().Set("Allow", "GET, POST, DELETE")
  http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
 }
}
