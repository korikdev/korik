package nat

import (
	"net"
	"testing"
)

func TestPriorityOrdering(t *testing.T) {
	v4 := net.ParseIP("192.168.1.5")
	v6 := net.ParseIP("fe80::1")
	pub := net.ParseIP("203.0.113.5")
	pHostV4 := PriorityFor(CandidateHost, v4, "tcp")
	pHostV6 := PriorityFor(CandidateHost, v6, "tcp")
	pSrflx := PriorityFor(CandidateSrflx, pub, "udp")
	if !(pHostV4 > pHostV6 && pHostV6 > pSrflx) {
		t.Fatalf("expected host-v4 > host-v6 > srflx, got %d %d %d", pHostV4, pHostV6, pSrflx)
	}
}

func TestStepAssignment(t *testing.T) {
	hostV4 := Candidate{Type: CandidateHost, IP: "192.168.1.5", Transport: "tcp"}
	hostV6 := Candidate{Type: CandidateHost, IP: "fe80::1", Transport: "tcp"}
	srflx := Candidate{Type: CandidateSrflx, IP: "203.0.113.5", Transport: "udp"}
	if StepFor(hostV4) != StepDirectLAN {
		t.Fatal("host v4 must map to direct-lan")
	}
	if StepFor(hostV6) != StepIPv6 {
		t.Fatal("host v6 must map to ipv6-direct")
	}
	if StepFor(srflx) != StepPublicUDP {
		t.Fatal("srflx must map to public-udp")
	}
}

func TestBundleRoundTrip(t *testing.T) {
	cands := []Candidate{
		{ID: "host-a", Type: CandidateHost, IP: "192.168.1.5", Port: 9999, Transport: "tcp", Priority: 1010},
		{ID: "srflx-v4-udp", Type: CandidateSrflx, IP: "203.0.113.5", Port: 41001, Transport: "udp", Priority: 700},
	}
	encoded := EncodeBundle("korik:abc", "cli-1", cands)
	if encoded == "" {
		t.Fatal("bundle must encode")
	}
	bundle, err := DecodeBundle(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.JID != "korik:abc" || len(bundle.Candidates) != 2 {
		t.Fatalf("bundle mismatch: %+v", bundle)
	}
}

func TestDecodeInvalidBundle(t *testing.T) {
	if _, err := DecodeBundle("not-base64!!!"); err == nil {
		t.Fatal("expected error for invalid bundle")
	}
}

func TestDeduplicate(t *testing.T) {
	cands := []Candidate{
		{ID: "a", Type: CandidateHost, IP: "192.168.1.5", Port: 9999, Transport: "tcp", Priority: 100},
		{ID: "b", Type: CandidateHost, IP: "192.168.1.5", Port: 9999, Transport: "tcp", Priority: 90},
		{ID: "c", Type: CandidateHost, IP: "192.168.1.6", Port: 9999, Transport: "tcp", Priority: 80},
	}
	got := Deduplicate(cands)
	if len(got) != 2 {
		t.Fatalf("expected 2 after dedup, got %d", len(got))
	}
}
