package main

import (
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Jackyhuang83/MiniProbe/internal/common"
)

// Platform checks are informational. A reachable landing page must never be
// presented as proof that paid content, login or playback is unlocked.
const platformScanInterval = 12 * time.Hour
const platformResponseLimit = 128 << 10

type platformSnapshot struct {
	Results       []common.PlatformCheck
	CheckedAt     time.Time
	EgressIP      string
	EgressCountry string
}

type platformProbe struct {
	name string
	run  func(*http.Client) common.PlatformCheck
}

func collectPlatformSnapshot() platformSnapshot {
	client := &http.Client{
		Timeout: 7 * time.Second,
		Transport: &http.Transport{
			Proxy: nil, // Probe this node's actual direct egress, not HTTP_PROXY.
			DialContext: (&net.Dialer{Timeout: 4 * time.Second}).DialContext,
			MaxIdleConns: 4, IdleConnTimeout: 15 * time.Second,
			TLSHandshakeTimeout: 4 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 || req.URL.Scheme != "https" || len(via) == 0 {
				return http.ErrUseLastResponse
			}
			// Never follow a third-party redirect to internal endpoints or
			// an unrelated host: these are fixed informational probes.
			if !samePlatformDomain(via[0].URL.Hostname(), req.URL.Hostname()) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	defer client.CloseIdleConnections()

	probes := []platformProbe{
		{"ChatGPT", checkChatGPT},
		{"Gemini", checkGemini},
		{"Claude", checkClaude},
		{"Netflix", checkNetflix},
		{"Disney+", checkDisney},
		{"YouTube", checkYouTube},
		{"TikTok", checkTikTok},
		{"Google", checkGoogle},
		{"Reddit", checkReddit},
	}
	results := make([]common.PlatformCheck, len(probes))
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for i, probe := range probes {
		wg.Add(1)
		go func(i int, probe platformProbe) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = probe.run(client)
			results[i].Name = probe.name
		}(i, probe)
	}
	// Cloudflare trace reports the observed direct egress address and country.
	// The IP is used transiently by Server for ASN lookup; it is not persisted.
	snap := platformSnapshot{}
	status, body, _ := platformGet(client, "https://www.cloudflare.com/cdn-cgi/trace")
	if status == http.StatusOK {
		fields := parseTrace(body)
		if ip := net.ParseIP(fields["ip"]); ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() {
			snap.EgressIP = ip.String()
		}
		snap.EgressCountry = platformCountry(fields["loc"])
	}
	wg.Wait()
	snap.Results = results
	snap.CheckedAt = time.Now().UTC()
	return snap
}

// A redirect is only allowed within the original service's DNS zone.
func samePlatformDomain(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if a == b {
		return true
	}
	for _, zone := range []string{"openai.com", "google.com", "claude.ai", "netflix.com", "disneyplus.com", "youtube.com", "tiktok.com", "reddit.com", "cloudflare.com"} {
		inZone := func(host string) bool { return host == zone || strings.HasSuffix(host, "."+zone) }
		if inZone(a) && inZone(b) {
			return true
		}
	}
	return false
}

func platformGet(client *http.Client, address string) (int, string, string) {
	req, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return 0, "", ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MiniProbe/PlatformCheck)")
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, platformResponseLimit))
	if err != nil {
		return 0, "", ""
	}
	return resp.StatusCode, string(body), resp.Request.URL.String()
}

func parseTrace(body string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(body, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			out[key] = strings.TrimSpace(value)
		}
	}
	return out
}

func platformCountry(raw string) string {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if len(raw) != 2 {
		return ""
	}
	for _, c := range raw {
		if c < 'A' || c > 'Z' {
			return ""
		}
	}
	if raw == "XX" || raw == "T1" {
		return ""
	}
	return raw
}

func platformResult(status, detail, region string) common.PlatformCheck {
	return common.PlatformCheck{Status: status, Detail: detail, Region: region}
}

func checkChatGPT(c *http.Client) common.PlatformCheck {
	status, body, _ := platformGet(c, "https://api.openai.com/compliance/cookie_requirements")
	if strings.Contains(strings.ToLower(body), "unsupported_country") {
		return platformResult("no", "地区不支持", "")
	}
	if status == 200 {
		return platformResult("reachable", "Web 接口可访问，未验证登录", "")
	}
	return platformResult("unknown", "无法确认", "")
}

var geminiRegion = regexp.MustCompile(`,2,1,200,"([A-Z]{3})"`)

