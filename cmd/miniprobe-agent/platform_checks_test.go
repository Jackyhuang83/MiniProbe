package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type platformRoundTrip func(*http.Request) (*http.Response, error)

func (f platformRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func platformStub(status int, body string) *http.Client {
	return &http.Client{Transport: platformRoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})}
}

func TestParseTraceAndPlatformCountry(t *testing.T) {
	v := parseTrace("ip=2001:db8::1\nloc=JP\nnoise\n")
	if v["ip"] != "2001:db8::1" || platformCountry(v["loc"]) != "JP" {
		t.Fatalf("unexpected trace %v", v)
	}
	for _, invalid := range []string{"", "ZZZ", "JP1", "T1", "xx"} {
		if platformCountry(invalid) != "" {
			t.Fatalf("should reject country %q", invalid)
		}
	}
	if platformCountry("jp") != "JP" {
		t.Fatal("known country codes should normalize to uppercase")
	}
}

func TestChatGPTWebsiteNotMistakenForFullUnlock(t *testing.T) {
	if got := checkChatGPT(platformStub(200, "{}")); got.Status != "reachable" {
		t.Fatalf("a Web response is not login proof: %+v", got)
	}
	if got := checkChatGPT(platformStub(200, `{"error":"unsupported_country"}`)); got.Status != "no" {
		t.Fatalf("explicit country block was ignored: %+v", got)
	}
}

func TestGeminiRegionSignal(t *testing.T) {
	if got := checkGemini(platformStub(200, `...,2,1,200,"JPN",...`)); got.Status != "yes" || got.Region != "JPN" {
		t.Fatalf("unexpected supported Gemini marker: %+v", got)
	}
	if got := checkGemini(platformStub(200, `...,2,1,200,"CHN",...`)); got.Status != "limited" {
		t.Fatalf("expected limited for restricted marker: %+v", got)
	}
	if got := checkGemini(platformStub(200, "Google page only")); got.Status != "reachable" {
		t.Fatalf("missing location marker should not imply unlock: %+v", got)
	}
}

func TestDisneyLandingPageDoesNotProveUnlock(t *testing.T) {
	if got := checkDisney(platformStub(200, "Disney+")); got.Status != "reachable" {
		t.Fatalf("landing page is only reachability evidence: %+v", got)
	}
}

func TestNetflixFilmPages(t *testing.T) {
	if got := checkNetflix(platformStub(404, "not found")); got.Status != "limited" {
		t.Fatalf("two missing titles should not be full unlock: %+v", got)
	}
	if got := checkNetflix(platformStub(403, "forbidden")); got.Status != "unknown" {
		t.Fatalf("HTTP 403 may be anti-bot: %+v", got)
	}
	if got := checkNetflix(platformStub(200, "Netflix")); got.Status != "yes" {
		t.Fatalf("expected available title signal: %+v", got)
	}
}

func TestPlatformRedirectIsolation(t *testing.T) {
	if !samePlatformDomain("www.netflix.com", "help.netflix.com") {
		t.Fatal("same provider should be permitted")
	}
	for _, other := range []string{"127.0.0.1", "169.254.169.254", "netflix.com.evil.example", "youtube.com"} {
		if samePlatformDomain("www.netflix.com", other) {
			t.Fatalf("cross-provider or local redirect must be rejected: %s", other)
		}
	}
}
