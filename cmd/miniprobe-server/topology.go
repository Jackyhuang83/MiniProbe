package main

import (
    "fmt"
    "net"
    "net/http"
    "sort"
    "strconv"
    "strings"
    "time"
    "unicode"
    "unicode/utf8"
    "bufio"
)

const (
    topologyMaxNodes = 32
    topologyMaxLinks = 64
)

type topologyNodeConfig struct {
    ID string `json:"id"`
    Role string `json:"role"` // entry, relay, exit
    Group string `json:"group,omitempty"`
}
type topologyLinkConfig struct {
    From string `json:"from"`
    To string `json:"to"`
    Label string `json:"label,omitempty"`
    ProbeAddress string `json:"probe_address,omitempty"` // local socket only, never public API
    ProbePort uint16 `json:"probe_port,omitempty"`
}
type topologyConfig struct {
    Enabled bool `json:"enabled"`
    Revision int64 `json:"revision"`
    Nodes []topologyNodeConfig `json:"nodes"`
    Links []topologyLinkConfig `json:"links"`
}
type topologyNodeView struct {
    ID string `json:"id"`
    Name string `json:"name"`
    Role string `json:"role"`
    Group string `json:"group"`
    Online bool `json:"online"`
    LastSeen time.Time `json:"last_seen,omitempty"`
    AgentVersion string `json:"agent_version,omitempty"`
    OS string `json:"os,omitempty"`
    Arch string `json:"arch,omitempty"`
    NetworkTypes []string `json:"network_types,omitempty"`
}
type topologyLinkView struct {
    ID string `json:"id"`
    From string `json:"from"`
    To string `json:"to"`
    Label string `json:"label,omitempty"`
    EndpointStatus string `json:"endpoint_status"` // online / endpoint_offline
    Reachability string `json:"reachability"` // not_tested in DEV1
    ProxyHealth string `json:"proxy_health"` // not_tested in DEV1
    TrafficStatus string `json:"traffic_status"` // not_collected until DEV3
    ProbeState string `json:"probe_state"` // not_configured / agent_unsupported / pending / measured
    NetworkStatus string `json:"network_status"` // healthy / degraded / interrupted / stale / pending
    RTTMS *float64 `json:"rtt_ms"`
    MinMS *float64 `json:"min_ms"`
    MaxMS *float64 `json:"max_ms"`
    AvgMS *float64 `json:"avg_ms"`
    LossPct *float64 `json:"loss_pct"`
    LastSuccess time.Time `json:"last_success,omitempty"`
    LastFailure time.Time `json:"last_failure,omitempty"`
    LastProbe time.Time `json:"last_probe,omitempty"`
}
type topologySummary struct {
    TotalLinks int `json:"total_links"`
    HealthyLinks *int `json:"healthy_links"` // unknown, NOT zero
    TrafficTodayBytes *uint64 `json:"traffic_today_bytes"` // unknown, not server NIC bytes
    AverageRTTMS *float64 `json:"average_rtt_ms"` // unknown, not public-target RTT
}
type topologySnapshot struct {
    Enabled bool `json:"enabled"`
    Nodes []topologyNodeView `json:"nodes"`
    Links []topologyLinkView `json:"links"`
    Summary topologySummary `json:"summary"`
}

func topologyTextValid(v string, max int) bool {
    if utf8.RuneCountInString(v) > max { return false }
    for _, r := range v { if unicode.IsControl(r) { return false } }
    return true
}

