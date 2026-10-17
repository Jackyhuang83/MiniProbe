package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Jackyhuang83/MiniProbe/internal/common"
)

func TestIPAttributeDefaultOffAndOncePerUTCDay(t *testing.T) {
	var cfg settings
	if cfg.IPAttributesEnabled {
		t.Fatal("IP attribute calls must be opt-in")
	}
	midday := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if !utcSameDay(midday, midday.Add(10*time.Hour)) {
		t.Fatal("requests must not repeat in the same UTC day")
	}
	if utcSameDay(midday, midday.Add(12*time.Hour)) {
		t.Fatal("queries must be allowed on the next UTC day")
	}
	if utcSameDay(time.Time{}, midday) {
		t.Fatal("missing timestamps should not throttle the initial request")
	}
	attributes := common.IPAttributes{}
	setIPAttributes(&attributes, "ipv4", common.IPAttributeResult{CheckedAt: midday})
	if !utcSameDay(selectIPAttributes(attributes,"ipv4").CheckedAt,midday) {
		t.Fatal("IPv4 attempt stamp was lost")
	}
	if utcSameDay(selectIPAttributes(attributes,"ipv6").CheckedAt,midday) {
		t.Fatal("IPv4 should not consume IPv6 daily allowance")
	}
	encoded, err := json.Marshal(attributes)
	if err != nil { t.Fatal(err) }
	var restored common.IPAttributes
	if err := json.Unmarshal(encoded, &restored); err != nil { t.Fatal(err) }
	if !utcSameDay(restored.IPv4.CheckedAt,midday) {
		t.Fatal("a restart must not reset the daily guard")
	}
}

func TestIPAttributeOnlyAcceptsPublicMatchingFamily(t *testing.T) {
	for _, tc := range []struct{ input,family string; ok bool }{
		{"8.8.8.8", "ipv4", true}, {"2606:4700:4700::1111", "ipv6", true},
		{"127.0.0.1", "ipv4", false}, {"10.1.1.1", "ipv4", false},
		{"169.254.1.1", "ipv4", false}, {"::1", "ipv6", false},
		{"8.8.8.8", "ipv6", false}, {"2606:4700:4700::1111", "ipv4", false},
		{"not-an-ip", "ipv4", false},
	} {
		got := validPublicIP(tc.input,tc.family)
		if (got != "") != tc.ok {
			t.Errorf("validPublicIP(%q, %q)=%q; want valid=%v",tc.input,tc.family,got,tc.ok)
		}
	}
}

func TestIPAttributesConservativeClassification(t *testing.T) {
	type pair struct{ raw,class string }
	for _, tc := range []pair{
		{`{"ip":"8.8.8.8","location":{"country_code":"US"},"risk":{"is_datacenter":true,"is_mobile":false,"is_vpn":true}}`, "idc"},
		{`{"ip":"8.8.8.8","location":{"country_code":"JP"},"risk":{"is_datacenter":false,"is_mobile":true}}`, "mobile"},
		{`{"ip":"8.8.8.8","risk":{"is_datacenter":false,"is_mobile":false}}`, "non_idc"},
		{`{"ip":"8.8.8.8","risk":{}}`, "unknown"},
	} {
		got, err := parseIPAttributeJSON([]byte(tc.raw),"8.8.8.8")
		if err != nil || got.Class != tc.class || got.Native != "unknown" || got.Source != ipAttributeSource || got.State != "ok" {
			t.Errorf("unexpected result: %+v; err=%v want=%s",got,err,tc.class)
		}
	}
}

func TestIPAttributesRejectWrongIPAndNoFalseNative(t *testing.T) {
	for _, invalid := range []string{
		`{"ip":"1.1.1.1","risk":{"is_datacenter":false}}`,
		`{"error":"not found"}`,
		`not json`,
	} {
		if _, err := parseIPAttributeJSON([]byte(invalid),"8.8.8.8"); err == nil {
			t.Errorf("accepted bad response: %s",invalid)
		}
	}
	src := `{"ip":"8.8.8.8","risk":{"is_datacenter":false,"is_mobile":false}}`
	signal, err := parseIPAttributeJSON([]byte(src),"8.8.8.8")
	if err != nil { t.Fatal(err) }
	if signal.Class != "non_idc" || signal.Native != "unknown" {
		t.Fatalf("a non-hosting result must not be reported as confirmed residential/native: %+v",signal)
	}
}
