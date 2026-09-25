package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jackyhuang83/MiniProbe/internal/common"
)

//go:embed web/*
var assets embed.FS

const (
	cookieName           = "miniprobe_dashboard"
	pbkdf2Iters          = 210000
	databaseVer          = 6
	serverVersion        = "0.4.11-alpha"
	selfUpdateCapability = "self-update-v1"
	defaultListen        = ":28888"
	defaultAdminSock     = "/run/miniprobe/admin.sock"
	maxRequestBody       = 1 << 20
	softNodeLimit        = 15
	storageHardLimit     = uint64(2 * 1024 * 1024 * 1024)
	dataFileMaxBytes     = 256 * 1024 * 1024
	dashboardSessionTTL  = 30 * 24 * time.Hour
	longTermExpireDate   = "2036-01-01"
)

const (
	dashboardPublic      = "public"
	dashboardProtected   = "protected"
	dashboardDisabled    = "disabled"
	accessDirect         = "direct"
	accessTunnel         = "tunnel"
	probeModeOn          = "on"
	probeModeOff         = "off"
	probeRegionBeijing   = "beijing"
	probeRegionShanghai  = "shanghai"
	probeRegionGuangzhou = "guangzhou"
	probeProtocolICMP    = "icmp"
	probeProtocolTCP     = "tcp"
	probeProtocolUDP     = "udp"
)

var trafficWarnLevels = []int{70, 85, 90}

type probeTargetSet struct {
	City    string
	Telecom string
	Unicom  string
	Mobile  string
}

func probeTargetsFor(region string) (probeTargetSet, bool) {
	switch region {
	case probeRegionBeijing:
		return probeTargetSet{City: "北京", Telecom: "219.141.136.10", Unicom: "202.106.0.20", Mobile: "221.130.33.60"}, true
	case probeRegionShanghai:
		return probeTargetSet{City: "上海", Telecom: "202.96.209.133", Unicom: "210.22.70.3", Mobile: "211.136.112.50"}, true
	case probeRegionGuangzhou:
		return probeTargetSet{City: "广州", Telecom: "202.96.128.86", Unicom: "210.21.4.130", Mobile: "211.136.192.6"}, true
	default:
		return probeTargetSet{}, false
	}
}

func validProbeRegion(v string) bool {
	_, ok := probeTargetsFor(v)
	return ok
}

func validProbeProtocol(v string) bool {
	return v == probeProtocolICMP || v == probeProtocolTCP || v == probeProtocolUDP
}

func probeRegionLabel(v string) string {
	if t, ok := probeTargetsFor(v); ok {
		return t.City
	}
	return "未知"
}

func probeProtocolLabel(v string) string {
	switch v {
	case probeProtocolICMP:
		return "ICMP"
	case probeProtocolTCP:
		return "TCP"
	case probeProtocolUDP:
		return "UDP"
	default:
		return "未知"
	}
}

type passwordConfig struct {
	Salt string `json:"salt,omitempty"`
	Hash string `json:"hash,omitempty"`
}

type telegramConfig struct {
	Enabled        bool      `json:"enabled"`
	BotToken       string    `json:"bot_token,omitempty"`
	ChatID         string    `json:"chat_id,omitempty"`
	SummaryHours   int       `json:"summary_hours"`
	OfflineMinutes int       `json:"offline_minutes"`
	LastSummaryAt  time.Time `json:"last_summary_at,omitempty"`
}

type settings struct {
	PublicURL         string         `json:"public_url"`
	AccessMode        string         `json:"access_mode"`
	PolicyVersion     int64          `json:"policy_version"`
	DashboardMode     string         `json:"dashboard_mode"`
	DashboardPassword passwordConfig `json:"dashboard_password,omitempty"`
	ProbeMode         string         `json:"probe_mode"`
	ProbeRegion       string         `json:"probe_region"`
	ProbeProtocol     string         `json:"probe_protocol"`
	Telegram          telegramConfig `json:"telegram"`
}

type nodeConfig struct {
	ID                    string    `json:"id"`
	DisplayName           string    `json:"display_name"`
	TokenNonce            string    `json:"token_nonce"`
	MonthlyTrafficLimit   uint64    `json:"monthly_traffic_limit"`
	TrafficDirection      string    `json:"traffic_direction"`
	TrafficResetDay       int       `json:"traffic_reset_day"`
	TrafficResetTZMinutes int       `json:"traffic_reset_tz_minutes"`
	ShutdownEnabled       bool      `json:"shutdown_enabled"`
	ShutdownPercent       int       `json:"shutdown_percent"`
	MonthlyPrice          float64   `json:"monthly_price"`
	Currency              string    `json:"currency"`
	ExpireAt              string    `json:"expire_at"`
	Tags                  []string  `json:"tags"`
	CreatedAt             time.Time `json:"created_at"`
}

type nodeUpdateInput struct {
	ID                    string    `json:"id"`
	DisplayName           *string   `json:"display_name,omitempty"`
	TrafficGB             *float64  `json:"traffic_gb,omitempty"`
	TrafficDirection      *string   `json:"traffic_direction,omitempty"`
	TrafficResetDay       *int      `json:"traffic_reset_day,omitempty"`
	TrafficResetTZMinutes *int      `json:"traffic_reset_tz_minutes,omitempty"`
	ShutdownEnabled       *bool     `json:"shutdown_enabled,omitempty"`
	ShutdownPercent       *int      `json:"shutdown_percent,omitempty"`
	MonthlyPrice          *float64  `json:"monthly_price,omitempty"`
	Currency              *string   `json:"currency,omitempty"`
	ExpireAt              *string   `json:"expire_at,omitempty"`
	Tags                  *[]string `json:"tags,omitempty"`
}

type agentUpgradeRequest struct {
	RequestID     string    `json:"request_id"`
	TargetVersion string    `json:"target_version"`
	RequestedAt   time.Time `json:"requested_at"`
}

type agentAsset struct {
	Name   string
	SHA256 string
	Size   int64
}

type notificationState struct {
	OfflineAlerted  bool      `json:"offline_alerted,omitempty"`
	OfflineSince    time.Time `json:"offline_since,omitempty"`
	TrafficCycle    time.Time `json:"traffic_cycle,omitempty"`
	TrafficLevel    int       `json:"traffic_level,omitempty"`
	SummaryInbound  uint64    `json:"summary_inbound,omitempty"`
	SummaryOutbound uint64    `json:"summary_outbound,omitempty"`
	SummaryCycle    time.Time `json:"summary_cycle,omitempty"`
}

type database struct {
	Version       int                            `json:"version"`
	Secret        string                         `json:"secret"`
	Settings      settings                       `json:"settings"`
	Nodes         map[string]nodeConfig          `json:"nodes"`
	States        map[string]common.NodeView     `json:"states"`
	Notifications map[string]notificationState   `json:"notifications,omitempty"`
	AgentUpgrades map[string]agentUpgradeRequest `json:"agent_upgrades,omitempty"`
}

type loginFailures struct {
	Times []time.Time
}

type server struct {
	mu           sync.RWMutex
	db           database
	secret       []byte
	dataFile     string
	downloadsDir string
	adminSocket  string
	agentAssets  map[string]agentAsset

	loginMu  sync.Mutex
	failures map[string]loginFailures
	notifyCh chan string
}

type adminNodeView struct {
	ID                    string                 `json:"id"`
	DisplayName           string                 `json:"display_name"`
	MonthlyTrafficLimit   uint64                 `json:"monthly_traffic_limit"`
	TrafficDirection      string                 `json:"traffic_direction"`
	TrafficResetDay       int                    `json:"traffic_reset_day"`
	TrafficResetTZMinutes int                    `json:"traffic_reset_tz_minutes"`
	ShutdownEnabled       bool                   `json:"shutdown_enabled"`
	ShutdownPercent       int                    `json:"shutdown_percent"`
	MonthlyPrice          float64                `json:"monthly_price"`
	Currency              string                 `json:"currency"`
	ExpireAt              string                 `json:"expire_at"`
	Tags                  []string               `json:"tags"`
	CreatedAt             time.Time              `json:"created_at"`
	Online                bool                   `json:"online"`
	LastSeen              time.Time              `json:"last_seen"`
	ObservedIP            string                 `json:"observed_ip"`
	AgentVersion          string                 `json:"agent_version"`
	AgentEndpoint         string                 `json:"agent_endpoint"`
	Capabilities          []string               `json:"capabilities,omitempty"`
	UpgradeRequested      bool                   `json:"upgrade_requested"`
	PolicyVersion         int64                  `json:"policy_version"`
	Traffic               common.TrafficSnapshot `json:"traffic"`
}

