package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
