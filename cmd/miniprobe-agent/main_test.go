package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Jackyhuang83/MiniProbe/internal/common"
)

func TestBillingCycleStart(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	got := billingCycleStart(now, 20, 8*60)
	want := time.Date(2026, 8, 19, 16, 0, 0, 0, time.UTC) // Sep 20 00:00 UTC+8 has not arrived.
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestUpdateTrafficAndShutdown(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	start := billingCycleStart(now, 1, 0)
	st := agentState{
		Policy: common.AgentPolicy{
			NodeID: "n1", TrafficLimitBytes: 100, TrafficDirection: "outbound", TrafficResetDay: 1,
			ShutdownEnabled: true, ShutdownPercent: 50,
		},
		Traffic: trafficState{CycleStart: start, LastRawRx: 1000, LastRawTx: 2000},
	}
	snap, shutdown := updateTraffic(&st, netSample{rx: 1010, tx: 2060, at: now}, now)
	if snap.InboundBytes != 10 || snap.OutboundBytes != 60 || snap.UsedBytes != 60 {
		t.Fatalf("unexpected traffic: %+v", snap)
	}
	if !shutdown || !snap.ProtectionTriggered {
		t.Fatalf("expected traffic protection trigger: %+v", snap)
	}
}

func TestSignedPolicyAndEndpointMigration(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	p := common.AgentPolicy{Version: 2, NodeID: "node-1", Endpoint: ts.URL, TrafficDirection: "total", TrafficResetDay: 1, ShutdownPercent: 95}
	payload, _ := json.Marshal(p)
	signed := &common.SignedPolicy{Policy: p, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, payload))}
	st := agentState{Endpoint: "http://127.0.0.1:1", PolicyVersion: 1}
	changed, err := applySignedPolicy(ts.Client(), "node-1", pub, signed, &st)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || st.Endpoint != ts.URL || st.PolicyVersion != 2 {
		t.Fatalf("policy not applied: %+v", st)
	}
	// Replay must not lower the accepted version.
	old := p
	old.Version = 1
	oldPayload, _ := json.Marshal(old)
	oldSigned := &common.SignedPolicy{Policy: old, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, oldPayload))}
	if _, err := applySignedPolicy(ts.Client(), "node-1", pub, oldSigned, &st); err == nil {
		t.Fatal("expected replay rejection")
	}
}

func TestSignedProbePolicy(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	p := common.ProbePolicy{
		Version: 3, NodeID: "node-1", Enabled: true, Region: "guangzhou", Protocol: "udp",
		Telecom: "202.96.128.86", Unicom: "210.21.4.130", Mobile: "211.136.192.6",
	}
	payload, _ := json.Marshal(p)
	signed := &common.SignedProbePolicy{Policy: p, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, payload))}
	st := agentState{}
	changed, err := applySignedProbePolicy("node-1", pub, signed, &st)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || st.ProbePolicy.Protocol != "udp" || st.ProbePolicy.Region != "guangzhou" {
		t.Fatalf("probe policy not applied: %+v", st.ProbePolicy)
	}
}

func TestProbeConfigLabelsCityAndProtocol(t *testing.T) {
	cfg := probeConfigFromPolicyForFamilies(common.ProbePolicy{
		Enabled: true, Region: "shanghai", Protocol: "tcp",
		Telecom: "202.96.209.133", Unicom: "210.22.70.3", Mobile: "211.136.112.50",
	}, true, true)
	if !cfg.enabled || cfg.protocol != "tcp" || cfg.city != "上海" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
	if len(cfg.tasks) != 3 || cfg.tasks[1].name != "上海联通" {
		t.Fatalf("unexpected tasks: %+v", cfg.tasks)
	}
}