func main() {
	listen := flag.String("listen", getenv("MINIPROBE_LISTEN", defaultListen), "listen address")
	dataDir := flag.String("data-dir", getenv("MINIPROBE_DATA_DIR", "./data"), "data directory")
	downloads := flag.String("downloads", getenv("MINIPROBE_DOWNLOADS", "./dist"), "directory containing agent release binaries")
	adminSocket := flag.String("admin-socket", getenv("MINIPROBE_ADMIN_SOCKET", defaultAdminSock), "local-only Unix admin socket")
	initOnly := flag.Bool("init-only", false, "initialize database and exit")
	publicURL := flag.String("public-url", getenv("MINIPROBE_PUBLIC_URL", ""), "agent-facing public URL used only when initializing")
	initDashboardMode := flag.String("dashboard-mode", getenv("MINIPROBE_DASHBOARD_MODE", dashboardProtected), "initial dashboard mode: public, protected, disabled")
	initDashboardPassword := flag.String("dashboard-password", getenv("MINIPROBE_DASHBOARD_PASSWORD", ""), "initial dashboard password when protected")
	manage := flag.Bool("manage", false, "open local management menu")
	flag.Parse()

	if *manage {
		if err := runManager(*adminSocket); err != nil {
			fmt.Fprintln(os.Stderr, "MiniProbe 管理失败:", err)
			os.Exit(1)
		}
		return
	}

	dataFile := filepath.Join(*dataDir, "miniprobe.json")
	s := &server{
		dataFile: dataFile, downloadsDir: *downloads, adminSocket: *adminSocket,
		failures: map[string]loginFailures{}, notifyCh: make(chan string, 128),
	}
	created, generatedDashboardPass, err := s.loadOrInit(*publicURL, *initDashboardMode, *initDashboardPassword)
	if err != nil {
		log.Fatal(err)
	}
	s.agentAssets = loadAgentAssets(s.downloadsDir)
	if created {
		log.Printf("MiniProbe initialized. Dashboard mode: %s", s.db.Settings.DashboardMode)
		if generatedDashboardPass != "" {
			log.Printf("MiniProbe generated dashboard password: %s", generatedDashboardPass)
		}
	}
	if *initOnly {
		if generatedDashboardPass != "" {
			fmt.Printf("MINIPROBE_DASHBOARD_PASSWORD=%s\n", generatedDashboardPass)
		}
		return
	}

	go s.persistenceLoop()
	go s.notificationLoop()
	go s.offlineMonitorLoop()
	go s.trafficSummaryLoop()

	if err := s.startAdminSocket(); err != nil {
		log.Fatal(err)
	}

	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/api/v1/report", s.report)
	publicMux.HandleFunc("/api/v1/nodes", s.dashboardOnly(s.nodesAPI))
	publicMux.HandleFunc("/api/v1/dashboard/session", s.dashboardSessionInfo)
	publicMux.HandleFunc("/api/v1/dashboard/login", s.dashboardLogin)
	publicMux.HandleFunc("/api/v1/dashboard/logout", s.dashboardLogout)
	publicMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	publicMux.HandleFunc("/install-agent.sh", s.installAgentScript)
	if _, err := os.Stat(s.downloadsDir); err == nil {
		publicMux.Handle("/downloads/", http.StripPrefix("/downloads/", http.FileServer(http.Dir(s.downloadsDir))))
	}
	sub, _ := fs.Sub(assets, "web")
	publicMux.Handle("/", s.dashboardAssets(http.FileServer(http.FS(sub))))

	srv := &http.Server{
		Addr: *listen, Handler: secureHeaders(publicMux), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	log.Printf("MiniProbe server listening on %s", *listen)
	log.Fatal(srv.ListenAndServe())
}

func (s *server) loadOrInit(publicURL, dashboardMode, dashboardPassword string) (bool, string, error) {
	b, err := os.ReadFile(s.dataFile)
	if err == nil {
		if err := json.Unmarshal(b, &s.db); err != nil {
			return false, "", fmt.Errorf("decode database: %w", err)
		}
		if s.db.Nodes == nil {
			s.db.Nodes = map[string]nodeConfig{}
		}
		if s.db.States == nil {
			s.db.States = map[string]common.NodeView{}
		}
		if s.db.Notifications == nil {
			s.db.Notifications = map[string]notificationState{}
		}
		if s.db.AgentUpgrades == nil {
			s.db.AgentUpgrades = map[string]agentUpgradeRequest{}
		}
		sec, err := base64.RawURLEncoding.DecodeString(s.db.Secret)
		if err != nil || len(sec) < 32 {
			return false, "", errors.New("database secret is invalid")
		}
		s.secret = sec
		changed := false
		if !validDashboardMode(s.db.Settings.DashboardMode) {
			// v0.2.x had no dashboard mode. Migrate it to public so upgrades do not lock users out.
			s.db.Settings.DashboardMode = dashboardPublic
			changed = true
		}
		if s.db.Settings.AccessMode != accessDirect && s.db.Settings.AccessMode != accessTunnel {
			s.db.Settings.AccessMode = accessDirect
			changed = true
		}
		if s.db.Settings.PolicyVersion < 1 {
			s.db.Settings.PolicyVersion = 1
			changed = true
		}
		if s.db.Settings.ProbeMode != probeModeOn && s.db.Settings.ProbeMode != probeModeOff {
			s.db.Settings.ProbeMode = probeModeOn
			changed = true
		}
		if !validProbeRegion(s.db.Settings.ProbeRegion) {
			s.db.Settings.ProbeRegion = probeRegionGuangzhou
			changed = true
		}
		if !validProbeProtocol(s.db.Settings.ProbeProtocol) {
			s.db.Settings.ProbeProtocol = probeProtocolICMP
			changed = true
		}
		if s.db.Settings.Telegram.SummaryHours == 0 {
			s.db.Settings.Telegram.SummaryHours = 4
			changed = true
		}
		if s.db.Settings.Telegram.OfflineMinutes == 0 {
			s.db.Settings.Telegram.OfflineMinutes = 2
			changed = true
		}
		for id, cfg := range s.db.Nodes {
			if cfg.TrafficDirection == "" {
				cfg.TrafficDirection = "total"
				changed = true
			}
			if cfg.TrafficResetDay < 1 || cfg.TrafficResetDay > 28 {
				cfg.TrafficResetDay = 1
				changed = true
			}
			if cfg.ShutdownPercent == 0 {
				cfg.ShutdownPercent = 95
				changed = true
			}
			s.db.Nodes[id] = cfg
		}
		if s.db.Version < databaseVer {
			s.db.Version = databaseVer
			// v5 adds signed global probe settings; bump policy so existing Agents
			// receive the new city/protocol selection immediately after upgrade.
			s.db.Settings.PolicyVersion++
			changed = true
		}
		if changed {
			if err := s.save(); err != nil {
				return false, "", err
			}
		}
		return false, "", nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, "", err
	}
	if err := os.MkdirAll(filepath.Dir(s.dataFile), 0750); err != nil {
		return false, "", err
	}
	if publicURL != "" {
		u, err := validatePublicURL(publicURL)
		if err != nil {
			return false, "", err
		}
		publicURL = strings.TrimRight(u.String(), "/")
	}
	if !validDashboardMode(dashboardMode) {
		return false, "", errors.New("dashboard mode must be public, protected, or disabled")
	}
	generated := ""
	var dashboardHash passwordConfig
	if dashboardMode == dashboardProtected {
		if dashboardPassword == "" {
			dashboardPassword = randomPassword(20)
			generated = dashboardPassword
		}
		if len(dashboardPassword) < 10 {
			return false, "", errors.New("dashboard password must be at least 10 characters")
		}
		dashboardHash = hashPassword(dashboardPassword)
	}
	sec := randomBytes(32)
	s.db = database{
		Version: databaseVer, Secret: base64.RawURLEncoding.EncodeToString(sec),
		Settings: settings{PublicURL: publicURL, AccessMode: accessDirect, PolicyVersion: 1, DashboardMode: dashboardMode, DashboardPassword: dashboardHash,
			ProbeMode: probeModeOn, ProbeRegion: probeRegionGuangzhou, ProbeProtocol: probeProtocolICMP,
			Telegram: telegramConfig{SummaryHours: 4, OfflineMinutes: 2}},
		Nodes: map[string]nodeConfig{}, States: map[string]common.NodeView{}, Notifications: map[string]notificationState{}, AgentUpgrades: map[string]agentUpgradeRequest{},
	}
	s.secret = sec
	if err := s.save(); err != nil {
		return false, "", err
	}
	return true, generated, nil
}

func (s *server) report(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	nodeFromToken, ok := s.authAgent(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	defer r.Body.Close()
	var rep common.Report
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err := dec.Decode(&rep); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if rep.NodeID == "" || rep.NodeID != nodeFromToken {
		http.Error(w, "token does not match node_id", http.StatusForbidden)
		return
	}

	now := time.Now().UTC()
	var messages []string
	s.mu.Lock()
	cfg, exists := s.db.Nodes[rep.NodeID]
	if !exists {
		s.mu.Unlock()
		http.Error(w, "node not registered", http.StatusForbidden)
		return
	}
	previous := s.db.States[rep.NodeID]
	nv := common.NodeView{
		Report: rep, DisplayName: cfg.DisplayName, Online: true, ObservedIP: remoteIP(r.RemoteAddr), LastSeen: now,
		MonthlyTrafficLimit: cfg.MonthlyTrafficLimit, TrafficDirection: cfg.TrafficDirection,
		TrafficResetDay: cfg.TrafficResetDay, TrafficResetTZ: formatTZOffset(cfg.TrafficResetTZMinutes),
		ShutdownEnabled: cfg.ShutdownEnabled, ShutdownPercent: cfg.ShutdownPercent,
		MonthlyPrice: cfg.MonthlyPrice, Currency: cfg.Currency, ExpireAt: cfg.ExpireAt, Tags: append([]string(nil), cfg.Tags...),
	}
	s.db.States[rep.NodeID] = nv

	ns := s.db.Notifications[rep.NodeID]
	if ns.OfflineAlerted {
		since := ns.OfflineSince
		if since.IsZero() {
			since = previous.LastSeen
		}
		dur := time.Duration(0)
		if !since.IsZero() {
			dur = now.Sub(since)
		}
		messages = append(messages, fmt.Sprintf("🟢 MiniProbe 节点恢复\n\n节点：%s\n离线时长：%s\n当前状态：Online", cfg.DisplayName, humanDuration(dur)))
		ns.OfflineAlerted = false
		ns.OfflineSince = time.Time{}
	}
	if !rep.Traffic.CycleStart.IsZero() && !ns.TrafficCycle.Equal(rep.Traffic.CycleStart) {
		ns.TrafficCycle = rep.Traffic.CycleStart
		ns.TrafficLevel = 0
	}
	level := highestWarnLevel(rep.Traffic.UsagePercent)
	if cfg.MonthlyTrafficLimit > 0 && level > ns.TrafficLevel {
		ns.TrafficLevel = level
		messages = append(messages, fmt.Sprintf("⚠️ MiniProbe 流量告警\n\n节点：%s\n入网：%s\n出网：%s\n计费流量：%s / %s\n使用率：%.1f%%\n计费方向：%s",
			cfg.DisplayName, formatTrafficBytes(rep.Traffic.InboundBytes), formatTrafficBytes(rep.Traffic.OutboundBytes), formatTrafficBytes(rep.Traffic.UsedBytes),
			formatTrafficBytes(cfg.MonthlyTrafficLimit), rep.Traffic.UsagePercent, trafficDirectionLabel(cfg.TrafficDirection)))
	}
	if rep.Traffic.ProtectionTriggered && ns.TrafficLevel < cfg.ShutdownPercent {
		ns.TrafficLevel = cfg.ShutdownPercent
		messages = append(messages, fmt.Sprintf("🔴 MiniProbe 流量保护触发\n\n节点：%s\n计费流量：%s / %s\n使用率：%.1f%%\n保护阈值：%d%%\nAgent 将执行保护关机。",
			cfg.DisplayName, formatTrafficBytes(rep.Traffic.UsedBytes), formatTrafficBytes(cfg.MonthlyTrafficLimit), rep.Traffic.UsagePercent, cfg.ShutdownPercent))
	}
	s.db.Notifications[rep.NodeID] = ns
	policy := s.signedPolicyLocked(cfg)
	probePolicy := s.signedProbePolicyLocked(cfg)
	upgradePolicy := s.signedUpgradePolicyLocked(cfg, rep)
	if req, ok := s.db.AgentUpgrades[rep.NodeID]; ok && rep.AgentVersion == req.TargetVersion {
		delete(s.db.AgentUpgrades, rep.NodeID)
	}
	s.mu.Unlock()

	for _, msg := range messages {
		s.enqueueNotification(msg)
	}
	writeJSON(w, http.StatusOK, common.ReportResponse{OK: true, Policy: policy, ProbePolicy: probePolicy, UpgradePolicy: upgradePolicy})
}

func (s *server) signedPolicyLocked(cfg nodeConfig) *common.SignedPolicy {
	p := common.AgentPolicy{
		Version: s.db.Settings.PolicyVersion, NodeID: cfg.ID, Endpoint: s.db.Settings.PublicURL,
		TrafficLimitBytes: cfg.MonthlyTrafficLimit, TrafficDirection: cfg.TrafficDirection,
		TrafficResetDay: cfg.TrafficResetDay, TrafficResetTZMinutes: cfg.TrafficResetTZMinutes,
		ShutdownEnabled: cfg.ShutdownEnabled, ShutdownPercent: cfg.ShutdownPercent,
	}
	payload, _ := json.Marshal(p)
	priv := ed25519.NewKeyFromSeed(s.secret[:ed25519.SeedSize])
	sig := ed25519.Sign(priv, payload)
	return &common.SignedPolicy{Policy: p, Signature: base64.RawURLEncoding.EncodeToString(sig)}
}

func (s *server) signedProbePolicyLocked(cfg nodeConfig) *common.SignedProbePolicy {
	targets, _ := probeTargetsFor(s.db.Settings.ProbeRegion)
	p := common.ProbePolicy{
		Version: s.db.Settings.PolicyVersion, NodeID: cfg.ID, Enabled: s.db.Settings.ProbeMode != probeModeOff,
		Region: s.db.Settings.ProbeRegion, Protocol: s.db.Settings.ProbeProtocol,
		Telecom: targets.Telecom, Unicom: targets.Unicom, Mobile: targets.Mobile,
	}
	payload, _ := json.Marshal(p)
	priv := ed25519.NewKeyFromSeed(s.secret[:ed25519.SeedSize])
	sig := ed25519.Sign(priv, payload)
	return &common.SignedProbePolicy{Policy: p, Signature: base64.RawURLEncoding.EncodeToString(sig)}
}

func (s *server) signedUpgradePolicyLocked(cfg nodeConfig, rep common.Report) *common.SignedUpgradePolicy {
	req, ok := s.db.AgentUpgrades[cfg.ID]
	if !ok || req.TargetVersion == "" || rep.AgentVersion == req.TargetVersion || !hasCapability(rep.Capabilities, selfUpdateCapability) {
		return nil
	}
	arch := normalizeAgentArch(rep.Info.Arch)
	asset, ok := s.agentAssets[arch]
	if !ok {
		return nil
	}
	p := common.UpgradePolicy{
		RequestID: req.RequestID, NodeID: cfg.ID, TargetVersion: req.TargetVersion,
		Asset: asset.Name, SHA256: asset.SHA256, Size: asset.Size,
	}
	payload, _ := json.Marshal(p)
	priv := ed25519.NewKeyFromSeed(s.secret[:ed25519.SeedSize])
	sig := ed25519.Sign(priv, payload)
	return &common.SignedUpgradePolicy{Policy: p, Signature: base64.RawURLEncoding.EncodeToString(sig)}
}

func normalizeAgentArch(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "amd64", "x86_64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	case "arm", "armv7", "armv7l":
		return "armv7"
	default:
		return ""
	}
}

func hasCapability(caps []string, want string) bool {
	for _, c := range caps {
		if strings.TrimSpace(c) == want {
			return true
		}
	}
	return false
}

func loadAgentAssets(dir string) map[string]agentAsset {
	out := map[string]agentAsset{}
	for arch, name := range map[string]string{
		"amd64": "miniprobe-agent-linux-amd64",
		"arm64": "miniprobe-agent-linux-arm64",
		"armv7": "miniprobe-agent-linux-armv7",
	} {
		path := filepath.Join(dir, name)
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(f, 64<<20))
		_ = f.Close()
		if err != nil || n <= 0 {
			continue
		}
		out[arch] = agentAsset{Name: name, SHA256: fmt.Sprintf("%x", h.Sum(nil)), Size: n}
	}
	return out
}

func (s *server) serverPublicKey() string {
	s.mu.RLock()
	secret := append([]byte(nil), s.secret...)
	s.mu.RUnlock()
	priv := ed25519.NewKeyFromSeed(secret[:ed25519.SeedSize])
	pub := priv.Public().(ed25519.PublicKey)
	return base64.RawURLEncoding.EncodeToString(pub)
}

func (s *server) nodesAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	now := time.Now()
	s.mu.RLock()
	out := make([]common.NodeView, 0, len(s.db.Nodes))
	for id, cfg := range s.db.Nodes {
		v, ok := s.db.States[id]
		if !ok {
			v.Report.NodeID = id
		}
		v.DisplayName = cfg.DisplayName
		v.MonthlyTrafficLimit = cfg.MonthlyTrafficLimit
		v.TrafficDirection = cfg.TrafficDirection
		v.TrafficResetDay = cfg.TrafficResetDay
		v.TrafficResetTZ = formatTZOffset(cfg.TrafficResetTZMinutes)
		v.ShutdownEnabled = cfg.ShutdownEnabled
		v.ShutdownPercent = cfg.ShutdownPercent
		v.MonthlyPrice = cfg.MonthlyPrice
		v.Currency = cfg.Currency
		v.ExpireAt = cfg.ExpireAt
		v.Tags = append([]string(nil), cfg.Tags...)
		// ObservedIP is useful for local SSH administration, but the Dashboard
		// API must not expose node public IPs (especially in Public mode).
		v.ObservedIP = ""
		v.Online = !v.LastSeen.IsZero() && now.Sub(v.LastSeen) < 20*time.Second
		out = append(out, v)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].DisplayName < out[j].DisplayName })
	if len(out) > softNodeLimit {
		w.Header().Set("X-MiniProbe-Node-Warning", fmt.Sprintf("%d nodes exceeds recommended limit %d", len(out), softNodeLimit))
	}
	noStore(w)
	writeJSON(w, http.StatusOK, out)
}

func (s *server) dashboardSessionInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	noStore(w)
	mode := s.dashboardMode()
	authenticated := mode == dashboardPublic || (mode == dashboardProtected && s.hasDashboardSession(r))
	writeJSON(w, http.StatusOK, map[string]any{
		"mode": mode, "authenticated": authenticated, "secure": requestIsTLS(r), "trust_days": 30,
	})
}

func (s *server) dashboardLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	noStore(w)
	if s.dashboardMode() != dashboardProtected {
		http.NotFound(w, r)
		return
	}
	ip := remoteIP(r.RemoteAddr)
	if !s.loginAllowed(ip) {
		http.Error(w, "too many login attempts", http.StatusTooManyRequests)
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	pc := s.db.Settings.DashboardPassword
	s.mu.RUnlock()
	if !verifyPassword(in.Password, pc) {
		s.noteLoginFailure(ip)
		time.Sleep(250 * time.Millisecond)
		http.Error(w, "invalid password", http.StatusUnauthorized)
		return
	}
	s.clearLoginFailures(ip)
	expires := time.Now().Add(dashboardSessionTTL)
	token := s.newDashboardSessionToken(expires)
	cookie := &http.Cookie{
		Name: cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Expires: expires, MaxAge: int(dashboardSessionTTL / time.Second),
	}
	if requestIsTLS(r) {
		cookie.Secure = true
	}
	http.SetCookie(w, cookie)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "secure": requestIsTLS(r), "trusted_until": expires.UTC()})
}

