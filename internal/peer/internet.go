package peer

import (
	"fmt"
	"net"
	"sync"
	"time"

	"korik/internal/nat"
	"korik/internal/status"
)

// InternetTransport manages P2P connectivity across networks (not just one
// LAN/hotspot) using techniques:
//  1. STUN - query public server "what is my public address from NAT?"
//     (STUN server ONLY answers address, it never relays chat content)
//  2. UDP hole punching - both sides send datagrams to each other's public
//     addresses almost simultaneously, so that each NAT router opens a
//     "hole" for return traffic from that peer.
//
// There's no relay server (TURN) that traffic goes through - if hole
// punching fails (symmetric/strict NAT on both sides), connection simply won't
// work, that's the trade-off for "no public server relaying traffic".
type InternetTransport struct {
	node *Node

	mu       sync.Mutex
	conn     *net.UDPConn
	udpPeers map[string]*udpConn // key: peer's public "ip:port"
	pubAddr  *nat.PublicAddr
}

// StartInternet opens UDP socket, queries STUN server to discover our public
// address, and starts listening for incoming datagrams. The returned public
// address must be shared (outside this app - other chat, QR, etc) to peers
// so they can connect back.
func (n *Node) StartInternet(stunServer string) (*nat.PublicAddr, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: n.Port})
	if err != nil {
		return nil, fmt.Errorf("failed to open udp socket: %w", err)
	}

	pub, err := nat.DiscoverPublicAddr(conn, stunServer, 5*time.Second)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to query stun server (%s): %w", stunServer, err)
	}

	it := &InternetTransport{
		node:     n,
		conn:     conn,
		udpPeers: make(map[string]*udpConn),
		pubAddr:  pub,
	}
	n.internet = it

	go it.readLoop()

	return pub, nil
}

func (it *InternetTransport) readLoop() {
	defer func() {
		_ = recover()
	}()
	buf := make([]byte, 4096)
	for {
		n, remote, err := it.conn.ReadFromUDP(buf)
		if err != nil {
			return // socket closed
		}
		key := remote.String()

		it.mu.Lock()
		uc, exists := it.udpPeers[key]
		if !exists {
			uc = newUDPConn(it.conn, remote)
			it.udpPeers[key] = uc
		}
		it.mu.Unlock()

		if !exists {
			// new incoming connection (other peer initiated punch to us first)
			go it.node.handleIncoming(uc)
		}
		uc.push(buf[:n])
	}
}

// ConnectPeerPublicAddr attempts to connect to peer via their STUN-discovered
// public address (obtained manually outside this app, e.g. shared via other chat).
// Sends several "punch" datagrams first so our NAT opens an outbound path,
// then follows normal handshake flow (hello -> e2e) like LAN transport.
func (n *Node) ConnectPeerPublicAddr(addrStr string) error {
	if n.internet == nil {
		return fmt.Errorf("internet mode not active yet, run StartInternet first")
	}
	it := n.internet

	remote, err := net.ResolveUDPAddr("udp", normalizeAddr(addrStr))
	if err != nil {
		return fmt.Errorf("invalid address: %w", err)
	}

	it.mu.Lock()
	uc, exists := it.udpPeers[remote.String()]
	if !exists {
		uc = newUDPConn(it.conn, remote)
		it.udpPeers[remote.String()] = uc
	}
	it.mu.Unlock()

	// burst "punch": send several copies of Hello message (valid format, JSON+newline)
	// in sequence, so our NAT opens up AND there's higher chance one arrives when
	// peer on the other side is also opening their mapping. Duplicate hello messages
	// are safe to receive multiple times (idempotent, session key result is deterministic).
	helloMsg := Message{
		Type:      MsgHello,
		From:      n.Name,
		JID:       n.Identity.JID,
		LID:       n.Identity.LID,
		DeviceID:  n.DeviceID,
		PubKey:    n.Identity.PublicKeyHex(),
		Timestamp: time.Now(),
	}
	encoded, err := encodeMessage(helloMsg)
	if err != nil {
		return err
	}
	go func() {
		for i := 0; i < 6; i++ {
			_, _ = uc.Write(encoded)
			time.Sleep(300 * time.Millisecond)
		}
	}()

	if !exists {
		go n.handleIncoming(uc)
	}
	return nil
}

func normalizeAddr(addr string) string {
	trimmed := addr
	// Already bracketed IPv6 or plain ip:port: leave as-is.
	return trimmed
}

// LocalCandidates gathers host LAN candidates plus the server-reflexive
// candidate when internet mode is active. Never fails: worst case host only.
func (n *Node) LocalCandidates() []nat.Candidate {
	host := nat.GatherHostCandidates(n.Port)
	out := append([]nat.Candidate(nil), host...)
	if n.internet != nil && n.internet.conn != nil && n.internet.pubAddr != nil {
		pub := n.internet.pubAddr
		ip := net.ParseIP(stripBrackets(pub.IP))
		candidateType := nat.CandidateSrflx
		out = append(out, nat.Candidate{
			ID:        "srflx-conn-udp",
			Type:      candidateType,
			IP:        stripBrackets(pub.IP),
			Port:      pub.Port,
			Transport: "udp",
			Priority:  nat.PriorityFor(candidateType, ip, "udp"),
		})
	}
	nat.Prioritize(out)
	return nat.Deduplicate(out)
}

