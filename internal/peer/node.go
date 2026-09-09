package peer

import (
	"bufio"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"korik/internal/circuit"
	"korik/internal/compress"
	"korik/internal/contacts"
	cryptoutil "korik/internal/crypto"
	"korik/internal/dedup"
	"korik/internal/filetransfer"
	"korik/internal/group"
	"korik/internal/history"
	"korik/internal/identity"
	"korik/internal/queue"
	"korik/internal/ratelimit"
	"korik/internal/status"
)

// safeGo runs fn in a goroutine with panic recovery so one peer's
// malformed input or bug can never take down the whole process.
func safeGo(fn func()) {
	go func() {
		defer func() {
			_ = recover()
		}()
		fn()
	}()
}

// Node represents a single korik instance on the network (LAN/WiFi hotspot).
// Callbacks OnChat/OnPeerJoin/OnPeerLeave are used by the web GUI to get
// realtime events without node.go needing to know anything about HTTP/SSE.
type Node struct {
	Name          string
	Port          int
	DiscoveryPort int
	Identity      *identity.Identity
	DeviceID      string

	mu    sync.Mutex
	conns map[string]*peerConn // key: "ip:port" -> active connection

	internet *InternetTransport // active when internet mode (STUN/hole-punching) is enabled

	// Features
	History         *history.History
	FileTransfer    *filetransfer.Manager
	ContactBook     *contacts.ContactBook
	ReadReceipts    bool
	TypingIndicator bool
	AutoReconnect   bool

	// Reliability and observability
	Status   *status.Tracker
	MsgQueue *queue.Queue

	// Hardening and rich messaging (nil-safe: all uses check for nil
	// so bare nodes in tests keep working without wiring).
	Groups  *group.Store
	Limiter *ratelimit.Limiter
	Breaker *circuit.Breaker

	// Lifecycle for graceful shutdown.
	stopMu    sync.Mutex
	listeners []net.Listener
	stopCh    chan struct{}
	stopOnce  sync.Once

	// Liveness tracking: JID -> last heartbeat time
	heartbeatMu sync.Mutex
	lastBeat    map[string]time.Time

	// Rekey tracking: addr -> last rotation time
	rekeyMu   sync.Mutex
	lastRekey map[string]time.Time

	// Typing state
	typingMu    sync.Mutex
	typingState map[string]time.Time // JID -> last typing time

	// Deduplication of inbound envelopes (reconnect / retry / replay safety).
	seen *dedup.Seen
	seq  atomic.Uint64

	OnChat         func(lid, jid, name, body string, ts time.Time)
	OnGroupChat    func(groupID, lid, jid, name, body, replyTo string, ts time.Time)
	OnPeerJoin     func(lid, jid, name, addr string)
	OnPeerLeave    func(lid, jid, name, addr string)
	OnTyping       func(lid, jid, name string)
	OnFileOffer    func(lid, jid, name, fileID, fileName string, fileSize int64)
	OnFileProgress func(fileID string, progress int)
}

type peerConn struct {
	name          string
	jid           string
	lid           string
	deviceID      string
	addr          string
	conn          net.Conn
	w             *bufio.Writer
	sessionKey    []byte // AES-256 key from ECDH, nil until handshake completes
	reconnectAddr string // address to reconnect to if connection drops
}

func NewNode(name string, port, discoveryPort int, id *identity.Identity) *Node {
	return &Node{
		Name:            name,
		Port:            port,
		DiscoveryPort:   discoveryPort,
		Identity:        id,
		conns:           make(map[string]*peerConn),
		FileTransfer:    filetransfer.NewManager(),
		typingState:     make(map[string]time.Time),
		TypingIndicator: true,
		AutoReconnect:   true,
		Status:          status.NewTracker(),
		seen:            dedup.New(10000, time.Hour),
		Limiter:         ratelimit.New(20, 40),
		Breaker:         circuit.New(5, time.Minute),
		stopCh:          make(chan struct{}),
		lastBeat:        make(map[string]time.Time),
		lastRekey:       make(map[string]time.Time),
	}
}

// nextEnvelopeID mints a unique message envelope ID:
// senderJID | deviceID | seq | unixnano | rand.
func (n *Node) nextEnvelopeID() (id string, seq uint64) {
	seq = n.seq.Add(1)
	var randSuffix [4]byte
	_, _ = rand.Read(randSuffix[:])
	device := n.DeviceID
	if device == "" {
		device = "cli"
	}
	jid := ""
	if n.Identity != nil {
		jid = n.Identity.JID
	}
	id = fmt.Sprintf("%s|%s|%d|%d|%s", jid, device, seq, time.Now().UnixNano(), hex.EncodeToString(randSuffix[:]))
	return id, seq
}

// Start opens TCP listeners on IPv4 and IPv6 (dual-stack) to accept peers.
// Either family may be unavailable; at least one must succeed.
func (n *Node) Start() error {
	var listeners []net.Listener
	if ln4, err := net.Listen("tcp4", fmt.Sprintf(":%d", n.Port)); err == nil {
		listeners = append(listeners, ln4)
	}
	if ln6, err := net.Listen("tcp6", fmt.Sprintf(":%d", n.Port)); err == nil {
		listeners = append(listeners, ln6)
	}
	if len(listeners) == 0 {
		// Fallback to generic tcp (single-stack systems).
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", n.Port))
		if err != nil {
			return err
		}
		listeners = append(listeners, ln)
	}
	for _, ln := range listeners {
		go func(l net.Listener) {
			defer func() {
				_ = recover()
			}()
			for {
				conn, err := l.Accept()
				if err != nil {
					select {
					case <-n.stopCh:
						return
					default:
					}
					continue
				}
				go n.handleIncoming(conn)
			}
		}(ln)
	}
	n.stopMu.Lock()
	n.listeners = append(n.listeners, listeners...)
	n.stopMu.Unlock()
	return nil
}

// Stop closes listeners and the internet socket for graceful shutdown.
// In-flight queue items are already persisted on enqueue, so nothing
// is lost; discovery goroutines exit via stopCh or process exit.
func (n *Node) Stop() {
	n.stopOnce.Do(func() {
		close(n.stopCh)
		n.stopMu.Lock()
		defer n.stopMu.Unlock()
		for _, ln := range n.listeners {
			_ = ln.Close()
		}
		n.listeners = nil
		if n.internet != nil && n.internet.conn != nil {
			_ = n.internet.conn.Close()
		}
	})
}

// Stopped reports whether Stop was called.
func (n *Node) Stopped() bool {
	select {
	case <-n.stopCh:
		return true
	default:
		return false
	}
}