func (s *server) dashboardLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !validSameOriginMutation(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	cookie := &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode}
	if requestIsTLS(r) {
		cookie.Secure = true
	}
	http.SetCookie(w, cookie)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) dashboardOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mode := s.dashboardMode()
		switch mode {
		case dashboardPublic:
			next(w, r)
		case dashboardProtected:
			if !s.hasDashboardSession(r) {
				noStore(w)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, r)
		case dashboardDisabled:
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}
}

func (s *server) dashboardAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.dashboardMode() == dashboardDisabled {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) dashboardMode() string {
	s.mu.RLock()
	mode := s.db.Settings.DashboardMode
	s.mu.RUnlock()
	if !validDashboardMode(mode) {
		return dashboardDisabled
	}
	return mode
}

func (s *server) hasDashboardSession(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil || len(c.Value) < 20 {
		return false
	}
	return s.validDashboardSessionToken(c.Value, time.Now())
}

func (s *server) newDashboardSessionToken(expires time.Time) string {
	payload := make([]byte, 8+16)
	binary.BigEndian.PutUint64(payload[:8], uint64(expires.Unix()))
	copy(payload[8:], randomBytes(16))
	mac := s.dashboardSessionMAC(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac...))
}

func (s *server) validDashboardSessionToken(raw string, now time.Time) bool {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(b) != 8+16+sha256.Size {
		return false
	}
	payload, gotMAC := b[:24], b[24:]
	expiresUnix := int64(binary.BigEndian.Uint64(payload[:8]))
	if expiresUnix <= now.Unix() || expiresUnix > now.Add(dashboardSessionTTL+5*time.Minute).Unix() {
		return false
	}
	wantMAC := s.dashboardSessionMAC(payload)
	return hmac.Equal(gotMAC, wantMAC)
}

