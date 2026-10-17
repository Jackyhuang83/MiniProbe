package main

import (
	"testing"

	"github.com/Jackyhuang83/MiniProbe/internal/common"
)

func TestInspectionParsersAreConservative(t *testing.T) {
	if got := parseTraceRegion("fl=42\nloc=JP\n"); got != "JP" { t.Fatalf("region = %q", got) }
	if got := parseTraceRegion("loc=??\n"); got != "" { t.Fatalf("bad region = %q", got) }
	if !blockedGeminiRegion("CHN") || blockedGeminiRegion("JPN") { t.Fatal("Gemini region list broken") }
	if !blockedClaudeRegion("HK") || blockedClaudeRegion("JP") { t.Fatal("Claude region list broken") }
	if netflixTitleStatus(404,404)!="limited" { t.Fatal("404-only title coverage must be limited") }
	if netflixTitleStatus(200,404)!="reachable" { t.Fatal("visible title must be reachable, not verified playback") }
	if netflixTitleStatus(403,403)!="unknown" { t.Fatal("WAF denial must not be called geo-blocking") }
}

func TestMyTVSuperRegionProbeDoesNotConfuseHomepageWithUnlock(t *testing.T) {
	cases := []struct{ body, status, region string }{
		{`{"region":1}`, "reachable", "HK"},
		{`{"region":"1"}`, "reachable", "HK"},
		{`{"region":0}`, "blocked", ""},
		{`{"region":"0"}`, "blocked", ""},
		{`{"region":2}`, "unknown", ""},
		{`{"region":null}`, "unknown", ""},
		{`{"region":true}`, "unknown", ""},
		{`{"region":"HK"}`, "unknown", ""},
		{`{"status":"ok"}`, "unknown", ""},
		{`<html><title>myTV SUPER</title></html>`, "unknown", ""},
		{"", "unknown", ""},
	}
	for _, tc := range cases {
		status, region := myTVSuperRegionStatus(tc.body)
		if status != tc.status || region != tc.region {
			t.Errorf("body %q status/region=%q/%q; want %q/%q", tc.body, status, region, tc.status, tc.region)
		}
	}
}

func TestInspectionRegionSources(t *testing.T) {
	input := []common.UnlockResult{
		{Service: "gemini", Status: "reachable", Region: "JPN"},
		{Service: "youtube", Status: "reachable", Region: "US"},
		{Service: "netflix", Status: "limited"},
		{Service: "disney", Status: "unknown"},
		{Service: "mytvsuper", Status: "blocked"},
	}
	actual := assignInspectionRegions(input, "JP")
	want := []struct{ region, source string }{
		{"JPN", "site"}, {"US", "site"}, {"JP", "egress"}, {"JP", "egress"}, {"JP", "egress"},
	}
	for i, expected := range want {
		if actual[i].Region != expected.region || actual[i].RegionSource != expected.source {
			t.Errorf("%s region/source = %q/%q; want %q/%q", actual[i].Service, actual[i].Region, actual[i].RegionSource, expected.region, expected.source)
		}
	}
	noReference := assignInspectionRegions([]common.UnlockResult{{Service: "google"}}, "")
	if noReference[0].Region != "" || noReference[0].RegionSource != "" {
		t.Fatal("must not invent countries when egress region is unknown")
	}
	if got := parseTraceRegion("ip=redacted\nloc=XX\n"); got != "" {
		t.Fatalf("unknown country code accepted: %s", got)
	}
	if got := parseTraceRegion("loc=JP\n"); got != "JP" {
		t.Fatalf("valid country code lost: %s", got)
	}
	for _, unknown := range []string{"XX", "ZZ", "EU", "AP", "??"} {
		if got := parseTraceRegion("loc="+unknown+"\n"); got != "" {
			t.Errorf("pseudo-country %q accepted as %q", unknown, got)
		}
	}
}