// Validation runs only on the local root-only admin socket. No addresses,
// ports, secrets, proxy configuration or remote commands are accepted.
func validateTopology(in topologyConfig, known map[string]nodeConfig) (topologyConfig, error) {
    if len(in.Nodes) > topologyMaxNodes || len(in.Links) > topologyMaxLinks {
        return topologyConfig{}, fmt.Errorf("拓扑最多 %d 个节点、%d 条链路", topologyMaxNodes, topologyMaxLinks)
    }
    out := topologyConfig{Enabled: in.Enabled, Nodes: make([]topologyNodeConfig, 0, len(in.Nodes)), Links: make([]topologyLinkConfig, 0, len(in.Links))}
    seen := make(map[string]bool, len(in.Nodes))
    for _, n := range in.Nodes {
        if _, ok := known[n.ID]; !ok { return topologyConfig{}, fmt.Errorf("未知监控节点 %q", n.ID) }
        if seen[n.ID] { return topologyConfig{}, fmt.Errorf("重复拓扑节点 %q", n.ID) }
        if n.Role != "entry" && n.Role != "relay" && n.Role != "exit" {
            return topologyConfig{}, fmt.Errorf("节点 %q 的角色无效", n.ID)
        }
        n.Group = strings.TrimSpace(n.Group)
        if !topologyTextValid(n.Group, 32) { return topologyConfig{}, fmt.Errorf("节点 %q 的分组名称无效", n.ID) }
        seen[n.ID] = true
        out.Nodes = append(out.Nodes, n)
    }
    edgeSeen := make(map[string]bool, len(in.Links))
    tasksPerSource:=map[string]int{}
    for _, l := range in.Links {
        if l.From == l.To { return topologyConfig{}, fmt.Errorf("链路不能指向自身 %q", l.From) }
        if !seen[l.From] || !seen[l.To] { return topologyConfig{}, fmt.Errorf("链路 %q → %q 的端点未加入拓扑", l.From, l.To) }
        key := l.From + "\x00" + l.To
        if edgeSeen[key] { return topologyConfig{}, fmt.Errorf("重复有向链路 %q → %q", l.From, l.To) }
        l.Label = strings.TrimSpace(l.Label)
        if !topologyTextValid(l.Label, 40) { return topologyConfig{}, fmt.Errorf("链路名称无效") }
        l.ProbeAddress=strings.TrimSpace(l.ProbeAddress)
        if (l.ProbeAddress=="") != (l.ProbePort==0) {return topologyConfig{}, fmt.Errorf("定向探测须同时配置目标 IP 和端口")}
        if l.ProbeAddress!="" {
            if net.ParseIP(l.ProbeAddress)==nil || !validTopologyProbeIP(l.ProbeAddress) {return topologyConfig{}, fmt.Errorf("目标地址必须是非本地/非链路本地的 IPv4/IPv6 字面量")}
            tasksPerSource[l.From]++
            if tasksPerSource[l.From]>topologyMaxTargetsPerAgent {return topologyConfig{}, fmt.Errorf("每台来源 VPS 最多配置 %d 个探测目标",topologyMaxTargetsPerAgent)}
        }
        edgeSeen[key] = true
        out.Links = append(out.Links, l)
    }
    return out, nil
}

func (s *server) localTopology(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodGet:
        s.mu.RLock()
        cfg := s.db.Topology
        s.mu.RUnlock()
        noStore(w)
        writeJSON(w, http.StatusOK, cfg)
    case http.MethodPut:
        var in topologyConfig
        if err := decodeJSON(w, r, &in); err != nil {
            http.Error(w, "bad json", http.StatusBadRequest)
            return
        }
        s.mu.Lock()
        next, err := validateTopology(in, s.db.Nodes)
        if err != nil {
            s.mu.Unlock()
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }
        next.Revision=s.db.Topology.Revision+1
        if next.Revision<=1 {next.Revision=2}
        s.db.Topology = next
        s.mu.Unlock()
        s.resetTopologyStats()
        // Existing MiniProbe persistence loop also writes the database every 30s.
        if err := s.save(); err != nil {
            http.Error(w, "save failed", http.StatusInternalServerError)
            return
        }
        noStore(w)
        writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
    default:
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
    }
}

