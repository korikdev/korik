package peer

import (
	"testing"
)

// FuzzDecodeMessage ensures malformed peer input can never panic the reader.
// Valid JSON yields a Message (err may be nil); garbage must error, never panic.
func FuzzDecodeMessage(f *testing.F) {
	seeds := []string{
		`{"type":"chat","body":"hi"}`,
		"{\"type\":\"hello\",\"jid\":\"korik:abc\",\"pubkey\":\"zz\"}\n",
		"",
		"\n",
		"not json at all",
		"{\"type\":",
		"{\"type\":\"chat\",\"missing\":[1,2,}",
		string([]byte{0xff, 0xfe, 0x00, 0x01}),
		"{\"type\":\"file_chunk\",\"chunk_data\":\"!!!\"}",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		msg, err := decodeMessage(data)
		if err != nil {
			return
		}
		_ = msg.Type
		_ = msg.Body
		_ = msg.Missing
	})
}

// FuzzParseDiscoveryPayload ensures hostile broadcast packets never panic
// and only well-formed announcements are accepted.
func FuzzParseDiscoveryPayload(f *testing.F) {
	seeds := []string{
		"korik|korik:abc|tcp|192.168.1.5|9999|cli-1",
		"korik|korik:abc|tcp|fe80::1|9999|cli-2",
		"korik|korik:abc|9999",
		"korik|korik:abc|9999|cli-9",
		"",
		"korik",
		"a|b|c|d|e|f|g|h|i|j",
		"korik|\xff|tcp|1.2.3.4|9999|d",
		"korik|korik:abc|tcp||9999|",
		"korik|korik:abc|tcp|1.2.3.4|0|d",
		"korik|korik:abc|tcp|1.2.3.4|70000|d",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, payload string) {
		d, ok := ParseDiscoveryPayload(payload, "10.0.0.1")
		if !ok {
			return
		}
		if d.JID == "" || d.Port == "" {
			t.Fatalf("accepted payload missing required fields: %q", payload)
		}
		_ = d.DialAddress("10.0.0.1")
	})
}
