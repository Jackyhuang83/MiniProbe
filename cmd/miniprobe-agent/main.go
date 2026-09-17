package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Jackyhuang83/MiniProbe/internal/common"
)

const (
	agentVersion  = "0.4.2-alpha"
	probeInterval = 10 * time.Second
)

type cfg struct {
	Endpoint    string
	Token       string
	NodeID      string
	ServerKey   string
	BootstrapID string
	StateFile   string
	Interval    time.Duration
	Interface   string
}

type cpuSample struct{ idle, total uint64 }
type netSample struct {
	rx, tx uint64
	at     time.Time
}

type probeState struct {
	mu       sync.Mutex
	hist     []int
	latHist  []int
	lossHist []int
}

type probeTask struct {
	name   string
	target string
}

type probeConfig struct {
	enabled  bool
	region   string
	city     string
	protocol string
	tasks    []probeTask
}

type trafficState struct {
	CycleStart          time.Time `json:"cycle_start"`
	InboundBytes        uint64    `json:"inbound_bytes"`
	OutboundBytes       uint64    `json:"outbound_bytes"`
	LastRawRx           uint64    `json:"last_raw_rx"`
	LastRawTx           uint64    `json:"last_raw_tx"`
	ProtectionTriggered bool      `json:"protection_triggered"`
}

type agentState struct {
	Endpoint           string             `json:"endpoint,omitempty"`
	BootstrapID        string             `json:"bootstrap_id,omitempty"`
	PolicyVersion      int64              `json:"policy_version,omitempty"`
	Policy             common.AgentPolicy `json:"policy"`
	ProbePolicyVersion int64              `json:"probe_policy_version,omitempty"`
	ProbePolicy        common.ProbePolicy `json:"probe_policy"`
	Traffic            trafficState       `json:"traffic"`
}

var probeStates sync.Map

