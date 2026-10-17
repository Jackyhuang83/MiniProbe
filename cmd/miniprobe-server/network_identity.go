package main

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/Jackyhuang83/MiniProbe/internal/common"
)

// Run only on the Server. No public IP is saved to disk, returned by the
// Dashboard, or emitted in routine logs. Unknown values never become a label.
func (s *server) scheduleIdentityLookup(nodeID, v4, v6 string) {
	v4, v6 = validatedEgressIP(v4, 4), validatedEgressIP(v6, 6)
	if v4 == "" && v6 == "" { return }

	s.identityMu.Lock()
	if s.identityPending == nil { s.identityPending = map[string]bool{} }
	if s.identityChecked == nil { s.identityChecked = map[string]time.Time{} }
	if s.identityGate == nil { s.identityGate = make(chan struct{}, 3) }
	if s.identityPending[nodeID] || time.Since(s.identityChecked[nodeID]) < 24*time.Hour {
		s.identityMu.Unlock()
		return
	}
	s.identityPending[nodeID] = true
	s.identityChecked[nodeID] = time.Now()
	gate := s.identityGate
	s.identityMu.Unlock()

	go func() {
		gate <- struct{}{}
		defer func() {
			<-gate
			s.identityMu.Lock()
			delete(s.identityPending, nodeID)
			s.identityMu.Unlock()
		}()
		identity := common.NetworkIdentity{CheckedAt: time.Now().UTC()}
		if v4 != "" { identity.IPv4 = s.resolveEgressASN(v4) }
		if v6 != "" { identity.IPv6 = s.resolveEgressASN(v6) }
		s.mu.Lock()
		if node, ok := s.db.States[nodeID]; ok {
			if _, exists := s.db.Nodes[nodeID]; exists {
				node.Identity = identity
				s.db.States[nodeID] = node
			}
		}
		s.mu.Unlock()
	}()
}

func validatedEgressIP(raw string, bits int) string {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || !publicRouteIP(ip) { return "" }
	if bits == 4 && ip.To4() == nil { return "" }
	if bits == 6 && ip.To4() != nil { return "" }
	return ip.String()
}

func (s *server) resolveEgressASN(ip string) common.ASNInfo {
	asn := s.lookupASN(ip) // Team Cymru origin ASN (existing cached Server-only resolver)
	// Multi-origin mapping must not arbitrarily choose a provider.
	if asn == "" || strings.Contains(asn, "/") { return common.ASNInfo{} }
	num := strings.TrimPrefix(asn, "AS")
	if _, err := strconv.ParseUint(num, 10, 32); err != nil { return common.ASNInfo{} }
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	txt, err := net.DefaultResolver.LookupTXT(ctx, "AS"+num+".asn.cymru.com")
	info := common.ASNInfo{Number: asn}
	if err != nil { return info }
	for _, record := range txt {
		if org := parseCymruASName(record); org != "" {
			info.Org = org
			break
		}
	}
	return info
}

func parseCymruASName(record string) string {
	fields := strings.Split(record, "|")
	if len(fields) < 5 { return "" }
	// This is the routing-AS label, not a verified end-customer or residential type.
	name := strings.TrimSpace(strings.Join(fields[4:], "|"))
	if len(name) > 120 { name = name[:120] }
	return strings.ToValidUTF8(name, "")
}