// Never infer link health from two online nodes, server NIC traffic,
// or RTT measurements to unrelated public test targets.
func (s *server) topologyAPI(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    out := topologySnapshot{
        Nodes: make([]topologyNodeView, 0),
        Links: make([]topologyLinkView, 0),
    }
    s.mu.RLock()
    cfg := s.db.Topology
    out.Enabled = cfg.Enabled
    if cfg.Enabled {
        now := time.Now()
        healthyCount:=0
        checkedCount:=0
        totalRTT:=0.0
        rttCount:=0
        included := make(map[string]topologyNodeView, len(cfg.Nodes))
        for _, n := range cfg.Nodes {
            node, exists := s.db.Nodes[n.ID]
            if !exists { continue } // legacy/orphan entries cannot leak data
            state := s.db.States[n.ID]
            v := topologyNodeView{
                ID:n.ID, Name:node.DisplayName, Role:n.Role, Group:n.Group,
                Online:!state.LastSeen.IsZero() && now.Sub(state.LastSeen) < 20*time.Second,
                LastSeen:state.LastSeen, AgentVersion:state.AgentVersion,
                OS:state.Info.OS, Arch:state.Info.Arch,
                NetworkTypes:append([]string(nil), state.Info.NetworkTypes...),
            }
            included[n.ID] = v
            out.Nodes = append(out.Nodes, v)
        }
        for _, l := range cfg.Links {
            from, okFrom := included[l.From]
            to, okTo := included[l.To]
            if !okFrom || !okTo { continue }
            endpoint := "online"
            if !from.Online || !to.Online { endpoint = "endpoint_offline" }
            view:=topologyLinkView{
                ID:topologyEdgeID(l.From,l.To), From:l.From, To:l.To, Label:l.Label,
                EndpointStatus:endpoint, Reachability:"not_tested",
                ProxyHealth:"not_tested", TrafficStatus:"not_collected",
                ProbeState:"not_configured",NetworkStatus:"pending",
            }
            if l.ProbeAddress!="" && l.ProbePort!=0 {
                view.ProbeState="agent_unsupported"
                state:=s.db.States[l.From]
                if hasCapability(state.Capabilities,topologyProbeCapability) {
                    view.ProbeState="pending"
                    m:=s.topologyMeasurement(view.ID,cfg.Revision,now)
                    if m.HasData {
                        view.ProbeState="measured"
                        view.Reachability=m.Reachability
                        view.NetworkStatus=m.NetworkStatus
                        view.RTTMS=m.RTTMS;view.MinMS=m.MinMS;view.MaxMS=m.MaxMS;view.AvgMS=m.AvgMS
                        view.LossPct=m.LossPct
                        view.LastProbe=m.LastProbe;view.LastSuccess=m.LastSuccess;view.LastFailure=m.LastFailure
                        if m.NetworkStatus!="stale" && endpoint=="online" {
                            checkedCount++
                            if m.NetworkStatus=="healthy" {healthyCount++}
                            if m.RTTMS!=nil {totalRTT+=*m.RTTMS;rttCount++}
                        }
                    }
                }
            }
            out.Links=append(out.Links,view)
        }
        if checkedCount>0 {out.Summary.HealthyLinks=&healthyCount}
        if rttCount>0 {avg:=totalRTT/float64(rttCount);out.Summary.AverageRTTMS=&avg}
        sort.Slice(out.Nodes,func(i,j int)bool{
            if out.Nodes[i].Role != out.Nodes[j].Role { return topologyRoleOrder(out.Nodes[i].Role) < topologyRoleOrder(out.Nodes[j].Role) }
            if out.Nodes[i].Group != out.Nodes[j].Group { return out.Nodes[i].Group < out.Nodes[j].Group }
            return out.Nodes[i].Name < out.Nodes[j].Name
        })
    }
    s.mu.RUnlock()
    out.Summary.TotalLinks = len(out.Links)
    noStore(w)
    writeJSON(w, http.StatusOK, out)
}
// Keep persisted topology referentially consistent when a monitored node is deleted.
func purgeTopologyNodeLocked(cfg *topologyConfig, id string) {
    nodes := make([]topologyNodeConfig,0,len(cfg.Nodes))
    for _,n := range cfg.Nodes { if n.ID != id { nodes=append(nodes,n) } }
    links := make([]topologyLinkConfig,0,len(cfg.Links))
    for _,l := range cfg.Links { if l.From != id && l.To != id { links=append(links,l) } }
    cfg.Nodes=nodes
    cfg.Links=links
    cfg.Revision++
    if cfg.Revision<=1 {cfg.Revision=2}
}

func topologyRoleOrder(v string) int {
    switch v { case "entry": return 0; case "relay": return 1; case "exit": return 2 }
    return 3
}

func topologyRoleLabel(role string) string {
    switch role { case "entry": return "入口"; case "relay": return "中转"; case "exit": return "出口" }
    return "未知"
}

