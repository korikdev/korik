package shell

import (
	"testing"
)

func TestFuzzyFilterFindsJohn(t *testing.T) {
	candidates := []string{"John", "Alice", "Marijo", "Bob"}
	got := FuzzyFilter("jo", candidates)
	if len(got) == 0 {
		t.Fatal("expected matches for 'jo'")
	}
	found := false
	for _, v := range got {
		if v == "John" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected John in results, got %v", got)
	}
}

func TestCompleteCommands(t *testing.T) {
	s := New("/tmp/korik-shell-test-history", []string{"/peers", "/connect", "/quit"})
	got := s.Complete("/pe")
	if len(got) != 1 || got[0] != "/peers" {
		t.Fatalf("expected [/peers], got %v", got)
	}
}
