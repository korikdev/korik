package nat

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

// CandidateType classifies where a dialable address came from.
type CandidateType string

const (
	CandidateHost  CandidateType = "host"
	CandidateSrflx CandidateType = "srflx"
)

// Candidate is one dialable path to reach this node.
type Candidate struct {
	ID        string        `json:"id"`
	Type      CandidateType `json:"type"`
	IP        string        `json:"ip"`
	Port      int           `json:"port"`
	Interface string        `json:"iface,omitempty"`
	Transport string        `json:"transport"`
	Priority  int           `json:"priority"`
}

// Address renders ip:port, bracketing IPv6.
func (c Candidate) Address() string {
	if strings.Contains(c.IP, ":") && !strings.HasPrefix(c.IP, "[") {
		return fmt.Sprintf("[%s]:%d", c.IP, c.Port)
	}
	return fmt.Sprintf("%s:%d", c.IP, c.Port)
}

// StrategyStep names the connection strategy stage.
type StrategyStep int

const (
	StepDirectLAN StrategyStep = 1
	StepIPv6      StrategyStep = 2
	StepPublicUDP StrategyStep = 3
	StepPunching  StrategyStep = 4
	StepFailed    StrategyStep = 5
)

func (s StrategyStep) String() string {
	switch s {
	case StepDirectLAN:
		return "direct-lan"
	case StepIPv6:
		return "ipv6-direct"
	case StepPublicUDP:
		return "public-udp"
	case StepPunching:
		return "hole-punching"
	default:
		return "failed"
	}
}

// StepFor assigns a candidate to its strategy stage.
func StepFor(c Candidate) StrategyStep {
	ip := net.ParseIP(c.IP)
	isV6 := ip != nil && ip.To4() == nil && strings.Contains(c.IP, ":")
	switch {
	case c.Type == CandidateHost && !isV6:
		return StepDirectLAN
	case c.Type == CandidateHost && isV6:
		return StepIPv6
	case c.Type == CandidateSrflx && !isV6:
		return StepPublicUDP
	case c.Type == CandidateSrflx && isV6:
		// Public IPv6 usually needs no punching, treat as public-udp stage
		// but keep distinct priority below IPv4 srflx.
		return StepPublicUDP
	default:
		return StepPunching
	}
}

// PriorityFor computes ICE-like priority: host LAN > host v6 > srflx v4 > srflx v6.
func PriorityFor(candidateType CandidateType, ip net.IP, transport string) int {
	base := 0
	if candidateType == CandidateHost {
		if ip.To4() != nil {
			base = 1000
		} else {
			base = 900
		}
	} else {
		if ip.To4() != nil {
			base = 700
		} else {
			base = 650
		}
	}
	if transport == "tcp" {
		base += 10
	}
	// Prefer non-link-local and non-temporary addresses slightly.
	if ip.IsPrivate() {
		base += 5
	}
	if ip.IsGlobalUnicast() && !ip.IsPrivate() && candidateType == CandidateHost {
		base += 2
	}
	return base
}

// GatherHostCandidates enumerates up/non-loopback interfaces for LAN paths.
// One TCP + one UDP candidate per address so direct and punch stages both run.
func GatherHostCandidates(tcpPort int) []Candidate {
	var out []Candidate
	interfaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
				continue
			}
			// Skip link-local multicast etc, keep unicast + link-local unicast for LAN.
			if ip.IsMulticast() {
				continue
			}
			ipStr := ip.String()
			// Normalize IPv4-in-IPv6.
			if ip.To4() != nil {
				ipStr = ip.To4().String()
			}
			for _, transport := range []string{"tcp", "udp"} {
				out = append(out, Candidate{
					ID:        fmt.Sprintf("host-%s-%s-%s", shortIP(ipStr), iface.Name, transport),
					Type:      CandidateHost,
					IP:        ipStr,
					Port:      tcpPort,
					Interface: iface.Name,
					Transport: transport,
					Priority:  PriorityFor(CandidateHost, ip, transport),
				})
			}
		}
	}
	Prioritize(out)
	return out
}

