package verify

import (
	"strings"
	"testing"
)

func TestSafetyNumberStable(t *testing.T) {
	hexKey := "9d5c5c8e8f0a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4"
	first, err := SafetyNumber(hexKey)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SafetyNumber(hexKey)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("safety number must be deterministic")
	}
	if len(strings.Fields(first)) != 12 {
		t.Fatalf("expected 12 groups, got %q", first)
	}
}

func TestSafetyNumberInvalidHex(t *testing.T) {
	if _, err := SafetyNumber("not-hex"); err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestShortCodeFormat(t *testing.T) {
	hexKey := "9d5c5c8e8f0a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4"
	code := ShortCode(hexKey)
	if len(code) != 6 {
		t.Fatalf("expected 6-digit code, got %q", code)
	}
}