func manageTopology(r *bufio.Reader, client *localClient) {
    for {
        var cfg topologyConfig
        if err := client.request(http.MethodGet, "/v1/topology", nil, &cfg); err != nil {
            fmt.Println("读取拓扑设置失败:", err)
            return
        }
        nodes, err := getLocalNodes(client)
        if err != nil { fmt.Println("读取节点失败:", err); return }
        byID := map[string]string{}
        for _, n := range nodes { byID[n.ID] = n.DisplayName }
        fmt.Println("\n===== 网络拓扑（仅监控，不修改代理）=====")
        fmt.Printf("状态: %s；节点 %d；逻辑链路 %d\n", map[bool]string{true:"启用",false:"关闭"}[cfg.Enabled], len(cfg.Nodes), len(cfg.Links))
        for _, n := range cfg.Nodes { fmt.Printf(" · %s [%s] %s\n", byID[n.ID], topologyRoleLabel(n.Role), n.Group) }
        for i, l := range cfg.Links { state:="未配置探测";if l.ProbeAddress!="" {state="TCP 定向探测已配置"};fmt.Printf(" %d. %s → %s  %s [%s]\n", i+1, byID[l.From], byID[l.To], l.Label,state) }
        fmt.Println("1. 启用/关闭拓扑  2. 设置节点角色/分组  3. 从拓扑移除节点")
        fmt.Println("4. 添加有向逻辑链路  5. 删除链路  6. 设置定向 TCP 探测  0. 返回")
        choice := prompt(r, "请选择: ")
        save := true
        switch choice {
        case "1":
            cfg.Enabled = !cfg.Enabled
        case "2":
            id := pickTopologyNode(r, nodes, "选择监控节点")
            if id == "" { save = false; break }
            role := strings.ToLower(prompt(r, "角色 entry(入口)/relay(中转)/exit(出口): "))
            group := prompt(r, "分组名称（可空）: ")
            found := false
            for i := range cfg.Nodes { if cfg.Nodes[i].ID == id { cfg.Nodes[i].Role = role; cfg.Nodes[i].Group = group; found = true; break } }
            if !found { cfg.Nodes = append(cfg.Nodes, topologyNodeConfig{ID:id, Role:role, Group:group}) }
        case "3":
            if len(cfg.Nodes) == 0 { fmt.Println("暂无节点"); save = false; break }
            id := pickTopologyNode(r, nodes, "选择要移除的节点")
            if id == "" { save = false; break }
            filteredNodes := cfg.Nodes[:0]
            for _, n := range cfg.Nodes { if n.ID != id { filteredNodes = append(filteredNodes, n) } }
            cfg.Nodes = filteredNodes
            filteredLinks := cfg.Links[:0]
            for _, l := range cfg.Links { if l.From != id && l.To != id { filteredLinks = append(filteredLinks, l) } }
            cfg.Links = filteredLinks
        case "4":
            present := make([]adminNodeView, 0, len(cfg.Nodes))
            for _, n := range nodes { for _, configured := range cfg.Nodes { if n.ID == configured.ID { present = append(present, n) } } }
            if len(present) < 2 { fmt.Println("请先添加至少两个拓扑节点"); save = false; break }
            from := pickTopologyNode(r, present, "选择来源 VPS")
            to := pickTopologyNode(r, present, "选择目标 VPS")
            if from == "" || to == "" { save = false; break }
            label := prompt(r, "链路备注（可空；不填写敏感凭据）: ")
            cfg.Links = append(cfg.Links, topologyLinkConfig{From:from, To:to, Label:label})
        case "5":
            idx, err := strconv.Atoi(prompt(r, "删除链路序号: "))
            if err != nil || idx < 1 || idx > len(cfg.Links) { fmt.Println("链路序号无效"); save = false; break }
            cfg.Links = append(cfg.Links[:idx-1],cfg.Links[idx:]...)
        case "6":
            idx,err:=strconv.Atoi(prompt(r,"设置链路序号: "))
            if err!=nil || idx<1 || idx>len(cfg.Links) {fmt.Println("链路序号无效");save=false;break}
            fmt.Println("只执行 TCP 握手，不发送代理密码，不检测代理协议。NAT/IPv6 节点请填来源 VPS 可直达的已有业务监听地址。")
            fmt.Println("禁止环回/链路本地地址；无需开放额外的 MiniProbe 入站端口。")
            ip:=strings.TrimSpace(prompt(r,"目标 IP（留空关闭该链路探测）: "))
            if ip=="" {cfg.Links[idx-1].ProbeAddress="";cfg.Links[idx-1].ProbePort=0;break}
            n,err:=strconv.Atoi(prompt(r,"目标 TCP 端口（1-65535）: "))
            if err!=nil || n<1 || n>65535 {fmt.Println("端口无效");save=false;break}
            cfg.Links[idx-1].ProbeAddress=ip
            cfg.Links[idx-1].ProbePort=uint16(n)
        case "0","q","Q":
            return
        default:
            fmt.Println("无效选项")
            save = false
        }
        if !save { continue }
        if err := client.request(http.MethodPut, "/v1/topology", cfg, nil); err != nil {
            fmt.Println("保存失败（配置未通过校验）:",err)
        } else { fmt.Println("已保存。新 Agent 自动接受 Server 签名的 TCP 定向探测策略；转发流量仍将在 DEV3 实现。") }
    }
}
func pickTopologyNode(r *bufio.Reader, nodes []adminNodeView, title string) string {
    if len(nodes) == 0 { fmt.Println("无可用监控节点"); return "" }
    fmt.Println(title)
    for i,n := range nodes { fmt.Printf(" %d. %s (%s)\n", i+1, n.DisplayName, n.ID) }
    idx, err := strconv.Atoi(prompt(r, "序号（其他内容取消）: "))
    if err != nil || idx < 1 || idx > len(nodes) { return "" }
    return nodes[idx-1].ID
}