// DiscoverSrflxCandidates queries STUN for server-reflexive addresses.
// It tries IPv4 and IPv6 independently; failures are skipped, never fatal.
func DiscoverSrflxCandidates(stunServer string, timeout time.Duration) []Candidate {
	var out []Candidate
	if pub, err := discoverViaDial("udp4", stunServer, timeout); err == nil && pub != nil {
		ip := net.ParseIP(pub.IP)
		out = append(out, Candidate{
			ID:        "srflx-v4-udp",
			Type:      CandidateSrflx,
			IP:        pub.IP,
			Port:      pub.Port,
			Transport: "udp",
			Priority:  PriorityFor(CandidateSrflx, ip, "udp"),
		})
	}
	if pub, err := discoverViaDial("udp6", stunServer, timeout); err == nil && pub != nil {
		ip := net.ParseIP(pub.IP)
		out = append(out, Candidate{
			ID:        "srflx-v6-udp",
			Type:      CandidateSrflx,
			IP:        pub.IP,
			Port:      pub.Port,
			Transport: "udp",
			Priority:  PriorityFor(CandidateSrflx, ip, "udp"),
		})
	}
	Prioritize(out)
	return out
}

// DiscoverSrflxOnConn reuses an existing UDP socket so the mapped port
// matches the real listening socket (preferred when internet mode is active).
func DiscoverSrflxOnConn(conn *net.UDPConn, stunServer string, timeout time.Duration) *Candidate {
	pub, err := DiscoverPublicAddr(conn, stunServer, timeout)
	if err != nil || pub == nil {
		return nil
	}
	ip := net.ParseIP(pub.IP)
	return &Candidate{
		ID:        "srflx-conn-udp",
		Type:      CandidateSrflx,
		IP:        pub.IP,
		Port:      pub.Port,
		Transport: "udp",
		Priority:  PriorityFor(CandidateSrflx, ip, "udp"),
	}
}

func discoverViaDial(network, stunServer string, timeout time.Duration) (*PublicAddr, error) {
	serverAddr, err := net.ResolveUDPAddr(network, stunServer)
	if err != nil {
		// Fall back: some STUN hosts only resolve over v4.
		if network == "udp6" {
			return nil, err
		}
		return nil, err
	}
	conn, err := net.DialUDP(network, nil, serverAddr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	// For a dialed socket, ReadFromUDP still works for the STUN reply.
	return DiscoverPublicAddr(conn, serverAddr.String(), timeout)
}

// Prioritize sorts candidates highest priority first, stable by ID.
func Prioritize(cands []Candidate) {
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Priority != cands[j].Priority {
			return cands[i].Priority > cands[j].Priority
		}
		return cands[i].ID < cands[j].ID
	})
}

// Deduplicate removes identical ip:port/transport pairs, keeping highest priority.
func Deduplicate(cands []Candidate) []Candidate {
	seen := make(map[string]bool)
	out := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		key := c.Transport + "|" + strings.ToLower(c.Address())
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

// Bundle is the out-of-band shareable candidate set (QR / copy-paste).
type Bundle struct {
	Version    int         `json:"v"`
	JID        string      `json:"jid"`
	DeviceID   string      `json:"device,omitempty"`
	Candidates []Candidate `json:"cands"`
}

// EncodeBundle serializes candidates to a compact shareable string.
func EncodeBundle(jid, deviceID string, cands []Candidate) string {
	bundle := Bundle{Version: 1, JID: jid, DeviceID: deviceID, Candidates: cands}
	raw, err := json.Marshal(bundle)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeBundle parses a shareable candidate string.
func DecodeBundle(encoded string) (Bundle, error) {
	var bundle Bundle
	trimmed := strings.TrimSpace(encoded)
	// Allow pasting with or without surrounding whitespace/newlines from QR scan.
	trimmed = strings.ReplaceAll(trimmed, "\n", "")
	trimmed = strings.ReplaceAll(trimmed, " ", "")
	raw, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		// Try standard encoding as fallback.
		raw, err = base64.URLEncoding.DecodeString(trimmed)
		if err != nil {
			return bundle, fmt.Errorf("invalid candidate bundle: must be base64 from /candidates")
		}
	}
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return bundle, fmt.Errorf("invalid candidate bundle: corrupted JSON")
	}
	if bundle.Version != 1 {
		return bundle, fmt.Errorf("unsupported bundle version %d", bundle.Version)
	}
	if len(bundle.Candidates) == 0 {
		return bundle, fmt.Errorf("bundle has no candidates")
	}
	return bundle, nil
}

// Fingerprint returns a short hash identifying a bundle for phone verification.
func Fingerprint(bundle Bundle) string {
	raw, _ := json.Marshal(bundle.Candidates)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:12]
}

func shortIP(ip string) string {
	stripped := strings.ReplaceAll(strings.ReplaceAll(ip, ":", ""), ".", "")
	if len(stripped) > 8 {
		return stripped[:8]
	}
	return stripped
}