func main() {
	var c cfg
	flag.StringVar(&c.Endpoint, "endpoint", getenv("MINIPROBE_ENDPOINT", ""), "MiniProbe server URL")
	flag.StringVar(&c.Token, "token", getenv("MINIPROBE_TOKEN", ""), "agent token")
	flag.StringVar(&c.NodeID, "node-id", getenv("MINIPROBE_NODE_ID", ""), "node id (default hostname)")
	flag.StringVar(&c.ServerKey, "server-key", getenv("MINIPROBE_SERVER_KEY", ""), "base64 Ed25519 server public key")
	flag.StringVar(&c.BootstrapID, "bootstrap-id", getenv("MINIPROBE_BOOTSTRAP_ID", ""), "local installer generation id")
	flag.StringVar(&c.StateFile, "state-file", getenv("MINIPROBE_STATE_FILE", "/var/lib/miniprobe-agent/state.json"), "persistent agent state")
	flag.DurationVar(&c.Interval, "interval", 2*time.Second, "report interval")
	flag.StringVar(&c.Interface, "interface", getenv("MINIPROBE_INTERFACE", "auto"), "network interface or auto")
	flag.Parse()
	if c.Endpoint == "" || c.Token == "" {
		fmt.Fprintln(os.Stderr, "endpoint and token are required")
		os.Exit(2)
	}
	if c.NodeID == "" {
		c.NodeID, _ = os.Hostname()
	}
	c.Endpoint = strings.TrimRight(c.Endpoint, "/")

	state := loadAgentState(c.StateFile)
	if state.Policy.NodeID != "" && state.Policy.NodeID != c.NodeID {
		state = agentState{Endpoint: c.Endpoint, BootstrapID: c.BootstrapID}
	}
	// A manual reinstall/update is an explicit local administrator action. A new
	// bootstrap ID makes the installer-provided endpoint authoritative while
	// preserving the billing-cycle traffic counters. Normal service restarts keep
	// the signed-policy migrated endpoint from state.
	if c.BootstrapID != "" && c.BootstrapID != state.BootstrapID {
		state.Endpoint = c.Endpoint
		state.BootstrapID = c.BootstrapID
		state.PolicyVersion = 0
		state.Policy = common.AgentPolicy{}
		state.ProbePolicyVersion = 0
		state.ProbePolicy = common.ProbePolicy{}
	}
	if state.Endpoint == "" {
		state.Endpoint = c.Endpoint
	}

	pubKey, err := decodeServerKey(c.ServerKey)
	if err != nil && c.ServerKey != "" {
		fmt.Fprintf(os.Stderr, "MiniProbe Agent: invalid server key: %v\n", err)
	}

	info := collectStatic()
	prevCPU := readCPU()
	prevNet := readNet(c.Interface)
	client := &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: true}}
	lastSave := time.Time{}
	lastErrLog := time.Time{}
	reportingFailed := false
	lastProbeAt := time.Time{}
	lastProbeKey := ""
	var lastProbes []common.ProbeResult

	// Establish the traffic baseline without counting traffic that happened
	// before MiniProbe was installed.
	if state.Traffic.LastRawRx == 0 && state.Traffic.LastRawTx == 0 {
		state.Traffic.LastRawRx = prevNet.rx
		state.Traffic.LastRawTx = prevNet.tx
		_ = saveAgentState(c.StateFile, state)
	}

	for {
		start := time.Now()
		curCPU := readCPU()
		curNet := readNet(c.Interface)
		m := collectMetrics(prevCPU, curCPU, prevNet, curNet)
		prevCPU, prevNet = curCPU, curNet

		traffic, shouldShutdown := updateTraffic(&state, curNet, time.Now())
		probeCfg := probeConfigFromPolicy(state.ProbePolicy)
		probeKey := probeConfigKey(probeCfg)
		if probeKey != lastProbeKey || lastProbeAt.IsZero() || time.Since(lastProbeAt) >= probeInterval {
			if probeCfg.enabled {
				lastProbes = runProbes(probeCfg)
			} else {
				lastProbes = nil
			}
			lastProbeAt = time.Now()
			lastProbeKey = probeKey
		}
		endpoint := strings.TrimRight(state.Endpoint, "/")
		probeProtocol := probeCfg.protocol
		if !probeCfg.enabled {
			probeProtocol = "off"
		}
		rep := common.Report{
			NodeID: c.NodeID, AgentVersion: agentVersion, AgentEndpoint: endpoint, PolicyVersion: state.PolicyVersion,
			ProbeRegion: probeCfg.region, ProbeProtocol: probeProtocol,
			Info: info, Metrics: m, Traffic: traffic, Probes: lastProbes, At: time.Now().UTC(),
		}
		response, sendErr := send(client, endpoint, c.Token, rep)
		if sendErr != nil {
			if !reportingFailed || time.Since(lastErrLog) >= time.Minute {
				fmt.Fprintf(os.Stderr, "%s MiniProbe report failed: %v\n", time.Now().Format(time.RFC3339), sendErr)
				lastErrLog = time.Now()
			}
			reportingFailed = true
		} else {
			if reportingFailed {
				fmt.Fprintf(os.Stderr, "%s MiniProbe connection recovered\n", time.Now().Format(time.RFC3339))
			}
			reportingFailed = false
			if response != nil && len(pubKey) == ed25519.PublicKeySize {
				stateChanged := false
				if response.Policy != nil {
					if changed, err := applySignedPolicy(client, c.NodeID, pubKey, response.Policy, &state); err != nil {
						if time.Since(lastErrLog) >= time.Minute {
							fmt.Fprintf(os.Stderr, "%s MiniProbe policy ignored: %v\n", time.Now().Format(time.RFC3339), err)
							lastErrLog = time.Now()
						}
					} else if changed {
						stateChanged = true
					}
				}
				if response.ProbePolicy != nil {
					if changed, err := applySignedProbePolicy(c.NodeID, pubKey, response.ProbePolicy, &state); err != nil {
						if time.Since(lastErrLog) >= time.Minute {
							fmt.Fprintf(os.Stderr, "%s MiniProbe probe policy ignored: %v\n", time.Now().Format(time.RFC3339), err)
							lastErrLog = time.Now()
						}
					} else if changed {
						stateChanged = true
					}
				}
				if stateChanged {
					_ = saveAgentState(c.StateFile, state)
					lastSave = time.Now()
				}
			}
		}

		if shouldShutdown {
			state.Traffic.ProtectionTriggered = true
			_ = saveAgentState(c.StateFile, state)
			fmt.Fprintf(os.Stderr, "%s MiniProbe traffic protection triggered; powering off host\n", time.Now().Format(time.RFC3339))
			time.Sleep(2 * time.Second)
			if err := powerOffHost(); err != nil {
				fmt.Fprintf(os.Stderr, "MiniProbe poweroff failed: %v\n", err)
			}
		}

		if lastSave.IsZero() || time.Since(lastSave) >= 30*time.Second {
			_ = saveAgentState(c.StateFile, state)
			lastSave = time.Now()
		}
		d := c.Interval - time.Since(start)
		if d > 0 {
			time.Sleep(d)
		}
	}
}