// ConnectTo creates outgoing connection to peer at address "ip:port".
// The circuit breaker skips addresses that failed handshakes repeatedly.
func (n *Node) ConnectTo(addr string) error {
	n.mu.Lock()
	if _, exists := n.conns[addr]; exists {
		n.mu.Unlock()
		return nil // already connected
	}
	n.mu.Unlock()

	if n.Breaker != nil && n.Breaker.Skip(addr) {
		return fmt.Errorf("skipping %s: repeated handshake failures, cooling down", addr)
	}

	if n.Status != nil {
		n.Status.Set(status.PhaseConnecting, "dialing "+addr)
	}
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		if n.Breaker != nil {
			_ = n.Breaker.RecordFailure(addr)
		}
		if n.Status != nil {
			n.Status.Set(status.PhaseFailed, status.HumanizeError(err))
		}
		return err
	}
	if n.Status != nil {
		n.Status.Set(status.PhaseHandshaking, "handshaking "+addr)
	}
	go n.setupConn(conn, addr)
	return nil
}

// ConnectWithRetry dials with exponential backoff and progress reporting.
// onProgress receives human-readable lines; maxAttempts <= 0 means infinite
// when AutoReconnect is enabled, otherwise a single attempt.
func (n *Node) ConnectWithRetry(addr string, maxAttempts int, onProgress func(string)) error {
	attempt := 0
	for {
		attempt++
		if n.Status != nil {
			n.Status.Set(status.PhaseRetrying, fmt.Sprintf("attempt %d to %s", attempt, addr))
		}
		if onProgress != nil {
			onProgress(status.ProgressLine(attempt, maxAttempts, addr))
		}
		if err := n.ConnectTo(addr); err == nil {
			// Wait briefly for handshake to establish a session key.
			for i := 0; i < 20; i++ {
				n.mu.Lock()
				pc, ok := n.conns[addr]
				ready := ok && pc.sessionKey != nil
				n.mu.Unlock()
				if ready {
					if n.Status != nil {
						n.Status.Set(status.PhaseConnected, "connected "+addr)
					}
					return nil
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
		if maxAttempts > 0 && attempt >= maxAttempts {
			if n.Status != nil {
				n.Status.Set(status.PhaseFailed, "gave up dialing "+addr)
			}
			return fmt.Errorf("gave up after %d attempts", attempt)
		}
		if !n.AutoReconnect && maxAttempts <= 0 {
			return fmt.Errorf("connection failed and auto-reconnect is disabled")
		}
		time.Sleep(status.RetryPlan(attempt))
	}
}

// StartHeartbeat sends periodic ping/heartbeat and drops stale peers.
// Interval 15s, timeout 60s without heartbeat.
func (n *Node) StartHeartbeat(interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	go func() {
		defer func() {
			_ = recover()
		}()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if n.Stopped() {
				return
			}
			n.mu.Lock()
			peers := make([]*peerConn, 0, len(n.conns))
			for _, pc := range n.conns {
				peers = append(peers, pc)
			}
			n.mu.Unlock()
			for _, pc := range peers {
				n.sendRaw(pc, Message{
					Type:     MsgHeartbeat,
					From:     n.Name,
					JID:      n.Identity.JID,
					LID:      n.Identity.LID,
					DeviceID: n.DeviceID,
				})
			}
			// Detect stale peers.
			n.heartbeatMu.Lock()
			now := time.Now()
			for jid, last := range n.lastBeat {
				if now.Sub(last) > 60*time.Second {
					delete(n.lastBeat, jid)
				}
			}
			n.heartbeatMu.Unlock()
			// Opportunistic session rekey every 10 minutes.
			n.maybeRekey()
		}
	}()
}

// LastHeartbeat returns the last heartbeat time for a JID.
func (n *Node) LastHeartbeat(jid string) (time.Time, bool) {
	n.heartbeatMu.Lock()
	defer n.heartbeatMu.Unlock()
	ts, ok := n.lastBeat[jid]
	return ts, ok
}

// FlushQueue delivers queued messages to a peer that just came online.
// deliver calls SendTo for each pending message addressed to jid.
func (n *Node) FlushQueue(jid string, deliver func(toJID, body string) error) int {
	if n.MsgQueue == nil {
		return 0
	}
	sent := 0
	for _, m := range n.MsgQueue.Pending() {
		if m.ToJID != jid {
			continue
		}
		n.MsgQueue.MarkAttempt(m.ID)
		if err := n.SendToWithID(m.ToJID, m.Body, m.ID); err == nil {
			_ = n.MsgQueue.Remove(m.ID)
			sent++
		} else if deliver != nil {
			// Legacy custom-deliver path (tests); still count ID as attempted.
			if err := deliver(m.ToJID, m.Body); err == nil {
				_ = n.MsgQueue.Remove(m.ID)
				sent++
			}
		}
	}
	return sent
}

// QueueOrSend tries direct delivery and queues the message on failure.
// The envelope ID is minted upfront so a later flush reuses the same ID
// and the receiver deduplicates the replay.
func (n *Node) QueueOrSend(jid, body string) error {
	envelopeID, _ := n.nextEnvelopeID()
	if err := n.SendToWithID(jid, body, envelopeID); err == nil {
		return nil
	} else {
		if n.MsgQueue == nil {
			return err
		}
		_ = n.MsgQueue.Enqueue(queue.OutgoingMessage{
			ID:        envelopeID,
			ToJID:     jid,
			Body:      body,
			CreatedAt: time.Now(),
		})
		return fmt.Errorf("peer offline, message queued for %s", jid)
	}
}

// maybeRekey rotates session keys for long-lived connections.
func (n *Node) maybeRekey() {
	n.rekeyMu.Lock()
	defer n.rekeyMu.Unlock()
	n.mu.Lock()
	peers := make([]*peerConn, 0, len(n.conns))
	for _, pc := range n.conns {
		peers = append(peers, pc)
	}
	n.mu.Unlock()
	now := time.Now()
	for _, pc := range peers {
		last := n.lastRekey[pc.addr]
		if last.IsZero() || now.Sub(last) > 10*time.Minute {
			if err := n.initiateRekey(pc); err == nil {
				n.lastRekey[pc.addr] = now
			}
		}
	}
}

// initiateRekey starts a fresh ECDH exchange to achieve forward secrecy.
// The new session key mixes the old key with the fresh shared secret.
func (n *Node) initiateRekey(pc *peerConn) error {
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	n.mu.Lock()
	pcAddr := pc.addr
	oldKey := pc.sessionKey
	n.mu.Unlock()
	if oldKey == nil {
		return fmt.Errorf("no session yet")
	}
	// Store ephemeral privately keyed by address for the response round-trip.
	rekeySlots.mu.Lock()
	rekeySlots.keys[pcAddr] = ephemeral
	rekeySlots.mu.Unlock()
	n.sendRaw(pc, Message{
		Type:        MsgRekey,
		From:        n.Name,
		JID:         n.Identity.JID,
		LID:         n.Identity.LID,
		DeviceID:    n.DeviceID,
		RekeyPubKey: hex.EncodeToString(ephemeral.PublicKey().Bytes()),
	})
	return nil
}

var rekeySlots = struct {
	mu   sync.Mutex
	keys map[string]*ecdh.PrivateKey
}{keys: make(map[string]*ecdh.PrivateKey)}

func (n *Node) handleIncoming(conn net.Conn) {
	addr := conn.RemoteAddr().String()
	n.setupConn(conn, addr)
}

func (n *Node) setupConn(conn net.Conn, addr string) {
	defer func() {
		_ = recover()
		n.removePeer(addr)
	}()
	w := bufio.NewWriter(conn)
	pc := &peerConn{addr: addr, conn: conn, w: w, reconnectAddr: addr}

	n.mu.Lock()
	n.conns[addr] = pc
	n.mu.Unlock()

	// handshake: send our JID, LID, name, device, and ECDH public key (plaintext -
	// this is normal for key exchange, what's kept secret is CHAT CONTENT, not pubkey)
	n.sendRaw(pc, Message{
		Type:     MsgHello,
		From:     n.Name,
		JID:      n.Identity.JID,
		LID:      n.Identity.LID,
		DeviceID: n.DeviceID,
		PubKey:   n.Identity.PublicKeyHex(),
	})

	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			n.removePeer(addr)
			return
		}
		msg, err := decodeMessage(line)
		if err != nil {
			continue
		}
		n.handleMessage(pc, msg)
	}
}

