package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Jackyhuang83/MiniProbe/internal/common"
)

const ipAttributeSource = "ipquery.io"
const ipAttributeEndpoint = "https://api.ipquery.io/"

type ipAttrJob struct {
	nodeID string
	family string
	ip     string // held only in transient memory, never persisted or logged
}

// This is provider data, not proof of a residential or "native" IP.
type ipQueryResponse struct {
	IP       string `json:"ip"`
	Location struct {
		CountryCode string `json:"country_code"`
	} `json:"location"`
	Risk struct {
		IsDatacenter *bool `json:"is_datacenter"`
		IsMobile     *bool `json:"is_mobile"`
		IsVPN        *bool `json:"is_vpn"`
		IsProxy      *bool `json:"is_proxy"`
		IsTor        *bool `json:"is_tor"`
	} `json:"risk"`
}

func utcSameDay(a, b time.Time) bool {
	y1, m1, d1 := a.UTC().Date()
	y2, m2, d2 := b.UTC().Date()
	return !a.IsZero() && y1 == y2 && m1 == m2 && d1 == d2
}

func selectIPAttributes(a common.IPAttributes, family string) common.IPAttributeResult {
	if family == "ipv4" {
		return a.IPv4
	}
	return a.IPv6
}

func setIPAttributes(a *common.IPAttributes, family string, v common.IPAttributeResult) {
	if family == "ipv4" {
		a.IPv4 = v
	} else {
		a.IPv6 = v
	}
}

func validPublicIP(ipstr, family string) string {
	p := net.ParseIP(strings.TrimSpace(ipstr))
	if p == nil || !p.IsGlobalUnicast() || p.IsPrivate() || p.IsLoopback() || p.IsLinkLocalUnicast() {
		return ""
	}
	if family == "ipv4" && p.To4() == nil {
		return ""
	}
	if family == "ipv6" && p.To4() != nil {
		return ""
	}
	return p.String()
}

func (s *server) scheduleIPAttributes(nodeID, ipv4, ipv6 string) {
	if s.ipAttrQueue == nil {
		return
	}
	s.mu.RLock()
	enabled := s.db.Settings.IPAttributesEnabled
	view := s.db.States[nodeID]
	_, registered := s.db.Nodes[nodeID]
	s.mu.RUnlock()
	if !enabled || !registered {
		return
	}
	now := time.Now().UTC()
	for _, item := range []struct{ family, addr string }{{"ipv4", ipv4}, {"ipv6", ipv6}} {
		ip := validPublicIP(item.addr, item.family)
		if ip == "" || utcSameDay(selectIPAttributes(view.IPAttributes, item.family).CheckedAt, now) {
			continue
		}
		key := nodeID + "/" + item.family
		s.ipAttrMu.Lock()
		if !s.ipAttrPending[key] {
			// Enforce one concurrent pending job per node/address-family.
			select {
			case s.ipAttrQueue <- ipAttrJob{nodeID, item.family, ip}:
				s.ipAttrPending[key] = true
			default:
				// A full queue is safe: the next Agent report can retry.
			}
		}
		s.ipAttrMu.Unlock()
	}
}

// One serial worker keeps the optional provider traffic bounded.
func (s *server) ipAttributeWorker() {
	for job := range s.ipAttrQueue {
		s.processIPAttribute(job)
		s.ipAttrMu.Lock()
		delete(s.ipAttrPending, job.nodeID+"/"+job.family)
		s.ipAttrMu.Unlock()
		// Space requests from different nodes rather than a 30-request burst.
		time.Sleep(time.Second)
	}
}

func (s *server) processIPAttribute(job ipAttrJob) {
	now := time.Now().UTC()
	s.mu.Lock()
	view, ok := s.db.States[job.nodeID]
	_, registered := s.db.Nodes[job.nodeID]
	if !ok || !registered || !s.db.Settings.IPAttributesEnabled ||
		utcSameDay(selectIPAttributes(view.IPAttributes, job.family).CheckedAt, now) {
		s.mu.Unlock()
		return
	}
	// Record the ATTEMPT before contacting the third party. Failed calls also
	// consume today's allowance; crashes/reboots cannot spam a provider.
	pending := common.IPAttributeResult{Class: "unknown", Native: "unknown", State: "unknown", Source: ipAttributeSource, CheckedAt: now}
	setIPAttributes(&view.IPAttributes, job.family, pending)
	s.db.States[job.nodeID] = view
	s.mu.Unlock()
	// Persist attempted day before request. If persistence fails, do not send
	// anything externally: the daily guard would not survive restart.
	if err := s.save(); err != nil {
		return
	}
	result, err := queryIPAttributes(context.Background(), ipAttributeEndpoint, job.ip)
	if err != nil {
		return // checked_at still records a single attempt for this day
	}
	result.CheckedAt = now
	s.mu.Lock()
	if !s.db.Settings.IPAttributesEnabled {
		s.mu.Unlock()
		return
	}
	view, ok = s.db.States[job.nodeID]
	if ok {
		// A newer attempt is never overwritten by an old or slow request.
		current := selectIPAttributes(view.IPAttributes, job.family)
		if current.CheckedAt.Equal(now) {
			setIPAttributes(&view.IPAttributes, job.family, result)
			s.db.States[job.nodeID] = view
		}
	}
	s.mu.Unlock()
}