func (s *server) dashboardSessionMAC(payload []byte) []byte {
	// Bind long-lived sessions to the current Dashboard password hash. Changing
	// the password therefore invalidates every previously trusted device without
	// storing raw session tokens or passwords on disk.
	s.mu.RLock()
	passwordHash := s.db.Settings.DashboardPassword.Hash
	s.mu.RUnlock()
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte("miniprobe-dashboard-session-v1\x00"))
	_, _ = mac.Write([]byte(passwordHash))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func (s *server) installAgentScript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b, err := assets.ReadFile("web/install-agent.sh")
	if err != nil {
		http.Error(w, "installer unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(b)
}

func (s *server) authAgent(r *http.Request) (string, bool) {
	v := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(v, "Bearer ") {
		return "", false
	}
	parts := strings.Split(strings.TrimSpace(strings.TrimPrefix(v, "Bearer ")), ".")
	if len(parts) != 3 {
		return "", false
	}
	nodeBytes, e1 := base64.RawURLEncoding.DecodeString(parts[0])
	nonce, e2 := base64.RawURLEncoding.DecodeString(parts[1])
	sig, e3 := base64.RawURLEncoding.DecodeString(parts[2])
	if e1 != nil || e2 != nil || e3 != nil || len(nodeBytes) == 0 || len(nonce) != 16 {
		return "", false
	}
	id := string(nodeBytes)
	s.mu.RLock()
	cfg, ok := s.db.Nodes[id]
	secret := append([]byte(nil), s.secret...)
	s.mu.RUnlock()
	if !ok || cfg.TokenNonce != parts[1] {
		return "", false
	}
	m := hmac.New(sha256.New, secret)
	m.Write(nodeBytes)
	m.Write([]byte{0})
	m.Write(nonce)
	if !hmac.Equal(sig, m.Sum(nil)) {
		return "", false
	}
	return id, true
}

func (s *server) issueNodeTokenLocked(cfg nodeConfig) string {
	nonce, _ := base64.RawURLEncoding.DecodeString(cfg.TokenNonce)
	node := []byte(cfg.ID)
	m := hmac.New(sha256.New, s.secret)
	m.Write(node)
	m.Write([]byte{0})
	m.Write(nonce)
	return base64.RawURLEncoding.EncodeToString(node) + "." + cfg.TokenNonce + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// startAdminSocket creates the management plane on a root-only Unix socket.
// None of these handlers are registered on the public TCP listener.
func (s *server) startAdminSocket() error {
	if strings.TrimSpace(s.adminSocket) == "" {
		return errors.New("admin socket path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.adminSocket), 0750); err != nil {
		return fmt.Errorf("create admin socket directory: %w", err)
	}
	_ = os.Remove(s.adminSocket)
	ln, err := net.Listen("unix", s.adminSocket)
	if err != nil {
		return fmt.Errorf("listen admin socket: %w", err)
	}
	if err := os.Chmod(s.adminSocket, 0600); err != nil {
		_ = ln.Close()
		return fmt.Errorf("chmod admin socket: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/config", s.localConfig)
	mux.HandleFunc("/v1/nodes", s.localNodes)
	mux.HandleFunc("/v1/node-command", s.localNodeCommand)
	mux.HandleFunc("/v1/agent-upgrade", s.localAgentUpgrade)
	mux.HandleFunc("/v1/telegram-test", s.localTelegramTest)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("admin socket server: %v", err)
		}
	}()
	log.Printf("MiniProbe local management socket: %s", s.adminSocket)
	return nil
}

func (s *server) localConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		tg := s.db.Settings.Telegram
		out := map[string]any{
			"public_url":               s.db.Settings.PublicURL,
			"access_mode":              s.db.Settings.AccessMode,
			"policy_version":           s.db.Settings.PolicyVersion,
			"dashboard_mode":           s.db.Settings.DashboardMode,
			"has_dashboard_password":   s.db.Settings.DashboardPassword.Hash != "",
			"probe_mode":               s.db.Settings.ProbeMode,
			"probe_region":             s.db.Settings.ProbeRegion,
			"probe_region_label":       probeRegionLabel(s.db.Settings.ProbeRegion),
			"probe_protocol":           s.db.Settings.ProbeProtocol,
			"probe_protocol_label":     probeProtocolLabel(s.db.Settings.ProbeProtocol),
			"telegram_enabled":         tg.Enabled,
			"telegram_has_token":       tg.BotToken != "",
			"telegram_chat_id":         tg.ChatID,
			"telegram_summary_hours":   tg.SummaryHours,
			"telegram_offline_minutes": tg.OfflineMinutes,
			"node_soft_limit":          softNodeLimit,
			"storage_hard_limit":       storageHardLimit,
		}
		s.mu.RUnlock()
		out["storage_used"] = s.storageUsage()
		writeJSON(w, http.StatusOK, out)
	case http.MethodPut:
		var in struct {
			PublicURL              *string `json:"public_url"`
			AccessMode             *string `json:"access_mode"`
			DashboardMode          *string `json:"dashboard_mode"`
			DashboardPassword      *string `json:"dashboard_password"`
			ProbeMode              *string `json:"probe_mode"`
			ProbeRegion            *string `json:"probe_region"`
			ProbeProtocol          *string `json:"probe_protocol"`
			TelegramEnabled        *bool   `json:"telegram_enabled"`
			TelegramBotToken       *string `json:"telegram_bot_token"`
			TelegramChatID         *string `json:"telegram_chat_id"`
			TelegramSummaryHours   *int    `json:"telegram_summary_hours"`
			TelegramOfflineMinutes *int    `json:"telegram_offline_minutes"`
		}
		if err := decodeJSON(w, r, &in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		policyChanged := false
		if in.PublicURL != nil {
			u, err := validatePublicURL(*in.PublicURL)
			if err != nil {
				s.mu.Unlock()
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			v := strings.TrimRight(u.String(), "/")
			if v != s.db.Settings.PublicURL {
				s.db.Settings.PublicURL = v
				policyChanged = true
			}
		}
		if in.AccessMode != nil {
			mode := strings.TrimSpace(*in.AccessMode)
			if mode != accessDirect && mode != accessTunnel {
				s.mu.Unlock()
				http.Error(w, "invalid access mode", http.StatusBadRequest)
				return
			}
			s.db.Settings.AccessMode = mode
		}
		if in.DashboardMode != nil {
			mode := strings.TrimSpace(*in.DashboardMode)
			if !validDashboardMode(mode) {
				s.mu.Unlock()
				http.Error(w, "invalid dashboard mode", http.StatusBadRequest)
				return
			}
			if mode == dashboardProtected {
				if in.DashboardPassword != nil && *in.DashboardPassword != "" {
					if len(*in.DashboardPassword) < 10 {
						s.mu.Unlock()
						http.Error(w, "dashboard password must be at least 10 characters", http.StatusBadRequest)
						return
					}
					s.db.Settings.DashboardPassword = hashPassword(*in.DashboardPassword)
				} else if s.db.Settings.DashboardPassword.Hash == "" {
					s.mu.Unlock()
					http.Error(w, "dashboard password is required", http.StatusBadRequest)
					return
				}
			}
			s.db.Settings.DashboardMode = mode
		}
		if in.DashboardPassword != nil && *in.DashboardPassword != "" && in.DashboardMode == nil {
			if len(*in.DashboardPassword) < 10 {
				s.mu.Unlock()
				http.Error(w, "dashboard password must be at least 10 characters", http.StatusBadRequest)
				return
			}
			s.db.Settings.DashboardPassword = hashPassword(*in.DashboardPassword)
		}
		if in.ProbeMode != nil {
			mode := strings.TrimSpace(strings.ToLower(*in.ProbeMode))
			if mode != probeModeOn && mode != probeModeOff {
				s.mu.Unlock()
				http.Error(w, "probe mode must be on or off", http.StatusBadRequest)
				return
			}
			if mode != s.db.Settings.ProbeMode {
				s.db.Settings.ProbeMode = mode
				policyChanged = true
			}
		}
		if in.ProbeRegion != nil {
			region := strings.TrimSpace(strings.ToLower(*in.ProbeRegion))
			if !validProbeRegion(region) {
				s.mu.Unlock()
				http.Error(w, "probe region must be beijing, shanghai, or guangzhou", http.StatusBadRequest)
				return
			}
			if region != s.db.Settings.ProbeRegion {
				s.db.Settings.ProbeRegion = region
				policyChanged = true
			}
		}
		if in.ProbeProtocol != nil {
			protocol := strings.TrimSpace(strings.ToLower(*in.ProbeProtocol))
			if !validProbeProtocol(protocol) {
				s.mu.Unlock()
				http.Error(w, "probe protocol must be icmp, tcp, or udp", http.StatusBadRequest)
				return
			}
			if protocol != s.db.Settings.ProbeProtocol {
				s.db.Settings.ProbeProtocol = protocol
				policyChanged = true
			}
		}

		if in.TelegramBotToken != nil {
			s.db.Settings.Telegram.BotToken = strings.TrimSpace(*in.TelegramBotToken)
		}
		if in.TelegramChatID != nil {
			s.db.Settings.Telegram.ChatID = strings.TrimSpace(*in.TelegramChatID)
		}
		if in.TelegramSummaryHours != nil {
			if *in.TelegramSummaryHours != 0 && *in.TelegramSummaryHours != 4 && *in.TelegramSummaryHours != 8 && *in.TelegramSummaryHours != 12 && *in.TelegramSummaryHours != 24 {
				s.mu.Unlock()
				http.Error(w, "summary hours must be 0, 4, 8, 12, or 24", http.StatusBadRequest)
				return
			}
			s.db.Settings.Telegram.SummaryHours = *in.TelegramSummaryHours
			s.db.Settings.Telegram.LastSummaryAt = time.Now().UTC()
		}
		if in.TelegramOfflineMinutes != nil {
			if *in.TelegramOfflineMinutes < 1 || *in.TelegramOfflineMinutes > 60 {
				s.mu.Unlock()
				http.Error(w, "offline minutes must be 1..60", http.StatusBadRequest)
				return
			}
			s.db.Settings.Telegram.OfflineMinutes = *in.TelegramOfflineMinutes
		}
		if in.TelegramEnabled != nil {
			if *in.TelegramEnabled && (s.db.Settings.Telegram.BotToken == "" || s.db.Settings.Telegram.ChatID == "") {
				s.mu.Unlock()
				http.Error(w, "Telegram token and chat id are required", http.StatusBadRequest)
				return
			}
			s.db.Settings.Telegram.Enabled = *in.TelegramEnabled
			if *in.TelegramEnabled && s.db.Settings.Telegram.LastSummaryAt.IsZero() {
				s.db.Settings.Telegram.LastSummaryAt = time.Now().UTC()
			}
		}
		if policyChanged {
			s.db.Settings.PolicyVersion++
		}
		s.mu.Unlock()
		if err := s.save(); err != nil {
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) localTelegramTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.sendTelegram("✅ MiniProbe Telegram 测试成功\n\n通知功能已经可以正常使用。"); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) localNodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		now := time.Now()
		s.mu.RLock()
		out := make([]adminNodeView, 0, len(s.db.Nodes))
		for id, cfg := range s.db.Nodes {
			v := s.db.States[id]
			_, upgradeRequested := s.db.AgentUpgrades[id]
			out = append(out, adminNodeView{
				ID: id, DisplayName: cfg.DisplayName, MonthlyTrafficLimit: cfg.MonthlyTrafficLimit,
				TrafficDirection: cfg.TrafficDirection, TrafficResetDay: cfg.TrafficResetDay, TrafficResetTZMinutes: cfg.TrafficResetTZMinutes,
				ShutdownEnabled: cfg.ShutdownEnabled, ShutdownPercent: cfg.ShutdownPercent,
				MonthlyPrice: cfg.MonthlyPrice, Currency: cfg.Currency, ExpireAt: cfg.ExpireAt, Tags: append([]string(nil), cfg.Tags...), CreatedAt: cfg.CreatedAt,
				Online: !v.LastSeen.IsZero() && now.Sub(v.LastSeen) < 20*time.Second, LastSeen: v.LastSeen, ObservedIP: v.ObservedIP,
				AgentVersion: v.AgentVersion, AgentEndpoint: v.AgentEndpoint, Capabilities: append([]string(nil), v.Capabilities...), UpgradeRequested: upgradeRequested, PolicyVersion: v.PolicyVersion, Traffic: v.Traffic,
			})
		}
		s.mu.RUnlock()
		sort.Slice(out, func(i, j int) bool { return out[i].DisplayName < out[j].DisplayName })
		writeJSON(w, http.StatusOK, out)

	case http.MethodPost:
		var in struct {
			DisplayName           string   `json:"display_name"`
			TrafficGB             float64  `json:"traffic_gb"`
			TrafficDirection      string   `json:"traffic_direction"`
			TrafficResetDay       int      `json:"traffic_reset_day"`
			TrafficResetTZMinutes int      `json:"traffic_reset_tz_minutes"`
			ShutdownEnabled       bool     `json:"shutdown_enabled"`
			ShutdownPercent       int      `json:"shutdown_percent"`
			MonthlyPrice          float64  `json:"monthly_price"`
			Currency              string   `json:"currency"`
			ExpireAt              string   `json:"expire_at"`
			Tags                  []string `json:"tags"`
		}
		if err := decodeJSON(w, r, &in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		cfg, err := validateNodeInput("", in.DisplayName, in.TrafficGB, in.TrafficDirection, in.TrafficResetDay, in.TrafficResetTZMinutes, in.ShutdownEnabled, in.ShutdownPercent, in.MonthlyPrice, in.Currency, in.ExpireAt, in.Tags)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cfg.ID = "node-" + hex.EncodeToString(randomBytes(5))
		cfg.TokenNonce = base64.RawURLEncoding.EncodeToString(randomBytes(16))
		cfg.CreatedAt = time.Now().UTC()
		s.mu.Lock()
		s.db.Nodes[cfg.ID] = cfg
		public := s.db.Settings.PublicURL
		token := s.issueNodeTokenLocked(cfg)
		nodeCount := len(s.db.Nodes)
		s.mu.Unlock()
		if err := s.save(); err != nil {
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
		if nodeCount == softNodeLimit+1 {
			s.enqueueNotification(fmt.Sprintf("⚠️ MiniProbe 节点数量提醒\n\n当前已配置 %d 个节点，超过建议规模 %d。MiniProbe 仍会继续工作，但建议保持个人小规模使用。", nodeCount, softNodeLimit))
		}
		resp := map[string]any{"id": cfg.ID, "display_name": cfg.DisplayName, "token": token, "node_count": nodeCount, "recommended_limit": softNodeLimit}
		if public != "" {
			resp["install_command"] = installCommand(public, cfg.ID, token, s.serverPublicKey())
		}
		writeJSON(w, http.StatusCreated, resp)

	case http.MethodPut:
		var in nodeUpdateInput
		if err := decodeJSON(w, r, &in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(in.ID) == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		old, ok := s.db.Nodes[in.ID]
		if !ok {
			s.mu.Unlock()
			http.Error(w, "node not found", http.StatusNotFound)
			return
		}
		updated, err := mergeNodeUpdate(old, in)
		if err != nil {
			s.mu.Unlock()
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.db.Nodes[in.ID] = updated
		s.db.Settings.PolicyVersion++
		s.mu.Unlock()
		if err := s.save(); err != nil {
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})

	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		_, ok := s.db.Nodes[id]
		delete(s.db.Nodes, id)
		delete(s.db.States, id)
		delete(s.db.Notifications, id)
		delete(s.db.AgentUpgrades, id)
		s.mu.Unlock()
		if !ok {
			http.Error(w, "node not found", http.StatusNotFound)
			return
		}
		if err := s.save(); err != nil {
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) localAgentUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		NodeIDs []string `json:"node_ids"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if len(in.NodeIDs) == 0 {
		http.Error(w, "node_ids required", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if s.db.AgentUpgrades == nil {
		s.db.AgentUpgrades = map[string]agentUpgradeRequest{}
	}
	type skippedNode struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	scheduled := make([]string, 0, len(in.NodeIDs))
	skipped := make([]skippedNode, 0)
	seen := map[string]bool{}
	for _, raw := range in.NodeIDs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if _, ok := s.db.Nodes[id]; !ok {
			skipped = append(skipped, skippedNode{ID: id, Reason: "node not found"})
			continue
		}
		v := s.db.States[id]
		if v.AgentVersion == serverVersion {
			skipped = append(skipped, skippedNode{ID: id, Reason: "already current"})
			continue
		}
		if !hasCapability(v.Capabilities, selfUpdateCapability) {
			skipped = append(skipped, skippedNode{ID: id, Reason: "Agent needs one-time manual bootstrap"})
			continue
		}
		arch := normalizeAgentArch(v.Info.Arch)
		if _, ok := s.agentAssets[arch]; !ok {
			skipped = append(skipped, skippedNode{ID: id, Reason: "release asset unavailable"})
			continue
		}
		s.db.AgentUpgrades[id] = agentUpgradeRequest{
			RequestID:     base64.RawURLEncoding.EncodeToString(randomBytes(12)),
			TargetVersion: serverVersion,
			RequestedAt:   time.Now().UTC(),
		}
		scheduled = append(scheduled, id)
	}
	s.mu.Unlock()
	if len(scheduled) > 0 {
		if err := s.save(); err != nil {
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"target_version": serverVersion,
		"scheduled":      scheduled,
		"skipped":        skipped,
	})
}

func mergeNodeUpdate(old nodeConfig, in nodeUpdateInput) (nodeConfig, error) {
	displayName := old.DisplayName
	trafficGB := float64(old.MonthlyTrafficLimit) / 1_000_000_000
	direction := old.TrafficDirection
	resetDay := old.TrafficResetDay
	resetTZ := old.TrafficResetTZMinutes
	shutdownEnabled := old.ShutdownEnabled
	shutdownPercent := old.ShutdownPercent
	monthlyPrice := old.MonthlyPrice
	currency := old.Currency
	expire := old.ExpireAt
	tags := append([]string(nil), old.Tags...)

	if in.DisplayName != nil {
		displayName = *in.DisplayName
	}
	if in.TrafficGB != nil {
		trafficGB = *in.TrafficGB
	}
	if in.TrafficDirection != nil {
		direction = *in.TrafficDirection
	}
	if in.TrafficResetDay != nil {
		resetDay = *in.TrafficResetDay
	}
	if in.TrafficResetTZMinutes != nil {
		resetTZ = *in.TrafficResetTZMinutes
	}
	if in.ShutdownEnabled != nil {
		shutdownEnabled = *in.ShutdownEnabled
	}
	if in.ShutdownPercent != nil {
		shutdownPercent = *in.ShutdownPercent
	}
	if in.MonthlyPrice != nil {
		monthlyPrice = *in.MonthlyPrice
	}
	if in.Currency != nil {
		currency = *in.Currency
	}
	if in.ExpireAt != nil {
		expire = *in.ExpireAt
	}
	if in.Tags != nil {
		tags = append([]string(nil), (*in.Tags)...)
	}

	updated, err := validateNodeInput(old.ID, displayName, trafficGB, direction, resetDay, resetTZ, shutdownEnabled, shutdownPercent, monthlyPrice, currency, expire, tags)
	if err != nil {
		return nodeConfig{}, err
	}
	updated.TokenNonce = old.TokenNonce
	updated.CreatedAt = old.CreatedAt
	return updated, nil
}

func validateNodeInput(id, displayName string, trafficGB float64, direction string, resetDay, resetTZ int, shutdownEnabled bool, shutdownPercent int, monthlyPrice float64, currency, expire string, tags []string) (nodeConfig, error) {
	name := strings.TrimSpace(displayName)
	if name == "" || len([]rune(name)) > 80 {
		return nodeConfig{}, errors.New("invalid display name")
	}
	if trafficGB < 0 || trafficGB > 1024*1024 {
		return nodeConfig{}, errors.New("invalid traffic limit")
	}
	direction = strings.ToLower(strings.TrimSpace(direction))
	if direction == "" {
		direction = "total"
	}
	if direction != "inbound" && direction != "outbound" && direction != "total" {
		return nodeConfig{}, errors.New("traffic direction must be inbound, outbound, or total")
	}
	if resetDay == 0 {
		resetDay = 1
	}
	if resetDay < 1 || resetDay > 28 {
		return nodeConfig{}, errors.New("traffic reset day must be 1..28")
	}
	if resetTZ < -14*60 || resetTZ > 14*60 {
		return nodeConfig{}, errors.New("traffic reset timezone out of range")
	}
	if shutdownPercent == 0 {
		shutdownPercent = 95
	}
	if shutdownPercent < 50 || shutdownPercent > 100 {
		return nodeConfig{}, errors.New("shutdown percent must be 50..100")
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "USD"
	}
	if len(currency) > 8 {
		return nodeConfig{}, errors.New("invalid currency")
	}
	expire, err := normalizeExpireInput(expire)
	if err != nil {
		return nodeConfig{}, err
	}
	return nodeConfig{
		ID: id, DisplayName: name, MonthlyTrafficLimit: gbToBytes(trafficGB), TrafficDirection: direction,
		TrafficResetDay: resetDay, TrafficResetTZMinutes: resetTZ, ShutdownEnabled: shutdownEnabled, ShutdownPercent: shutdownPercent,
		MonthlyPrice: monthlyPrice, Currency: currency, ExpireAt: expire, Tags: cleanTags(tags),
	}, nil
}

func normalizeExpireInput(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	switch strings.ToLower(v) {
	case "l", "long", "long-term", "longterm":
		return longTermExpireDate, nil
	}
	if v == "长期" {
		return longTermExpireDate, nil
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return "", errors.New("到期日格式必须为 YYYY-MM-DD，或输入 L/长期")
	}
	return v, nil
}

func expireDisplay(v string) string {
	v = strings.TrimSpace(v)
	if v == longTermExpireDate {
		return "长期"
	}
	return v
}

func (s *server) localNodeCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		ID     string `json:"id"`
		Rotate bool   `json:"rotate"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	cfg, ok := s.db.Nodes[in.ID]
	if !ok {
		s.mu.Unlock()
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	if in.Rotate {
		cfg.TokenNonce = base64.RawURLEncoding.EncodeToString(randomBytes(16))
		s.db.Nodes[in.ID] = cfg
	}
	public := s.db.Settings.PublicURL
	token := s.issueNodeTokenLocked(cfg)
	s.mu.Unlock()
	if in.Rotate {
		if err := s.save(); err != nil {
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
	}
	if public == "" {
		http.Error(w, "set agent access URL first", http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"install_command": installCommand(public, cfg.ID, token, s.serverPublicKey()), "token": token})
}

func (s *server) save() error {
	s.mu.RLock()
	b, err := json.MarshalIndent(s.db, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	if len(b) > dataFileMaxBytes {
		return fmt.Errorf("MiniProbe database exceeds hard safety limit: %d bytes", len(b))
	}
	if err := os.MkdirAll(filepath.Dir(s.dataFile), 0750); err != nil {
		return err
	}
	tmp := s.dataFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.dataFile)
}

func (s *server) persistenceLoop() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for range t.C {
		if err := s.save(); err != nil {
			log.Printf("save state: %v", err)
		}
	}
}

func (s *server) enqueueNotification(msg string) {
	s.mu.RLock()
	enabled := s.db.Settings.Telegram.Enabled && s.db.Settings.Telegram.BotToken != "" && s.db.Settings.Telegram.ChatID != ""
	s.mu.RUnlock()
	if !enabled || strings.TrimSpace(msg) == "" {
		return
	}
	select {
	case s.notifyCh <- msg:
	default:
		log.Printf("Telegram queue full; dropping notification")
	}
}

func (s *server) notificationLoop() {
	for msg := range s.notifyCh {
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			if err = s.sendTelegram(msg); err == nil {
				break
			}
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
		}
		if err != nil {
			log.Printf("Telegram send failed: %v", err)
		}
	}
}

func (s *server) sendTelegram(msg string) error {
	s.mu.RLock()
	token := s.db.Settings.Telegram.BotToken
	chatID := s.db.Settings.Telegram.ChatID
	s.mu.RUnlock()
	if token == "" || chatID == "" {
		return errors.New("Telegram Bot Token / Chat ID 未配置")
	}
	form := url.Values{}
	form.Set("chat_id", chatID)
	form.Set("text", msg)
	form.Set("disable_web_page_preview", "true")
	req, err := http.NewRequest(http.MethodPost, "https://api.telegram.org/bot"+token+"/sendMessage", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("Telegram returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (s *server) offlineMonitorLoop() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for now := range t.C {
		var messages []string
		s.mu.Lock()
		tg := s.db.Settings.Telegram
		if !tg.Enabled || tg.BotToken == "" || tg.ChatID == "" {
			s.mu.Unlock()
			continue
		}
		minutes := tg.OfflineMinutes
		if minutes <= 0 {
			minutes = 2
		}
		threshold := time.Duration(minutes) * time.Minute
		for id, cfg := range s.db.Nodes {
			v := s.db.States[id]
			if v.LastSeen.IsZero() || now.Sub(v.LastSeen) < threshold {
				continue
			}
			ns := s.db.Notifications[id]
			if ns.OfflineAlerted {
				continue
			}
			ns.OfflineAlerted = true
			ns.OfflineSince = v.LastSeen
			s.db.Notifications[id] = ns
			messages = append(messages, fmt.Sprintf("🔴 MiniProbe 节点离线\n\n节点：%s\n已离线：%s\n最后在线：%s", cfg.DisplayName, humanDuration(now.Sub(v.LastSeen)), v.LastSeen.Local().Format("2006-01-02 15:04:05")))
		}
		s.mu.Unlock()
		for _, msg := range messages {
			s.enqueueNotification(msg)
		}
	}
}

func (s *server) trafficSummaryLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for now := range t.C {
		var msg string
		s.mu.Lock()
		tg := s.db.Settings.Telegram
		if !tg.Enabled || tg.SummaryHours <= 0 || tg.BotToken == "" || tg.ChatID == "" {
			s.mu.Unlock()
			continue
		}
		interval := time.Duration(tg.SummaryHours) * time.Hour
		if !tg.LastSummaryAt.IsZero() && now.Sub(tg.LastSummaryAt) < interval {
			s.mu.Unlock()
			continue
		}
		type item struct {
			name string
			cfg  nodeConfig
			v    common.NodeView
			ns   notificationState
		}
		items := make([]item, 0, len(s.db.Nodes))
		for id, cfg := range s.db.Nodes {
			v, ok := s.db.States[id]
			if !ok || v.LastSeen.IsZero() {
				continue
			}
			items = append(items, item{name: cfg.DisplayName, cfg: cfg, v: v, ns: s.db.Notifications[id]})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
		if len(items) == 0 {
			s.db.Settings.Telegram.LastSummaryAt = now.UTC()
			s.mu.Unlock()
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "📊 MiniProbe 流量摘要\n%s\n", now.Local().Format("2006-01-02 15:04"))
		for _, it := range items {
			t := it.v.Traffic
			fmt.Fprintf(&b, "\n%s\n入网：%s\n出网：%s\n", it.name, formatTrafficBytes(t.InboundBytes), formatTrafficBytes(t.OutboundBytes))
			if it.cfg.MonthlyTrafficLimit > 0 {
				fmt.Fprintf(&b, "计费：%s / %s\n使用：%.1f%%\n", formatTrafficBytes(t.UsedBytes), formatTrafficBytes(it.cfg.MonthlyTrafficLimit), t.UsagePercent)
			} else {
				fmt.Fprintf(&b, "计费：未设置流量上限\n")
			}
			ns := it.ns
			if !ns.SummaryCycle.IsZero() && ns.SummaryCycle.Equal(t.CycleStart) && (t.InboundBytes >= ns.SummaryInbound || t.OutboundBytes >= ns.SummaryOutbound) {
				dIn, dOut := uint64(0), uint64(0)
				if t.InboundBytes >= ns.SummaryInbound {
					dIn = t.InboundBytes - ns.SummaryInbound
				}
				if t.OutboundBytes >= ns.SummaryOutbound {
					dOut = t.OutboundBytes - ns.SummaryOutbound
				}
				fmt.Fprintf(&b, "近%d小时：入 +%s / 出 +%s\n", tg.SummaryHours, formatTrafficBytes(dIn), formatTrafficBytes(dOut))
			}
			fmt.Fprintf(&b, "重置：每月%d日 %s\n", it.cfg.TrafficResetDay, formatTZOffset(it.cfg.TrafficResetTZMinutes))
			ns.SummaryInbound = t.InboundBytes
			ns.SummaryOutbound = t.OutboundBytes
			ns.SummaryCycle = t.CycleStart
			s.db.Notifications[it.v.NodeID] = ns
		}
		s.db.Settings.Telegram.LastSummaryAt = now.UTC()
		msg = b.String()
		s.mu.Unlock()
		_ = s.save()
		if len(msg) > 4000 {
			msg = msg[:4000]
		}
		s.enqueueNotification(msg)
	}
}

func (s *server) storageUsage() uint64 {
	paths := []string{filepath.Dir(s.dataFile), s.downloadsDir}
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, exe)
	}
	seen := map[string]bool{}
	var total uint64
	for _, p := range paths {
		p = filepath.Clean(p)
		if seen[p] {
			continue
		}
		seen[p] = true
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !st.IsDir() {
			total += uint64(st.Size())
			continue
		}
		_ = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if info, e := d.Info(); e == nil && info.Mode().IsRegular() {
				total += uint64(info.Size())
			}
			return nil
		})
	}
	return total
}

func highestWarnLevel(pct float64) int {
	level := 0
	for _, x := range trafficWarnLevels {
		if pct >= float64(x) {
			level = x
		}
	}
	return level
}

func formatBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit && exp < 5; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func formatTrafficBytes(n uint64) string {
	const unit = uint64(1000)
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := unit, 0
	for v := n / unit; v >= unit && exp < 5; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func humanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%d秒", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d分%d秒", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%d小时%d分", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%d天%d小时", int(d.Hours())/24, int(d.Hours())%24)
}

func trafficDirectionLabel(v string) string {
	switch v {
	case "inbound":
		return "入网"
	case "outbound":
		return "出网"
	default:
		return "入网 + 出网"
	}
}

func formatTZOffset(minutes int) string {
	sign := "+"
	if minutes < 0 {
		sign = "-"
		minutes = -minutes
	}
	return fmt.Sprintf("UTC%s%02d:%02d", sign, minutes/60, minutes%60)
}

func validDashboardMode(mode string) bool {
	return mode == dashboardPublic || mode == dashboardProtected || mode == dashboardDisabled
}

func hashPassword(pass string) passwordConfig {
	salt := randomBytes(16)
	hash := pbkdf2SHA256([]byte(pass), salt, pbkdf2Iters, 32)
	return passwordConfig{Salt: base64.RawURLEncoding.EncodeToString(salt), Hash: base64.RawURLEncoding.EncodeToString(hash)}
}

func verifyPassword(pass string, pc passwordConfig) bool {
	salt, e1 := base64.RawURLEncoding.DecodeString(pc.Salt)
	want, e2 := base64.RawURLEncoding.DecodeString(pc.Hash)
	if e1 != nil || e2 != nil || len(want) != 32 {
		return false
	}
	got := pbkdf2SHA256([]byte(pass), salt, pbkdf2Iters, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// pbkdf2SHA256 is kept in-tree so MiniProbe remains a static single binary.
func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	hLen := 32
	blocks := (keyLen + hLen - 1) / hLen
	out := make([]byte, 0, blocks*hLen)
	for i := 1; i <= blocks; i++ {
		m := hmac.New(sha256.New, password)
		m.Write(salt)
		m.Write([]byte{byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)})
		u := m.Sum(nil)
		t := append([]byte(nil), u...)
		for j := 1; j < iterations; j++ {
			m = hmac.New(sha256.New, password)
			m.Write(u)
			u = m.Sum(nil)
			for k := range t {
				t[k] ^= u[k]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

func (s *server) loginAllowed(ip string) bool {
	now := time.Now()
	cutoff := now.Add(-10 * time.Minute)
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	f := s.failures[ip]
	kept := f.Times[:0]
	for _, t := range f.Times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	f.Times = kept
	s.failures[ip] = f
	return len(f.Times) < 8
}

func (s *server) noteLoginFailure(ip string) {
	s.loginMu.Lock()
	f := s.failures[ip]
	f.Times = append(f.Times, time.Now())
	s.failures[ip] = f
	s.loginMu.Unlock()
}

func (s *server) clearLoginFailures(ip string) {
	s.loginMu.Lock()
	delete(s.failures, ip)
	s.loginMu.Unlock()
}

func validSameOriginMutation(r *http.Request) bool {
	if r.Header.Get("X-MiniProbe-Request") != "1" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

func requestIsTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	// MiniProbe's supported HTTPS proxy is the local cloudflared process. Do not
	// trust X-Forwarded-Proto from arbitrary Direct-mode clients on the Internet.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}

func validatePublicURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("接入地址必须是 http:// 或 https:// 开头的完整地址")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("接入地址不能包含用户名、查询参数或片段")
	}
	if u.Path != "" && u.Path != "/" {
		return nil, errors.New("当前版本接入地址不能包含子路径")
	}
	u.Path = ""
	return u, nil
}

func installCommand(publicURL, nodeID, token, serverKey string) string {
	base := strings.TrimRight(publicURL, "/")
	return "curl -fsSL " + shellQuote(base+"/install-agent.sh") + " | sh -s -- --server " + shellQuote(base) + " --token " + shellQuote(token) + " --node-id " + shellQuote(nodeID) + " --server-key " + shellQuote(serverKey)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func cleanTags(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" || len([]rune(t)) > 30 || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) == 8 {
			break
		}
	}
	return out
}

func gbToBytes(v float64) uint64 {
	if v <= 0 {
		return 0
	}
	// Traffic quotas use decimal GB (1 GB = 1,000,000,000 bytes), which
	// matches how network/cloud traffic quotas are normally specified. Using
	// GiB here would let a nominal 200 GB quota grow to ~214.7 decimal GB.
	return uint64(v * 1_000_000_000)
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func randomPassword(n int) string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789-_.@"
	b := randomBytes(n)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	defer r.Body.Close()
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody)).Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func noStore(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-store") }

func remoteIP(a string) string {
	h, _, err := net.SplitHostPort(a)
	if err == nil {
		return h
	}
	return a
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// -------------------- local interactive management client --------------------

type localClient struct {
	http *http.Client
}

func newLocalClient(socketPath string) *localClient {
	tr := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.DialTimeout("unix", socketPath, 2*time.Second)
		},
	}
	return &localClient{http: &http.Client{Transport: tr, Timeout: 10 * time.Second}}
}

func (c *localClient) request(method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://unix"+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = resp.Status
		}
		return errors.New(msg)
	}
	if out != nil && len(bytes.TrimSpace(b)) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			return err
		}
	}
	return nil
}

