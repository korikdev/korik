package peer

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// Discovery payload formats:
//
//	New (preferred):
//	  korik|<jid>|<transport>|<ip>|<port>|<deviceID>
//	  transport: tcp | udp, ip: literal v4 or v6, port: tcp/udp port
//	Legacy (still accepted):
//	  korik|<jid>|<port>
//	  korik|<jid>|<port>|<deviceID>
//
// Examples:
//
//	korik|korik:abc|tcp|192.168.1.5|9999|cli-1a2b3c4d
//	korik|korik:abc|tcp|fe80::1|9999|cli-1a2b3c4d
//	korik|korik:abc|udp|203.0.113.5|9999|cli-1a2b3c4d
const (
	discoveryPrefix = "korik"
	discoveryIPv6MC = "ff02::1"
	// transportTCP/UDP tag discovery payloads; receivers dial TCP
	// directly or punch UDP depending on the tag.
	transportTCP = "tcp"
	transportUDP = "udp"
)

// ParsedDiscovery is a validated discovery announcement.
type ParsedDiscovery struct {
	JID       string
	Transport string // tcp | udp (default tcp for legacy payloads)
	IP        string // explicit advertised IP, may be empty for legacy
	Port      string
	DeviceID  string
}

// DialAddress renders the address to dial (brackets IPv6).
func (d ParsedDiscovery) DialAddress(fallbackIP string) string {
	ip := d.IP
	if ip == "" {
		ip = fallbackIP
	}
	if strings.Contains(ip, ":") && !strings.HasPrefix(ip, "[") {
		return fmt.Sprintf("[%s]:%s", ip, d.Port)
	}
	return fmt.Sprintf("%s:%s", ip, d.Port)
}

// BuildDiscoveryPayload renders the new 6-field format.
func BuildDiscoveryPayload(jid, transport, ip string, port int, deviceID string) string {
	if transport != transportTCP && transport != transportUDP {
		transport = transportTCP
	}
	return fmt.Sprintf("%s|%s|%s|%s|%d|%s", discoveryPrefix, jid, transport, ip, port, deviceID)
}

// ParseDiscoveryPayload accepts new and legacy formats.
// remoteIP is used when the payload carries no explicit IP.
func ParseDiscoveryPayload(payload, remoteIP string) (ParsedDiscovery, bool) {
	var d ParsedDiscovery
	parts := strings.Split(strings.TrimSpace(payload), "|")
	if len(parts) < 3 || parts[0] != discoveryPrefix {
		return d, false
	}
	d.JID = strings.TrimSpace(parts[1])
	if d.JID == "" {
		return d, false
	}

	switch len(parts) {
	case 3:
		// korik|jid|port
		d.Transport = transportTCP
		d.Port = strings.TrimSpace(parts[2])
	case 4:
		// korik|jid|port|device (legacy with device)
		d.Transport = transportTCP
		d.Port = strings.TrimSpace(parts[2])
		d.DeviceID = strings.TrimSpace(parts[3])
	case 5:
		// korik|jid|transport|ip|port (new without device)
		transport := strings.ToLower(strings.TrimSpace(parts[2]))
		if transport != transportTCP && transport != transportUDP {
			return d, false
		}
		d.Transport = transport
		d.IP = strings.TrimSpace(parts[3])
		d.Port = strings.TrimSpace(parts[4])
		if net.ParseIP(stripZone(d.IP)) == nil {
			return d, false
		}
	default:
		// 6+: korik|jid|transport|ip|port|device
		transport := strings.ToLower(strings.TrimSpace(parts[2]))
		if transport != transportTCP && transport != transportUDP {
			return d, false
		}
		d.Transport = transport
		d.IP = strings.TrimSpace(parts[3])
		d.Port = strings.TrimSpace(parts[4])
		d.DeviceID = strings.TrimSpace(parts[5])
		if net.ParseIP(stripZone(d.IP)) == nil {
			return d, false
		}
	}

	if !validPort(d.Port) {
		return d, false
	}
	_ = remoteIP
	return d, true
}

func validPort(port string) bool {
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n <= 65535
}

func stripZone(ip string) string {
	if i := strings.Index(ip, "%"); i >= 0 {
		return ip[:i]
	}
	return strings.Trim(ip, "[]")
}

// StartDiscovery listens on UDPv4 (broadcast) and UDPv6 (multicast)
// and announces presence on both families.
func (n *Node) StartDiscovery() {
	go n.listenDiscoveryFamily("udp4", "0.0.0.0")
	go n.listenDiscoveryFamily("udp6", "::")
	go n.broadcastPresence()
}