func TestProbeConfigIPv6OnlyUsesCarrierWebsiteTCP6(t *testing.T) {
	p := common.ProbePolicy{
		Enabled: true, Region: "guangzhou", Protocol: "icmp",
		Telecom: "202.96.128.86", Unicom: "210.21.4.130", Mobile: "211.136.192.6",
	}
	cfg := probeConfigFromPolicyForFamilies(p, false, true)
	if cfg.city != "IPv6" || cfg.protocol != "tcp" || len(cfg.tasks) != 3 {
		t.Fatalf("unexpected IPv6-only cfg: %+v", cfg)
	}
	want := []string{"www.189.cn", "www.chinaunicom.com.cn", "www.10086.cn"}
	for i, task := range cfg.tasks {
		if task.target != want[i] {
			t.Fatalf("IPv6-only target got %s want %s", task.target, want[i])
		}
		if task.network != "tcp6" || task.port != "80" {
			t.Fatalf("IPv6-only task must force tcp6/TCP 80: %+v", task)
		}
		if !strings.HasPrefix(task.name, "IPv6") {
			t.Fatalf("IPv6-only row must not claim city precision: %+v", task)
		}
	}
}

func TestProbeTCPOnceTCP6(t *testing.T) {
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	defer ln.Close()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
		}
		close(done)
	}()
	ms, ok := probeTCPOnce("::1", "tcp6", port)
	if !ok || ms < 0 {
		t.Fatalf("tcp6 probe failed: ok=%v latency=%.3f", ok, ms)
	}
	<-done
}

func TestReportEndpointsUseOnlyLoopbackForServerHost(t *testing.T) {
	got := reportEndpoints("https://probe.example.com/", true)
	if len(got) != 1 || got[0] != localServerEndpoint {
		t.Fatalf("server-host Agent must use loopback only: %v", got)
	}
	got = reportEndpoints("https://probe.example.com", false)
	if len(got) != 1 || got[0] != "https://probe.example.com" {
		t.Fatalf("unexpected remote endpoints: %v", got)
	}
}

func TestProbeConfigDualStackKeepsIPv4CityTargets(t *testing.T) {
	p := common.ProbePolicy{
		Enabled: true, Region: "shanghai", Protocol: "tcp",
		Telecom: "202.96.209.133", Unicom: "210.22.70.3", Mobile: "211.136.112.50",
	}
	cfg := probeConfigFromPolicyForFamilies(p, true, true)
	if cfg.city != "上海" || cfg.tasks[0].target != "202.96.209.133" || cfg.tasks[1].name != "上海联通" {
		t.Fatalf("dual-stack should preserve city IPv4 probe policy: %+v", cfg)
	}
}

func TestDNSQuery(t *testing.T) {
	q := dnsQuery(0x1234)
	if len(q) < 20 || q[0] != 0x12 || q[1] != 0x34 {
		t.Fatalf("invalid DNS query: %x", q)
	}
}

func TestICMPProbeIDsAndReplySourceIsolation(t *testing.T) {
	idTelecom := icmpProbeID("广州电信", "202.96.128.86")
	idUnicom := icmpProbeID("广州联通", "210.21.4.130")
	idMobile := icmpProbeID("广州移动", "211.136.192.6")
	if idTelecom == 0 || idUnicom == 0 || idMobile == 0 {
		t.Fatal("ICMP identifier must never be zero")
	}
	if idTelecom == idUnicom || idTelecom == idMobile || idUnicom == idMobile {
		t.Fatalf("carrier probes unexpectedly share identifiers: %d %d %d", idTelecom, idUnicom, idMobile)
	}
	target := net.ParseIP("202.96.128.86").To4()
	if !icmpReplyFromTarget(&net.IPAddr{IP: net.ParseIP("202.96.128.86")}, target) {
		t.Fatal("matching ICMP source should be accepted")
	}
	if icmpReplyFromTarget(&net.IPAddr{IP: net.ParseIP("211.136.192.6")}, target) {
		t.Fatal("reply from another carrier target must be rejected")
	}
}

