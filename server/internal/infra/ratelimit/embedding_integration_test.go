//go:build integration

package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestEmbeddingAllowanceIsSharedAndAtomic(t *testing.T) {
	container, err := testcontainers.GenericContainer(t.Context(), testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{Image: "redis:8.2-alpine", ExposedPorts: []string{"6379/tcp"}, WaitingFor: wait.ForListeningPort("6379/tcp")}, Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate Redis container: %v", err)
		}
	})
	address, err := container.Endpoint(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { _ = client.Close() })
	budgets := []*EmbeddingBudget{NewEmbeddingBudget(client, "test", 24000), NewEmbeddingBudget(client, "test", 24000)}
	var allowed atomic.Int32
	var group sync.WaitGroup
	for i := range 32 {
		group.Go(func() {
			delay, err := budgets[i%2].Reserve(t.Context(), 1000, 1)
			if err != nil {
				t.Error(err)
				return
			}
			if delay == 0 {
				allowed.Add(1)
			} else if delay < 50*time.Second || delay > 61*time.Second {
				t.Errorf("retry delay=%s", delay)
			}
		})
	}
	group.Wait()
	if allowed.Load() != 24 {
		t.Fatalf("allowed=%d, want 24 across both clients", allowed.Load())
	}
	requestBudget := NewEmbeddingBudget(client, "requests", 24000)
	for i := range 3 {
		delay, err := requestBudget.Reserve(t.Context(), 1, 40)
		if err != nil || (delay == 0) != (i < 2) {
			t.Fatalf("request allowance attempt=%d delay=%s error=%v", i, delay, err)
		}
	}
	expired := NewEmbeddingBudget(client, "expired", 24000)
	if err := client.ZAdd(t.Context(), expired.key, redis.Z{Score: float64(time.Now().Add(-61 * time.Second).UnixMilli()), Member: "old:24000:80"}).Err(); err != nil {
		t.Fatal(err)
	}
	if delay, err := expired.Reserve(t.Context(), 1, 1); err != nil || delay != 0 {
		t.Fatalf("expired reservations still block work: delay=%s error=%v", delay, err)
	}
}