func runManager(socketPath string) error {
	client := newLocalClient(socketPath)
	if err := client.request(http.MethodGet, "/healthz", nil, nil); err != nil {
		return fmt.Errorf("无法连接本机管理接口 %s；请确认 MiniProbe Server 已启动: %w", socketPath, err)
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		cfg, _ := getLocalConfig(client)
		nodes, _ := getLocalNodes(client)
		fmt.Println()
		fmt.Println("============================================================")
		fmt.Println("                     MiniProbe")
		fmt.Println("============================================================")
		fmt.Printf(" Agent 接入地址 : %s\n", dash(cfg.PublicURL))
		fmt.Printf(" 网络模式        : %s\n", accessModeLabel(cfg.AccessMode))
		fmt.Printf(" Dashboard       : %s\n", dashboardModeLabel(cfg.DashboardMode))
		probeState := "关闭"
		if cfg.ProbeMode != probeModeOff {
			probeState = cfg.ProbeRegionLabel + " · " + cfg.ProbeProtocolLabel
		}
		fmt.Printf(" 国内线路测试    : %s\n", probeState)
		fmt.Printf(" 节点            : %d / %d（建议上限）\n", len(nodes), cfg.NodeSoftLimit)
		if len(nodes) > cfg.NodeSoftLimit {
			fmt.Printf(" ⚠ 节点数量已超过 MiniProbe 建议规模 %d。\n", cfg.NodeSoftLimit)
		}
		fmt.Println("------------------------------------------------------------")
		fmt.Println(" 1. 添加监控节点")
		fmt.Println(" 2. 查看节点")
		fmt.Println(" 3. 修改节点 / 流量策略")
		fmt.Println(" 4. 获取 Agent 安装命令")
		fmt.Println(" 5. 删除节点")
		fmt.Println(" 6. 重置节点 Token")
		fmt.Println(" 7. Dashboard 设置")
		fmt.Println(" 8. 安全访问（HTTPS / Cloudflare Tunnel）")
		fmt.Println(" 9. Telegram 通知")
		fmt.Println("10. 存储 / 当前设置")
		fmt.Println("11. 国内线路测试（北京 / 上海 / 广州）")
		fmt.Println("12. Agent 版本管理 / 集中升级")
		fmt.Println(" 0. 退出")
		fmt.Println("------------------------------------------------------------")
		choice := prompt(reader, "请选择: ")
		switch choice {
		case "1":
			manageAddNode(reader, client)
		case "2":
			manageListNodes(client)
		case "3":
			manageModifyNode(reader, client)
		case "4":
			manageNodeCommand(reader, client, false)
		case "5":
			manageDeleteNode(reader, client)
		case "6":
			manageNodeCommand(reader, client, true)
		case "7":
			manageDashboard(reader, client)
		case "8":
			manageNetworkAccess(reader, client, socketPath)
		case "9":
			manageTelegram(reader, client)
		case "10":
			manageShowConfig(client)
		case "11":
			manageProbeSettings(reader, client)
		case "12":
			manageAgentUpgrades(reader, client)
		case "0", "q", "Q":
			return nil
		default:
			fmt.Println("无效选项。")
		}
	}
}

type localConfigView struct {
	PublicURL              string `json:"public_url"`
	AccessMode             string `json:"access_mode"`
	PolicyVersion          int64  `json:"policy_version"`
	DashboardMode          string `json:"dashboard_mode"`
	HasDashboardPassword   bool   `json:"has_dashboard_password"`
	ProbeMode              string `json:"probe_mode"`
	ProbeRegion            string `json:"probe_region"`
	ProbeRegionLabel       string `json:"probe_region_label"`
	ProbeProtocol          string `json:"probe_protocol"`
	ProbeProtocolLabel     string `json:"probe_protocol_label"`
	TelegramEnabled        bool   `json:"telegram_enabled"`
	TelegramHasToken       bool   `json:"telegram_has_token"`
	TelegramChatID         string `json:"telegram_chat_id"`
	TelegramSummaryHours   int    `json:"telegram_summary_hours"`
	TelegramOfflineMinutes int    `json:"telegram_offline_minutes"`
	NodeSoftLimit          int    `json:"node_soft_limit"`
	StorageHardLimit       uint64 `json:"storage_hard_limit"`
	StorageUsed            uint64 `json:"storage_used"`
}

func getLocalConfig(c *localClient) (localConfigView, error) {
	var out localConfigView
	err := c.request(http.MethodGet, "/v1/config", nil, &out)
	return out, err
}

func getLocalNodes(c *localClient) ([]adminNodeView, error) {
	var out []adminNodeView
	err := c.request(http.MethodGet, "/v1/nodes", nil, &out)
	return out, err
}

func manageAddNode(r *bufio.Reader, c *localClient) {
	fmt.Println("\n添加监控节点（可选项直接回车跳过）")
	name := prompt(r, "节点名称: ")
	if name == "" {
		fmt.Println("节点名称不能为空。")
		return
	}
	body := promptNodePolicy(r, nil)
	body["display_name"] = name
	var out struct {
		ID               string `json:"id"`
		InstallCommand   string `json:"install_command"`
		NodeCount        int    `json:"node_count"`
		RecommendedLimit int    `json:"recommended_limit"`
	}
	if err := c.request(http.MethodPost, "/v1/nodes", body, &out); err != nil {
		fmt.Println("创建失败:", err)
		return
	}
	fmt.Printf("\n节点创建成功：%s\n", out.ID)
	if out.NodeCount > out.RecommendedLimit {
		fmt.Printf("⚠ 当前节点 %d 个，已超过建议上限 %d；MiniProbe 仍允许继续使用，但会持续提醒。\n", out.NodeCount, out.RecommendedLimit)
	}
	if out.InstallCommand == "" {
		fmt.Println("尚未设置 Agent 接入地址，请先配置网络接入方式，然后再生成安装命令。")
		return
	}
	fmt.Print("\n请在需要监控的 VPS 上执行：\n\n")
	fmt.Println(out.InstallCommand)
}

