package contacts

import (
	"testing"
)

func TestFuzzySearchFindsJohn(t *testing.T) {
	cb, err := NewContactBook(t.TempDir() + "/contacts.json")
	if err != nil {
		t.Fatal(err)
	}
	cb.Add("korik:johnkey1234567890", "John")
	cb.Add("korik:alicekey12345678", "Alice")

	results := cb.Search("jo")
	if len(results) == 0 {
		t.Fatal("expected 'jo' to match John")
	}
	if results[0].Name != "John" {
		t.Fatalf("expected John first, got %s", results[0].Name)
	}
}

func TestFuzzySearchSubsequence(t *testing.T) {
	cb, err := NewContactBook(t.TempDir() + "/contacts.json")
	if err != nil {
		t.Fatal(err)
	}
	cb.Add("korik:marijo1234567890ab", "Marijo")

	if len(cb.Search("mjo")) == 0 {
		t.Fatal("expected subsequence 'mjo' to match Marijo")
	}
}