func send(client *http.Client, endpoint, token string, r common.Report) (*common.ReportResponse, error) {
	b, _ := json.Marshal(r)
	req, err := http.NewRequest(http.MethodPost, endpoint+"/api/v1/report", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("server returned %s", resp.Status)
	}
	var out common.ReportResponse
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, fmt.Errorf("decode server response: %w", err)
		}
	}
	return &out, nil
}

func decodeServerKey(raw string) (ed25519.PublicKey, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("server public key must be base64url Ed25519 key")
	}
	return ed25519.PublicKey(b), nil
}

func applySignedPolicy(client *http.Client, nodeID string, key ed25519.PublicKey, signed *common.SignedPolicy, st *agentState) (bool, error) {
	if signed == nil {
		return false, nil
	}
	p := signed.Policy
	if p.NodeID != nodeID {
		return false, errors.New("policy node mismatch")
	}
	if p.Version < st.PolicyVersion {
		return false, errors.New("replayed policy version")
	}
	sig, err := base64.RawURLEncoding.DecodeString(signed.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false, errors.New("invalid policy signature encoding")
	}
	payload, _ := json.Marshal(p)
	if !ed25519.Verify(key, payload, sig) {
		return false, errors.New("invalid policy signature")
	}
	nextEndpoint := strings.TrimRight(strings.TrimSpace(p.Endpoint), "/")
	if nextEndpoint == "" {
		return false, errors.New("policy endpoint is empty")
	}
	if nextEndpoint != st.Endpoint {
		if err := healthCheckEndpoint(client, nextEndpoint); err != nil {
			return false, fmt.Errorf("new endpoint not reachable: %w", err)
		}
	}
	changed := p.Version != st.PolicyVersion || nextEndpoint != st.Endpoint || st.Policy != p
	st.Endpoint = nextEndpoint
	st.PolicyVersion = p.Version
	st.Policy = p
	return changed, nil
}

func applySignedProbePolicy(nodeID string, key ed25519.PublicKey, signed *common.SignedProbePolicy, st *agentState) (bool, error) {
	if signed == nil {
		return false, nil
	}
	p := signed.Policy
	if p.NodeID != nodeID {
		return false, errors.New("probe policy node mismatch")
	}
	if p.Version < st.ProbePolicyVersion {
		return false, errors.New("replayed probe policy version")
	}
	if p.Region != "beijing" && p.Region != "shanghai" && p.Region != "guangzhou" {
		return false, errors.New("invalid probe region")
	}
	if p.Protocol != "icmp" && p.Protocol != "tcp" && p.Protocol != "udp" {
		return false, errors.New("invalid probe protocol")
	}
	if net.ParseIP(p.Telecom) == nil || net.ParseIP(p.Unicom) == nil || net.ParseIP(p.Mobile) == nil {
		return false, errors.New("invalid probe target")
	}
	sig, err := base64.RawURLEncoding.DecodeString(signed.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false, errors.New("invalid probe policy signature encoding")
	}
	payload, _ := json.Marshal(p)
	if !ed25519.Verify(key, payload, sig) {
		return false, errors.New("invalid probe policy signature")
	}
	changed := p.Version != st.ProbePolicyVersion || st.ProbePolicy != p
	st.ProbePolicyVersion = p.Version
	st.ProbePolicy = p
	return changed, nil
}

