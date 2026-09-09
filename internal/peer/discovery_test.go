package peer

import (
	"testing"
)

func TestParseDiscoveryNewFormatV4(t *testing.T) {
	d, ok := ParseDiscoveryPayload("korik|korik:abc|tcp|192.168.1.5|9999|cli-1", "10.0.0.9")
	if !ok {
		t.Fatal("new v4 payload must parse")
	}
	if d.Transport != "tcp" || d.IP != "192.168.1.5" || d.Port != "9999" || d.DeviceID != "cli-1" {
		t.Fatalf("unexpected parse: %+v", d)
	}
	if got := d.DialAddress("10.0.0.9"); got != "192.168.1.5:9999" {
		t.Fatalf("unexpected dial address %q", got)
	}
}

func TestParseDiscoveryNewFormatV6(t *testing.T) {
	d, ok := ParseDiscoveryPayload("korik|korik:abc|tcp|fe80::1|9999|cli-2", "fe80::9")
	if !ok {
		t.Fatal("new v6 payload must parse")
	}
	if got := d.DialAddress("fe80::9"); got != "[fe80::1]:9999" {
		t.Fatalf("IPv6 must be bracketed, got %q", got)
	}
}

func TestParseDiscoveryLegacyCompat(t *testing.T) {
	d, ok := ParseDiscoveryPayload("korik|korik:abc|9999", "10.0.0.9")
	if !ok || d.Transport != "tcp" {
		t.Fatalf("legacy 3-field must parse as tcp: %+v", d)
	}
	if got := d.DialAddress("10.0.0.9"); got != "10.0.0.9:9999" {
		t.Fatalf("legacy must fall back to remote IP, got %q", got)
	}
	d, ok = ParseDiscoveryPayload("korik|korik:abc|9999|cli-9", "10.0.0.9")
	if !ok || d.DeviceID != "cli-9" {
		t.Fatalf("legacy 4-field must keep device: %+v", d)
	}
}

func TestParseDiscoveryRejectsBad(t *testing.T) {
	for _, bad := range []string{
		"",
		"korik|onlyjid",
		"other|korik:abc|tcp|1.2.3.4|9999|d",
		"korik|korik:abc|ftp|1.2.3.4|9999|d",
		"korik|korik:abc|tcp|not-an-ip|9999|d",
		"korik|korik:abc|tcp|1.2.3.4|99999|d",
	} {
		if _, ok := ParseDiscoveryPayload(bad, "10.0.0.1"); ok {
			t.Fatalf("payload must be rejected: %q", bad)
		}
	}
}

func TestBuildDiscoveryRoundTrip(t *testing.T) {
	payload := BuildDiscoveryPayload("korik:abc", "udp", "203.0.113.5", 9999, "cli-7")
	d, ok := ParseDiscoveryPayload(payload, "10.0.0.1")
	if !ok {
		t.Fatalf("built payload must parse: %q", payload)
	}
	if d.Transport != "udp" || d.IP != "203.0.113.5" || d.DeviceID != "cli-7" {
		t.Fatalf("round trip mismatch: %+v", d)
	}
}