func TestClassifyNetworkTypes(t *testing.T) {
	cases := []struct {
		name      string
		v4        []string
		v6        []string
		has4Route bool
		has6Route bool
		want      []string
	}{
		{name: "public v4 with route", v4: []string{"203.0.113.9"}, has4Route: true, want: []string{"V4"}},
		{name: "private or cgnat v4 with route is V4", v4: []string{"10.0.0.2", "100.64.0.9"}, has4Route: true, want: []string{"V4"}},
		{name: "private v4 without route is not V4", v4: []string{"10.10.10.10"}, has4Route: false, want: nil},
		{name: "public v6 with route", v6: []string{"2001:db8::9"}, has6Route: true, want: []string{"V6"}},
		{name: "ipv6 only with unrouted private v4", v4: []string{"10.10.10.10"}, v6: []string{"2606:4700::1111"}, has4Route: false, has6Route: true, want: []string{"V6"}},
		{name: "dual stack routes", v4: []string{"192.168.1.2"}, v6: []string{"2606:4700::1111"}, has4Route: true, has6Route: true, want: []string{"V4", "V6"}},
		{name: "ignore link local", v4: []string{"169.254.10.2"}, v6: []string{"fe80::1"}, has4Route: true, has6Route: true, want: nil},
		{name: "ignore private ula", v6: []string{"fc00::1"}, has6Route: true, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyNetworkTypesForFamilies(tc.v4, tc.v6, tc.has4Route, tc.has6Route)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v want %v", got, tc.want)
				}
			}
		})
	}
}

func TestRollingLossMatchesTwentyRoundWindow(t *testing.T) {
	key := "test-rolling-loss-window"
	probeStates.Delete(key)
	defer probeStates.Delete(key)

	var rolling float64
	for i := 0; i < probeHistoryWindow; i++ {
		lost := 0
		if i == 0 {
			lost = 1
		}
		_, _, _, rolling = appendHistories(key, 0, 0, lossQuality(float64(lost)*25), 4, lost)
	}
	if rolling != 1.25 { // 1 lost out of 80 packets.
		t.Fatalf("rolling loss got %.4f want 1.25", rolling)
	}

	// The 21st round pushes the first (lossy) round out of the displayed
	// 20-round history window, so the displayed rolling loss must return to 0.
	_, _, _, rolling = appendHistories(key, 0, 0, 0, 4, 0)
	if rolling != 0 {
		t.Fatalf("rolling loss after window shift got %.4f want 0", rolling)
	}
}

func TestSignedUpgradePolicyIsNarrowAndVerified(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	asset, err := expectedAgentAsset()
	if err != nil {
		t.Fatal(err)
	}
	p := common.UpgradePolicy{
		RequestID: "req-1", NodeID: "node-1", TargetVersion: "0.4.10-alpha",
		Asset: asset, SHA256: strings.Repeat("a", 64), Size: 12345,
	}
	payload, _ := json.Marshal(p)
	signed := &common.SignedUpgradePolicy{Policy: p, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, payload))}
	got, err := verifySignedUpgradePolicy("node-1", pub, signed)
	if err != nil {
		t.Fatal(err)
	}
	if got.Asset != asset || got.TargetVersion != p.TargetVersion {
		t.Fatalf("unexpected upgrade policy: %+v", got)
	}

	tampered := *signed
	tampered.Policy.TargetVersion = "9.9.9"
	if _, err := verifySignedUpgradePolicy("node-1", pub, &tampered); err == nil {
		t.Fatal("tampered upgrade policy must be rejected")
	}

	wrongAsset := p
	wrongAsset.Asset = "miniprobe-agent-linux-not-this-arch"
	b, _ := json.Marshal(wrongAsset)
	wrongSigned := &common.SignedUpgradePolicy{Policy: wrongAsset, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, b))}
	if _, err := verifySignedUpgradePolicy("node-1", pub, wrongSigned); err == nil {
		t.Fatal("upgrade policy must not authorize an arbitrary asset")
	}
}
