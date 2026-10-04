package cache

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type record struct {
	Name string `json:"name"`
}

func newTestStore(t *testing.T) (*JSONStore, *miniredis.Miniredis) {
	t.Helper()
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(server.Close)
	return NewJSONStore(redis.NewClient(&redis.Options{Addr: server.Addr()})), server
}

func TestSetStoresJSONWithJitteredHourlyExpiry(t *testing.T) {
	store, server := newTestStore(t)
	key := ArticleKey(uuid.New())

	if err := store.Set(t.Context(), key, record{Name: "one"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	raw, err := server.Get(key)
	if err != nil {
		t.Fatalf("read cached value: %v", err)
	}
	if raw != `{"name":"one"}` {
		t.Fatalf("value = %q, want the JSON encoding", raw)
	}
	if ttl := server.TTL(key); ttl > time.Hour || ttl < 54*time.Minute {
		t.Fatalf("ttl = %s, want between 54m and 1h", ttl)
	}
}

func TestGetManyReportsHitsAndMisses(t *testing.T) {
	store, _ := newTestStore(t)
	hit := ArticleKey(uuid.New())
	miss := ArticleKey(uuid.New())
	if err := store.Set(t.Context(), hit, record{Name: "hit"}); err != nil {
		t.Fatalf("set: %v", err)
	}

	found, err := store.GetMany(t.Context(), []string{hit, miss})
	if err != nil {
		t.Fatalf("get many: %v", err)
	}
	if raw, ok := found[hit]; !ok || string(raw) != `{"name":"hit"}` {
		t.Fatalf("hit = %q present=%t, want the stored value", raw, ok)
	}
	if _, ok := found[miss]; ok {
		t.Fatal("missing key reported as a hit")
	}
}

func TestNilStorePassesThrough(t *testing.T) {
	var store *JSONStore
	found, err := store.GetMany(t.Context(), []string{ArticleKey(uuid.New())})
	if err != nil || found != nil {
		t.Fatalf("get many = (%v, %v), want (nil, nil)", found, err)
	}
	if err := store.Set(t.Context(), ArticleKey(uuid.New()), record{Name: "x"}); err != nil {
		t.Fatalf("set = %v, want nil", err)
	}
}

func TestGetManyReportsUnreachableServer(t *testing.T) {
	store, server := newTestStore(t)
	server.Close()

	if _, err := store.GetMany(t.Context(), []string{ArticleKey(uuid.New())}); err == nil {
		t.Fatal("get many error = nil, want an error when Redis is unreachable")
	}
}
