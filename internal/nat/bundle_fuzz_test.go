package nat

import (
	"testing"
)

// FuzzDecodeBundle ensures hostile shared bundles never panic the client.
func FuzzDecodeBundle(f *testing.F) {
	seeds := []string{
		"",
		"not-base64!!!",
		"e30=",
		"bnVsbA==",
		"eyJ2Ijo5OTksImNhbmRzIjpbXX0=",
		"eyJ2IjoxLCJqaWQiOiJ4IiwiY2FuZHMiOltdfQ==",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, encoded string) {
		bundle, err := DecodeBundle(encoded)
		if err != nil {
			return
		}
		if len(bundle.Candidates) == 0 {
			t.Fatal("accepted bundle without candidates")
		}
		_ = Fingerprint(bundle)
	})
}
