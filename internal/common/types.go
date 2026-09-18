package common

import "time"

type ProbeResult struct {
	Name           string  `json:"name"`
	Available      bool    `json:"available"`
	Target         string  `json:"target"`
	LatencyMS      float64 `json:"latency_ms"`
	LossPct        float64 `json:"loss_pct"`
	History        []int   `json:"history"`                   // backward-compatible combined quality history
	LatencyHistory []int   `json:"latency_history,omitempty"` // 0=good 1=warn 2=bad 3=timeout
	LossHistory    []int   `json:"loss_history,omitempty"`    // 0=0% 1=minor 2=major 3=100%
}

type StaticInfo struct {
	Hostname       string   `json:"hostname"`
	OS             string   `json:"os"`
	Kernel         string   `json:"kernel"`
	Arch           string   `json:"arch"`
	Virtualization string   `json:"virtualization"`
	CPUModel       string   `json:"cpu_model"`
	CPUCores       int      `json:"cpu_cores"`
	IPv4           []string `json:"ipv4"`
	IPv6           []string `json:"ipv6"`
	NetworkTypes   []string `json:"network_types,omitempty"`
	MemTotal       uint64   `json:"mem_total"`
	SwapTotal      uint64   `json:"swap_total"`
	DiskTotal      uint64   `json:"disk_total"`
}

type Metrics struct {
	CPUPercent     float64 `json:"cpu_percent"`
	MemUsed        uint64  `json:"mem_used"`
	SwapUsed       uint64  `json:"swap_used"`
	DiskUsed       uint64  `json:"disk_used"`
	Load1          float64 `json:"load1"`
	Load5          float64 `json:"load5"`
	Load15         float64 `json:"load15"`
	NetRxBps       uint64  `json:"net_rx_bps"`
	NetTxBps       uint64  `json:"net_tx_bps"`
	NetRxTotal     uint64  `json:"net_rx_total"`
	NetTxTotal     uint64  `json:"net_tx_total"`
	TCPConnections int     `json:"tcp_connections"`
	UDPConnections int     `json:"udp_connections"`
	ProcessCount   int     `json:"process_count"`
	UptimeSeconds  uint64  `json:"uptime_seconds"`
}

// TrafficSnapshot is the Agent's persistent billing-cycle traffic counter.
// It is deliberately separate from /proc/net/dev totals, which reset on reboot.
type TrafficSnapshot struct {
	CycleStart          time.Time `json:"cycle_start"`
	InboundBytes        uint64    `json:"inbound_bytes"`
	OutboundBytes       uint64    `json:"outbound_bytes"`
	UsedBytes           uint64    `json:"used_bytes"`
	LimitBytes          uint64    `json:"limit_bytes"`
	UsagePercent        float64   `json:"usage_percent"`
	Direction           string    `json:"direction"` // inbound, outbound, total
	ResetDay            int       `json:"reset_day"`
	ResetTZMinutes      int       `json:"reset_tz_minutes"`
	ProtectionTriggered bool      `json:"protection_triggered"`
}

type Report struct {
	NodeID        string          `json:"node_id"`
	AgentVersion  string          `json:"agent_version,omitempty"`
	AgentEndpoint string          `json:"agent_endpoint,omitempty"`
	PolicyVersion int64           `json:"policy_version,omitempty"`
	ProbeRegion   string          `json:"probe_region,omitempty"`
	ProbeProtocol string          `json:"probe_protocol,omitempty"`
	Info          StaticInfo      `json:"info"`
	Metrics       Metrics         `json:"metrics"`
	Traffic       TrafficSnapshot `json:"traffic"`
	Probes        []ProbeResult   `json:"probes"`
	At            time.Time       `json:"at"`
}

// AgentPolicy carries endpoint and traffic-safety configuration. ProbePolicy
// is signed separately so older Agents can ignore the new probe field without
// breaking verification of the existing policy. Neither policy contains a
// generic command-execution primitive.
type AgentPolicy struct {
	Version               int64  `json:"version"`
	NodeID                string `json:"node_id"`
	Endpoint              string `json:"endpoint"`
	TrafficLimitBytes     uint64 `json:"traffic_limit_bytes"`
	TrafficDirection      string `json:"traffic_direction"`
	TrafficResetDay       int    `json:"traffic_reset_day"`
	TrafficResetTZMinutes int    `json:"traffic_reset_tz_minutes"`
	ShutdownEnabled       bool   `json:"shutdown_enabled"`
	ShutdownPercent       int    `json:"shutdown_percent"`
}

type ProbePolicy struct {
	Version  int64  `json:"version"`
	NodeID   string `json:"node_id"`
	Enabled  bool   `json:"enabled"`
	Region   string `json:"region"`
	Protocol string `json:"protocol"`
	Telecom  string `json:"telecom"`
	Unicom   string `json:"unicom"`
	Mobile   string `json:"mobile"`
}

type SignedProbePolicy struct {
	Policy    ProbePolicy `json:"policy"`
	Signature string      `json:"signature"`
}

type SignedPolicy struct {
	Policy    AgentPolicy `json:"policy"`
	Signature string      `json:"signature"`
}

type ReportResponse struct {
	OK          bool               `json:"ok"`
	Policy      *SignedPolicy      `json:"policy,omitempty"`
	ProbePolicy *SignedProbePolicy `json:"probe_policy,omitempty"`
}

type NodeView struct {
	Report
	DisplayName         string    `json:"display_name"`
	Online              bool      `json:"online"`
	ObservedIP          string    `json:"observed_ip"`
	LastSeen            time.Time `json:"last_seen"`
	MonthlyTrafficLimit uint64    `json:"monthly_traffic_limit"`
	TrafficDirection    string    `json:"traffic_direction"`
	TrafficResetDay     int       `json:"traffic_reset_day"`
	TrafficResetTZ      string    `json:"traffic_reset_tz"`
	ShutdownEnabled     bool      `json:"shutdown_enabled"`
	ShutdownPercent     int       `json:"shutdown_percent"`
	MonthlyPrice        float64   `json:"monthly_price"`
	Currency            string    `json:"currency"`
	ExpireAt            string    `json:"expire_at"`
	Tags                []string  `json:"tags"`
}