func healthCheckEndpoint(client *http.Client, endpoint string) error {
	req, err := http.NewRequest(http.MethodGet, endpoint+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned %s", resp.Status)
	}
	return nil
}

func updateTraffic(st *agentState, cur netSample, now time.Time) (common.TrafficSnapshot, bool) {
	p := st.Policy
	if p.TrafficDirection == "" {
		p.TrafficDirection = "total"
	}
	if p.TrafficResetDay < 1 || p.TrafficResetDay > 28 {
		p.TrafficResetDay = 1
	}
	if p.ShutdownPercent <= 0 || p.ShutdownPercent > 100 {
		p.ShutdownPercent = 95
	}
	start := billingCycleStart(now, p.TrafficResetDay, p.TrafficResetTZMinutes)
	ts := &st.Traffic
	if ts.CycleStart.IsZero() || !ts.CycleStart.Equal(start) {
		*ts = trafficState{CycleStart: start, LastRawRx: cur.rx, LastRawTx: cur.tx}
	} else {
		if cur.rx >= ts.LastRawRx {
			ts.InboundBytes += cur.rx - ts.LastRawRx
		}
		if cur.tx >= ts.LastRawTx {
			ts.OutboundBytes += cur.tx - ts.LastRawTx
		}
		// When the kernel counter resets (reboot/interface recreation), the
		// new counter becomes the baseline; we never subtract traffic.
		ts.LastRawRx = cur.rx
		ts.LastRawTx = cur.tx
	}
	used := trafficUsed(p.TrafficDirection, ts.InboundBytes, ts.OutboundBytes)
	pct := 0.0
	if p.TrafficLimitBytes > 0 {
		pct = float64(used) * 100 / float64(p.TrafficLimitBytes)
	}
	shouldShutdown := p.ShutdownEnabled && p.TrafficLimitBytes > 0 && pct >= float64(p.ShutdownPercent) && !ts.ProtectionTriggered
	snap := common.TrafficSnapshot{
		CycleStart: ts.CycleStart, InboundBytes: ts.InboundBytes, OutboundBytes: ts.OutboundBytes,
		UsedBytes: used, LimitBytes: p.TrafficLimitBytes, UsagePercent: pct, Direction: p.TrafficDirection,
		ResetDay: p.TrafficResetDay, ResetTZMinutes: p.TrafficResetTZMinutes,
		ProtectionTriggered: shouldShutdown || ts.ProtectionTriggered,
	}
	return snap, shouldShutdown
}

func billingCycleStart(now time.Time, resetDay, tzMinutes int) time.Time {
	if resetDay < 1 || resetDay > 28 {
		resetDay = 1
	}
	if tzMinutes < -14*60 {
		tzMinutes = -14 * 60
	}
	if tzMinutes > 14*60 {
		tzMinutes = 14 * 60
	}
	loc := time.FixedZone("billing", tzMinutes*60)
	t := now.In(loc)
	y, m := t.Year(), t.Month()
	if t.Day() < resetDay {
		m--
		if m < time.January {
			m = time.December
			y--
		}
	}
	return time.Date(y, m, resetDay, 0, 0, 0, 0, loc).UTC()
}

func trafficUsed(direction string, in, out uint64) uint64 {
	switch direction {
	case "inbound":
		return in
	case "outbound":
		return out
	default:
		if ^uint64(0)-in < out {
			return ^uint64(0)
		}
		return in + out
	}
}

func loadAgentState(path string) agentState {
	var st agentState
	b, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st
}

func saveAgentState(path string, st agentState) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func powerOffHost() error {
	if os.Getenv("MINIPROBE_DISABLE_POWEROFF") == "1" {
		return errors.New("poweroff disabled by MINIPROBE_DISABLE_POWEROFF")
	}
	candidates := [][]string{
		{"systemctl", "poweroff", "--no-block"},
		{"shutdown", "-h", "now"},
		{"poweroff"},
	}
	var last error
	for _, args := range candidates {
		path, err := exec.LookPath(args[0])
		if err != nil {
			last = err
			continue
		}
		cmd := exec.Command(path, args[1:]...)
		if err := cmd.Start(); err == nil {
			return nil
		} else {
			last = err
		}
	}
	if last == nil {
		last = errors.New("no supported poweroff command found")
	}
	return last
}

