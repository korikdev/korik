package compress

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

// Tuning: only bulk plaintext is compressed, and decompression is
// strictly bounded. This keeps chat lightweight while avoiding
// zip-bomb resource exhaustion on untrusted input.
//
// CRIME-style note: compression happens before E2E encryption on
// user-authored bulk content. An observer who can both inject chosen
// plaintext into your messages and measure ciphertext length could in
// theory probe content. Mitigations applied here: compress only
// messages above Threshold (no tiny probes), never compress mixed
// secret+attacker blobs (there is no such API — callers pass the whole
// user message), and cap decompressed output at MaxDecompressed.
const (
	Threshold       = 1024
	MaxDecompressed = 1 << 20 // 1 MiB
)

// Encode compresses plain when it exceeds Threshold.
// It returns the payload plus whether compression was applied.
func Encode(plain []byte) (payload []byte, compressed bool, err error) {
	if len(plain) < Threshold {
		return plain, false, nil
	}
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(plain); err != nil {
		return nil, false, err
	}
	if err := writer.Close(); err != nil {
		return nil, false, err
	}
	// Avoid pathological expansion on incompressible input.
	if buf.Len() >= len(plain) {
		return plain, false, nil
	}
	return buf.Bytes(), true, nil
}

// Decode reverses Encode when compressed is set, bounding output.
func Decode(payload []byte, compressed bool) ([]byte, error) {
	if !compressed {
		if len(payload) > MaxDecompressed {
			return nil, fmt.Errorf("plaintext exceeds %d bytes", MaxDecompressed)
		}
		return payload, nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	limited := io.LimitReader(reader, MaxDecompressed+1)
	out, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(out) > MaxDecompressed {
		return nil, fmt.Errorf("decompressed payload exceeds %d bytes", MaxDecompressed)
	}
	return out, nil
}
