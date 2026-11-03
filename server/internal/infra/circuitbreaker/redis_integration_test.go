//go:build integration

package circuitbreaker

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestSharedCircuitRecoveryAndStaleOutcomes(t *testing.T) {
	container, err := testcontainers.GenericContainer(t.Context(), testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{Image: "redis:8.2-alpine", ExposedPorts: []string{"6379/tcp"}, WaitingFor: wait.ForListeningPort("6379/tcp")}, Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Error(err)
		}
	})
	address, err := container.Endpoint(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { _ = client.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	first, second := New(client, logger), New(client, logger)
	policy := Policy{Threshold: 2, Window: time.Minute, Cooldown: 40 * time.Millisecond, MaxCooldown: time.Second, ProbeLease: time.Second}
	acquire := func(breaker *Breaker, scope string) Permit {
		t.Helper()
		permit, err := breaker.Acquire(t.Context(), scope, policy)
		if err != nil {
			t.Fatal(err)
		}
		return permit
	}
	finish := func(breaker *Breaker, permit Permit, outcome Outcome) {
		t.Helper()
		if err := breaker.Finish(t.Context(), permit, outcome); err != nil {
			t.Fatal(err)
		}
	}
	stale := acquire(second, "publisher-a")
	finish(first, acquire(first, "publisher-a"), Outcome{Failed: true, Reason: "provider_unavailable"})
	finish(second, acquire(second, "publisher-a"), Outcome{Failed: true, Reason: "provider_unavailable"})
	finish(second, stale, Outcome{})
	if _, err := first.Acquire(t.Context(), "publisher-a", policy); err == nil {
		t.Fatal("late success closed an open circuit")
	}
	_ = acquire(first, "publisher-b")
	time.Sleep(60 * time.Millisecond)
	var group sync.WaitGroup
	winners := make(chan Permit, 24)
	for i := range 24 {
		group.Go(func() {
			breaker := first
			if i%2 == 1 {
				breaker = second
			}
			permit, err := breaker.Acquire(t.Context(), "publisher-a", policy)
			if err == nil {
				winners <- permit
			} else if _, ok := Paused(err); !ok {
				t.Error(err)
			}
		})
	}
	group.Wait()
	close(winners)
	var probe Permit
	count := 0
	for permit := range winners {
		probe = permit
		count++
	}
	if count != 1 || !probe.Probe {
		t.Fatalf("probes=%d permit=%+v", count, probe)
	}
	// A cancelled caller releases the probe without marking the provider unhealthy.
	finish(first, probe, Outcome{Neutral: true})
	crashed := acquire(second, "publisher-a")
	finish(first, probe, Outcome{})
	if _, err := first.Acquire(t.Context(), "publisher-a", policy); err == nil {
		t.Fatal("stale probe released another caller's lease")
	}
	time.Sleep(policy.ProbeLease + 20*time.Millisecond)
	replacement := acquire(first, "publisher-a")
	finish(second, crashed, Outcome{Failed: true, Reason: "late_failure"})
	if _, err := second.Acquire(t.Context(), "publisher-a", policy); err == nil {
		t.Fatal("expired probe outcome released the new probe")
	}
	finish(first, replacement, Outcome{})
	if fresh := acquire(second, "publisher-a"); fresh.Probe {
		t.Fatal("successful recovery did not close circuit")
	}
	finish(first, acquire(first, "quota"), Outcome{Failed: true, Immediate: true, Pause: 2 * time.Minute, Reason: "provider_daily_quota"})
	_, err = second.Acquire(t.Context(), "quota", policy)
	pause, ok := Paused(err)
	if !ok || pause.After < 119*time.Second || pause.Reason != "provider_daily_quota" {
		t.Fatalf("quota pause=%v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = first.Acquire(t.Context(), "unavailable-store", policy)
	if pause, ok := Paused(err); !ok || pause.Reason != "circuit_store_unavailable" {
		t.Fatalf("store failure did not defer: %v", err)
	}
}