// BundleForShare encodes local candidates for QR / copy-paste exchange.
func (n *Node) BundleForShare() string {
	return nat.EncodeBundle(n.Identity.JID, n.DeviceID, n.LocalCandidates())
}

// ConnectWithCandidates tries a peer bundle across the 5-stage strategy:
// 1 direct LAN (host IPv4 TCP), 2 IPv6 direct, 3 public UDP single punch,
// 4 parallel hole punching to all UDP candidates, 5 failed.
// onStep receives progress lines for CLI feedback. It returns nil on the
// first stage that yields an e2e-ready session for the bundle JID.
func (n *Node) ConnectWithCandidates(bundle nat.Bundle, onStep func(nat.StrategyStep, string)) error {
	cands := append([]nat.Candidate(nil), bundle.Candidates...)
	nat.Prioritize(cands)
	targetJID := bundle.JID

	report := func(step nat.StrategyStep, msg string) {
		if onStep != nil {
			onStep(step, msg)
		}
		if n.Status != nil {
			phase := phaseForStep(step)
			n.Status.Set(phase, msg)
		}
	}

	tryTCP := func(step nat.StrategyStep, filter func(nat.Candidate) bool) bool {
		tried := 0
		for _, c := range cands {
			if !filter(c) || c.Transport != transportTCP {
				continue
			}
			if tried >= 3 {
				break
			}
			tried++
			report(step, "trying "+step.String()+" "+c.Address()+" ("+c.ID+")")
			_ = n.ConnectTo(c.Address())
			if waitForPeerJID(n, targetJID, 2*time.Second) {
				report(step, "connected via "+c.Address())
				return true
			}
		}
		return false
	}

	// Stage 1: direct LAN IPv4 TCP.
	if tryTCP(nat.StepDirectLAN, func(c nat.Candidate) bool {
		return nat.StepFor(c) == nat.StepDirectLAN
	}) {
		return nil
	}
	// Stage 2: IPv6 direct TCP.
	if tryTCP(nat.StepIPv6, func(c nat.Candidate) bool {
		return nat.StepFor(c) == nat.StepIPv6
	}) {
		return nil
	}

	// UDP stages need internet mode for the shared socket.
	if n.internet == nil {
		report(nat.StepFailed, "internet mode off: skipping public-udp and hole-punching stages")
		return fmt.Errorf("no direct path and internet mode is off (enable -internet for UDP stages)")
	}

	// Stage 3: public UDP single punch, highest-priority srflx first.
	for _, c := range cands {
		if c.Type != nat.CandidateSrflx || c.Transport != transportUDP {
			continue
		}
		report(nat.StepPublicUDP, "punching "+c.Address()+" ("+c.ID+")")
		_ = n.ConnectPeerPublicAddr(c.Address())
		if waitForPeerJID(n, targetJID, 3*time.Second) {
			report(nat.StepPublicUDP, "connected via "+c.Address())
			return nil
		}
		break // only the best srflx here; rest belong to stage 4
	}

	// Stage 4: parallel hole punching to every UDP candidate at once.
	var udpTargets []nat.Candidate
	for _, c := range cands {
		if c.Transport == "udp" {
			udpTargets = append(udpTargets, c)
		}
	}
	if len(udpTargets) > 0 {
		report(nat.StepPunching, fmt.Sprintf("parallel punching %d UDP candidates", len(udpTargets)))
		for _, c := range udpTargets {
			_ = n.ConnectPeerPublicAddr(c.Address())
		}
		if waitForPeerJID(n, targetJID, 5*time.Second) {
			report(nat.StepPunching, "connected via hole punching")
			return nil
		}
	}

	// Stage 5: failed. Likely symmetric NAT on both sides without relay.
	report(nat.StepFailed, "all stages failed (direct, ipv6, public-udp, punching)")
	return fmt.Errorf("all connection stages failed: try same WiFi, check firewall, or use a relay")
}

func waitForPeerJID(n *Node, jid string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		n.mu.Lock()
		for _, pc := range n.conns {
			if pc.jid == jid && pc.sessionKey != nil {
				n.mu.Unlock()
				return true
			}
		}
		n.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func phaseForStep(step nat.StrategyStep) status.Phase {
	switch step {
	case nat.StepDirectLAN, nat.StepIPv6:
		return status.PhaseConnecting
	case nat.StepPublicUDP, nat.StepPunching:
		return status.PhaseTraversing
	case nat.StepFailed:
		return status.PhaseFailed
	default:
		return status.PhaseConnecting
	}
}

func stripBrackets(ip string) string {
	if len(ip) >= 2 && ip[0] == '[' && ip[len(ip)-1] == ']' {
		return ip[1 : len(ip)-1]
	}
	return ip
}