// rateKey identifies a peer for flood protection (stable JID preferred).
func rateKey(pc *peerConn) string {
	if pc.jid != "" {
		return "jid:" + pc.jid
	}
	return "addr:" + pc.addr
}

// allowInbound enforces per-peer rate limiting on heavy message types.
func (n *Node) allowInbound(pc *peerConn) bool {
	if n.Limiter == nil {
		return true
	}
	return n.Limiter.Allow(rateKey(pc))
}

func (n *Node) handleMessage(pc *peerConn, msg Message) {
	// Generic envelope dedup: reconnects, retries, and replays reuse the
	// same message ID, so a repeated ID must never be processed twice.
	// Typing/heartbeat are ephemeral and intentionally excluded.
	switch msg.Type {
	case MsgChat, MsgGroupChat, MsgRekey, MsgFileOffer, MsgFileAccept, MsgFileReject,
		MsgFileComplete, MsgChunkRequest, MsgReadReceipt,
		MsgGroupInvite, MsgGroupLeave, MsgSyncRequest, MsgSyncData:
		if msg.ID != "" && n.seen != nil && !n.seen.Add(msg.ID) {
			return
		}
	case MsgFileChunk:
		// Chunks dedup by (fileID, index) in WriteChunk; still collapse
		// exact envelope replays so progress hooks do not fire twice.
		if msg.ID != "" && n.seen != nil && !n.seen.Add(msg.ID) {
			return
		}
	}
	switch msg.Type {
	case MsgHello:
		theirPub, err := identity.ParsePeerPublicKey(msg.PubKey)
		if err != nil {
			return
		}
		// verify that JID is truly a fingerprint of the sent pubkey (simple anti-spoof)
		if identity.JIDFromPublicKeyHex(msg.PubKey) != msg.JID {
			fmt.Printf("\n[!] peer %s: JID doesn't match pubkey, connection suspicious, rejected\n> ", pc.addr)
			pc.conn.Close()
			return
		}
		sessionKey, err := cryptoutil.DeriveSessionKey(n.Identity.Private, theirPub)
		if err != nil {
			return
		}

		n.mu.Lock()
		alreadyJoined := pc.sessionKey != nil
		pc.name = msg.From
		pc.jid = msg.JID
		pc.lid = msg.LID
		pc.deviceID = msg.DeviceID
		pc.sessionKey = sessionKey
		n.mu.Unlock()

		if alreadyJoined {
			return // duplicate hello (e.g. from burst hole-punch), session exists, no need to emit event again
		}
		if n.Breaker != nil {
			n.Breaker.RecordSuccess(pc.addr)
		}
		if n.Limiter != nil {
			n.Limiter.Reset(rateKey(pc))
		}

		// Capture previous sighting before refresh for delta sync.
		var prevSeen time.Time
		if n.ContactBook != nil {
			if c, ok := n.ContactBook.Get(msg.JID); ok {
				prevSeen = c.LastSeen
			}
		}

		// Auto-add to contacts (skip our own linked devices to avoid self-contact spam).
		isLinkedDevice := msg.JID == n.Identity.JID && msg.DeviceID != "" && msg.DeviceID != n.DeviceID
		if n.ContactBook != nil && !isLinkedDevice {
			n.ContactBook.Add(msg.JID, msg.From)
			n.ContactBook.UpdateLastSeen(msg.JID, pc.addr)
		}

		if isLinkedDevice {
			fmt.Printf("\n[+] linked device %s (device %s) joined (%s)\n> ", msg.From, msg.DeviceID, pc.addr)
		} else {
			fmt.Printf("\n[+] %s (LID:%s) joined, e2e ready (%s)\n> ", msg.From, msg.LID, pc.addr)
		}
		if n.Status != nil {
			n.Status.Set(status.PhaseConnected, "e2e ready "+pc.addr)
		}
		if n.OnPeerJoin != nil {
			n.OnPeerJoin(msg.LID, msg.JID, msg.From, pc.addr)
		}
		// Deliver any queued messages for this peer, reusing each queued
		// envelope ID so replays deduplicate on the receiver.
		if n.MsgQueue != nil {
			pending := n.MsgQueue.Pending()
			for _, m := range pending {
				if m.ToJID == msg.JID {
					if err := n.SendToWithID(m.ToJID, m.Body, m.ID); err == nil {
						_ = n.MsgQueue.Remove(m.ID)
						fmt.Printf("\n[queued → %s] delivered\n> ", msg.From)
					}
				}
			}
		}
		// Delta sync both directions: push what we sent while they were
		// away, and ask for what they sent while we were away.
		// Only direct (non-group) messages sync; rooms sync on demand.
		if !prevSeen.IsZero() {
			safeGo(func() {
				n.syncMissedTo(pc, prevSeen)
				n.sendRaw(pc, Message{
					Type:      MsgSyncRequest,
					From:      n.Name,
					JID:       n.Identity.JID,
					LID:       n.Identity.LID,
					SyncSince: prevSeen.Unix(),
				})
			})
		}

	case MsgChat:
		if !n.allowInbound(pc) {
			return
		}
		n.mu.Lock()
		key := pc.sessionKey
		n.mu.Unlock()
		if key == nil {
			return // not handshaked yet, can't decrypt
		}

		// Check if sender is blocked
		if n.ContactBook != nil && n.ContactBook.IsBlocked(pc.jid) {
			return // ignore messages from blocked contacts
		}

		raw, err := base64.StdEncoding.DecodeString(msg.Body)
		if err != nil {
			return
		}
		plaintext, err := cryptoutil.Decrypt(key, raw)
		if err != nil {
			fmt.Printf("\n[!] failed to decrypt message from %s\n> ", pc.name)
			return
		}
		plaintext, err = compress.Decode(plaintext, msg.Compressed)
		if err != nil {
			return
		}

		// Increment contact message count
		if n.ContactBook != nil {
			n.ContactBook.IncrementMessageCount(pc.jid)
		}

		fmt.Printf("\n%s: %s\n> ", pc.name, string(plaintext))
		if n.OnChat != nil {
			n.OnChat(pc.lid, pc.jid, pc.name, string(plaintext), time.Now())
		}
		if n.History != nil {
			_ = n.History.Add(history.ChatMessage{
				ID: msg.ID, From: pc.name, JID: pc.jid, LID: pc.lid,
				SenderDevice: msg.DeviceID, Seq: msg.Seq, ReplyTo: msg.ReplyTo,
				Body: string(plaintext), Timestamp: time.Now(), Direction: "received",
			})
		}

	case MsgGroupInvite:
		n.handleGroupInvite(pc, msg)

	case MsgGroupChat:
		n.handleGroupChat(pc, msg)

	case MsgGroupLeave:
		if msg.GroupID != "" && n.Groups != nil {
			_ = n.Groups.RemoveMember(msg.GroupID, msg.JID)
			fmt.Printf("\n[-] %s left group %s\n> ", pc.name, msg.GroupID)
		}

	case MsgSyncRequest:
		n.handleSyncRequest(pc, msg)

	case MsgSyncData:
		n.handleSyncData(pc, msg)

	case MsgPing, MsgHeartbeat:
		n.heartbeatMu.Lock()
		if pc.jid != "" {
			n.lastBeat[pc.jid] = time.Now()
		}
		n.heartbeatMu.Unlock()

	case MsgRekey:
		// Ephemeral-ephemeral rotation for forward secrecy.
		// If we initiated (slot exists), complete with ECDH(ourEphemeral, theirEphemeral).
		// If peer initiated, generate a one-time response ephemeral and do not
		// create a new slot, preventing infinite rekey ping-pong.
		theirPub, err := identity.ParsePeerPublicKey(msg.RekeyPubKey)
		if err != nil || theirPub == nil {
			return
		}
		n.mu.Lock()
		oldKey := pc.sessionKey
		n.mu.Unlock()
		if oldKey == nil {
			return
		}
		rekeySlots.mu.Lock()
		ourEphemeral, hasSlot := rekeySlots.keys[pc.addr]
		if hasSlot {
			delete(rekeySlots.keys, pc.addr)
		}
		rekeySlots.mu.Unlock()

		var shared []byte
		if hasSlot {
			raw, err := ourEphemeral.ECDH(theirPub)
			if err != nil {
				return
			}
			shared = raw
		} else {
			response, err := ecdh.X25519().GenerateKey(rand.Reader)
			if err != nil {
				return
			}
			raw, err := response.ECDH(theirPub)
			if err != nil {
				return
			}
			shared = raw
			n.sendRaw(pc, Message{
				Type:        MsgRekey,
				From:        n.Name,
				JID:         n.Identity.JID,
				LID:         n.Identity.LID,
				DeviceID:    n.DeviceID,
				RekeyPubKey: hex.EncodeToString(response.PublicKey().Bytes()),
			})
		}
		mixed := cryptoutil.MixKeys(oldKey, shared)
		n.mu.Lock()
		pc.sessionKey = mixed
		n.mu.Unlock()
		n.rekeyMu.Lock()
		n.lastRekey[pc.addr] = time.Now()
		n.rekeyMu.Unlock()

	case MsgChunkRequest:
		// Receiver asks for missing chunks after reconnect (resume).
		if _, ok := n.FileTransfer.GetTransfer(msg.FileID); !ok || len(msg.Missing) == 0 {
			return
		}
		go n.resendChunks(pc, msg.FileID, msg.Missing)

	case MsgTyping:
		if n.TypingIndicator {
			n.typingMu.Lock()
			n.typingState[pc.jid] = time.Now()
			n.typingMu.Unlock()

			fmt.Printf("\n[%s is typing...]\n> ", pc.name)
			if n.OnTyping != nil {
				n.OnTyping(pc.lid, pc.jid, pc.name)
			}
		}

	case MsgReadReceipt:
		if n.ReadReceipts {
			fmt.Printf("\n[✓✓ message read by %s]\n> ", pc.name)
		}

	case MsgFileOffer:
		// FileID is the idempotency key for offers: retries with the same
		// FileID must not prompt twice. Create the receive-side transfer
		// here so replays collapse even across reconnects.
		if msg.FileID == "" {
			return
		}
		if _, exists := n.FileTransfer.GetTransfer(msg.FileID); exists {
			return
		}
		if _, restored := n.FileTransfer.RestorePartial(msg.FileID); restored {
			return
		}
		n.FileTransfer.CreateReceiveTransfer(msg.FileID, msg.FileName, pc.jid, msg.FileSize, msg.ChunkTotal)
		fmt.Printf("\n[📁] %s wants to send file: %s (%.2f MB)\n", pc.name, msg.FileName, float64(msg.FileSize)/1024/1024)
		fmt.Printf("Accept? Type: /accept %s  or  /reject %s\n> ", msg.FileID, msg.FileID)

		if n.OnFileOffer != nil {
			n.OnFileOffer(pc.lid, pc.jid, pc.name, msg.FileID, msg.FileName, msg.FileSize)
		}

	case MsgFileAccept:
		fmt.Printf("\n[✓] %s accepted file transfer %s\n> ", pc.name, msg.FileID)
		go n.startFileSend(pc, msg.FileID)

	case MsgFileReject:
		fmt.Printf("\n[✗] %s rejected file transfer %s\n> ", pc.name, msg.FileID)
		if t, ok := n.FileTransfer.GetTransfer(msg.FileID); ok {
			t.Cancel()
		}

	case MsgFileChunk:
		transfer, ok := n.FileTransfer.GetTransfer(msg.FileID)
		if !ok {
			// Try to restore a checkpointed partial transfer (resume).
			restored, found := n.FileTransfer.RestorePartial(msg.FileID)
			if !found {
				return
			}
			transfer = restored
		}
		t := transfer

		data, err := filetransfer.DecodeChunk(msg.ChunkData)
		if err != nil {
			return
		}

		if err := t.WriteChunk(msg.ChunkIndex, data); err != nil {
			return
		}
		// Persist progress so interruption resumes instead of restarting.
		_ = n.FileTransfer.Checkpoint(t)

		if t.IsComplete() {
			savePath, err := n.FileTransfer.SaveFile(t)
			if err != nil {
				fmt.Printf("\n[✗] failed to save file: %v\n> ", err)
				return
			}
			n.FileTransfer.ClearPartial(msg.FileID)
			fmt.Printf("\n[✓] file saved: %s\n> ", savePath)

			// Send completion confirmation
			n.sendRaw(pc, Message{
				Type:   MsgFileComplete,
				From:   n.Name,
				JID:    n.Identity.JID,
				LID:    n.Identity.LID,
				FileID: msg.FileID,
			})
		} else {
			progress := t.ProgressPercent()
			if progress%10 == 0 { // Show every 10%
				if n.OnFileProgress != nil {
					n.OnFileProgress(msg.FileID, progress)
				}
			}
		}

	case MsgFileComplete:
		fmt.Printf("\n[✓] file transfer completed: %s\n> ", msg.FileID)
		if t, ok := n.FileTransfer.GetTransfer(msg.FileID); ok {
			t.Status = "completed"
		}
	}
}

