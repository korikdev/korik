package peer

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"korik/internal/group"
	"korik/internal/identity"
)

// freePort reserves an ephemeral loopback port for test nodes.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

type chatCapture struct {
	mu     sync.Mutex
	bodies []string
}

func (c *chatCapture) add(body string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bodies = append(c.bodies, body)
}

func (c *chatCapture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bodies)
}

// waitReady polls until target sees jid with a ready session.
func waitReady(t *testing.T, n *Node, jid string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		for _, p := range n.Peers() {
			if p.JID == jid && p.Ready {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("handshake timeout waiting for %s", jid)
}

// setupPair starts two nodes connected over loopback (discovery off).
func setupPair(t *testing.T) (nodeA, nodeB *Node) {
	t.Helper()
	dirA, dirB := t.TempDir(), t.TempDir()
	idA, err := identity.LoadOrCreate(dirA + "/identity.json")
	if err != nil {
		t.Fatal(err)
	}
	idB, err := identity.LoadOrCreate(dirB + "/identity.json")
	if err != nil {
		t.Fatal(err)
	}
	portA, portB := freePort(t), freePort(t)
	nodeA = NewNode("alice", portA, freePort(t), idA)
	nodeA.DeviceID = "cli-a"
	nodeB = NewNode("bob", portB, freePort(t), idB)
	nodeB.DeviceID = "cli-b"
	groupsA, err := group.New(dirA + "/groups.json")
	if err != nil {
		t.Fatal(err)
	}
	nodeA.Groups = groupsA
	groupsB, err := group.New(dirB + "/groups.json")
	if err != nil {
		t.Fatal(err)
	}
	nodeB.Groups = groupsB
	if err := nodeA.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nodeA.Stop)
	if err := nodeB.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nodeB.Stop)
	if err := nodeA.ConnectTo(fmt.Sprintf("127.0.0.1:%d", portB)); err != nil {
		t.Fatal(err)
	}
	waitReady(t, nodeA, idB.JID)
	waitReady(t, nodeB, idA.JID)
	return nodeA, nodeB
}

func TestEndToEndChat(t *testing.T) {
	nodeA, nodeB := setupPair(t)
	got := &chatCapture{}
	nodeB.OnChat = func(lid, jid, name, body string, ts time.Time) { got.add(body) }
	if err := nodeA.SendTo(nodeB.Identity.JID, "hello e2e"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && got.count() == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if got.count() != 1 {
		t.Fatalf("expected 1 delivery, got %d", got.count())
	}
}

func TestEndToEndDedupReplay(t *testing.T) {
	nodeA, nodeB := setupPair(t)
	got := &chatCapture{}
	nodeB.OnChat = func(lid, jid, name, body string, ts time.Time) { got.add(body) }
	envID := "e2e-test-fixed-id"
	if err := nodeA.SendToWithID(nodeB.Identity.JID, "once", envID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && got.count() == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	// Replay the same envelope (reconnect/retry simulation).
	if err := nodeA.SendToWithID(nodeB.Identity.JID, "once", envID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if got.count() != 1 {
		t.Fatalf("replay must collapse to 1 delivery, got %d", got.count())
	}
}

func TestEndToEndGroupChat(t *testing.T) {
	nodeA, nodeB := setupPair(t)
	room, err := nodeA.CreateRoom("team")
	if err != nil {
		t.Fatal(err)
	}
	if err := nodeA.InviteToRoom(room.ID, nodeB.Identity.JID); err != nil {
		t.Fatal(err)
	}
	// Wait for B to store the invite.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := nodeB.Groups.Get(room.ID); ok {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, ok := nodeB.Groups.Get(room.ID); !ok {
		t.Fatal("bob never received the group invite")
	}
	got := &chatCapture{}
	nodeB.OnGroupChat = func(groupID, lid, jid, name, body, replyTo string, ts time.Time) {
		got.add(groupID + ":" + body)
	}
	if _, err := nodeA.SendGroup(room.ID, "hello room", ""); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && got.count() == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if got.count() != 1 {
		t.Fatalf("expected 1 group delivery, got %d", got.count())
	}
}
