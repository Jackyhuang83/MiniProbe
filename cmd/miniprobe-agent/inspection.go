package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Jackyhuang83/MiniProbe/internal/common"
)

const inspectionBodyMax = 256 << 10

type inspectionSnapshot struct {
	Unlock []common.UnlockResult
	IPv4   string
	IPv6   string
}

var geminiCountryPattern = regexp.MustCompile(`,2,1,200,"([A-Z]{3})"`)
var youtubeCountryPattern = regexp.MustCompile(`"(?:GL|countryCode|country_code)"\s*:\s*"([A-Za-z]{2})"`)

func inspectionClient() *http.Client {
	return &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			// Probe the VPS itself, not an optional environment HTTP proxy.
			DialContext: (&net.Dialer{Timeout: 4 * time.Second}).DialContext,
			TLSHandshakeTimeout: 4 * time.Second,
			DisableKeepAlives: true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func inspectNode() inspectionSnapshot {
	var snap inspectionSnapshot
	var mu sync.Mutex
	var wg sync.WaitGroup
	var egressRegion string
	names := []string{"chatgpt", "gemini", "claude", "google", "netflix", "disney", "youtube", "tiktok", "reddit", "mytvsuper"}
	results := make([]common.UnlockResult, len(names))
	// Small bounded concurrency: no background upload, speed test or video transfer.
	gate := make(chan struct{}, 3)
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			gate <- struct{}{}
			defer func() { <-gate }()
			results[i] = inspectPlatform(inspectionClient(), name)
		}(i, name)
	}
	// One additional small HTTPS probe for the *default* egress region.
	// Cloudflare's edge-local loc field is only a network-origin reference,
	// not proof that any particular platform considers the account unlocked.
	wg.Add(1)
	go func() {
		defer wg.Done()
		gate <- struct{}{}
		defer func() { <-gate }()
		status, body := inspectFetch(inspectionClient(), "https://cloudflare.com/cdn-cgi/trace")
		if status == http.StatusOK {
			region := parseTraceRegion(body)
			mu.Lock()
			egressRegion = region
			mu.Unlock()
		}
	}()
	wg.Add(2)
	go func() {
		defer wg.Done()
		ip := inspectEgressIP("tcp4", "https://api4.ipify.org")
		mu.Lock()
		snap.IPv4 = ip
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		ip := inspectEgressIP("tcp6", "https://api6.ipify.org")
		mu.Lock()
		snap.IPv6 = ip
		mu.Unlock()
	}()
	wg.Wait()
	snap.Unlock = assignInspectionRegions(results, egressRegion)
	return snap
}

// RegionSource identifies the provenance of each label. Never present the
// common egress location as the platform's own market/entitlement decision.
func assignInspectionRegions(results []common.UnlockResult, egressRegion string) []common.UnlockResult {
	if len(egressRegion) != 2 {
		egressRegion = ""
	}
	for i := range results {
		if results[i].Region != "" {
			results[i].RegionSource = "site"
		} else if egressRegion != "" {
			results[i].Region = egressRegion
			results[i].RegionSource = "egress"
		}
	}
	return results
}

// Egress addresses are strictly transient: sent to the authenticated Server
// for ASN resolution, never written into the Agent state or Dashboard API.
func inspectEgressIP(network, endpoint string) string {
	client := inspectionClient()
	tr := client.Transport.(*http.Transport)
	tr.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 4 * time.Second}).DialContext(ctx, network, addr)
	}
	defer tr.CloseIdleConnections()
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil { return "" }
	resp, err := client.Do(req)
	if err != nil { return "" }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { return "" }
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 80))
	if err != nil { return "" }
	ip := net.ParseIP(strings.TrimSpace(string(raw)))
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() { return "" }
	if (network == "tcp4") != (ip.To4() != nil) { return "" }
	return ip.String()
}

func inspectFetch(client *http.Client, endpoint string) (int, string) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil { return 0, "" }
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MiniProbe/1.0; +https://github.com/Jackyhuang83/MiniProbe)")
	req.Header.Set("Accept-Language", "en")
	resp, err := client.Do(req)
	if err != nil { return 0, "" }
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, inspectionBodyMax))
	if err != nil { return 0, "" }
	return resp.StatusCode, string(data)
}

