package nat

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// NATType classifies the type of NAT traversal behavior.
type NATType int

const (
	FullCone NATType = iota
	RestrictedCone
	PortRestrictedCone
	Symmetric
)

func (n NATType) String() string {
	switch n {
	case FullCone:
		return "Full Cone"
	case RestrictedCone:
		return "Restricted Cone"
	case PortRestrictedCone:
		return "Port Restricted Cone"
	case Symmetric:
		return "Symmetric"
	default:
		return "Unknown"
	}
}

// TestResult holds the complete NAT traversal test outcome.
type TestResult struct {
	NATType         NATType
	LocalIP         string
	PublicIP        string
	IsSymmetric     bool
	SymmetricNAT    bool
	HolesPunched    int
	Latency         time.Duration
	Compatibility   string
	Recommendations []string
}

// DetectNATType performs STUN queries to multiple servers and classifies
// the current NAT type, measuring latency and hole punching success rate.
func DetectNATType(stunServer string, timeout time.Duration) (*TestResult, error) {
	if stunServer == "" {
		stunServer = "stun.l.google.com:19302"
	}

	localConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, fmt.Errorf("cannot open UDP socket for NAT detection: %w", err)
	}
	defer localConn.Close()

	localAddr := localConn.LocalAddr().(*net.UDPAddr)
	localIP := localAddr.IP.String()

	latency, _ := MeasureLatency(stunServer, timeout)

	pubAddr, stunErr := DiscoverPublicAddr(localConn, stunServer, timeout)
	if stunErr != nil {
		return nil, fmt.Errorf("STUN query failed: %w", stunErr)
	}
	publicIP := pubAddr.IP

	altServers := []string{
		"stun1.l.google.com:19302",
		"stun2.l.google.com:19302",
		"stun3.l.google.com:19302",
	}

	responses := make(map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	queryServer := func(server string) {
		defer wg.Done()
		conn, derr := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
		if derr != nil {
			return
		}
		defer conn.Close()
		addr, qerr := DiscoverPublicAddr(conn, server, timeout)
		if qerr != nil {
			return
		}
		mu.Lock()
		responses[addr.IP+":"+fmt.Sprintf("%d", addr.Port)] = true
		mu.Unlock()
	}

	wg.Add(1)
	go queryServer(stunServer)
	for _, s := range altServers {
		wg.Add(1)
		go queryServer(s)
	}
	wg.Wait()

	bindingSuccess := stunErr == nil
	natType := ClassifyNATType(len(responses), bindingSuccess)

	holesPunched := testHolePunching(localConn, timeout)

	isSymmetric := natType == Symmetric
	symmetricNAT := isSymmetric

	compatibility := "Compatible"
	if isSymmetric {
		compatibility = "Partial: direct P2P may fail with symmetric NAT on both sides"
	}

	recs := Recommendations(natType)

	return &TestResult{
		NATType:         natType,
		LocalIP:         localIP,
		PublicIP:        publicIP,
		IsSymmetric:     isSymmetric,
		SymmetricNAT:    symmetricNAT,
		HolesPunched:    holesPunched,
		Latency:         latency,
		Compatibility:   compatibility,
		Recommendations: recs,
	}, nil
}

// ClassifyNATType determines NAT type from the number of distinct STUN
// responses and whether the binding request succeeded.
func ClassifyNATType(responseCount int, bindingSuccess bool) NATType {
	if !bindingSuccess {
		return Symmetric
	}
	if responseCount > 1 {
		return Symmetric
	}
	if responseCount == 1 {
		return FullCone
	}
	return RestrictedCone
}

// IsCompatible checks if two NAT types can establish a direct P2P connection.
func IsCompatible(a, b NATType) bool {
	if a == Symmetric || b == Symmetric {
		return false
	}
	return true
}

// Recommendations returns actionable suggestions based on the detected NAT type.
func Recommendations(t NATType) []string {
	var recs []string
	switch t {
	case FullCone:
		recs = append(recs, "No action needed: Full Cone NAT allows direct P2P connections.")
		recs = append(recs, "Use LAN mode for the fastest connection within the same network.")
	case RestrictedCone:
		recs = append(recs, "Direct P2P works if both peers have exchanged UDP packets first.")
		recs = append(recs, "Use LAN mode when both peers are on the same network.")
		recs = append(recs, "If P2P fails, configure port forwarding on your router.")
	case PortRestrictedCone:
		recs = append(recs, "P2P requires both peers to have sent packets to each other first.")
		recs = append(recs, "Consider configuring port forwarding for reliable connections.")
		recs = append(recs, "Use a relay server (TURN) as a fallback for guaranteed connectivity.")
	case Symmetric:
		recs = append(recs, "WARNING: Direct P2P will likely fail with Symmetric NAT on both sides.")
		recs = append(recs, "Use a relay server (TURN) for reliable connectivity.")
		recs = append(recs, "Configure port forwarding on your router as a workaround.")
		recs = append(recs, "Try LAN mode if both peers are on the same local network.")
	}
	return recs
}

// MeasureLatency sends a STUN binding request and returns the round-trip time.
func MeasureLatency(stunServer string, timeout time.Duration) (time.Duration, error) {
	if stunServer == "" {
		stunServer = "stun.l.google.com:19302"
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return 0, fmt.Errorf("cannot create UDP socket for latency measurement: %w", err)
	}
	defer conn.Close()

	start := time.Now()
	_, err = DiscoverPublicAddr(conn, stunServer, timeout)
	elapsed := time.Since(start)
	if err != nil {
		return 0, fmt.Errorf("latency measurement failed: %w", err)
	}
	return elapsed, nil
}

// testHolePunching tests UDP hole punching by attempting to send packets
// through the NAT and counting successful round-trips. Returns the count
// of successful hole-punch attempts (0-4).
func testHolePunching(conn *net.UDPConn, timeout time.Duration) int {
	successes := 0

	stunAddr, err := net.ResolveUDPAddr("udp", "stun.l.google.com:19302")
	if err != nil {
		return 0
	}

	conn.SetDeadline(time.Now().Add(timeout))
	defer func() { _ = conn.SetDeadline(time.Time{}) }()

	for i := 0; i < 3; i++ {
		_, err := conn.WriteToUDP([]byte("korik-punch-test"), stunAddr)
		if err != nil {
			continue
		}
		buf := make([]byte, 1024)
		n, _, err := conn.ReadFromUDP(buf)
		if err == nil && n > 0 && string(buf[:n]) != "" {
			successes++
		}
	}

	return successes
}
