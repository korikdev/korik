package peer

import (
	"errors"
	"net"
	"time"
)

// udpConn wraps a single "logical connection" on top of a UDP socket that is
// shared among multiple peers simultaneously. UDP is connectionless, but the
// current Node code is designed for net.Conn (stream), so this adapter makes
// one remote address appear as a separate connection:
//   - Write()      -> send datagram to remote via shared socket
//   - Read()       -> get datagram that has been demuxed for this remote
//
// Because each korik message is sent as a single complete UDP datagram, and UDP
// naturally preserves message boundaries, this automatically fits with how node.go
// reads messages using bufio.ReadBytes('\n') - one Read() = one complete message.
type udpConn struct {
	local    *net.UDPConn
	remote   *net.UDPAddr
	incoming chan []byte
	closed   chan struct{}
}

func newUDPConn(local *net.UDPConn, remote *net.UDPAddr) *udpConn {
	return &udpConn{
		local:    local,
		remote:   remote,
		incoming: make(chan []byte, 64),
		closed:   make(chan struct{}),
	}
}

func (c *udpConn) Read(b []byte) (int, error) {
	select {
	case data, ok := <-c.incoming:
		if !ok {
			return 0, errors.New("udp conn closed")
		}
		n := copy(b, data)
		return n, nil
	case <-c.closed:
		return 0, errors.New("udp conn closed")
	}
}

func (c *udpConn) Write(b []byte) (int, error) {
	return c.local.WriteToUDP(b, c.remote)
}

func (c *udpConn) Close() error {
	select {
	case <-c.closed:
		// already closed
	default:
		close(c.closed)
	}
	return nil
}

func (c *udpConn) LocalAddr() net.Addr                { return c.local.LocalAddr() }
func (c *udpConn) RemoteAddr() net.Addr               { return c.remote }
func (c *udpConn) SetDeadline(t time.Time) error      { return nil }
func (c *udpConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *udpConn) SetWriteDeadline(t time.Time) error { return nil }

// push inserts newly received datagram into channel, called from the central
// UDP socket read-loop (see internet.go)
func (c *udpConn) push(data []byte) {
	cp := make([]byte, len(data))
	copy(cp, data)
	select {
	case c.incoming <- cp:
	default:
		// channel full (peer slow to read), drop old packet - acceptable for text chat
	}
}
