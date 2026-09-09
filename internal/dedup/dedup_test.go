package dedup

import (
	"testing"
	"time"
)

func TestAddOnceThenDuplicate(t *testing.T) {
	s := New(10, time.Minute)
	if !s.Add("id-1") {
		t.Fatal("first add must be accepted")
	}
	if s.Add("id-1") {
		t.Fatal("replay of same ID must be rejected")
	}
	if !s.Has("id-1") {
		t.Fatal("seen ID must report Has=true")
	}
}

func TestEmptyIDAlwaysPasses(t *testing.T) {
	s := New(10, time.Minute)
	if !s.Add("") {
		t.Fatal("empty ID must not be treated as duplicate")
	}
}

func TestCapacityEviction(t *testing.T) {
	s := New(2, time.Minute)
	s.Add("a")
	s.Add("b")
	s.Add("c") // must evict oldest, never grow unbounded
	if s.Len() != 2 {
		t.Fatalf("expected cap 2, got %d", s.Len())
	}
	if !s.Has("c") {
		t.Fatal("newest ID must survive eviction")
	}
}

func TestExpiry(t *testing.T) {
	s := New(10, 20*time.Millisecond)
	s.Add("temp")
	time.Sleep(40 * time.Millisecond)
	if s.Has("temp") {
		t.Fatal("expired ID must be forgotten")
	}
	if !s.Add("temp") {
		t.Fatal("expired ID must be accepted again")
	}
}