func ipAttributeClient() *http.Client {
	return &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			Proxy: nil, // do not send monitoring-node addresses to a local proxy
			DialContext: (&net.Dialer{Timeout: 4 * time.Second}).DialContext,
			TLSHandshakeTimeout: 4 * time.Second,
			DisableKeepAlives: true,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("IP 属性查询拒绝重定向")
		},
	}
}

func parseIPAttributeJSON(payload []byte, requestedIP string) (common.IPAttributeResult, error) {
	result := common.IPAttributeResult{Class:"unknown", Native:"unknown", State:"unknown", Source:ipAttributeSource}
	var raw ipQueryResponse
	if err := json.Unmarshal(payload, &raw); err != nil {
		return result, err
	}
	// Reject responses for another IP and malformed/error payloads.
	expected := net.ParseIP(requestedIP)
	actual := net.ParseIP(strings.TrimSpace(raw.IP))
	if expected == nil || actual == nil || !expected.Equal(actual) {
		return result, errors.New("IP 属性响应的地址与查询不一致")
	}
	switch {
	case raw.Risk.IsMobile != nil && *raw.Risk.IsMobile:
		result.Class = "mobile"
	case raw.Risk.IsDatacenter != nil && *raw.Risk.IsDatacenter:
		result.Class = "idc"
	case raw.Risk.IsDatacenter != nil && !*raw.Risk.IsDatacenter &&
		raw.Risk.IsMobile != nil && !*raw.Risk.IsMobile:
		// Negative hosting/mobile evidence != proof of a real home connection.
		result.Class = "non_idc"
	}
	cc := strings.ToUpper(strings.TrimSpace(raw.Location.CountryCode))
	if len(cc) == 2 && cc[0] >= 'A' && cc[0] <= 'Z' && cc[1] >= 'A' && cc[1] <= 'Z' &&
		cc != "ZZ" && cc != "XX" && cc != "EU" && cc != "AP" {
		result.Country = cc
	}
	result.VPN = raw.Risk.IsVPN != nil && *raw.Risk.IsVPN
	result.Proxy = raw.Risk.IsProxy != nil && *raw.Risk.IsProxy
	result.Tor = raw.Risk.IsTor != nil && *raw.Risk.IsTor
	result.State = "ok"
	return result, nil
}

// The production caller passes a fixed HTTPS origin, never user-supplied URLs.
// 'endpoint' is injectable only for local httptest unit tests.
func queryIPAttributes(ctx context.Context, endpoint, ipstr string) (common.IPAttributeResult, error) {
	ip := net.ParseIP(ipstr)
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return common.IPAttributeResult{}, errors.New("不支持查询非公网 IP")
	}
	dest := strings.TrimRight(endpoint, "/") + "/" + url.PathEscape(ip.String())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dest, nil)
	if err != nil {
		return common.IPAttributeResult{}, err
	}
	req.Header.Set("User-Agent", "MiniProbe-IP-Attributes")
	resp, err := ipAttributeClient().Do(req)
	if err != nil {
		return common.IPAttributeResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return common.IPAttributeResult{}, fmt.Errorf("IP 属性数据源 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10+1))
	if err != nil {
		return common.IPAttributeResult{}, err
	}
	if len(raw) > 64<<10 {
		return common.IPAttributeResult{}, errors.New("IP 属性响应过大")
	}
	return parseIPAttributeJSON(raw, ip.String())
}

func manageIPAttributeSettings(r *bufio.Reader, c *localClient) {
	cfg, err := getLocalConfig(c)
	if err != nil {
		fmt.Println("读取配置失败:", err)
		return
	}
	fmt.Println("\n========= IP 属性检测 =========")
	fmt.Printf("当前状态：%s\n", map[bool]string{true:"开启", false:"关闭"}[cfg.IPAttributesEnabled])
	fmt.Println("第三方数据源：ipquery.io（HTTPS；不需要 API Key）")
	fmt.Println("每节点 IPv4/IPv6 各自最多每天查询一次（UTC），失败也计一次。")
	fmt.Println("查询会向第三方提供该节点公网出口 IP。结果仅供参考，非 IDC 不等于家宽；原生属性无法可靠判定。")
	fmt.Println("结果只保存属性和查询日期，不存储公网出口 IP；Dashboard 默认折叠且不能修改设置。")
	fmt.Println("1. 开启")
	fmt.Println("2. 关闭")
	fmt.Println("0. 返回")
	switch prompt(r, "请选择: ") {
	case "1":
		if cfg.IPAttributesEnabled {
			fmt.Println("已是开启状态。")
			return
		}
		if strings.ToUpper(prompt(r, "确认同意将节点出口 IP 发给 ipquery.io？输入 YES 开启：")) != "YES" {
			fmt.Println("已取消。")
			return
		}
		if err := c.request(http.MethodPut, "/v1/config", map[string]any{"ip_attributes_enabled": true}, nil); err != nil {
			fmt.Println("启用失败:", err)
		} else {
			fmt.Println("已启用；等待下次 Agent 上报后执行当日查询。")
		}
	case "2":
		if err := c.request(http.MethodPut, "/v1/config", map[string]any{"ip_attributes_enabled": false}, nil); err != nil {
			fmt.Println("关闭失败:", err)
		} else {
			fmt.Println("已关闭；不再调度新的查询。")
		}
	}
}
