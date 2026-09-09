package compress

import (
	"bytes"
	"strings"
	"testing"
)

func TestSmallBypasses(t *testing.T) {
	payload, compressed, err := Encode([]byte("hi"))
	if err != nil || compressed {
		t.Fatal("small messages must skip compression")
	}
	out, err := Decode(payload, false)
	if err != nil || !bytes.Equal(out, []byte("hi")) {
		t.Fatal("plain round trip failed")
	}
}

func TestLargeRoundTrip(t *testing.T) {
	big := []byte(strings.Repeat("korik chat message. ", 200))
	payload, compressed, err := Encode(big)
	if err != nil || !compressed {
		t.Fatal("bulk messages must compress")
	}
	out, err := Decode(payload, true)
	if err != nil || !bytes.Equal(out, big) {
		t.Fatal("compressed round trip failed")
	}
}

func TestZipBombRejected(t *testing.T) {
	if _, err := Decode([]byte("not-gzip"), true); err == nil {
		t.Fatal("corrupt gzip must error, not panic")
	}
}