// decryptSessionBody base64-decodes, session-decrypts, and decompresses a body.
func (n *Node) decryptSessionBody(pc *peerConn, msg Message) ([]byte, bool) {
	n.mu.Lock()
	key := pc.sessionKey
	n.mu.Unlock()
	if key == nil {
		return nil, false
	}
	raw, err := base64.StdEncoding.DecodeString(msg.Body)
	if err != nil {
		return nil, false
	}
	plaintext, err := cryptoutil.Decrypt(key, raw)
	if err != nil {
		return nil, false
	}
	plaintext, err = compress.Decode(plaintext, msg.Compressed)
	if err != nil {
		return nil, false
	}
	return plaintext, true
}

// groupInvitePayload is distributed over pairwise E2E sessions only.
type groupInvitePayload struct {
	GroupID   string   `json:"group_id"`
	GroupName string   `json:"group_name"`
	GroupKey  string   `json:"group_key"`
	Members   []string `json:"members"`
}

func (n *Node) handleGroupInvite(pc *peerConn, msg Message) {
	if !n.allowInbound(pc) || n.Groups == nil {
		return
	}
	plaintext, ok := n.decryptSessionBody(pc, msg)
	if !ok {
		return
	}
	var inner groupInvitePayload
	if err := json.Unmarshal(plaintext, &inner); err != nil {
		return
	}
	key, err := base64.StdEncoding.DecodeString(inner.GroupKey)
	if err != nil || len(key) != 32 || inner.GroupID == "" {
		return
	}
	room := &group.Room{
		ID: inner.GroupID, Name: inner.GroupName, Creator: msg.JID,
		Key: key, Members: inner.Members, CreatedAt: time.Now(),
	}
	found := false
	for _, m := range room.Members {
		if m == n.Identity.JID {
			found = true
			break
		}
	}
	if !found {
		room.Members = append(room.Members, n.Identity.JID)
	}
	if err := n.Groups.Add(room); err != nil {
		return
	}
	display := room.Name
	if display == "" {
		display = room.ID
	}
	fmt.Printf("\n[+] invited to group %s by %s (members: %d)\n> ", display, pc.name, len(room.Members))
}

