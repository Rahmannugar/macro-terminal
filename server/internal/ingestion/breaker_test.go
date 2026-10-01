package ingestion

import (
	"testing"
	"time"
)

func TestBreakerAllowsBelowThreshold(t *testing.T) {
	breaker := NewBreaker(3, time.Minute)
	breaker.RecordFailure("source-a")
	breaker.RecordFailure("source-a")
	if !breaker.Allow("source-a") {
		t.Fatalf("Allow below threshold = false, want true")
	}
	if !breaker.Allow("source-b") {
		t.Fatalf("unrelated key must stay closed")
	}
}

func TestBreakerOpensAfterThreshold(t *testing.T) {
	breaker := NewBreaker(3, time.Minute)
	for range 3 {
		breaker.RecordFailure("source-a")
	}
	if breaker.Allow("source-a") {
		t.Fatalf("Allow after threshold = true, want false")
	}
}

func TestBreakerHalfOpenProbeAfterCooldown(t *testing.T) {
	breaker := NewBreaker(1, time.Minute)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	breaker.now = func() time.Time { return now }

	breaker.RecordFailure("source-a")
	if breaker.Allow("source-a") {
		t.Fatalf("Allow during cooldown = true, want false")
	}

	now = now.Add(time.Minute)
	if !breaker.Allow("source-a") {
		t.Fatalf("Allow after cooldown = false, want single probe")
	}
	if breaker.Allow("source-a") {
		t.Fatalf("second Allow during probe = true, want false (one probe at a time)")
	}

	breaker.RecordSuccess("source-a")
	if !breaker.Allow("source-a") {
		t.Fatalf("success must close the circuit")
	}
}

func TestBreakerFailedProbeReopens(t *testing.T) {
	breaker := NewBreaker(1, time.Minute)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	breaker.now = func() time.Time { return now }

	breaker.RecordFailure("source-a")
	now = now.Add(time.Minute)
	if !breaker.Allow("source-a") {
		t.Fatalf("probe should be allowed after cooldown")
	}
	breaker.RecordFailure("source-a")
	if breaker.Allow("source-a") {
		t.Fatalf("failed probe must reopen the circuit immediately")
	}

	now = now.Add(time.Minute)
	if breaker.Allow("source-a") {
		t.Fatalf("second cooldown must be longer than the first")
	}
	now = now.Add(time.Minute)
	if !breaker.Allow("source-a") {
		t.Fatalf("probe must be allowed once the doubled cooldown elapses")
	}
}

func TestBreakerCooldownEscalatesAndCaps(t *testing.T) {
	breaker := NewBreaker(1, time.Minute)

	for attempt, want := range []time.Duration{
		time.Minute,
		2 * time.Minute,
		4 * time.Minute,
		8 * time.Minute,
		16 * time.Minute,
		breakerMaxCooldown,
		breakerMaxCooldown,
	} {
		if got := breaker.cooldownFor(attempt + 1); got != want {
			t.Fatalf("cooldownFor(%d) = %v, want %v", attempt+1, got, want)
		}
	}
}
