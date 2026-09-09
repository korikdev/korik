package status

import (
	"errors"
	"testing"
	"time"
)

func TestHumanizeConnectionRefused(t *testing.T) {
	msg := HumanizeError(errors.New("dial tcp: connection refused"))
	if msg == "" || len(msg) < 10 {
		t.Fatal("expected helpful message")
	}
}

func TestHumanizeNil(t *testing.T) {
	if HumanizeError(nil) != "" {
		t.Fatal("nil error must yield empty string")
	}
}

func TestRetryPlanBackoff(t *testing.T) {
	if RetryPlan(1) != time.Second {
		t.Fatalf("attempt 1 should be 1s, got %v", RetryPlan(1))
	}
	if RetryPlan(2) != 2*time.Second {
		t.Fatalf("attempt 2 should be 2s, got %v", RetryPlan(2))
	}
	if RetryPlan(100) != 30*time.Second {
		t.Fatalf("backoff must cap at 30s, got %v", RetryPlan(100))
	}
}

func TestTrackerSetAndSnapshot(t *testing.T) {
	tr := NewTracker()
	tr.Set(PhaseConnecting, "dialing peer")
	phase, detail, _ := tr.Snapshot()
	if phase != PhaseConnecting || detail != "dialing peer" {
		t.Fatalf("unexpected snapshot %v %q", phase, detail)
	}
}