func (n *Node) handleGroupChat(pc *peerConn, msg Message) {
	if !n.allowInbound(pc) || n.Groups == nil || msg.GroupID == "" {
		return
	}
	room, ok := n.Groups.Get(msg.GroupID)
	if !ok {
		return
	}
	self := ""
	if n.Identity != nil {
		self = n.Identity.JID
	}
	if !n.Groups.IsMember(msg.GroupID, pc.jid) || !n.Groups.IsMember(msg.GroupID, self) {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(msg.Body)
	if err != nil {
		return
	}
	plaintext, err := cryptoutil.Decrypt(room.Key, raw)
	if err != nil {
		return
	}
	plaintext, err = compress.Decode(plaintext, msg.Compressed)
	if err != nil {
		return
	}
	if n.ContactBook != nil {
		n.ContactBook.IncrementMessageCount(pc.jid)
	}
	fmt.Printf("\n[%s] %s: %s\n> ", room.Name, pc.name, string(plaintext))
	if n.OnGroupChat != nil {
		n.OnGroupChat(msg.GroupID, pc.lid, pc.jid, pc.name, string(plaintext), msg.ReplyTo, time.Now())
	}
	if n.History != nil {
		_ = n.History.Add(history.ChatMessage{
			ID: msg.ID, From: pc.name, JID: pc.jid, LID: pc.lid,
			SenderDevice: msg.DeviceID, Seq: msg.Seq, GroupID: msg.GroupID,
			ReplyTo: msg.ReplyTo, Body: string(plaintext),
			Timestamp: time.Now(), Direction: "received",
		})
	}
}

// handleSyncRequest answers delta sync with our sent-direct messages.
func (n *Node) handleSyncRequest(pc *peerConn, msg Message) {
	if n.History == nil || msg.SyncSince <= 0 {
		return
	}
	since := time.Unix(msg.SyncSince, 0)
	sent := 0
	for _, m := range n.History.GetSince(since) {
		if m.Direction != "sent" || m.GroupID != "" || sent >= 50 {
			continue
		}
		_ = n.sendSyncData(pc, m.Body, m.ReplyTo)
		sent++
	}
}

// handleSyncData stores and displays a re-sent missed message.
func (n *Node) handleSyncData(pc *peerConn, msg Message) {
	plaintext, ok := n.decryptSessionBody(pc, msg)
	if !ok {
		return
	}
	fmt.Printf("\n%s [sync]: %s\n> ", pc.name, string(plaintext))
	if n.OnChat != nil {
		n.OnChat(pc.lid, pc.jid, pc.name, string(plaintext), time.Now())
	}
	if n.History != nil {
		_ = n.History.Add(history.ChatMessage{
			ID: msg.ID, From: pc.name, JID: pc.jid, LID: pc.lid,
			SenderDevice: msg.DeviceID, Seq: msg.Seq, ReplyTo: msg.ReplyTo,
			Body: string(plaintext), Timestamp: time.Now(), Direction: "received",
		})
	}
}

// sendSyncData ships one missed message with a fresh envelope ID.
func (n *Node) sendSyncData(pc *peerConn, body, replyTo string) error {
	payload, compressed, err := compress.Encode([]byte(body))
	if err != nil {
		return err
	}
	n.mu.Lock()
	key := pc.sessionKey
	n.mu.Unlock()
	if key == nil {
		return fmt.Errorf("e2e session not ready")
	}
	ciphertext, err := cryptoutil.Encrypt(key, payload)
	if err != nil {
		return err
	}
	n.sendRaw(pc, Message{
		Type: MsgSyncData, From: n.Name, JID: n.Identity.JID, LID: n.Identity.LID,
		Body:    base64.StdEncoding.EncodeToString(ciphertext),
		ReplyTo: replyTo, Compressed: compressed,
	})
	return nil
}

// syncMissedTo pushes direct messages the peer missed while away.
func (n *Node) syncMissedTo(pc *peerConn, since time.Time) {
	if n.History == nil || since.IsZero() {
		return
	}
	sent := 0
	for _, m := range n.History.GetSince(since) {
		if m.Direction != "sent" || m.GroupID != "" || sent >= 50 {
			continue
		}
		if err := n.SendTo(pc.jid, m.Body); err != nil {
			return
		}
		sent++
	}
	if sent > 0 {
		fmt.Printf("\n[sync → %s] %d missed message(s)\n> ", pc.name, sent)
	}
}

// encryptForSend compresses large plaintext then encrypts with key.
func encryptForSend(key, plaintext []byte) (ciphertext []byte, compressed bool, err error) {
	payload, compressed, err := compress.Encode(plaintext)
	if err != nil {
		return nil, false, err
	}
	ciphertext, err = cryptoutil.Encrypt(key, payload)
	if err != nil {
		return nil, false, err
	}
	return ciphertext, compressed, nil
}

func (n *Node) removePeer(addr string) {
	n.mu.Lock()
	pc, ok := n.conns[addr]
	delete(n.conns, addr)
	n.mu.Unlock()
	if ok {
		fmt.Printf("\n[-] %s disconnected (%s)\n> ", pc.name, addr)
		if n.OnPeerLeave != nil {
			n.OnPeerLeave(pc.lid, pc.jid, pc.name, addr)
		}
	}
}

// sendRaw sends message without encryption (used specifically for MsgHello)
func (n *Node) sendRaw(pc *peerConn, msg Message) {
	msg.Timestamp = time.Now()
	if msg.DeviceID == "" {
		msg.DeviceID = n.DeviceID
	}
	if msg.ID == "" {
		msg.ID, msg.Seq = n.nextEnvelopeID()
	} else if msg.Seq == 0 {
		_, msg.Seq = n.nextEnvelopeID()
		// Keep the caller's ID (queue retry reuse) but assign a fresh seq.
		// Dedup keys on ID only, so retries still collapse.
		msg.ID = msgIDOnly(msg.ID)
	}
	b, err := encodeMessage(msg)
	if err != nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	_, _ = pc.w.Write(b)
	_ = pc.w.Flush()
}

// msgIDOnly guards against callers accidentally passing composite values;
// queue IDs are already envelope-safe, so this is a pass-through that
// documents the dedup contract in one place.
func msgIDOnly(id string) string {
	return id
}

// SendTo encrypts & sends message to ONE specific peer (by JID), not broadcast.
// Used by GUI for "per-contact chat" mode like WhatsApp.
func (n *Node) SendTo(jid, text string) error {
	id, _ := n.nextEnvelopeID()
	return n.SendToWithID(jid, text, id)
}

// SendToWithID sends with a caller-supplied envelope ID so queue retries
// reuse the original ID and receivers deduplicate replays.
func (n *Node) SendToWithID(jid, text, envelopeID string) error {
	n.mu.Lock()
	var target *peerConn
	for _, pc := range n.conns {
		if pc.jid == jid {
			target = pc
			break
		}
	}
	n.mu.Unlock()

	if target == nil {
		return fmt.Errorf("peer with JID %s not found / not connected yet", jid)
	}
	n.mu.Lock()
	key := target.sessionKey
	n.mu.Unlock()
	if key == nil {
		return fmt.Errorf("e2e session with this peer not ready yet")
	}
	ciphertext, compressed, err := encryptForSend(key, []byte(text))
	if err != nil {
		return err
	}
	msg := Message{
		ID:         envelopeID,
		Type:       MsgChat,
		From:       n.Name,
		JID:        n.Identity.JID,
		LID:        n.Identity.LID,
		DeviceID:   n.DeviceID,
		Body:       base64.StdEncoding.EncodeToString(ciphertext),
		Compressed: compressed,
	}
	n.sendRaw(target, msg)
	return nil
}

// Broadcast encrypts & sends chat message to all e2e-ready peers.
// One envelope ID is shared across peers so a retried broadcast
// still collapses to a single delivery per receiver.
// It returns the envelope ID for history bookkeeping.
func (n *Node) Broadcast(text string) string {
	return n.broadcastWithReply(text, "")
}

// BroadcastReply is Broadcast with reply context.
func (n *Node) BroadcastReply(text, replyTo string) string {
	return n.broadcastWithReply(text, replyTo)
}

func (n *Node) broadcastWithReply(text, replyTo string) string {
	n.mu.Lock()
	peers := make([]*peerConn, 0, len(n.conns))
	for _, pc := range n.conns {
		peers = append(peers, pc)
	}
	n.mu.Unlock()

	envelopeID, _ := n.nextEnvelopeID()
	for _, pc := range peers {
		n.mu.Lock()
		key := pc.sessionKey
		n.mu.Unlock()
		if key == nil {
			continue // handshake not completed yet, skip
		}

		// Check if contact is blocked
		if n.ContactBook != nil && n.ContactBook.IsBlocked(pc.jid) {
			continue
		}

		ciphertext, compressed, err := encryptForSend(key, []byte(text))
		if err != nil {
			continue
		}
		msg := Message{
			ID:         envelopeID,
			Type:       MsgChat,
			From:       n.Name,
			JID:        n.Identity.JID,
			LID:        n.Identity.LID,
			DeviceID:   n.DeviceID,
			Body:       base64.StdEncoding.EncodeToString(ciphertext),
			ReplyTo:    replyTo,
			Compressed: compressed,
		}
		n.sendRaw(pc, msg)
	}
	return envelopeID
}

// CreateRoom creates a group room owned by this node.
func (n *Node) CreateRoom(name string) (*group.Room, error) {
	if n.Groups == nil {
		return nil, fmt.Errorf("group store not initialized")
	}
	return n.Groups.Create(name, n.Identity.JID)
}

// InviteToRoom distributes the room key to a member over the pairwise
// E2E session. The member must be online with a ready session.
func (n *Node) InviteToRoom(roomID, memberJID string) error {
	if n.Groups == nil {
		return fmt.Errorf("group store not initialized")
	}
	room, ok := n.Groups.Get(roomID)
	if !ok {
		return fmt.Errorf("unknown room %s", roomID)
	}
	n.mu.Lock()
	var target *peerConn
	for _, pc := range n.conns {
		if pc.jid == memberJID && pc.sessionKey != nil {
			target = pc
			break
		}
	}
	key := []byte(nil)
	if target != nil {
		key = target.sessionKey
	}
	n.mu.Unlock()
	if target == nil || key == nil {
		return fmt.Errorf("member %s not connected or session not ready", memberJID)
	}
	inner, err := json.Marshal(groupInvitePayload{
		GroupID: room.ID, GroupName: room.Name,
		GroupKey: base64.StdEncoding.EncodeToString(room.Key),
		Members:  room.Members,
	})
	if err != nil {
		return err
	}
	ciphertext, err := cryptoutil.Encrypt(key, inner)
	if err != nil {
		return err
	}
	n.sendRaw(target, Message{
		Type: MsgGroupInvite, From: n.Name, JID: n.Identity.JID, LID: n.Identity.LID,
		Body: base64.StdEncoding.EncodeToString(ciphertext),
	})
	_ = n.Groups.AddMember(room.ID, memberJID)
	return nil
}

// SendGroup encrypts a message with the room key and delivers it to
// every online member (except self). It returns the envelope ID.
// Offline members are skipped in v1 (rooms sync on demand, not queued).
func (n *Node) SendGroup(roomID, text, replyTo string) (string, error) {
	if n.Groups == nil {
		return "", fmt.Errorf("group store not initialized")
	}
	room, ok := n.Groups.Get(roomID)
	if !ok {
		return "", fmt.Errorf("unknown room %s", roomID)
	}
	self := n.Identity.JID
	if !n.Groups.IsMember(roomID, self) {
		return "", fmt.Errorf("you are not a member of %s", room.Name)
	}
	payload, compressed, err := compress.Encode([]byte(text))
	if err != nil {
		return "", err
	}
	ciphertext, err := cryptoutil.Encrypt(room.Key, payload)
	if err != nil {
		return "", err
	}
	envelopeID, _ := n.nextEnvelopeID()
	n.mu.Lock()
	peers := make([]*peerConn, 0, len(n.conns))
	for _, pc := range n.conns {
		peers = append(peers, pc)
	}
	n.mu.Unlock()
	delivered := 0
	for _, pc := range peers {
		member := false
		for _, m := range room.Members {
			if m == pc.jid {
				member = true
				break
			}
		}
		if !member || pc.jid == self {
			continue
		}
		n.mu.Lock()
		ready := pc.sessionKey != nil
		n.mu.Unlock()
		if !ready {
			continue
		}
		n.sendRaw(pc, Message{
			ID: envelopeID, Type: MsgGroupChat, From: n.Name,
			JID: n.Identity.JID, LID: n.Identity.LID,
			GroupID: room.ID, ReplyTo: replyTo,
			Body:       base64.StdEncoding.EncodeToString(ciphertext),
			Compressed: compressed,
		})
		delivered++
	}
	if delivered == 0 {
		return envelopeID, fmt.Errorf("no room members online")
	}
	return envelopeID, nil
}

// LeaveRoom notifies online members and drops the local copy.
func (n *Node) LeaveRoom(roomID string) error {
	if n.Groups == nil {
		return fmt.Errorf("group store not initialized")
	}
	room, ok := n.Groups.Get(roomID)
	if !ok {
		return fmt.Errorf("unknown room %s", roomID)
	}
	n.mu.Lock()
	peers := make([]*peerConn, 0, len(n.conns))
	for _, pc := range n.conns {
		peers = append(peers, pc)
	}
	n.mu.Unlock()
	for _, pc := range peers {
		n.sendRaw(pc, Message{
			Type: MsgGroupLeave, From: n.Name, JID: n.Identity.JID, LID: n.Identity.LID,
			GroupID: room.ID,
		})
	}
	_ = n.Groups.RemoveMember(room.ID, n.Identity.JID)
	return nil
}

// RekeyRoom rotates the room key and re-invites remaining online members.
// Removed members keep the old key but stop receiving new messages;
// for strict forward secrecy create a fresh room instead.
func (n *Node) RekeyRoom(roomID string) error {
	if n.Groups == nil {
		return fmt.Errorf("group store not initialized")
	}
	room, err := n.Groups.Rekey(roomID)
	if err != nil {
		return err
	}
	self := n.Identity.JID
	for _, m := range room.Members {
		if m == self {
			continue
		}
		_ = n.InviteToRoom(room.ID, m)
	}
	return nil
}

type PeerInfo struct {
	Name     string `json:"name"`
	JID      string `json:"jid"`
	LID      string `json:"lid"`
	DeviceID string `json:"device_id"`
	Addr     string `json:"addr"`
	Ready    bool   `json:"ready"` // whether e2e handshake is completed or not
}

// Peers returns snapshot of peer list (used by GUI & CLI)
func (n *Node) Peers() []PeerInfo {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]PeerInfo, 0, len(n.conns))
	for addr, pc := range n.conns {
		out = append(out, PeerInfo{
			Name:     pc.name,
			JID:      pc.jid,
			LID:      pc.lid,
			DeviceID: pc.deviceID,
			Addr:     addr,
			Ready:    pc.sessionKey != nil,
		})
	}
	return out
}

