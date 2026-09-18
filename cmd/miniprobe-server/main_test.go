package main

import (
	"net/http/httptest"
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
