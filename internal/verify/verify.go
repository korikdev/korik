package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// SafetyNumber derives a human-verifiable fingerprint from a peer public key.
// Format mirrors Signal: groups of 5 digits, e.g. "12345 67890 ...".
// Both sides compute the same number from the same key material.
func SafetyNumber(publicKeyHex string) (string, error) {
	raw, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		return "", fmt.Errorf("invalid public key: must be hex")
	}
	sum := sha256.Sum256(raw)
	return FormatFingerprint(sum[:]), nil
}

// FormatFingerprint converts 32 bytes into 12 groups of 5 digits.
func FormatFingerprint(hash []byte) string {
	groups := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		var v uint32
		for j := 0; j < 3; j++ {
			v = (v << 8) | uint32(hash[(i*3+j)%len(hash)])
		}
		groups = append(groups, fmt.Sprintf("%05d", v%100000))
	}
	return strings.Join(groups, " ")
}

// Compare normalizes two safety numbers for equality checks.
func Compare(a, b string) bool {
	norm := func(s string) string {
		return strings.Join(strings.Fields(s), " ")
	}
	return norm(a) == norm(b)
}

// ShortCode returns a compact 6-digit code for quick phone-call verification.
func ShortCode(publicKeyHex string) string {
	raw, err := hex.DecodeString(publicKeyHex)
	if err != nil || len(raw) == 0 {
		return "000000"
	}
	sum := sha256.Sum256(raw)
	v := int(sum[0])<<16 | int(sum[1])<<8 | int(sum[2])
	return fmt.Sprintf("%06d", v%1000000)
}

// ParseGroups validates user-typed safety number input.
func ParseGroups(input string) ([]int, error) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty safety number")
	}
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 || n > 99999 {
			return nil, fmt.Errorf("invalid group %q: must be 00000-99999", f)
		}
		out = append(out, n)
	}
	return out, nil
}
