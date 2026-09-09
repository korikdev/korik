// Package nat implements a minimal STUN client (RFC 5389).
//
// STUN is used ONLY to query public servers: "what is my public address from
// outside the NAT?" — STUN servers never relay chat/traffic content (unlike
// TURN which acts as a real relay). This remains true P2P, STUN is just an
// address discovery helper, not a man-in-the-middle.
package nat

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	stunMagicCookie      uint32 = 0x2112A442
	bindingRequest       uint16 = 0x0001
	bindingSuccessResp   uint16 = 0x0101
	attrMappedAddress    uint16 = 0x0001
	attrXorMappedAddress uint16 = 0x0020
	familyIPv4           byte   = 0x01
	familyIPv6           byte   = 0x02
)

// PublicAddr is the result of a STUN query: our public IP address and port
// from the internet's perspective (after NAT router translation).
type PublicAddr struct {
	IP   string
	Port int
}

func (p PublicAddr) String() string {
	if strings.Contains(p.IP, ":") {
		return fmt.Sprintf("[%s]:%d", p.IP, p.Port)
	}
	return fmt.Sprintf("%s:%d", p.IP, p.Port)
}

// DiscoverPublicAddr sends a STUN binding request from localConn (the SAME
// UDP socket that will be used for P2P) to one of the public STUN servers.
// Important: use the same socket so that the NAT-mapped port is the one we're
// actually listening on, not a random different port.
func DiscoverPublicAddr(localConn *net.UDPConn, stunServer string, timeout time.Duration) (*PublicAddr, error) {
	serverAddr, err := net.ResolveUDPAddr("udp", stunServer)
	if err != nil {
		return nil, fmt.Errorf("resolve stun server: %w", err)
	}

	txID := make([]byte, 12)
	if _, err := rand.Read(txID); err != nil {
		return nil, err
	}

	req := buildBindingRequest(txID)

	if err := localConn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	defer func() {
		_ = localConn.SetDeadline(time.Time{})
	}()

	if _, err := localConn.WriteToUDP(req, serverAddr); err != nil {
		return nil, fmt.Errorf("send stun request: %w", err)
	}

	buf := make([]byte, 512)
	n, _, err := localConn.ReadFromUDP(buf)
	if err != nil {
		return nil, fmt.Errorf("no stun response (firewall/strict NAT, or server down): %w", err)
	}

	return parseBindingResponse(buf[:n], txID)
}

func buildBindingRequest(txID []byte) []byte {
	packet := make([]byte, 20)
	binary.BigEndian.PutUint16(packet[0:2], bindingRequest)
	binary.BigEndian.PutUint16(packet[2:4], 0) // length: no attributes
	binary.BigEndian.PutUint32(packet[4:8], stunMagicCookie)
	copy(packet[8:20], txID)
	return packet
}

func parseBindingResponse(data, expectedTxID []byte) (*PublicAddr, error) {
	if len(data) < 20 {
		return nil, errors.New("stun response too short")
	}
	msgType := binary.BigEndian.Uint16(data[0:2])
	msgLen := binary.BigEndian.Uint16(data[2:4])
	magic := binary.BigEndian.Uint32(data[4:8])
	txID := data[8:20]

	if magic != stunMagicCookie {
		return nil, errors.New("stun magic cookie mismatch")
	}
	if msgType != bindingSuccessResp {
		return nil, fmt.Errorf("stun not successful, type=0x%04x", msgType)
	}
	for i := range txID {
		if txID[i] != expectedTxID[i] {
			return nil, errors.New("stun transaction id mismatch")
		}
	}

	body := data[20:]
	declared := int(msgLen)
	if declared > len(body) {
		declared = len(body)
	}
	body = body[:declared]

	mapped := parseMappedAttribute(body, txID)

	if mapped == nil {
		return nil, errors.New("stun response has no recognized address attribute")
	}
	return mapped, nil
}

func parseXorMappedAddress(val, txID []byte) (*PublicAddr, error) {
	if len(val) < 4 {
		return nil, errors.New("xor-mapped address too short")
	}
	switch val[1] {
	case familyIPv4:
		if len(val) < 8 {
			return nil, errors.New("xor-mapped IPv4 too short")
		}
		xport := binary.BigEndian.Uint16(val[2:4])
		port := xport ^ uint16(stunMagicCookie>>16)

		xaddr := binary.BigEndian.Uint32(val[4:8])
		addr := xaddr ^ stunMagicCookie

		ip := net.IPv4(byte(addr>>24), byte(addr>>16), byte(addr>>8), byte(addr))
		return &PublicAddr{IP: ip.String(), Port: int(port)}, nil
	case familyIPv6:
		if len(val) < 20 || len(txID) != 12 {
			return nil, errors.New("xor-mapped IPv6 too short")
		}
		xport := binary.BigEndian.Uint16(val[2:4])
		port := xport ^ uint16(stunMagicCookie>>16)

		// RFC 5389 section 15.2: 16-byte mask = magic cookie + transaction ID.
		mask := make([]byte, 16)
		binary.BigEndian.PutUint32(mask[0:4], stunMagicCookie)
		copy(mask[4:16], txID)
		raw := make([]byte, 16)
		for i := 0; i < 16; i++ {
			raw[i] = val[4+i] ^ mask[i]
		}
		ip := net.IP(raw)
		return &PublicAddr{IP: ip.String(), Port: int(port)}, nil
	default:
		return nil, errors.New("unsupported address family")
	}
}

func parseMappedAddress(val []byte) (*PublicAddr, error) {
	if len(val) < 4 {
		return nil, errors.New("mapped address too short")
	}
	switch val[1] {
	case familyIPv4:
		if len(val) < 8 {
			return nil, errors.New("only IPv4 supported")
		}
		port := binary.BigEndian.Uint16(val[2:4])
		ip := net.IPv4(val[4], val[5], val[6], val[7])
		return &PublicAddr{IP: ip.String(), Port: int(port)}, nil
	case familyIPv6:
		if len(val) < 20 {
			return nil, errors.New("mapped IPv6 too short")
		}
		port := binary.BigEndian.Uint16(val[2:4])
		ip := net.IP(append([]byte(nil), val[4:20]...))
		return &PublicAddr{IP: ip.String(), Port: int(port)}, nil
	default:
		return nil, errors.New("unsupported address family")
	}
}

// parseMappedAttribute walks STUN attributes preferring XOR-MAPPED-ADDRESS
// and falling back to MAPPED-ADDRESS, per RFC 5389 section 15.
func parseMappedAttribute(body, txID []byte) *PublicAddr {
	var mapped *PublicAddr
	for len(body) >= 4 {
		attrType := binary.BigEndian.Uint16(body[0:2])
		attrLen := binary.BigEndian.Uint16(body[2:4])
		if int(attrLen)+4 > len(body) {
			break
		}
		val := body[4 : 4+attrLen]

		switch attrType {
		case attrXorMappedAddress:
			if addr, err := parseXorMappedAddress(val, txID); err == nil {
				mapped = addr
			}
		case attrMappedAddress:
			if mapped == nil {
				if addr, err := parseMappedAddress(val); err == nil {
					mapped = addr
				}
			}
		}

		// each attribute is padded to multiple of 4 bytes
		padded := int(attrLen)
		if padded%4 != 0 {
			padded += 4 - (padded % 4)
		}
		if padded+4 > len(body) {
			break
		}
		body = body[4+padded:]
	}
	return mapped
}
