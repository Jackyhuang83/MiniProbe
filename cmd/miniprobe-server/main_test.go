package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseTZOffset(t *testing.T) {
	cases := map[string]int{"+08:00": 480, "UTC+08:00": 480, "-05:30": -330, "+00:00": 0}
	for in, want := range cases {
		got, err := parseTZOffset(in)
		if err != nil || got != want {
			t.Fatalf("%s => %d, %v; want %d", in, got, err, want)
		}
	}
}

func TestValidateNodeInputDefaults(t *testing.T) {
	cfg, err := validateNodeInput("n1", "Aliyun-HK", 200, "outbound", 1, 480, true, 95, 4.5, "usd", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MonthlyTrafficLimit != 200_000_000_000 || cfg.TrafficDirection != "outbound" || cfg.TrafficResetTZMinutes != 480 || !cfg.ShutdownEnabled || cfg.ShutdownPercent != 95 {
		t.Fatalf("bad config: %+v", cfg)
	}
}

func TestMergeNodeProbeFamilyAllowsSingleNodeIPv6Only(t *testing.T) {
	old := nodeConfig{ID: "node-1", DisplayName: "dual-stack",
		TrafficDirection: "total", TrafficResetDay: 1, ShutdownPercent: 95, Currency: "USD"}
	family := "ipv6"
	got, err := mergeNodeUpdate(old, nodeUpdateInput{ID: old.ID, ProbeFamily: &family})
	if err != nil || got.ProbeFamily != "ipv6" {
		t.Fatalf("failed to set IPv6 family: cfg=%+v err=%v", got, err)
	}
	if old.ProbeFamily != "" {
		t.Fatal("original node configuration changed")
	}
	blank := ""
	got2, err := mergeNodeUpdate(got, nodeUpdateInput{ID: old.ID, ProbeFamily: &blank})
	if err != nil || got2.ProbeFamily != "" {
		t.Fatalf("reset to auto failed: cfg=%+v err=%v", got2, err)
	}
	got3, err := mergeNodeUpdate(got, nodeUpdateInput{ID: old.ID})
	if err != nil || got3.ProbeFamily != "ipv6" {
		t.Fatalf("unrelated updates must preserve IP family: cfg=%+v err=%v", got3, err)
	}
}

func TestMergeNodeProbeFamilyRejectsUnknownValue(t *testing.T) {
	old := nodeConfig{ID: "n1", DisplayName: "dual-stack", TrafficDirection: "total",
		TrafficResetDay: 1, ShutdownPercent: 95, Currency: "USD"}
	family := "ipv4-and-ipv6"
	if _, err := mergeNodeUpdate(old, nodeUpdateInput{ID: old.ID, ProbeFamily: &family}); err == nil {
		t.Fatal("unknown family must fail closed")
	}
}

func TestNodeLimitIsSoft(t *testing.T) {
	if softNodeLimit != 15 {
		t.Fatalf("soft node limit changed: %d", softNodeLimit)
	}
}

func TestTrafficGBUsesDecimalUnits(t *testing.T) {
	if got := gbToBytes(200); got != 200_000_000_000 {
		t.Fatalf("200 GB => %d bytes; want 200000000000", got)
	}
	if got := formatTrafficBytes(200_000_000_000); got != "200.0 GB" {
		t.Fatalf("unexpected traffic format: %q", got)
	}
}

func TestDashboardSessionTokenTrustedForThirtyDaysAndPasswordBound(t *testing.T) {
	s := &server{
		secret: []byte("0123456789abcdef0123456789abcdef"),
		db:     database{Settings: settings{DashboardMode: dashboardProtected, DashboardPassword: hashPassword("correct horse battery staple")}},
	}
	now := time.Now().Truncate(time.Second)
	token := s.newDashboardSessionToken(now.Add(dashboardSessionTTL))
	if !s.validDashboardSessionToken(token, now) {
		t.Fatal("fresh 30-day Dashboard session token should be valid")
	}
	if s.validDashboardSessionToken(token, now.Add(dashboardSessionTTL+time.Second)) {
		t.Fatal("expired Dashboard session token should be rejected")
	}
	s.db.Settings.DashboardPassword = hashPassword("a different secure password")
	if s.validDashboardSessionToken(token, now) {
		t.Fatal("changing Dashboard password must invalidate trusted-device sessions")
	}
}

func TestForwardedHTTPSOnlyTrustedFromLoopback(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.test/", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.RemoteAddr = "203.0.113.9:43210"
	if requestIsTLS(r) {
		t.Fatal("public Direct client must not be able to spoof HTTPS with X-Forwarded-Proto")
	}
	r.RemoteAddr = "127.0.0.1:43210"
	if !requestIsTLS(r) {
		t.Fatal("local cloudflared-style HTTPS forwarding should be trusted")
	}
}

func TestMergeNodeUpdateKeepsUnspecifiedFields(t *testing.T) {
	old := nodeConfig{
		ID: "node-1", DisplayName: "HK-YECAOYUN-IPV4", TokenNonce: "nonce",
		MonthlyTrafficLimit: 100_000_000_000, TrafficDirection: "outbound", TrafficResetDay: 20, TrafficResetTZMinutes: 480,
		ShutdownEnabled: true, ShutdownPercent: 95, MonthlyPrice: 5.5, Currency: "USD", ExpireAt: "2027-09-18",
		Tags: []string{"HK", "main"}, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	traffic := 200.0
	got, err := mergeNodeUpdate(old, nodeUpdateInput{ID: old.ID, TrafficGB: &traffic})
	if err != nil {
		t.Fatal(err)
	}
	if got.MonthlyTrafficLimit != 200_000_000_000 {
		t.Fatalf("traffic not updated: %d", got.MonthlyTrafficLimit)
	}
	if got.DisplayName != old.DisplayName || got.TrafficDirection != old.TrafficDirection || got.TrafficResetDay != old.TrafficResetDay || got.TrafficResetTZMinutes != old.TrafficResetTZMinutes || got.ShutdownEnabled != old.ShutdownEnabled || got.ShutdownPercent != old.ShutdownPercent || got.MonthlyPrice != old.MonthlyPrice || got.Currency != old.Currency || got.ExpireAt != old.ExpireAt || got.TokenNonce != old.TokenNonce || !got.CreatedAt.Equal(old.CreatedAt) {
		t.Fatalf("unspecified fields changed: old=%+v got=%+v", old, got)
	}
	if len(got.Tags) != len(old.Tags) || got.Tags[0] != old.Tags[0] || got.Tags[1] != old.Tags[1] {
		t.Fatalf("tags changed unexpectedly: %v", got.Tags)
	}
}

func TestMergeNodeUpdateCanClearOptionalField(t *testing.T) {
	old := nodeConfig{ID: "node-1", DisplayName: "n1", TrafficDirection: "total", TrafficResetDay: 1, ShutdownPercent: 95, MonthlyPrice: 5, Currency: "USD", ExpireAt: "2027-01-01"}
	empty := ""
	zero := 0.0
	got, err := mergeNodeUpdate(old, nodeUpdateInput{ID: old.ID, MonthlyPrice: &zero, ExpireAt: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if got.MonthlyPrice != 0 || got.ExpireAt != "" {
		t.Fatalf("optional fields not cleared: %+v", got)
	}
}

func TestLongTermExpiryAlias(t *testing.T) {
	for _, in := range []string{"L", "long", "longterm", "长期", "2036-01-01"} {
		got, err := normalizeExpireInput(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != longTermExpireDate {
			t.Fatalf("%q => %q want %q", in, got, longTermExpireDate)
		}
	}
	if got := expireDisplay(longTermExpireDate); got != "长期" {
		t.Fatalf("long-term display got %q", got)
	}
}

func TestExpiryValidationRejectsInvalidDate(t *testing.T) {
	if _, err := normalizeExpireInput("2036-02-30"); err == nil {
		t.Fatal("invalid calendar date should be rejected")
	}
}

func TestLoadAgentAssetsAndCapability(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"miniprobe-agent-linux-amd64": "amd64-test-binary",
		"miniprobe-agent-linux-arm64": "arm64-test-binary",
		"miniprobe-agent-linux-armv7": "armv7-test-binary",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0755); err != nil {
			t.Fatal(err)
		}
	}
	assets := loadAgentAssets(dir)
	if len(assets) != 3 {
		t.Fatalf("got %d assets want 3", len(assets))
	}
	if assets["amd64"].Name != "miniprobe-agent-linux-amd64" || len(assets["amd64"].SHA256) != 64 {
		t.Fatalf("unexpected amd64 asset: %+v", assets["amd64"])
	}
	if !hasCapability([]string{"other", selfUpdateCapability}, selfUpdateCapability) {
		t.Fatal("self-update capability not detected")
	}
	if hasCapability([]string{"other"}, selfUpdateCapability) {
		t.Fatal("unexpected self-update capability")
	}
}

func TestDashboardAssetsRedirectAndVersionedCachePolicy(t *testing.T) {
	s := &server{db: database{Settings: settings{DashboardMode: dashboardPublic}}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := s.dashboardAssets(next)

	r := httptest.NewRequest(http.MethodGet, "/network.html?id=node-1", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("unversioned HTML status=%d want %d", w.Code, http.StatusTemporaryRedirect)
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "/ui/"+serverVersion+"/network.html") || !strings.Contains(location, "id=node-1") {
		t.Fatalf("redirect location %q does not preserve id and add versioned path", location)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("redirect Cache-Control=%q want no-store", got)
	}

	r = httptest.NewRequest(http.MethodGet, "/ui/"+serverVersion+"/network.js", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("versioned static status=%d", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("versioned static Cache-Control=%q want immutable", got)
	}
}

func TestVersionedDashboardRootDoesNotRedirectLoop(t *testing.T) {
	s := &server{db: database{Settings: settings{DashboardMode: dashboardPublic}}}
	sub, err := fs.Sub(assets, "web")
	if err != nil {
		t.Fatal(err)
	}
	h := s.dashboardAssets(http.FileServer(http.FS(sub)))

	// The canonical trailing-slash versioned root must serve index.html directly.
	r := httptest.NewRequest(http.MethodGet, "/ui/"+serverVersion+"/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("versioned root status=%d want 200; Location=%q", w.Code, w.Header().Get("Location"))
	}
	if got := w.Header().Get("Location"); got != "" {
		t.Fatalf("versioned root unexpectedly redirects to %q", got)
	}
	if !strings.Contains(w.Body.String(), "MiniProbe") {
		t.Fatal("versioned root did not serve embedded index.html")
	}

	// Missing trailing slash gets exactly one canonical redirect.
	r = httptest.NewRequest(http.MethodGet, "/ui/"+serverVersion, nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("versioned root without slash status=%d want 307", w.Code)
	}
	if got, want := w.Header().Get("Location"), "/ui/"+serverVersion+"/"; got != want {
		t.Fatalf("versioned root redirect Location=%q want %q", got, want)
	}
}


func TestLegacyDashboardLinksSurviveVersionUpgrade(t *testing.T) {
	s := &server{db: database{Settings: settings{DashboardMode: dashboardPublic}}}
	sub, err := fs.Sub(assets, "web")
	if err != nil {
		t.Fatal(err)
	}
	h := s.dashboardAssets(http.FileServer(http.FS(sub)))
	cases := []struct {
		old string
		want string
	}{
		{"/ui/0.4.16/", "/ui/" + serverVersion + "/"},
		{"/ui/0.4.17-alpha", "/ui/" + serverVersion + "/"},
		{"/ui/0.4.18-alpha/index.html", "/ui/" + serverVersion + "/"},
		{"/ui/0.4.18-alpha/network.html?id=node-1", "/ui/" + serverVersion + "/network.html?id=node-1"},
		{"/ui/0.4.18-alpha/network.html?id=node-1&v=old", "/ui/" + serverVersion + "/network.html?id=node-1"},
		{"/ui/0.4.17-alpha/app.js", "/ui/" + serverVersion + "/app.js"},
		{"/ui/0.4.17-alpha/network.js", "/ui/" + serverVersion + "/network.js"},
		{"/ui/0.4.24-alpha/traffic.html", "/ui/" + serverVersion + "/traffic.html"},
		{"/ui/0.4.28-alpha/subscriptions.html", "/ui/" + serverVersion + "/subscriptions.html"},
		{"/ui/0.4.16/style.css", "/ui/" + serverVersion + "/style.css"},
		{"/ui/0.4.16/debian.svg", "/ui/" + serverVersion + "/debian.svg"},
	}
	for _, tc := range cases {
		t.Run(tc.old, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.old, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusTemporaryRedirect {
				t.Fatalf("old link %q got HTTP %d, want 307", tc.old, w.Code)
			}
			if location := w.Header().Get("Location"); location != tc.want {
				t.Fatalf("old link %q redirects to %q, want %q", tc.old, location, tc.want)
			}
			if got := w.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("old link redirect Cache-Control=%q; want no-store", got)
			}
			// Following the redirect must reach an actual bundled resource.
			follow := httptest.NewRequest(http.MethodGet, tc.want, nil)
			next := httptest.NewRecorder()
			h.ServeHTTP(next, follow)
			if next.Code != http.StatusOK {
				t.Fatalf("redirected link %q got HTTP %d, want 200", tc.want, next.Code)
			}
		})
	}
}

func TestLegacyDashboardRejectsUnrecognizedPaths(t *testing.T) {
	for _, p := range []string{
		"/ui/not-a-version/", "/ui/0.4.18-alpha/private",
		"/ui/0.4.18-alpha/foo/bar.js",
		"/ui/0.4.18-alpha/network.html/extra",
		"/api/v1/nodes",
	} {
		if target, ok := legacyDashboardTarget(p); ok {
			t.Fatalf("unexpected old UI redirect for %q: %q", p, target)
		}
	}
	if target, ok := legacyDashboardTarget("/ui/" + serverVersion + "/"); ok {
		t.Fatalf("current UI must not redirect to itself: %q", target)
	}
	s := &server{db: database{Settings: settings{DashboardMode: dashboardDisabled}}}
	h := s.dashboardAssets(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ui/0.4.17-alpha/", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("disabled Dashboard leaked old UI redirect: %d", w.Code)
	}
}

func TestTrafficDashboardUnversionedURLAndCachePolicy(t *testing.T) {
	s := &server{db: database{Settings: settings{DashboardMode: dashboardPublic}}}
	sub, err := fs.Sub(assets, "web")
	if err != nil { t.Fatal(err) }
	handler := s.dashboardAssets(http.FileServer(http.FS(sub)))
	for _, path := range []string{"/traffic.html", "/ui/0.4.24-alpha/traffic.html"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "/ui/"+serverVersion+"/traffic.html" {
			t.Fatalf("traffic redirect %s: %d %q", path, w.Code, w.Header().Get("Location"))
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ui/"+serverVersion+"/traffic.html", nil))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("traffic HTML response: %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
}

func TestEmbeddedDashboardUsesServerVersion(t *testing.T) {
	for _, name := range []string{"web/index.html", "web/network.html", "web/app.js", "web/network.js", "web/traffic.html", "web/traffic.js", "web/subscriptions.html"} {
		b, err := assets.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(b), serverVersion) {
			t.Fatalf("%s does not contain server version %s for cache busting", name, serverVersion)
		}
	}
}


// The same explicit examples must appear when creating and editing VPS nodes.
func TestNodeCurrencyPromptExamples(t *testing.T) {
  for _,tc:=range []struct{currency string;want string}{
    {"USD","币种 [USD]"},{"GBP","币种 [GBP]"},{"CNY","币种 [CNY]"},
  } {
    prompt:=nodeCurrencyInputPrompt(tc.currency)
    if !strings.Contains(prompt,tc.want){t.Fatalf("current value not preserved: %q",prompt)}
    for _,code:=range []string{"USD","CNY","HKD","EUR","CAD","GBP"} {
      if !strings.Contains(prompt,code){t.Fatalf("missing hint %s in %q",code,prompt)}
    }
    if strings.Contains(prompt,"GBD"){t.Fatalf("nonexistent currency code leaked into prompt: %q",prompt)}
  }
}

func TestNodeBillingCycleLegacyAndYearly(t *testing.T) {
 // Historical persisted nodes have no cycle; do not change their monthly price.
 old:=nodeConfig{ID:"legacy",DisplayName:"Legacy VPS",MonthlyPrice:10,Currency:"USD",TrafficDirection:"total",TrafficResetDay:1,ShutdownPercent:95}
 got,err:=mergeNodeUpdate(old,nodeUpdateInput{ID:"legacy"})
 if err!=nil || got.BillingCycle!="monthly" || got.MonthlyPrice!=10 {t.Fatalf("legacy billing changed: %+v %v",got,err)}
 yearly:="yearly"
 annual:=120.0
 got,err=mergeNodeUpdate(old,nodeUpdateInput{ID:"legacy",BillingCycle:&yearly,MonthlyPrice:&annual})
 if err!=nil || got.BillingCycle!="yearly" || got.MonthlyPrice!=120 {t.Fatalf("annual billing not retained: %+v %v",got,err)}
 changedName:="Renamed"
 got,err=mergeNodeUpdate(got,nodeUpdateInput{ID:"legacy",DisplayName:&changedName})
 if err!=nil || got.BillingCycle!="yearly" || got.MonthlyPrice!=120 {t.Fatalf("unrelated node edit changed billing: %+v %v",got,err)}
 unknown:="biannual"
 if _,err=mergeNodeUpdate(old,nodeUpdateInput{ID:"legacy",BillingCycle:&unknown});err==nil {t.Fatal("invalid node billing cycle accepted")}
}