func promptNodePolicy(r *bufio.Reader, current *adminNodeView) map[string]any {
	curTraffic := ""
	curDirection := "total"
	curResetDay := 1
	curTZ := 0
	curShutdown := false
	curShutdownPct := 95
	curPrice := ""
	curCurrency := "USD"
	curExpire := ""
	curTags := ""
	if current != nil {
		curTraffic = trimFloat(float64(current.MonthlyTrafficLimit) / 1_000_000_000)
		curDirection = current.TrafficDirection
		curResetDay = current.TrafficResetDay
		curTZ = current.TrafficResetTZMinutes
		curShutdown = current.ShutdownEnabled
		curShutdownPct = current.ShutdownPercent
		curPrice = trimFloat(current.MonthlyPrice)
		curCurrency = current.Currency
		curExpire = current.ExpireAt
		curTags = strings.Join(current.Tags, ",")
	}
	trafficPrompt := "月流量 GB [可选]: "
	if current != nil {
		trafficPrompt = fmt.Sprintf("月流量 GB [%s]: ", curTraffic)
	}
	trafficRaw := prompt(r, trafficPrompt)
	if trafficRaw == "" {
		trafficRaw = curTraffic
	}
	traffic := parseFloat(trafficRaw)

	direction := curDirection
	resetDay := curResetDay
	resetTZ := curTZ
	shutdown := curShutdown
	shutdownPct := curShutdownPct
	if traffic > 0 {
		fmt.Printf("流量计费方向：1=出网  2=入网  3=入网+出网 [当前 %s]\n", trafficDirectionLabel(curDirection))
		d := prompt(r, "选择 [3]: ")
		if d != "" {
			switch d {
			case "1":
				direction = "outbound"
			case "2":
				direction = "inbound"
			case "3":
				direction = "total"
			default:
				fmt.Println("无效选择，使用入网+出网。")
				direction = "total"
			}
		}
		if current == nil && d == "" {
			direction = "total"
		}
		rd := prompt(r, fmt.Sprintf("每月流量重置日 [%d]（1-28）: ", resetDay))
		if rd != "" {
			if v, e := strconv.Atoi(rd); e == nil {
				resetDay = v
			}
		}
		tz := prompt(r, fmt.Sprintf("计费时区 [%s]，例如 +08:00: ", formatTZOffset(resetTZ)))
		if tz != "" {
			if v, err := parseTZOffset(tz); err == nil {
				resetTZ = v
			} else {
				fmt.Println("时区格式无效，保持原值。")
			}
		}
		ans := strings.ToLower(prompt(r, fmt.Sprintf("达到 %d%% 时自动保护关机？[y/N%s]: ", shutdownPct, boolCurrent(shutdown))))
		if ans == "y" || ans == "yes" {
			shutdown = true
		}
		if ans == "n" || ans == "no" {
			shutdown = false
		}
		if shutdown {
			sp := prompt(r, fmt.Sprintf("保护关机阈值 %% [%d]: ", shutdownPct))
			if sp != "" {
				if v, e := strconv.Atoi(sp); e == nil {
					shutdownPct = v
				}
			}
		}
	} else {
		direction, resetDay, resetTZ, shutdown, shutdownPct = "total", 1, 0, false, 95
	}
	pricePrompt := "月租 [可选]: "
	if current != nil {
		pricePrompt = fmt.Sprintf("月租 [%s]: ", curPrice)
	}
	priceRaw := prompt(r, pricePrompt)
	if priceRaw == "" {
		priceRaw = curPrice
	}
	currencyPrompt := fmt.Sprintf("币种 [%s]: ", curCurrency)
	currency := prompt(r, currencyPrompt)
	if currency == "" {
		currency = curCurrency
	}
	expPrompt := "到期日 YYYY-MM-DD [可选，L=长期]: "
	if current != nil {
		curExpireDisplay := expireDisplay(curExpire)
		expPrompt = fmt.Sprintf("到期日 [%s]（L=长期）: ", curExpireDisplay)
	}
	expire := prompt(r, expPrompt)
	if expire == "" {
		expire = curExpire
	}
	tagsPrompt := "标签，逗号分隔 [可选]: "
	if current != nil {
		tagsPrompt = fmt.Sprintf("标签 [%s]: ", curTags)
	}
	tagsRaw := prompt(r, tagsPrompt)
	if tagsRaw == "" {
		tagsRaw = curTags
	}
	var tags []string
	for _, t := range strings.FieldsFunc(tagsRaw, func(r rune) bool { return r == ',' || r == '，' }) {
		if strings.TrimSpace(t) != "" {
			tags = append(tags, strings.TrimSpace(t))
		}
	}
	return map[string]any{
		"traffic_gb": traffic, "traffic_direction": direction, "traffic_reset_day": resetDay, "traffic_reset_tz_minutes": resetTZ,
		"shutdown_enabled": shutdown, "shutdown_percent": shutdownPct, "monthly_price": parseFloat(priceRaw), "currency": currency,
		"expire_at": expire, "tags": tags,
	}
}

func manageModifyNode(r *bufio.Reader, c *localClient) {
	n, ok := chooseNode(r, c)
	if !ok {
		return
	}
	fmt.Printf("\n修改节点：%s\n", n.DisplayName)
	fmt.Println("提示：直接回车 = 保持原值；可选项输入 - = 清除。只会修改你实际填写的项目。")
	body := map[string]any{"id": n.ID}

	if v := prompt(r, fmt.Sprintf("节点名称 [%s]: ", n.DisplayName)); v != "" {
		body["display_name"] = v
	}

	trafficCurrent := "未设置"
	if n.MonthlyTrafficLimit > 0 {
		trafficCurrent = trimFloat(float64(n.MonthlyTrafficLimit)/1_000_000_000) + " GB"
	}
	trafficRaw := prompt(r, fmt.Sprintf("月流量 [%s]（GB，0 或 - 清除）: ", trafficCurrent))
	effectiveTraffic := float64(n.MonthlyTrafficLimit) / 1_000_000_000
	if trafficRaw != "" {
		if trafficRaw == "-" {
			effectiveTraffic = 0
			body["traffic_gb"] = float64(0)
		} else if v, err := strconv.ParseFloat(strings.TrimSpace(trafficRaw), 64); err != nil || v < 0 {
			fmt.Println("月流量格式无效，未做任何修改。")
			return
		} else {
			effectiveTraffic = v
			body["traffic_gb"] = v
		}
	}

	if effectiveTraffic > 0 {
		fmt.Printf("流量计费方向：1=出网  2=入网  3=入网+出网 [当前 %s]\n", trafficDirectionLabel(n.TrafficDirection))
		if v := prompt(r, "选择 [回车保持]: "); v != "" {
			switch v {
			case "1":
				body["traffic_direction"] = "outbound"
			case "2":
				body["traffic_direction"] = "inbound"
			case "3":
				body["traffic_direction"] = "total"
			default:
				fmt.Println("计费方向无效，未做任何修改。")
				return
			}
		}
		if v := prompt(r, fmt.Sprintf("每月流量重置日 [%d]（1-28）: ", n.TrafficResetDay)); v != "" {
			i, err := strconv.Atoi(v)
			if err != nil || i < 1 || i > 28 {
				fmt.Println("重置日无效，未做任何修改。")
				return
			}
			body["traffic_reset_day"] = i
		}
		if v := prompt(r, fmt.Sprintf("计费时区 [%s]，例如 +08:00: ", formatTZOffset(n.TrafficResetTZMinutes))); v != "" {
			i, err := parseTZOffset(v)
			if err != nil {
				fmt.Println("计费时区格式无效，未做任何修改。")
				return
			}
			body["traffic_reset_tz_minutes"] = i
		}
		ans := strings.ToLower(prompt(r, fmt.Sprintf("保护关机 [当前 %s]（y/n，回车保持）: ", map[bool]string{true: "开启", false: "关闭"}[n.ShutdownEnabled])))
		if ans != "" {
			switch ans {
			case "y", "yes":
				body["shutdown_enabled"] = true
			case "n", "no":
				body["shutdown_enabled"] = false
			default:
				fmt.Println("保护关机选项无效，未做任何修改。")
				return
			}
		}
		shutdownEffective := n.ShutdownEnabled
		if v, ok := body["shutdown_enabled"].(bool); ok {
			shutdownEffective = v
		}
		if shutdownEffective {
			if v := prompt(r, fmt.Sprintf("保护关机阈值 [%d]%%: ", n.ShutdownPercent)); v != "" {
				i, err := strconv.Atoi(v)
				if err != nil || i < 50 || i > 100 {
					fmt.Println("保护阈值无效，未做任何修改。")
					return
				}
				body["shutdown_percent"] = i
			}
		}
	}

	priceCurrent := "未设置"
	if n.MonthlyPrice > 0 {
		priceCurrent = trimFloat(n.MonthlyPrice)
	}
	if v := prompt(r, fmt.Sprintf("月租 [%s]（- 清除）: ", priceCurrent)); v != "" {
		if v == "-" {
			body["monthly_price"] = float64(0)
		} else if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err != nil || f < 0 {
			fmt.Println("月租格式无效，未做任何修改。")
			return
		} else {
			body["monthly_price"] = f
		}
	}
	if v := prompt(r, fmt.Sprintf("币种 [%s]: ", n.Currency)); v != "" {
		body["currency"] = v
	}
	expireCurrent := expireDisplay(n.ExpireAt)
	if expireCurrent == "" {
		expireCurrent = "未设置"
	}
	if v := prompt(r, fmt.Sprintf("到期日 [%s]（YYYY-MM-DD，L=长期，- 清除）: ", expireCurrent)); v != "" {
		if v == "-" {
			body["expire_at"] = ""
		} else {
			body["expire_at"] = v
		}
	}
	tagsCurrent := strings.Join(n.Tags, ",")
	if tagsCurrent == "" {
		tagsCurrent = "未设置"
	}
	if v := prompt(r, fmt.Sprintf("标签 [%s]（逗号分隔，- 清除）: ", tagsCurrent)); v != "" {
		if v == "-" {
			body["tags"] = []string{}
		} else {
			var tags []string
			for _, t := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '，' }) {
				if strings.TrimSpace(t) != "" {
					tags = append(tags, strings.TrimSpace(t))
				}
			}
			body["tags"] = tags
		}
	}

	if len(body) == 1 {
		fmt.Println("没有填写任何新值，节点保持原样。")
		return
	}
	if err := c.request(http.MethodPut, "/v1/nodes", body, nil); err != nil {
		fmt.Println("修改失败:", err)
		return
	}
	fmt.Println("节点设置已保存。未填写的项目保持原值；流量策略会通过签名配置自动同步到 Agent。")
}

func manageListNodes(c *localClient) {
	nodes, err := getLocalNodes(c)
	if err != nil {
		fmt.Println("读取节点失败:", err)
		return
	}
	fmt.Println()
	if len(nodes) == 0 {
		fmt.Println("暂无节点。")
		return
	}
	if len(nodes) > softNodeLimit {
		fmt.Printf("⚠ 当前 %d 个节点，超过建议上限 %d。\n", len(nodes), softNodeLimit)
	}
	fmt.Printf("%-3s %-18s %-8s %-12s %-12s %-10s %-18s\n", "#", "名称", "状态", "入网", "出网", "使用率", "Agent")
	for i, n := range nodes {
		status := "离线"
		if n.Online {
			status = "在线"
		}
		usage := "-"
		if n.MonthlyTrafficLimit > 0 {
			usage = fmt.Sprintf("%.1f%%", n.Traffic.UsagePercent)
		}
		fmt.Printf("%-3d %-18s %-8s %-12s %-12s %-10s %-18s\n", i+1, trimRunes(n.DisplayName, 18), status, formatBytes(n.Traffic.InboundBytes), formatBytes(n.Traffic.OutboundBytes), usage, trimRunes(n.AgentVersion, 18))
	}
}

func chooseNode(r *bufio.Reader, c *localClient) (adminNodeView, bool) {
	nodes, err := getLocalNodes(c)
	if err != nil {
		fmt.Println("读取节点失败:", err)
		return adminNodeView{}, false
	}
	if len(nodes) == 0 {
		fmt.Println("暂无节点。")
		return adminNodeView{}, false
	}
	fmt.Println()
	for i, n := range nodes {
		status := "离线"
		if n.Online {
			status = "在线"
		}
		fmt.Printf(" %d. %s (%s) [%s]\n", i+1, n.DisplayName, n.ID, status)
	}
	s := prompt(r, "选择节点编号: ")
	i, err := strconv.Atoi(s)
	if err != nil || i < 1 || i > len(nodes) {
		fmt.Println("无效节点编号。")
		return adminNodeView{}, false
	}
	return nodes[i-1], true
}

func manageNodeCommand(r *bufio.Reader, c *localClient, rotate bool) {
	n, ok := chooseNode(r, c)
	if !ok {
		return
	}
	if rotate {
		confirm := strings.ToLower(prompt(r, "重置 Token 后旧 Agent 将立即失效，输入 YES 确认: "))
		if confirm != "yes" {
			fmt.Println("已取消。")
			return
		}
	}
	var out struct {
		InstallCommand string `json:"install_command"`
	}
	if err := c.request(http.MethodPost, "/v1/node-command", map[string]any{"id": n.ID, "rotate": rotate}, &out); err != nil {
		fmt.Println("生成失败:", err)
		return
	}
	fmt.Print("\nAgent 一键安装 / 更新命令：\n\n")
	fmt.Println(out.InstallCommand)
}

