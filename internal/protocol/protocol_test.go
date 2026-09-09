package protocol

import (
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	v, err := ParseVersion("1.2.3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Major != 1 || v.Minor != 2 || v.Patch != 3 {
		t.Fatalf("expected 1.2.3, got %d.%d.%d", v.Major, v.Minor, v.Patch)
	}

	_, err = ParseVersion("invalid")
	if err == nil {
		t.Fatal("expected error for invalid version")
	}

	_, err = ParseVersion("1.2")
	if err == nil {
		t.Fatal("expected error for short version")
	}
}

func TestCompare(t *testing.T) {
	a, _ := ParseVersion("1.0.0")
	b, _ := ParseVersion("1.0.0")
	if Compare(a, b) != 0 {
		t.Fatal("expected 0 for equal versions")
	}

	c, _ := ParseVersion("1.1.0")
	if Compare(a, c) != -1 {
		t.Fatal("expected -1 for a < c")
	}
	if Compare(c, a) != 1 {
		t.Fatal("expected 1 for c > a")
	}

	d, _ := ParseVersion("2.0.0")
	if Compare(a, d) != -1 {
		t.Fatal("expected -1 for a < d (major)")
	}
}

func TestIsCompatible(t *testing.T) {
	v, _ := ParseVersion("1.0.0")
	if !IsCompatible(v) {
		t.Fatal("expected 1.0.0 to be compatible")
	}

	v2, _ := ParseVersion("0.9.0")
	if IsCompatible(v2) {
		t.Fatal("expected 0.9.0 to be incompatible")
	}

	v3, _ := ParseVersion("1.2.0")
	if !IsCompatible(v3) {
		t.Fatal("expected 1.2.0 to be compatible (same major)")
	}

	v4, _ := ParseVersion("2.0.0")
	if IsCompatible(v4) {
		t.Fatal("expected 2.0.0 to be incompatible (different major)")
	}
}

func TestNegotiateVersion(t *testing.T) {
	peer, _ := ParseVersion("1.0.0")
	neg, mode := NegotiateVersion(peer)
	if neg == nil || formatVersion(neg) != "1.0.0" || mode != "normal" {
		t.Fatalf("expected 1.0.0 normal, got %v %s", neg, mode)
	}

	peer2, _ := ParseVersion("1.1.0")
	neg2, mode2 := NegotiateVersion(peer2)
	if neg2 == nil || mode2 != "compat" {
		t.Fatalf("expected compat mode for 1.1.0, got %v %s", neg2, mode2)
	}

	peer3, _ := ParseVersion("1.0.1")
	neg3, mode3 := NegotiateVersion(peer3)
	if neg3 == nil || mode3 != "normal" {
		t.Fatalf("expected normal mode for 1.0.1, got %v %s", neg3, mode3)
	}
}

func TestIsMajorMatch(t *testing.T) {
	a, _ := ParseVersion("1.2.3")
	b, _ := ParseVersion("1.5.0")
	if !IsMajorMatch(a, b) {
		t.Fatal("expected major match for 1.x")
	}

	c, _ := ParseVersion("2.0.0")
	if IsMajorMatch(a, c) {
		t.Fatal("expected major mismatch between 1.x and 2.x")
	}
}

func TestHandshakeVersion(t *testing.T) {
	if HandshakeVersion() != string(CurrentVersion) {
		t.Fatalf("expected %s, got %s", CurrentVersion, HandshakeVersion())
	}
}

func TestVersionMismatchError(t *testing.T) {
	peer, _ := ParseVersion("0.9.0")
	err := VersionMismatchError(peer)
	if err == nil {
		t.Fatal("expected error")
	}
	expected := "peer version 0.9.0, minimum required version 1.0.0"
	if !contains(err.Error(), expected) {
		t.Fatalf("expected error to contain %q, got %q", expected, err.Error())
	}
}

func TestCompatibilityMatrix(t *testing.T) {
	if !CompatibilityMatrix["1.0.0"] {
		t.Fatal("expected 1.0.0 in compatibility matrix")
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