func checkGemini(c *http.Client) common.PlatformCheck {
	status, body, finalURL := platformGet(c, "https://gemini.google.com/app")
	if status != 200 || !strings.HasPrefix(finalURL, "https://gemini.google.com/") {
		return platformResult("unknown", "未取得有效地区证据", "")
	}
	m := geminiRegion.FindStringSubmatch(body)
	if len(m) != 2 {
		return platformResult("reachable", "页面可访问，地区未确认", "")
	}
	region := m[1]
	switch region {
	case "CHN", "RUS", "BLR", "CUB", "IRN", "PRK", "SYR", "HKG", "MAC":
		return platformResult("limited", "检测地区可能受限制", region)
	default:
		return platformResult("yes", "地区服务页可用，未验证登录", region)
	}
}

func checkClaude(c *http.Client) common.PlatformCheck {
	status, body, _ := platformGet(c, "https://claude.ai/cdn-cgi/trace")
	if status != 200 {
		return platformResult("unknown", "访问状态未确认", "")
	}
	region := platformCountry(parseTrace(body)["loc"])
	if region == "" {
		return platformResult("unknown", "缺少地区证据", "")
	}
	return platformResult("reachable", "网页边缘节点可达，未验证登录", region)
}

func checkNetflix(c *http.Client) common.PlatformCheck {
	a, aBody, aURL := platformGet(c, "https://www.netflix.com/title/81280792")
	b, bBody, bURL := platformGet(c, "https://www.netflix.com/title/70143836")
	validTitle := func(status int, body, url, id string) bool {
		return status == 200 && strings.Contains(url, "/title/"+id) &&
			strings.Contains(strings.ToLower(body), "netflix")
	}
	if validTitle(a, aBody, aURL, "81280792") || validTitle(b, bBody, bURL, "70143836") {
		return platformResult("yes", "地区版权片名页可访问，播放未验证", "")
	}
	if a == 404 && b == 404 {
		return platformResult("limited", "可能仅自制剧，片库可能变化", "")
	}
	return platformResult("unknown", "无法据此判断片库解锁", "")
}

func checkDisney(c *http.Client) common.PlatformCheck {
	status, body, finalURL := platformGet(c, "https://www.disneyplus.com/")
	path := strings.ToLower(finalURL)
	if strings.Contains(path, "/unavailable") || strings.Contains(path, "/preview") {
		return platformResult("limited", "落地页提示地区不可用", "")
	}
	if status == 200 && strings.Contains(strings.ToLower(body), "disney") {
		return platformResult("reachable", "官网可访问，播放解锁未验证", "")
	}
	return platformResult("unknown", "无法确认", "")
}

var youtubeRegion = regexp.MustCompile(`(?i)"(?:GL|countryCode)"\s*:\s*"([a-z]{2})"`)

func checkYouTube(c *http.Client) common.PlatformCheck {
	status, body, _ := platformGet(c, "https://www.youtube.com/premium?hl=en")
	lower := strings.ToLower(body)
	if strings.Contains(lower, "premium is not available in your country") ||
		strings.Contains(lower, "premium is not available in your region") {
		return platformResult("no", "Premium 不支持当前地区", "")
	}
	if status != 200 {
		return platformResult("unknown", "无法确认", "")
	}
	region := ""
	if m := youtubeRegion.FindStringSubmatch(body); len(m) == 2 {
		region = platformCountry(m[1])
	}
	if strings.Contains(lower, "youtube premium") {
		return platformResult("reachable", "Premium 介绍页可访问，订阅未验证", region)
	}
	return platformResult("unknown", "缺少 Premium 页面证据", region)
}

func checkTikTok(c *http.Client) common.PlatformCheck {
	status, body, _ := platformGet(c, "https://www.tiktok.com/")
	lower := strings.ToLower(body)
	if status == 451 || strings.Contains(lower, "not available in your region") {
		return platformResult("no", "明确的地区限制", "")
	}
	if status == 200 && strings.Contains(lower, "tiktok") {
		return platformResult("reachable", "网页可访问，账号及视频未验证", "")
	}
	return platformResult("unknown", "访问状态未确认", "")
}

func checkGoogle(c *http.Client) common.PlatformCheck {
	status, body, finalURL := platformGet(c, "https://www.google.com/search?q=MiniProbe")
	if status == 200 && strings.Contains(finalURL, "/search?") && strings.Contains(strings.ToLower(body), "google") {
		return platformResult("reachable", "Google 搜索页面可访问", "")
	}
	return platformResult("unknown", "搜索访问未确认", "")
}

func checkReddit(c *http.Client) common.PlatformCheck {
	status, body, _ := platformGet(c, "https://www.reddit.com/r/all/about.json?limit=1")
	if status == 200 && strings.Contains(body, "\"kind\": \"Listing\"") {
		return platformResult("reachable", "公开列表 API 可访问", "")
	}
	if status == 200 && strings.Contains(body, "\"kind\":\"Listing\"") {
		return platformResult("reachable", "公开列表 API 可访问", "")
	}
	return platformResult("unknown", "访问状态未确认", "")
}
