package cache

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const resourceTTL = time.Hour
const resourceMaximumJitter = 10

const resourcePrefix = "macro_terminal:"

// JSONStore stores individual resource records as JSON so reads can skip
// a database round trip. Every method tolerates a nil receiver: a process
// built without a store simply reads and writes PostgreSQL directly.
type JSONStore struct {
	client *redis.Client
	ttl    time.Duration
}

func NewJSONStore(client *redis.Client) *JSONStore {
	return NewJSONStoreWithTTL(client, resourceTTL)
}

// NewJSONStoreWithTTL builds a store whose entries expire after ttl
// (still jittered downward by up to 10 percent).
func NewJSONStoreWithTTL(client *redis.Client, ttl time.Duration) *JSONStore {
	return &JSONStore{client: client, ttl: ttl}
}

func ArticleKey(id uuid.UUID) string {
	return resourcePrefix + "article:" + id.String()
}

func ExplainArticleKey(id uuid.UUID) string {
	return resourcePrefix + "explain:article:" + id.String()
}

func ExplainCalendarEventKey(id uuid.UUID) string {
	return resourcePrefix + "explain:calendar_event:" + id.String()
}

func ArticleEnrichmentKey(id uuid.UUID) string {
	return resourcePrefix + "article_enrichment:" + id.String()
}

func CalendarEventKey(id uuid.UUID) string {
	return resourcePrefix + "calendar_event:" + id.String()
}

func StoryClusterKey(id uuid.UUID) string {
	return resourcePrefix + "story_cluster:" + id.String()
}

// GetMany returns the values it could read; a key is absent when it is
// missing, unreadable, or the store is unreachable, so callers fall back
// to PostgreSQL without having to distinguish the causes.
func (store *JSONStore) GetMany(ctx context.Context, keys []string) (map[string]json.RawMessage, error) {
	if store == nil || len(keys) == 0 {
		return nil, nil
	}
	values, err := store.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	found := make(map[string]json.RawMessage, len(keys))
	for index, value := range values {
		if text, ok := value.(string); ok {
			found[keys[index]] = json.RawMessage(text)
		}
	}
	return found, nil
}

// Set is best effort by contract: callers keep working when the write
// fails, because PostgreSQL already holds the authoritative record.
func (store *JSONStore) Set(ctx context.Context, key string, value any) error {
	if store == nil {
		return nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return store.client.Set(ctx, key, payload, store.expiry()).Err()
}

// Delete drops cached values so the next read repopulates them from
// PostgreSQL. Best effort by the same contract as Set.
func (store *JSONStore) Delete(ctx context.Context, keys ...string) error {
	if store == nil || len(keys) == 0 {
		return nil
	}
	return store.client.Del(ctx, keys...).Err()
}

func (store *JSONStore) expiry() time.Duration {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return store.ttl
	}
	percent := binary.LittleEndian.Uint64(value[:]) % (resourceMaximumJitter + 1)
	return store.ttl - store.ttl*time.Duration(percent)/100
}
