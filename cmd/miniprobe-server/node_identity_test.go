package main

import (
	"testing"
	"strings"

	"github.com/Jackyhuang83/MiniProbe/internal/common"
)

func TestASNNameOnlyParsesPublishedDescription(t *testing.T) {
	if got := parseASNNameRecord("13335 | US | arin | 2010-07-14 | CLOUDFLARENET, US", "13335"); got != "CLOUDFLARENET, US" {
		t.Fatalf("ASN name got %q", got)
	}
	if got := parseASNNameRecord("13335 | US | arin | 2010-07-14 | CLOUDFLARENET, US", "16509"); got != "" {
		t.Fatalf("must not mix different ASNs: %q", got)
	}
	if got := parseASNNameRecord("garbled", "13335"); got != "" {
		t.Fatalf("invalid record should fail closed: %q", got)
	}
}

func TestBoundedPlatformResults(t *testing.T) {
	p := boundedPlatforms([]common.PlatformCheck{
		{Name: "ChatGPT", Status: "invalid", Detail: strings.Repeat("a", 300), Region: "jpn"},
		{Name: "ChatGPT", Status: "yes", Region: "US"},
		{Name: "Unknown-Service", Status: "yes"},
		{Name: "Gemini", Status: "reachable", Region: "invalid region"},
	})
	if len(p) != 2 || p[0].Status != "unknown" || p[0].Region != "JPN" || len([]rune(p[0].Detail)) != 96 {
		t.Fatalf("failed to bound platform results: %+v", p)
	}
	if p[1].Region != "" || p[1].Name != "Gemini" {
		t.Fatalf("untrusted region not rejected: %+v", p[1])
	}
}

func TestBoundedCountry(t *testing.T) {
	for _, value := range []string{"", "JP1", "JP US", "JP/US"} {
		if got := boundedCountry(value); got != "" {
			t.Fatalf("accepted invalid region %q as %q", value, got)
		}
	}
	if got := boundedCountry("jp"); got != "JP" {
		t.Fatalf("country normalization got %q", got)
	}
}