func (n *Node) listenDiscoveryFamily(network, listenIP string) {
	defer func() {
		_ = recover()
	}()
	addr, err := net.ResolveUDPAddr(network, fmt.Sprintf("[%s]:%d", listenIP, n.DiscoveryPort))
	if err != nil {
		// Fallback without brackets for IPv4.
		addr, err = net.ResolveUDPAddr(network, fmt.Sprintf("%s:%d", listenIP, n.DiscoveryPort))
		if err != nil {
			fmt.Println("discovery listen error:", err)
			return
		}
	}
	conn, err := net.ListenUDP(network, addr)
	if err != nil {
		// One family may be unavailable (e.g. no IPv6) — not fatal.
		fmt.Println("discovery listen error ("+network+"):", err)
		return
	}
	defer conn.Close()

	buf := make([]byte, 1024)
	for {
		size, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		remoteIP := ""
		if remote != nil && remote.IP != nil {
			remoteIP = remote.IP.String()
		}
		parsed, ok := ParseDiscoveryPayload(string(buf[:size]), remoteIP)
		if !ok {
			continue
		}
		if parsed.JID == n.Identity.JID && (parsed.DeviceID == "" || parsed.DeviceID == n.DeviceID) {
			continue // skip ourselves; same JID with different device is a linked device
		}
		peerAddr := parsed.DialAddress(remoteIP)
		n.mu.Lock()
		_, already := n.conns[peerAddr]
		n.mu.Unlock()
		if already {
			continue
		}
		switch parsed.Transport {
		case transportUDP:
			if n.internet != nil {
				go func(addr string) {
					_ = n.ConnectPeerPublicAddr(addr)
				}(peerAddr)
			} else {
				// Without internet mode there is no shared UDP socket;
				// fall back to TCP direct on the same ip:port.
				go func(addr string) {
					_ = n.ConnectTo(addr)
				}(peerAddr)
			}
		default:
			go func(addr string) {
				_ = n.ConnectTo(addr)
			}(peerAddr)
		}
	}
}

// broadcastPresence announces one payload per local IP on both families.
// Burst at startup for sub-second discovery, then 1s interval.
func (n *Node) broadcastPresence() {
	defer func() {
		_ = recover()
	}()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	announce := func() {
		if n.Stopped() {
			return
		}
		for _, payload := range n.discoveryPayloads() {
			sendDiscoveryPayload(n.DiscoveryPort, payload)
		}
	}
	// Initial burst.
	for i := 0; i < 5; i++ {
		if n.Stopped() {
			return
		}
		announce()
		time.Sleep(200 * time.Millisecond)
	}
	for range ticker.C {
		if n.Stopped() {
			return
		}
		announce()
	}
}

// discoveryPayloads builds new-format payloads, one per local unicast IP.
func (n *Node) discoveryPayloads() []discoverySend {
	var out []discoverySend
	interfaces, err := net.Interfaces()
	if err != nil {
		return []discoverySend{{
			network: "udp4",
			address: net.UDPAddr{Port: n.DiscoveryPort, IP: net.IPv4bcast},
			payload: BuildDiscoveryPayload(n.Identity.JID, transportTCP, localFallbackIP(), n.Port, n.DeviceID),
		}}
	}
	seen := make(map[string]bool)
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
			if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
				continue
			}
			isV6 := ip.To4() == nil && strings.Contains(ip.String(), ":")
			ipStr := ip.String()
			if ip.To4() != nil {
				ipStr = ip.To4().String()
			}
			// Strip IPv6 zone for payload; receiver dials without zone on same link.
			ipStr = stripZone(ipStr)
			key := ipStr
			if seen[key] {
				continue
			}
			seen[key] = true
			if isV6 {
				out = append(out, discoverySend{
					network: "udp6",
					address: net.UDPAddr{Port: n.DiscoveryPort, IP: net.ParseIP(discoveryIPv6MC)},
					payload: BuildDiscoveryPayload(n.Identity.JID, transportTCP, ipStr, n.Port, n.DeviceID),
				})
			} else {
				out = append(out, discoverySend{
					network: "udp4",
					address: net.UDPAddr{Port: n.DiscoveryPort, IP: net.IPv4bcast},
					payload: BuildDiscoveryPayload(n.Identity.JID, transportTCP, ipStr, n.Port, n.DeviceID),
				})
			}
		}
	}
	if len(out) == 0 {
		out = append(out, discoverySend{
			network: "udp4",
			address: net.UDPAddr{Port: n.DiscoveryPort, IP: net.IPv4bcast},
			payload: BuildDiscoveryPayload(n.Identity.JID, transportTCP, localFallbackIP(), n.Port, n.DeviceID),
		})
	}
	return out
}

type discoverySend struct {
	network string
	address net.UDPAddr
	payload string
}

func sendDiscoveryPayload(discoveryPort int, send discoverySend) {
	_ = discoveryPort
	conn, err := net.DialUDP(send.network, nil, &send.address)
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write([]byte(send.payload))
}

func localFallbackIP() string {
	conn, err := net.Dial("udp4", "255.255.255.255:1")
	if err != nil {
		return "0.0.0.0"
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok && addr.IP != nil {
		return addr.IP.String()
	}
	return "0.0.0.0"
}
