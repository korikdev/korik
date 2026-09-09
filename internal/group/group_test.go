package group

import (
	"testing"
)

func TestCreateInviteResolve(t *testing.T) {
	s, err := New(t.TempDir() + "/groups.json")
	if err != nil {
		t.Fatal(err)
	}
	room, err := s.Create("team", "korik:alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(room.Key) != 32 {
		t.Fatal("room key must be 32 bytes")
	}
	if !s.IsMember(room.ID, "korik:alice") {
		t.Fatal("creator must be a member")
	}
	if err := s.AddMember(room.ID, "korik:bob"); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Resolve(room.ID[:8])
	if !ok || got.ID != room.ID {
		t.Fatal("unique prefix must resolve")
	}
	if _, ok := s.Resolve("team"); !ok {
		t.Fatal("name must resolve")
	}
	if err := s.RemoveMember(room.ID, "korik:bob"); err != nil {
		t.Fatal(err)
	}
	if s.IsMember(room.ID, "korik:bob") {
		t.Fatal("bob must be removed")
	}
}

func TestRejectBadRoom(t *testing.T) {
	s, err := New(t.TempDir() + "/groups.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("", "korik:alice"); err == nil {
		t.Fatal("empty name must be rejected")
	}
	if err := s.Add(&Room{ID: "x", Key: []byte{1}}); err == nil {
		t.Fatal("short key must be rejected")
	}
}

func TestRekeyRotates(t *testing.T) {
	s, _ := New(t.TempDir() + "/groups.json")
	room, _ := s.Create("ops", "korik:alice")
	old := append([]byte(nil), room.Key...)
	rotated, err := s.Rekey(room.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range old {
		if old[i] != rotated.Key[i] {
			return // differs
		}
	}
	t.Fatal("rekey must change the key")
}