// LinkedDevices returns peers sharing our JID but with a different device ID.
// CLI-only linked devices: same private key imported via /export + /import,
// each device keeps its own DeviceID from config.
func (n *Node) LinkedDevices() []PeerInfo {
	all := n.Peers()
	out := make([]PeerInfo, 0)
	for _, p := range all {
		if p.JID == n.Identity.JID && p.DeviceID != "" && p.DeviceID != n.DeviceID {
			out = append(out, p)
		}
	}
	return out
}

// ListPeers displays list of currently connected peers (CLI)
func (n *Node) ListPeers() {
	peers := n.Peers()
	if len(peers) == 0 {
		fmt.Println("no peers connected yet")
		return
	}
	fmt.Println("active peers:")
	for _, p := range peers {
		label := p.Name
		if label == "" {
			label = "(not handshaked yet)"
		}
		status := "e2e ready"
		if !p.Ready {
			status = "handshake..."
		}
		device := ""
		if p.DeviceID != "" {
			device = " device:" + p.DeviceID
		}
		linked := ""
		if p.JID != "" && n.Identity != nil && p.JID == n.Identity.JID && p.DeviceID != n.DeviceID {
			linked = " [linked-device]"
		}
		fmt.Printf("  - %s [LID:%s] @ %s (%s%s)%s\n", label, p.LID, p.Addr, status, device, linked)
	}
}