func collectStatic() common.StaticInfo {
	h, _ := os.Hostname()
	kernelBytes, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	kernel := strings.TrimSpace(string(kernelBytes))
	memTotal, _, swapTotal, _ := readMem()
	dt, _ := diskUsage("/")
	return common.StaticInfo{Hostname: h, OS: readOS(), Kernel: kernel, Arch: runtime.GOARCH, Virtualization: detectVirt(), CPUModel: cpuModel(), CPUCores: runtime.NumCPU(), IPv4: ips(false), IPv6: ips(true), MemTotal: memTotal, SwapTotal: swapTotal, DiskTotal: dt}
}

func collectMetrics(a, b cpuSample, n1, n2 netSample) common.Metrics {
	mt, mu, st, su := readMem()
	_ = mt
	_ = st
	dt, du := diskUsage("/")
	_ = dt
	l1, l5, l15 := load()
	up := uptime()
	tcp := countNet("/proc/net/tcp") + countNet("/proc/net/tcp6")
	udp := countNet("/proc/net/udp") + countNet("/proc/net/udp6")
	secs := n2.at.Sub(n1.at).Seconds()
	var rx, tx uint64
	if secs > 0 {
		if n2.rx >= n1.rx {
			rx = uint64(float64(n2.rx-n1.rx) / secs)
		}
		if n2.tx >= n1.tx {
			tx = uint64(float64(n2.tx-n1.tx) / secs)
		}
	}
	return common.Metrics{CPUPercent: cpuPct(a, b), MemUsed: mu, SwapUsed: su, DiskUsed: du, Load1: l1, Load5: l5, Load15: l15, NetRxBps: rx, NetTxBps: tx, NetRxTotal: n2.rx, NetTxTotal: n2.tx, TCPConnections: tcp, UDPConnections: udp, ProcessCount: processCount(), UptimeSeconds: up}
}

