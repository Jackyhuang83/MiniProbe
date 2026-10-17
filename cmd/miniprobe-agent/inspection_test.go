package main

import (
	"testing"
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
