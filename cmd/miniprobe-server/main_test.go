package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
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