func readCPU() cpuSample {
	b, _ := os.ReadFile("/proc/stat")
	f := strings.Fields(strings.SplitN(string(b), "\n", 2)[0])
	var vals []uint64
	for _, s := range f[1:] {
		v, _ := strconv.ParseUint(s, 10, 64)
		vals = append(vals, v)
	}
	var total uint64
	for _, v := range vals {
		total += v
	}
	var idle uint64
	if len(vals) > 3 {
		idle = vals[3]
	}
	if len(vals) > 4 {
		idle += vals[4]
	}
	return cpuSample{idle, total}
}
func cpuPct(a, b cpuSample) float64 {
	td := b.total - a.total
	id := b.idle - a.idle
	if td == 0 {
		return 0
	}
	return float64(td-id) * 100 / float64(td)
}
func readMem() (uint64, uint64, uint64, uint64) {
	m := map[string]uint64{}
	f, _ := os.Open("/proc/meminfo")
	if f == nil {
		return 0, 0, 0, 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		p := strings.Fields(s.Text())
		if len(p) >= 2 {
			v, _ := strconv.ParseUint(p[1], 10, 64)
			m[strings.TrimSuffix(p[0], ":")] = v * 1024
		}
	}
	total := m["MemTotal"]
	avail := m["MemAvailable"]
	st := m["SwapTotal"]
	sf := m["SwapFree"]
	return total, total - avail, st, st - sf
}
func diskUsage(path string) (uint64, uint64) {
	var s syscall.Statfs_t
	if syscall.Statfs(path, &s) != nil {
		return 0, 0
	}
	total := s.Blocks * uint64(s.Bsize)
	free := s.Bavail * uint64(s.Bsize)
	return total, total - free
}
func load() (float64, float64, float64) {
	b, _ := os.ReadFile("/proc/loadavg")
	f := strings.Fields(string(b))
	if len(f) < 3 {
		return 0, 0, 0
	}
	a, _ := strconv.ParseFloat(f[0], 64)
	b1, _ := strconv.ParseFloat(f[1], 64)
	c, _ := strconv.ParseFloat(f[2], 64)
	return a, b1, c
}
func uptime() uint64 {
	b, _ := os.ReadFile("/proc/uptime")
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return uint64(v)
}
func processCount() int {
	e, _ := os.ReadDir("/proc")
	n := 0
	for _, x := range e {
		if x.IsDir() {
			if _, err := strconv.Atoi(x.Name()); err == nil {
				n++
			}
		}
	}
	return n
}
func countNet(p string) int {
	f, err := os.Open(p)
	if err != nil {
		return 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	n := -1
	for s.Scan() {
		n++
	}
	if n < 0 {
		return 0
	}
	return n
}

func readNet(iface string) netSample {
	f, _ := os.Open("/proc/net/dev")
	if f == nil {
		return netSample{at: time.Now()}
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	var rx, tx uint64
	for s.Scan() {
		line := s.Text()
		if !strings.Contains(line, ":") {
			continue
		}
		p := strings.SplitN(line, ":", 2)
		name := strings.TrimSpace(p[0])
		if name == "lo" || strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "br-") {
			continue
		}
		if iface != "auto" && iface != "" && name != iface {
			continue
		}
		fs := strings.Fields(p[1])
		if len(fs) >= 9 {
			r, _ := strconv.ParseUint(fs[0], 10, 64)
			t, _ := strconv.ParseUint(fs[8], 10, 64)
			rx += r
			tx += t
		}
	}
	return netSample{rx, tx, time.Now()}
}

func probeConfigFromPolicy(p common.ProbePolicy) probeConfig {
	region := strings.ToLower(strings.TrimSpace(p.Region))
	protocol := strings.ToLower(strings.TrimSpace(p.Protocol))
	enabled := p.Enabled

	// Empty probe fields mean the Agent has not received the v0.4.2 policy yet
	// (or is temporarily talking to an older Server). Use the new defaults until
	// the signed Server policy arrives.
	if region == "" && protocol == "" && p.Telecom == "" && p.Unicom == "" && p.Mobile == "" {
		region = "guangzhou"
		protocol = "icmp"
		enabled = true
	}
	if region != "beijing" && region != "shanghai" && region != "guangzhou" {
		region = "guangzhou"
	}
	if protocol != "icmp" && protocol != "tcp" && protocol != "udp" {
		protocol = "icmp"
	}

	city, telecom, unicom, mobile := builtInProbeTargets(region)
	if strings.TrimSpace(p.Telecom) != "" {
		telecom = strings.TrimSpace(p.Telecom)
	}
	if strings.TrimSpace(p.Unicom) != "" {
		unicom = strings.TrimSpace(p.Unicom)
	}
	if strings.TrimSpace(p.Mobile) != "" {
		mobile = strings.TrimSpace(p.Mobile)
	}
	return probeConfig{
		enabled: enabled, region: region, city: city, protocol: protocol,
		tasks: []probeTask{{name: city + "电信", target: telecom}, {name: city + "联通", target: unicom}, {name: city + "移动", target: mobile}},
	}
}

func probeConfigKey(cfg probeConfig) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%t|%s|%s", cfg.enabled, cfg.region, cfg.protocol)
	for _, t := range cfg.tasks {
		b.WriteByte('|')
		b.WriteString(t.target)
	}
	return b.String()
}

func builtInProbeTargets(region string) (city, telecom, unicom, mobile string) {
	switch region {
	case "beijing":
		return "北京", "219.141.136.10", "202.106.0.20", "221.130.33.60"
	case "shanghai":
		return "上海", "202.96.209.133", "210.22.70.3", "211.136.112.50"
	default:
		return "广州", "202.96.128.86", "210.21.4.130", "211.136.192.6"
	}
}

func runProbes(cfg probeConfig) []common.ProbeResult {
	out := make([]common.ProbeResult, len(cfg.tasks))
	var wg sync.WaitGroup
	for i, t := range cfg.tasks {
		wg.Add(1)
		go func(i int, t probeTask) {
			defer wg.Done()
			out[i] = probeTarget(cfg.protocol, t.name, t.target)
		}(i, t)
	}
	wg.Wait()
	return out
}