// SendTyping sends typing indicator to all peers
func (n *Node) SendTyping() {
	if !n.TypingIndicator {
		return
	}

	n.mu.Lock()
	peers := make([]*peerConn, 0, len(n.conns))
	for _, pc := range n.conns {
		if pc.sessionKey != nil {
			peers = append(peers, pc)
		}
	}
	n.mu.Unlock()

	msg := Message{
		Type:     MsgTyping,
		From:     n.Name,
		JID:      n.Identity.JID,
		LID:      n.Identity.LID,
		DeviceID: n.DeviceID,
	}

	for _, pc := range peers {
		n.sendRaw(pc, msg)
	}
}

// SendReadReceipt sends read receipt for a message
func (n *Node) SendReadReceipt(jid, messageID string) {
	if !n.ReadReceipts {
		return
	}

	n.mu.Lock()
	var target *peerConn
	for _, pc := range n.conns {
		if pc.jid == jid && pc.sessionKey != nil {
			target = pc
			break
		}
	}
	n.mu.Unlock()

	if target == nil {
		return
	}

	msg := Message{
		Type:      MsgReadReceipt,
		From:      n.Name,
		JID:       n.Identity.JID,
		LID:       n.Identity.LID,
		MessageID: messageID,
	}

	n.sendRaw(target, msg)
}

