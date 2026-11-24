package ticks

import (
	"math"
	"testing"
	"time"
)

func TestTicksConversions(t *testing.T) {
	if got := FromDuration(time.Second); got != TicksPerSecond {
		t.Fatalf("FromDuration(1s) = %d, want %d", got, TicksPerSecond)
	}
	if got := FromDuration(0); got != 0 {
		t.Fatalf("FromDuration(0) = %d, want 0", got)
	}
	if got := FromDuration(500 * time.Millisecond); got != 5_000_000 {
		t.Fatalf("FromDuration(500ms) = %d, want 5000000", got)
	}

	if got := FromSeconds(2.5); got != 25_000_000 {
		t.Fatalf("FromSeconds(2.5) = %d, want 25000000", got)
	}
	if got := FromSeconds(0); got != 0 {
		t.Fatalf("FromSeconds(0) = %d, want 0", got)
	}

	if got := ToDuration(TicksPerSecond); got != time.Second {
		t.Fatalf("ToDuration(1s) = %v, want 1s", got)
	}
	if got := ToDuration(0); got != 0 {
		t.Fatalf("ToDuration(0) = %v, want 0", got)
	}

	if got := ToSeconds(25_000_000); math.Abs(got-2.5) > 1e-9 {
		t.Fatalf("ToSeconds(25_000_000) = %v, want 2.5", got)
	}
	if got := ToSeconds(0); got != 0 {
		t.Fatalf("ToSeconds(0) = %v, want 0", got)
	}
}