func probeTarget(protocol, name, target string) common.ProbeResult {
	if !targetUsableOnHost(net.JoinHostPort(target, "53")) {
		return common.ProbeResult{Name: name, Target: target, Available: false, LatencyMS: -1, LossPct: 0}
	}
	const attempts = 4
	var ok int
	var sum float64
	for i := 0; i < attempts; i++ {
		var ms float64
		var success bool
		switch protocol {
		case "tcp":
			ms, success = probeTCPOnce(target)
		case "udp":
			ms, success = probeUDPOnce(target, uint16((time.Now().UnixNano()+int64(i))&0xffff))
		default:
			ms, success = probeICMPOnce(target, uint16(os.Getpid()&0xffff), uint16(i+1))
		}
		if success {
			ok++
			sum += ms
		}
	}

	loss := float64(attempts-ok) * 100 / attempts
	lat := -1.0
	if ok > 0 {
		lat = sum / float64(ok)
	}
	latState := latencyQuality(lat)
	lossState := lossQuality(loss)
	quality := latState
	if lossState > quality {
		quality = lossState
	}
	key := protocol + "|" + name
	hist, latHist, lossHist := appendHistories(key, quality, latState, lossState)
	return common.ProbeResult{Name: name, Target: target, Available: true, LatencyMS: lat, LossPct: loss, History: hist, LatencyHistory: latHist, LossHistory: lossHist}
}

func latencyQuality(lat float64) int {
	if lat < 0 {
		return 3
	}
	if lat > 200 {
		return 2
	}
	if lat > 100 {
		return 1
	}
	return 0
}

func lossQuality(loss float64) int {
	switch {
	case loss >= 100:
		return 3
	case loss >= 50:
		return 2
	case loss > 0:
		return 1
	default:
		return 0
	}
}

func probeTCPOnce(target string) (float64, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
	defer cancel()
	d := net.Dialer{Timeout: 900 * time.Millisecond}
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(target, "53"))
	if err != nil {
		return 0, false
	}
	_ = conn.Close()
	return float64(time.Since(start).Microseconds()) / 1000, true
}

func probeUDPOnce(target string, id uint16) (float64, bool) {
	conn, err := net.DialTimeout("udp", net.JoinHostPort(target, "53"), 900*time.Millisecond)
	if err != nil {
		return 0, false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(900 * time.Millisecond))
	query := dnsQuery(id)
	start := time.Now()
	if _, err := conn.Write(query); err != nil {
		return 0, false
	}
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil || n < 12 || binary.BigEndian.Uint16(buf[:2]) != id {
		return 0, false
	}
	return float64(time.Since(start).Microseconds()) / 1000, true
}

func dnsQuery(id uint16) []byte {
	q := make([]byte, 12, 64)
	binary.BigEndian.PutUint16(q[0:2], id)
	binary.BigEndian.PutUint16(q[2:4], 0x0100) // recursion desired
	binary.BigEndian.PutUint16(q[4:6], 1)      // one question
	for _, label := range strings.Split("www.baidu.com", ".") {
		q = append(q, byte(len(label)))
		q = append(q, label...)
	}
	q = append(q, 0)
	q = append(q, 0, 1) // A
	q = append(q, 0, 1) // IN
	return q
}

func probeICMPOnce(target string, id, seq uint16) (float64, bool) {
	ip := net.ParseIP(target)
	if ip == nil || ip.To4() == nil {
		return 0, false
	}
	conn, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return 0, false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(900 * time.Millisecond))
	packet := make([]byte, 8+16)
	packet[0] = 8 // echo request
	binary.BigEndian.PutUint16(packet[4:6], id)
	binary.BigEndian.PutUint16(packet[6:8], seq)
	binary.BigEndian.PutUint64(packet[8:16], uint64(time.Now().UnixNano()))
	binary.BigEndian.PutUint64(packet[16:24], uint64(seq))
	binary.BigEndian.PutUint16(packet[2:4], icmpChecksum(packet))
	start := time.Now()
	if _, err := conn.WriteTo(packet, &net.IPAddr{IP: ip}); err != nil {
		return 0, false
	}
	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return 0, false
		}
		msg := buf[:n]
		if len(msg) >= 20 && msg[0]>>4 == 4 {
			hl := int(msg[0]&0x0f) * 4
			if hl >= len(msg) {
				continue
			}
			msg = msg[hl:]
		}
		if len(msg) < 8 || msg[0] != 0 {
			continue
		}
		if binary.BigEndian.Uint16(msg[4:6]) != id || binary.BigEndian.Uint16(msg[6:8]) != seq {
			continue
		}
		return float64(time.Since(start).Microseconds()) / 1000, true
	}
}