func inspectPlatform(client *http.Client, service string) common.UnlockResult {
	r := common.UnlockResult{Service: service, Status: "unknown", CheckedAt: time.Now().UTC()}
	switch service {
	case "chatgpt":
		status, body := inspectFetch(client, "https://api.openai.com/compliance/cookie_requirements")
		lower := strings.ToLower(body)
		if strings.Contains(lower, "unsupported_country") { r.Status = "blocked" } else if status == 200 && strings.TrimSpace(body) != "" { r.Status = "reachable" }
		_, trace := inspectFetch(client, "https://chat.openai.com/cdn-cgi/trace")
		r.Region = parseTraceRegion(trace)
	case "gemini":
		status, body := inspectFetch(client, "https://gemini.google.com/")
		if status == 200 {
			region := geminiCountryPattern.FindStringSubmatch(body)
			if len(region) == 2 {
				r.Region = region[1]
				if blockedGeminiRegion(r.Region) { r.Status = "blocked" } else { r.Status = "reachable" }
			}
		}
	case "claude":
		status, body := inspectFetch(client, "https://claude.ai/cdn-cgi/trace")
		if status == 200 {
			r.Region = parseTraceRegion(body)
			if r.Region != "" {
				if blockedClaudeRegion(r.Region) { r.Status = "blocked" } else { r.Status = "reachable" }
			}
		}
	case "google":
		status, body := inspectFetch(client, "https://www.google.com/search?q=MiniProbe&hl=en")
		if status == 200 && len(body) > 0 { r.Status = "reachable" }
	case "netflix":
		first, _ := inspectFetch(client, "https://www.netflix.com/title/81280792")
		second, _ := inspectFetch(client, "https://www.netflix.com/title/70143836")
		r.Status = netflixTitleStatus(first, second)
	case "disney":
		status, body := inspectFetch(client, "https://www.disneyplus.com/")
		lower := strings.ToLower(body)
		if strings.Contains(lower, "not available in your region") { r.Status = "blocked" } else if status == 200 && len(body) > 0 { r.Status = "reachable" }
	case "youtube":
		status, body := inspectFetch(client, "https://www.youtube.com/premium?hl=en")
		lower := strings.ToLower(body)
		if strings.Contains(lower, "premium is not available in your country") || strings.Contains(lower, "premium is not available in your region") { r.Status = "blocked" } else if status == 200 && strings.Contains(lower, "youtube premium") { r.Status = "reachable" }
		code := youtubeCountryPattern.FindStringSubmatch(body)
		if len(code) == 2 { r.Region = strings.ToUpper(code[1]) }
	case "tiktok":
		status, body := inspectFetch(client, "https://www.tiktok.com/")
		lower := strings.ToLower(body)
		if strings.Contains(lower, "not available in your region") || strings.Contains(lower, "tiktok is not available") { r.Status = "blocked" } else if status == 200 && strings.Contains(lower, "tiktok") { r.Status = "reachable" }
	case "reddit":
		status, body := inspectFetch(client, "https://www.reddit.com/")
		if status == 200 && len(body) > 0 { r.Status = "reachable" }
	case "mytvsuper":
		// The homepage can open worldwide and does NOT prove HK playback access.
		// An unauthenticated regional session probe is only an indication of
		// geographic eligibility; this does not establish DRM/paid entitlement.
		status, body := inspectFetch(client, "https://www.mytvsuper.com/api/auth/getSession/self/")
		if status == http.StatusOK {
			r.Status, r.Region = myTVSuperRegionStatus(body)
		}
	}
	return r
}

// The existing myTV SUPER session API reports HK geographical eligibility as
// region=1 (RegionRestrictionCheck). Fail closed if the shape changes, if the
// session is unavailable, or if region contains any unexpected value.
func myTVSuperRegionStatus(body string) (status, region string) {
	var response struct { Region json.RawMessage `json:"region"` }
	if err := json.Unmarshal([]byte(body), &response); err != nil || len(response.Region) == 0 {
		return "unknown", ""
	}
	switch strings.TrimSpace(string(response.Region)) {
	case "1", `"1"`:
		return "reachable", "HK"
	case "0", `"0"`:
		return "blocked", ""
	default:
		return "unknown", ""
	}
}

func parseTraceRegion(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "loc=") {
			code := strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(line, "loc=")))
			if len(code) == 2 && code[0] >= 'A' && code[0] <= 'Z' && code[1] >= 'A' && code[1] <= 'Z' && code != "XX" && code != "ZZ" && code != "EU" && code != "AP" {
				return code
			}
		}
	}
	return ""
}

func blockedGeminiRegion(code string) bool {
	switch code { case "CHN", "RUS", "BLR", "CUB", "IRN", "PRK", "SYR", "HKG", "MAC": return true }
	return false
}

func blockedClaudeRegion(code string) bool {
	switch code { case "AF", "BY", "CN", "CU", "HK", "IR", "KP", "MO", "RU", "SY": return true }
	return false
}

// A title HTTP response is evidence of catalogue visibility, not playback
// entitlement. Avoid reporting full "unlock" without actual playback proof.
func netflixTitleStatus(a, b int) string {
	if a == 404 && b == 404 { return "limited" }
	if a == 200 || b == 200 { return "reachable" }
	return "unknown"
}
