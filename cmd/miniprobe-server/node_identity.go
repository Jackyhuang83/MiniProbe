package main

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"
)

type nodeASNWatch struct {
	IP        string
	CheckedAt time.Time
}

// enqueueNodeASN does not block the Agent's two-second reporting path.
// The raw egress IP is kept in memory only and never stored in miniprobe.json.
func (s *server) enqueueNodeASN(nodeID, rawIP string) {
	ip := net.ParseIP(strings.TrimSpace(rawIP))
	if !publicRouteIP(ip) {
		return
	}
	address := ip.String()
	now := time.Now()
	s.identityMu.Lock()
	if s.identityWatch == nil {
		s.identityWatch = make(map[string]nodeASNWatch)
	}
	last := s.identityWatch[nodeID]
	if last.IP == address && now.Sub(last.CheckedAt) < 24*time.Hour {
		s.identityMu.Unlock()
		return
	}
	changed := last.IP != address
	s.identityWatch[nodeID] = nodeASNWatch{IP: address, CheckedAt: now}
	s.identityMu.Unlock()

	if changed {
		s.mu.Lock()
		if node, ok := s.db.States[nodeID]; ok {
			node.ASN = ""
			node.ASNName = ""
			s.db.States[nodeID] = node
		}
		s.mu.Unlock()
	}
	go func() {
		asn := s.lookupASN(address)
		name := lookupASNName(asn)
		s.identityMu.Lock()
		current := s.identityWatch[nodeID]
		if current.IP != address {
			s.identityMu.Unlock()
			return
		}
		if asn == "" || name == "" {
			// A transient DNS failure should not cause query storms.
			s.identityWatch[nodeID] = nodeASNWatch{IP: address, CheckedAt: time.Now().Add(-23*time.Hour)}
		}
		s.identityMu.Unlock()
		s.mu.Lock()
		if node, ok := s.db.States[nodeID]; ok {
			node.ASN = asn
			node.ASNName = name
			s.db.States[nodeID] = node
		}
		s.mu.Unlock()
	}()
}

// Team Cymru's asN.asn.cymru.com TXT record supplies the published ASN
// description ("AS Name"), which is an operator name, not a legal ownership
// or residential/datacenter classification.
func lookupASNName(asn string) string {
	if !strings.HasPrefix(asn, "AS") || strings.Contains(asn, "/") {
		return ""
	}
	num := strings.TrimPrefix(asn, "AS")
	if _, err := strconv.ParseUint(num, 10, 32); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	records, err := net.DefaultResolver.LookupTXT(ctx, "as"+num+".asn.cymru.com")
	if err != nil {
		return ""
	}
	for _, record := range records {
		if name := parseASNNameRecord(record, num); name != "" {
			return name
		}
	}
	return ""
}

func parseASNNameRecord(record, number string) string {
	parts := strings.Split(record, "|")
	if len(parts) < 5 || strings.TrimSpace(parts[0]) != number {
		return ""
	}
	name := strings.TrimSpace(parts[4])
	// Keep DB and Dashboard bounded, including unexpectedly long TXT data.
	runes := []rune(name)
	if len(runes) > 96 {
		name = string(runes[:96])
	}
	return name
}