func manageAgentUpgrades(r *bufio.Reader, c *localClient) {
	nodes, err := getLocalNodes(c)
	if err != nil {
		fmt.Println("读取节点失败:", err)
		return
	}
	if len(nodes) == 0 {
		fmt.Println("暂无节点。")
		return
	}
	fmt.Printf("\nServer / 目标 Agent 版本：%s\n", serverVersion)
	fmt.Printf("%-3s %-20s %-16s %-12s\n", "#", "名称", "Agent", "升级状态")
	eligible := make([]adminNodeView, 0)
	for i, n := range nodes {
		status := "最新"
		switch {
		case n.AgentVersion == serverVersion:
			status = "最新"
		case !hasCapability(n.Capabilities, selfUpdateCapability):
			status = "需手动一次"
		case n.UpgradeRequested:
			status = "升级中"
		case !n.Online:
			status = "离线/可排队"
		default:
			status = "可集中升级"
		}
		if n.AgentVersion != serverVersion && hasCapability(n.Capabilities, selfUpdateCapability) && !n.UpgradeRequested {
			eligible = append(eligible, n)
		}
		fmt.Printf("%-3d %-20s %-16s %-12s\n", i+1, trimRunes(n.DisplayName, 20), trimRunes(n.AgentVersion, 16), status)
	}
	fmt.Println()
	fmt.Println("说明：首次升级到支持集中升级的 Agent 仍需手动安装一次；之后版本可在这里统一升级。")
	fmt.Println("1. 一键升级全部可升级 Agent")
	fmt.Println("2. 选择一个节点升级")
	fmt.Println("0. 返回")
	switch prompt(r, "请选择: ") {
	case "1":
		if len(eligible) == 0 {
			fmt.Println("当前没有可集中升级的 Agent。")
			return
		}
		if strings.ToUpper(prompt(r, fmt.Sprintf("将为 %d 个 Agent 排队升级到 %s，输入 YES 确认: ", len(eligible), serverVersion))) != "YES" {
			fmt.Println("已取消。")
			return
		}
		ids := make([]string, 0, len(eligible))
		for _, n := range eligible {
			ids = append(ids, n.ID)
		}
		queueAgentUpgrades(c, ids)
	case "2":
		n, ok := chooseNode(r, c)
		if !ok {
			return
		}
		if n.AgentVersion == serverVersion {
			fmt.Println("该节点已经是最新 Agent。")
			return
		}
		if !hasCapability(n.Capabilities, selfUpdateCapability) {
			fmt.Println("该节点 Agent 尚不支持集中升级；请使用菜单 4 手动更新一次。之后版本即可集中升级。")
			return
		}
		queueAgentUpgrades(c, []string{n.ID})
	case "0", "":
		return
	default:
		fmt.Println("无效选项。")
	}
}

func queueAgentUpgrades(c *localClient, ids []string) {
	var out struct {
		TargetVersion string   `json:"target_version"`
		Scheduled     []string `json:"scheduled"`
		Skipped       []struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		} `json:"skipped"`
	}
	if err := c.request(http.MethodPost, "/v1/agent-upgrade", map[string]any{"node_ids": ids}, &out); err != nil {
		fmt.Println("排队失败:", err)
		return
	}
	fmt.Printf("已排队 %d 个 Agent，目标版本 %s。在线节点通常会在数秒内开始升级；离线节点上线后会自动领取。\n", len(out.Scheduled), out.TargetVersion)
	if len(out.Skipped) > 0 {
		fmt.Printf("另有 %d 个节点未排队；可重新进入菜单 12 查看状态。\n", len(out.Skipped))
	}
}

func manageDeleteNode(r *bufio.Reader, c *localClient) {
	n, ok := chooseNode(r, c)
	if !ok {
		return
	}
	confirm := prompt(r, fmt.Sprintf("确定删除节点 %q？输入 DELETE 确认: ", n.DisplayName))
	if confirm != "DELETE" {
		fmt.Println("已取消。")
		return
	}
	if err := c.request(http.MethodDelete, "/v1/nodes?id="+url.QueryEscape(n.ID), nil, nil); err != nil {
		fmt.Println("删除失败:", err)
		return
	}
	fmt.Println("节点已删除，原 Token 已失效。")
}

func manageDashboard(r *bufio.Reader, c *localClient) {
	cfg, err := getLocalConfig(c)
	if err != nil {
		fmt.Println("读取设置失败:", err)
		return
	}
	fmt.Printf("\n当前 Dashboard 模式：%s\n", dashboardModeLabel(cfg.DashboardMode))
	fmt.Println(" 1. Public             - 公开只读 Dashboard")
	fmt.Println(" 2. Password Protected - 密码保护的只读 Dashboard")
	fmt.Println(" 3. Disabled           - 完全关闭 Dashboard")
	fmt.Println(" 0. 返回")
	choice := prompt(r, "请选择: ")
	var mode string
	body := map[string]any{}
	switch choice {
	case "1":
		mode = dashboardPublic
	case "2":
		mode = dashboardProtected
		p1 := promptPassword(r, "设置 Dashboard 密码（至少 10 位）: ")
		if len(p1) < 10 {
			fmt.Println("密码至少 10 位。")
			return
		}
		p2 := promptPassword(r, "再次输入密码: ")
		if p1 != p2 {
			fmt.Println("两次密码不一致。")
			return
		}
		body["dashboard_password"] = p1
	case "3":
		mode = dashboardDisabled
	case "0":
		return
	default:
		fmt.Println("无效选项。")
		return
	}
	body["dashboard_mode"] = mode
	if err := c.request(http.MethodPut, "/v1/config", body, nil); err != nil {
		fmt.Println("保存失败:", err)
		return
	}
	fmt.Printf("Dashboard 已切换为：%s\n", dashboardModeLabel(mode))
}

func manageNetworkAccess(r *bufio.Reader, c *localClient, socketPath string) {
	cfg, err := getLocalConfig(c)
	if err != nil {
		fmt.Println("读取设置失败:", err)
		return
	}
	fmt.Printf("\n当前模式：%s\n当前地址：%s\n", accessModeLabel(cfg.AccessMode), cfg.PublicURL)
	if cfg.AccessMode == accessDirect {
		fmt.Println("安全状态：HTTP Direct 未加密，不建议长期公网使用。")
	}
	fmt.Println(" 1. Direct HTTP（仅临时 / 故障恢复，不推荐长期公网使用）")
	fmt.Println(" 2. Cloudflare Tunnel HTTPS（推荐；成功后公网 IP:28888 自动停止监听）")
	fmt.Println(" 0. 返回")
	choice := prompt(r, "请选择: ")
	switch choice {
	case "1":
		manageSwitchDirect(r, c, socketPath, cfg)
	case "2":
		manageSwitchTunnel(r, c, socketPath, cfg)
	case "0":
		return
	default:
		fmt.Println("无效选项。")
	}
}

func manageSwitchTunnel(r *bufio.Reader, c *localClient, socketPath string, old localConfigView) {
	nodes, _ := getLocalNodes(c)
	for _, n := range nodes {
		if !n.Online {
			fmt.Printf("无法安全切换：节点 %s 当前离线。请先让所有节点上线，避免切换后失联。\n", n.DisplayName)
			return
		}
		if !supportsSignedPolicy(n.AgentVersion) {
			fmt.Printf("无法安全切换：节点 %s Agent 版本过旧（%s）。请先用菜单 4 的命令更新 Agent。\n", n.DisplayName, n.AgentVersion)
			return
		}
	}
	fmt.Println("\n请先在 Cloudflare 控制台创建 Remotely-managed Tunnel，并把 Public Hostname 的 Service 指向：")
	fmt.Println("  http://127.0.0.1:28888")
	fmt.Println("然后复制 Tunnel Token（eyJ...）。MiniProbe 不需要你的 Cloudflare API Key。")
	host := strings.TrimSpace(prompt(r, "Tunnel 域名（例如 probe.example.com）: "))
	if host == "" || strings.Contains(host, "/") {
		fmt.Println("域名无效。")
		return
	}
	token := promptPassword(r, "Tunnel Token: ")
	if len(token) < 20 {
		fmt.Println("Tunnel Token 看起来无效。")
		return
	}
	if err := ensureCloudflared(); err != nil {
		fmt.Println("安装 cloudflared 失败:", err)
		return
	}
	if err := installTunnelService(token); err != nil {
		fmt.Println("启动 Cloudflare Tunnel 失败:", err)
		return
	}
	tunnelURL := "https://" + host
	fmt.Println("正在验证 Tunnel HTTPS...")
	if err := waitHTTPHealth(tunnelURL, 60*time.Second); err != nil {
		_ = stopTunnelService()
		fmt.Println("Tunnel 验证失败，未关闭公网 28888：", err)
		return
	}
	if err := c.request(http.MethodPut, "/v1/config", map[string]any{"public_url": tunnelURL}, nil); err != nil {
		_ = stopTunnelService()
		fmt.Println("保存 Tunnel 地址失败:", err)
		return
	}
	newCfg, _ := getLocalConfig(c)
	fmt.Println("正在让在线 Agent 自动迁移到 HTTPS Tunnel...")
	if err := waitAgentMigration(c, tunnelURL, newCfg.PolicyVersion, 35*time.Second); err != nil {
		_ = c.request(http.MethodPut, "/v1/config", map[string]any{"public_url": old.PublicURL}, nil)
		_ = stopTunnelService()
		fmt.Println("Agent 迁移未完成，已保留 Direct 模式：", err)
		return
	}
	if err := setServerListen("127.0.0.1:28888"); err != nil {
		fmt.Println("关闭公网监听失败:", err)
		return
	}
	if err := restartMiniProbeServer(); err != nil {
		fmt.Println("重启 MiniProbe 失败:", err)
		return
	}
	if err := waitAdminSocket(socketPath, 20*time.Second); err != nil {
		fmt.Println("MiniProbe 重启后管理接口不可用:", err)
		return
	}
	if err := waitHTTPHealth(tunnelURL, 30*time.Second); err != nil {
		_ = setServerListen(":28888")
		_ = restartMiniProbeServer()
		_ = waitAdminSocket(socketPath, 20*time.Second)
		_ = c.request(http.MethodPut, "/v1/config", map[string]any{"public_url": old.PublicURL, "access_mode": accessDirect}, nil)
		_ = stopTunnelService()
		fmt.Println("Tunnel 在关闭公网端口后验证失败，已回滚 Direct 模式：", err)
		return
	}
	_ = c.request(http.MethodPut, "/v1/config", map[string]any{"access_mode": accessTunnel}, nil)
	fmt.Println("✓ Cloudflare Tunnel 已启用。MiniProbe 现在仅监听 127.0.0.1:28888，公网 IP:28888 不再提供访问。")
	fmt.Println("  Agent / Dashboard 地址：" + tunnelURL)
}

func manageSwitchDirect(r *bufio.Reader, c *localClient, socketPath string, old localConfigView) {
	v := prompt(r, "Direct 接入地址（例如 http://1.2.3.4:28888 或 IPv6 地址）: ")
	if v == "" {
		fmt.Println("未修改。")
		return
	}
	if _, err := validatePublicURL(v); err != nil {
		fmt.Println("地址无效:", err)
		return
	}
	// Open the direct listener first while Tunnel is still alive, then migrate
	// Agents, and only then stop Tunnel. This avoids a lockout window.
	if err := setServerListen(":28888"); err != nil {
		fmt.Println("恢复公网监听失败:", err)
		return
	}
	if err := restartMiniProbeServer(); err != nil {
		fmt.Println("重启 MiniProbe 失败:", err)
		return
	}
	if err := waitAdminSocket(socketPath, 20*time.Second); err != nil {
		fmt.Println("管理接口不可用:", err)
		return
	}
	if err := c.request(http.MethodPut, "/v1/config", map[string]any{"public_url": v, "access_mode": accessDirect}, nil); err != nil {
		fmt.Println("保存失败:", err)
		return
	}
	cfg, _ := getLocalConfig(c)
	if err := waitAgentMigration(c, strings.TrimRight(v, "/"), cfg.PolicyVersion, 35*time.Second); err != nil {
		fmt.Println("提示：部分 Agent 尚未迁移到 Direct 地址；Tunnel 暂时保留。", err)
		return
	}
	_ = stopTunnelService()
	fmt.Println("✓ 已切换 Direct 模式。公网通过 " + strings.TrimRight(v, "/") + " 访问。")
	_ = old
}

func manageTelegram(r *bufio.Reader, c *localClient) {
	for {
		cfg, err := getLocalConfig(c)
		if err != nil {
			fmt.Println("读取失败:", err)
			return
		}
		status := "Disabled"
		if cfg.TelegramEnabled {
			status = "Enabled"
		}
		fmt.Printf("\nTelegram：%s | Token: %s | Chat ID: %s | 摘要: %s | 离线告警: %d 分钟\n", status, yesNo(cfg.TelegramHasToken), dash(cfg.TelegramChatID), summaryLabel(cfg.TelegramSummaryHours), cfg.TelegramOfflineMinutes)
		fmt.Println(" 1. 设置 Bot Token / Chat ID")
		fmt.Println(" 2. 发送测试消息")
		fmt.Println(" 3. 启用 / 关闭 Telegram")
		fmt.Println(" 4. 定时流量摘要")
		fmt.Println(" 5. 掉线告警时间")
		fmt.Println(" 0. 返回")
		switch prompt(r, "请选择: ") {
		case "1":
			token := promptPassword(r, "Bot Token: ")
			chat := prompt(r, "Chat ID: ")
			if token == "" || chat == "" {
				fmt.Println("Token 和 Chat ID 不能为空。")
				continue
			}
			if err := c.request(http.MethodPut, "/v1/config", map[string]any{"telegram_bot_token": token, "telegram_chat_id": chat}, nil); err != nil {
				fmt.Println("保存失败:", err)
			} else {
				fmt.Println("已保存。建议先发送测试消息。")
			}
		case "2":
			if err := c.request(http.MethodPost, "/v1/telegram-test", nil, nil); err != nil {
				fmt.Println("测试失败:", err)
			} else {
				fmt.Println("测试消息已发送。")
			}
		case "3":
			en := !cfg.TelegramEnabled
			if err := c.request(http.MethodPut, "/v1/config", map[string]any{"telegram_enabled": en}, nil); err != nil {
				fmt.Println("修改失败:", err)
			} else {
				fmt.Printf("Telegram 已%s。\n", map[bool]string{true: "启用", false: "关闭"}[en])
			}
		case "4":
			fmt.Println(" 1. 每 4 小时（推荐）\n 2. 每 8 小时\n 3. 每 12 小时\n 4. 每天一次\n 5. 关闭定时摘要")
			m := map[string]int{"1": 4, "2": 8, "3": 12, "4": 24, "5": 0}
			if v, ok := m[prompt(r, "请选择: ")]; ok {
				_ = c.request(http.MethodPut, "/v1/config", map[string]any{"telegram_summary_hours": v}, nil)
			} else {
				fmt.Println("无效选项。")
			}
		case "5":
			v := prompt(r, fmt.Sprintf("连续离线多少分钟后告警 [%d]: ", cfg.TelegramOfflineMinutes))
			if v == "" {
				continue
			}
			m, err := strconv.Atoi(v)
			if err != nil || m < 1 || m > 60 {
				fmt.Println("请输入 1-60。")
				continue
			}
			_ = c.request(http.MethodPut, "/v1/config", map[string]any{"telegram_offline_minutes": m}, nil)
		case "0":
			return
		default:
			fmt.Println("无效选项。")
		}
	}
}

