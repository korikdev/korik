package circuit

import (
	"testing"
	"time"
)

func TestOpensAfterThreshold(t *testing.T) {
	b := New(3, time.Minute)
	for i := 0; i < 2; i++ {
		if err := b.RecordFailure("10.0.0.1:9999"); err != nil {
			t.Fatal("must stay closed below threshold")
		}
	}
	if err := b.RecordFailure("10.0.0.1:9999"); err == nil {
		t.Fatal("must open at threshold")
	}
	if !b.Skip("10.0.0.1:9999") {
		t.Fatal("open circuit must skip")
	}
	b.RecordSuccess("10.0.0.1:9999")
	if b.Skip("10.0.0.1:9999") {
		t.Fatal("success must close the circuit")
	}
}

func TestCooldownExpiry(t *testing.T) {
	b := New(1, 20*time.Millisecond)
	_ = b.RecordFailure("peer")
	if !b.Skip("peer") {
		t.Fatal("must skip while cooling down")
	}
	time.Sleep(40 * time.Millisecond)
	if b.Skip("peer") {
		t.Fatal("must allow after cooldown")
	}
}