// SendFile initiates a file transfer
func (n *Node) SendFile(jid, filePath string) error {
	n.mu.Lock()
	var target *peerConn
	for _, pc := range n.conns {
		if pc.jid == jid && pc.sessionKey != nil {
			target = pc
			break
		}
	}
	n.mu.Unlock()

	if target == nil {
		return fmt.Errorf("peer not connected or e2e not ready")
	}

	t, err := n.FileTransfer.CreateSendTransfer(filePath, jid)
	if err != nil {
		return err
	}

	msg := Message{
		Type:       MsgFileOffer,
		From:       n.Name,
		JID:        n.Identity.JID,
		LID:        n.Identity.LID,
		FileID:     t.ID,
		FileName:   t.FileName,
		FileSize:   t.FileSize,
		ChunkTotal: t.ChunkTotal,
	}

	n.sendRaw(target, msg)
	fmt.Printf("file transfer offered: %s (%s)\n", t.FileName, t.ID)
	return nil
}

// AcceptFile accepts an incoming file transfer
func (n *Node) AcceptFile(fileID string) error {
	// Find the peer who offered this file
	n.mu.Lock()
	var target *peerConn
	for _, pc := range n.conns {
		if pc.sessionKey != nil {
			target = pc
			break
		}
	}
	n.mu.Unlock()

	if target == nil {
		return fmt.Errorf("no peer connected")
	}

	msg := Message{
		Type:   MsgFileAccept,
		From:   n.Name,
		JID:    n.Identity.JID,
		LID:    n.Identity.LID,
		FileID: fileID,
	}

	n.sendRaw(target, msg)
	return nil
}

// RejectFile rejects an incoming file transfer
func (n *Node) RejectFile(fileID string) error {
	n.mu.Lock()
	var target *peerConn
	for _, pc := range n.conns {
		if pc.sessionKey != nil {
			target = pc
			break
		}
	}
	n.mu.Unlock()

	if target == nil {
		return fmt.Errorf("no peer connected")
	}

	msg := Message{
		Type:   MsgFileReject,
		From:   n.Name,
		JID:    n.Identity.JID,
		LID:    n.Identity.LID,
		FileID: fileID,
	}

	n.sendRaw(target, msg)
	return nil
}

func (n *Node) startFileSend(pc *peerConn, fileID string) {
	defer func() {
		_ = recover()
	}()
	t, ok := n.FileTransfer.GetTransfer(fileID)
	if !ok {
		return
	}

	fmt.Printf("\n[→] sending file: %s\n> ", t.FileName)

	for {
		chunk, index, err := t.ReadChunk()
		if err != nil {
			break
		}

		msg := Message{
			Type:       MsgFileChunk,
			From:       n.Name,
			JID:        n.Identity.JID,
			LID:        n.Identity.LID,
			FileID:     fileID,
			ChunkIndex: index,
			ChunkTotal: t.ChunkTotal,
			ChunkData:  filetransfer.EncodeChunk(chunk),
		}

		n.sendRaw(pc, msg)

		// Show progress
		progress := t.ProgressPercent()
		if progress%10 == 0 {
			fmt.Printf("\r[→] sending: %d%%", progress)
		}

		time.Sleep(10 * time.Millisecond)
	}

	fmt.Printf("\n[✓] file sent: %s\n> ", t.FileName)
}

// resendChunks re-sends specific chunks requested by the receiver (resume).
// It uses random-access reads so only missing indexes hit the disk.
func (n *Node) resendChunks(pc *peerConn, fileID string, missing []int) {
	defer func() {
		_ = recover()
	}()
	t, ok := n.FileTransfer.GetTransfer(fileID)
	if !ok {
		return
	}
	seen := make(map[int]bool, len(missing))
	sent := 0
	for _, idx := range missing {
		if seen[idx] {
			continue
		}
		seen[idx] = true
		chunk, err := t.ReadChunkAt(idx)
		if err != nil {
			continue
		}
		n.sendRaw(pc, Message{
			Type:       MsgFileChunk,
			From:       n.Name,
			JID:        n.Identity.JID,
			LID:        n.Identity.LID,
			DeviceID:   n.DeviceID,
			FileID:     fileID,
			ChunkIndex: idx,
			ChunkTotal: t.ChunkTotal,
			ChunkData:  filetransfer.EncodeChunk(chunk),
		})
		sent++
		time.Sleep(5 * time.Millisecond)
	}
	fmt.Printf("\n[resume → %s] replayed %d/%d missing chunks for %s\n> ", pc.name, sent, len(seen), t.FileName)
}

// RequestResume asks the sender to re-transmit missing chunks.
func (n *Node) RequestResume(fileID string) error {
	t, ok := n.FileTransfer.GetTransfer(fileID)
	if !ok {
		if restored, found := n.FileTransfer.RestorePartial(fileID); found {
			t = restored
		} else {
			return fmt.Errorf("unknown transfer %s", fileID)
		}
	}
	missing := t.MissingChunks()
	if len(missing) == 0 {
		return fmt.Errorf("transfer already complete, nothing to resume")
	}
	n.mu.Lock()
	var target *peerConn
	for _, pc := range n.conns {
		if pc.sessionKey != nil {
			target = pc
			break
		}
	}
	n.mu.Unlock()
	if target == nil {
		return fmt.Errorf("no peer connected")
	}
	n.sendRaw(target, Message{
		Type:    MsgChunkRequest,
		From:    n.Name,
		JID:     n.Identity.JID,
		LID:     n.Identity.LID,
		FileID:  fileID,
		Missing: missing,
	})
	fmt.Printf("resume requested: %d missing chunks for %s\n", len(missing), t.FileName)
	return nil
}

// StartTypingCleaner starts a goroutine that cleans up stale typing indicators
func (n *Node) StartTypingCleaner() {
	go func() {
		defer func() {
			_ = recover()
		}()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			if n.Stopped() {
				return
			}
			n.typingMu.Lock()
			now := time.Now()
			for jid, lastTyping := range n.typingState {
				if now.Sub(lastTyping) > 3*time.Second {
					delete(n.typingState, jid)
				}
			}
			n.typingMu.Unlock()
		}
	}()
}