func manageProbeSettings(r *bufio.Reader, c *localClient) {
	for {
		cfg, err := getLocalConfig(c)
		if err != nil {
			fmt.Println("读取线路测试设置失败:", err)
			return
		}
		state := "关闭"
		if cfg.ProbeMode != probeModeOff {
			state = cfg.ProbeRegionLabel + " · " + cfg.ProbeProtocolLabel
		}
		fmt.Println("\n国内线路测试")
		fmt.Println("------------------------------------------------------------")
		fmt.Printf("当前：%s\n", state)
		fmt.Println("1. 选择测试城市")
		fmt.Println("2. 选择测试协议")
		if cfg.ProbeMode == probeModeOff {
			fmt.Println("3. 开启线路测试")
		} else {
			fmt.Println("3. 关闭线路测试")
		}
		fmt.Println("0. 返回")
		switch prompt(r, "请选择: ") {
		case "1":
			fmt.Println(" 1. 北京\n 2. 上海\n 3. 广州")
			regions := map[string]string{"1": probeRegionBeijing, "2": probeRegionShanghai, "3": probeRegionGuangzhou}
			v, ok := regions[prompt(r, "请选择城市: ")]
			if !ok {
				fmt.Println("无效选项。")
				continue
			}
			if err := c.request(http.MethodPut, "/v1/config", map[string]any{"probe_region": v, "probe_mode": probeModeOn}, nil); err != nil {
				fmt.Println("修改失败:", err)
			} else {
				fmt.Printf("已切换到%s三网测试。\n", probeRegionLabel(v))
			}
		case "2":
			fmt.Println(" 1. ICMP（Ping 延迟 / 丢包）\n 2. TCP（53 端口连接延迟 / 失败率）\n 3. UDP（DNS 查询延迟 / 失败率）")
			protocols := map[string]string{"1": probeProtocolICMP, "2": probeProtocolTCP, "3": probeProtocolUDP}
			v, ok := protocols[prompt(r, "请选择协议: ")]
			if !ok {
				fmt.Println("无效选项。")
				continue
			}
			if err := c.request(http.MethodPut, "/v1/config", map[string]any{"probe_protocol": v, "probe_mode": probeModeOn}, nil); err != nil {
				fmt.Println("修改失败:", err)
			} else {
				fmt.Printf("已切换到 %s 测试。\n", probeProtocolLabel(v))
			}
		case "3":
			next := probeModeOff
			if cfg.ProbeMode == probeModeOff {
				next = probeModeOn
			}
			if err := c.request(http.MethodPut, "/v1/config", map[string]any{"probe_mode": next}, nil); err != nil {
				fmt.Println("修改失败:", err)
			} else if next == probeModeOn {
				fmt.Println("线路测试已开启。")
			} else {
				fmt.Println("线路测试已关闭。")
			}
		case "0":
			return
		default:
			fmt.Println("无效选项。")
		}
	}
}

func manageShowConfig(c *localClient) {
	cfg, err := getLocalConfig(c)
	if err != nil {
		fmt.Println("读取失败:", err)
		return
	}
	nodes, _ := getLocalNodes(c)
	fmt.Println("\nMiniProbe 当前设置")
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("Agent 接入地址   : %s\n", dash(cfg.PublicURL))
	fmt.Printf("网络模式         : %s\n", accessModeLabel(cfg.AccessMode))
	fmt.Printf("Dashboard        : %s\n", dashboardModeLabel(cfg.DashboardMode))
	probeState := "关闭"
	if cfg.ProbeMode != probeModeOff {
		probeState = cfg.ProbeRegionLabel + " · " + cfg.ProbeProtocolLabel
	}
	fmt.Printf("国内线路测试     : %s\n", probeState)
	fmt.Printf("节点数量         : %d / %d\n", len(nodes), cfg.NodeSoftLimit)
	fmt.Printf("Telegram         : %s\n", map[bool]string{true: "Enabled", false: "Disabled"}[cfg.TelegramEnabled])
	fmt.Printf("流量摘要         : %s\n", summaryLabel(cfg.TelegramSummaryHours))
	fmt.Printf("MiniProbe 存储   : %s / %s（%.1f%%）\n", formatBytes(cfg.StorageUsed), formatBytes(cfg.StorageHardLimit), percentU64(cfg.StorageUsed, cfg.StorageHardLimit))
	fmt.Println("存储策略         : 不持久化 1 秒遥测；数据库只保存节点最新状态与小型配置。运行时间本身不会让数据无限增长。")
	fmt.Println("管理方式         : 本机 Unix Socket（无公网 Web 管理 API）")
	if cfg.StorageUsed >= cfg.StorageHardLimit {
		fmt.Println("🔴 存储已达到 2 GiB 硬上限，请立即检查 MiniProbe 目录。")
	}
}

func parseTZOffset(v string) (int, error) {
	v = strings.TrimSpace(strings.ToUpper(v))
	v = strings.TrimPrefix(v, "UTC")
	if v == "" {
		return 0, nil
	}
	sign := 1
	if v[0] == '-' {
		sign = -1
		v = v[1:]
	} else if v[0] == '+' {
		v = v[1:]
	} else {
		return 0, errors.New("timezone must start with + or -")
	}
	parts := strings.Split(v, ":")
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	m := 0
	if len(parts) > 1 {
		m, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, err
		}
	}
	if len(parts) > 2 || h > 14 || m < 0 || m > 59 {
		return 0, errors.New("invalid timezone")
	}
	minutes := sign * (h*60 + m)
	if minutes < -14*60 || minutes > 14*60 {
		return 0, errors.New("timezone out of range")
	}
	return minutes, nil
}

func trimFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func boolCurrent(v bool) string {
	if v {
		return "/当前=Y"
	}
	return "/当前=N"
}

func accessModeLabel(mode string) string {
	if mode == accessTunnel {
		return "Cloudflare Tunnel"
	}
	return "Direct HTTP :28888（未加密）"
}

func yesNo(v bool) string {
	if v {
		return "已配置"
	}
	return "未配置"
}
func summaryLabel(hours int) string {
	if hours <= 0 {
		return "关闭"
	}
	if hours == 24 {
		return "每天一次"
	}
	return fmt.Sprintf("每 %d 小时", hours)
}
func percentU64(a, b uint64) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) * 100 / float64(b)
}

func supportsSignedPolicy(v string) bool {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return false
	}
	maj, e1 := strconv.Atoi(parts[0])
	min, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil {
		return false
	}
	return maj > 0 || min >= 4
}

func waitAgentMigration(c *localClient, target string, policyVersion int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	target = strings.TrimRight(target, "/")
	for time.Now().Before(deadline) {
		nodes, err := getLocalNodes(c)
		if err == nil {
			all := true
			for _, n := range nodes {
				if !n.Online || strings.TrimRight(n.AgentEndpoint, "/") != target || n.PolicyVersion < policyVersion {
					all = false
					break
				}
			}
			if all {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return errors.New("等待 Agent 自动迁移超时")
}

func waitHTTPHealth(base string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 8 * time.Second}
	var last error
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/healthz", nil)
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			last = fmt.Errorf("health check returned %s", resp.Status)
		} else {
			last = err
		}
		time.Sleep(2 * time.Second)
	}
	if last == nil {
		last = errors.New("health check timeout")
	}
	return last
}

func waitAdminSocket(socketPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c := newLocalClient(socketPath)
		if err := c.request(http.MethodGet, "/healthz", nil, nil); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return errors.New("本机管理 Socket 等待超时")
}

func setServerListen(addr string) error {
	if err := os.MkdirAll("/etc/miniprobe", 0750); err != nil {
		return err
	}
	content := "MINIPROBE_LISTEN=" + shellQuote(addr) + "\n"
	return os.WriteFile("/etc/miniprobe/server.env", []byte(content), 0600)
}

func restartMiniProbeServer() error {
	if _, err := exec.LookPath("systemctl"); err == nil {
		cmd := exec.Command("systemctl", "restart", "miniprobe-server")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl restart: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if _, err := exec.LookPath("rc-service"); err == nil {
		cmd := exec.Command("rc-service", "miniprobe-server", "restart")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("rc-service restart: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return errors.New("未检测到 systemd/OpenRC")
}

func ensureCloudflared() error {
	if path, err := exec.LookPath("cloudflared"); err == nil {
		if exec.Command(path, "--version").Run() == nil {
			return nil
		}
	}
	arch := runtime.GOARCH
	var asset string
	switch arch {
	case "amd64":
		asset = "cloudflared-linux-amd64"
	case "arm64":
		asset = "cloudflared-linux-arm64"
	default:
		return fmt.Errorf("cloudflared 自动安装暂不支持 %s", arch)
	}
	url := "https://github.com/cloudflare/cloudflared/releases/latest/download/" + asset
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("download cloudflared: %s", resp.Status)
	}
	tmp := "/usr/local/bin/.cloudflared.miniprobe.tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	_, cpErr := io.Copy(f, io.LimitReader(resp.Body, 100*1024*1024))
	closeErr := f.Close()
	if cpErr != nil {
		_ = os.Remove(tmp)
		return cpErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Chmod(tmp, 0755); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, "/usr/local/bin/cloudflared"); err != nil {
		return err
	}
	if out, err := exec.Command("/usr/local/bin/cloudflared", "--version").CombinedOutput(); err != nil {
		return fmt.Errorf("cloudflared check failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func installTunnelService(token string) error {
	if err := os.MkdirAll("/etc/miniprobe", 0750); err != nil {
		return err
	}
	if err := os.MkdirAll("/opt/miniprobe", 0755); err != nil {
		return err
	}
	if err := os.WriteFile("/etc/miniprobe/cloudflared.env", []byte("TUNNEL_TOKEN="+shellQuote(token)+"\n"), 0600); err != nil {
		return err
	}
	runner := "#!/bin/sh\nset -eu\n. /etc/miniprobe/cloudflared.env\nexec /usr/local/bin/cloudflared tunnel --no-autoupdate run --token \"$TUNNEL_TOKEN\"\n"
	if err := os.WriteFile("/opt/miniprobe/run-cloudflared.sh", []byte(runner), 0700); err != nil {
		return err
	}
	if _, err := exec.LookPath("systemctl"); err == nil {
		unit := `[Unit]
Description=MiniProbe Cloudflare Tunnel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/opt/miniprobe/run-cloudflared.sh
Restart=always
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true

[Install]
WantedBy=multi-user.target
`
		if err := os.WriteFile("/etc/systemd/system/miniprobe-cloudflared.service", []byte(unit), 0644); err != nil {
			return err
		}
		if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
			return fmt.Errorf("daemon-reload: %v: %s", err, strings.TrimSpace(string(out)))
		}
		if out, err := exec.Command("systemctl", "enable", "--now", "miniprobe-cloudflared").CombinedOutput(); err != nil {
			return fmt.Errorf("start tunnel: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if _, err := exec.LookPath("rc-service"); err == nil {
		rc := `#!/sbin/openrc-run
name="MiniProbe Cloudflare Tunnel"
command="/opt/miniprobe/run-cloudflared.sh"
command_background="yes"
pidfile="/run/miniprobe-cloudflared.pid"
output_log="/dev/null"
error_log="/dev/null"
`
		if err := os.WriteFile("/etc/init.d/miniprobe-cloudflared", []byte(rc), 0755); err != nil {
			return err
		}
		_ = exec.Command("rc-update", "add", "miniprobe-cloudflared", "default").Run()
		if out, err := exec.Command("rc-service", "miniprobe-cloudflared", "restart").CombinedOutput(); err != nil {
			return fmt.Errorf("start tunnel: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return errors.New("未检测到 systemd/OpenRC")
}

func stopTunnelService() error {
	if _, err := exec.LookPath("systemctl"); err == nil {
		_ = exec.Command("systemctl", "disable", "--now", "miniprobe-cloudflared").Run()
		_ = os.Remove("/etc/systemd/system/miniprobe-cloudflared.service")
		_ = exec.Command("systemctl", "daemon-reload").Run()
	} else if _, err := exec.LookPath("rc-service"); err == nil {
		_ = exec.Command("rc-service", "miniprobe-cloudflared", "stop").Run()
		_ = exec.Command("rc-update", "del", "miniprobe-cloudflared", "default").Run()
		_ = os.Remove("/etc/init.d/miniprobe-cloudflared")
	}
	_ = os.Remove("/etc/miniprobe/cloudflared.env")
	return nil
}

func prompt(r *bufio.Reader, label string) string {
	fmt.Print(label)
	s, _ := r.ReadString('\n')
	return strings.TrimSpace(s)
}

func promptPassword(r *bufio.Reader, label string) string {
	fmt.Print(label)
	hidden := false
	if _, err := exec.LookPath("stty"); err == nil {
		cmd := exec.Command("stty", "-echo")
		cmd.Stdin = os.Stdin
		if cmd.Run() == nil {
			hidden = true
		}
	}
	s, _ := r.ReadString('\n')
	if hidden {
		cmd := exec.Command("stty", "echo")
		cmd.Stdin = os.Stdin
		_ = cmd.Run()
		fmt.Println()
	}
	return strings.TrimSpace(s)
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if v < 0 {
		return 0
	}
	return v
}

func dashboardModeLabel(mode string) string {
	switch mode {
	case dashboardPublic:
		return "Public"
	case dashboardProtected:
		return "Password Protected"
	case dashboardDisabled:
		return "Disabled"
	default:
		return mode
	}
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "未设置"
	}
	return s
}

func trimRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