func icmpChecksum(b []byte) uint16 {
	var sum uint32
	for len(b) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(b[:2]))
		b = b[2:]
	}
	if len(b) == 1 {
		sum += uint32(b[0]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func targetUsableOnHost(target string) bool {
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return true
	}
	host = strings.Trim(host, "[]")
	has4, has6 := localFamilies()
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			return has4
		}
		return has6
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return true // DNS/transient failures are measured by the actual probe.
	}
	for _, a := range addrs {
		if a.IP.To4() != nil && has4 {
			return true
		}
		if a.IP.To4() == nil && has6 {
			return true
		}
	}
	return false
}

func localFamilies() (bool, bool) {
	var has4, has6 bool
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		ip, _, err := net.ParseCIDR(a.String())
		if err != nil || ip.IsLoopback() || !ip.IsGlobalUnicast() {
			continue
		}
		if ip.To4() != nil {
			has4 = true
		} else {
			has6 = true
		}
	}
	return has4, has6
}

func appendHistories(name string, quality, latency, loss int) ([]int, []int, []int) {
	x, _ := probeStates.LoadOrStore(name, &probeState{})
	ps := x.(*probeState)
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.hist = append(ps.hist, quality)
	ps.latHist = append(ps.latHist, latency)
	ps.lossHist = append(ps.lossHist, loss)
	if len(ps.hist) > 30 {
		ps.hist = ps.hist[len(ps.hist)-30:]
	}
	if len(ps.latHist) > 30 {
		ps.latHist = ps.latHist[len(ps.latHist)-30:]
	}
	if len(ps.lossHist) > 30 {
		ps.lossHist = ps.lossHist[len(ps.lossHist)-30:]
	}
	return append([]int(nil), ps.hist...), append([]int(nil), ps.latHist...), append([]int(nil), ps.lossHist...)
}

func readOS() string {
	b, _ := os.ReadFile("/etc/os-release")
	name, ver := "Linux", ""
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(l, "PRETTY_NAME="), "\"")
		}
		if strings.HasPrefix(l, "NAME=") {
			name = strings.Trim(strings.TrimPrefix(l, "NAME="), "\"")
		}
		if strings.HasPrefix(l, "VERSION_ID=") {
			ver = strings.Trim(strings.TrimPrefix(l, "VERSION_ID="), "\"")
		}
	}
	return strings.TrimSpace(name + " " + ver)
}
func cpuModel() string {
	b, _ := os.ReadFile("/proc/cpuinfo")
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "model name") || strings.HasPrefix(l, "Hardware") {
			if p := strings.SplitN(l, ":", 2); len(p) == 2 {
				return strings.TrimSpace(p[1])
			}
		}
	}
	return runtime.GOARCH
}
func ips(v6 bool) []string {
	var out []string
	as, _ := net.InterfaceAddrs()
	for _, a := range as {
		ip, _, err := net.ParseCIDR(a.String())
		if err != nil || ip.IsLoopback() {
			continue
		}
		is6 := ip.To4() == nil
		if is6 == v6 {
			out = append(out, ip.String())
		}
	}
	return out
}
func detectVirt() string {
	for _, c := range [][]string{{"systemd-detect-virt"}, {"virt-what"}} {
		if p, err := exec.Command(c[0]).Output(); err == nil && strings.TrimSpace(string(p)) != "" {
			return strings.TrimSpace(string(p))
		}
	}
	b, _ := os.ReadFile("/proc/1/cgroup")
	s := string(b)
	for _, v := range []string{"docker", "lxc", "kubepods"} {
		if strings.Contains(s, v) {
			return v
		}
	}
	return "unknown"
}
func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

var _ = errors.New
