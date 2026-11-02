package ratelimit

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const embeddingWindowScript = `
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - 60000)
local entries = redis.call('ZRANGE', KEYS[1], 0, -1, 'WITHSCORES')
local tokens, requests = tonumber(ARGV[1]), tonumber(ARGV[2])
for i = 1, #entries, 2 do
  local t, r = string.match(entries[i], ':(%d+):(%d+)$')
  tokens = tokens + tonumber(t)
  requests = requests + tonumber(r)
end
if tokens <= tonumber(ARGV[3]) and requests <= 80 then
  redis.call('ZADD', KEYS[1], now, ARGV[4] .. ':' .. ARGV[1] .. ':' .. ARGV[2])
  redis.call('PEXPIRE', KEYS[1], 61000)
  return 0
end
for i = 1, #entries, 2 do
  local t, r = string.match(entries[i], ':(%d+):(%d+)$')
  tokens = tokens - tonumber(t)
  requests = requests - tonumber(r)
  if tokens <= tonumber(ARGV[3]) and requests <= 80 then
    return math.max(1, tonumber(entries[i+1]) + 60001 - now)
  end
end
return 60001
`

type EmbeddingBudget struct {
	client redis.UniversalClient
	key    string
	limit  int
}

// All callers share one conservative allowance even when the backup key belongs
// to another project. Switching keys must not multiply the application's budget.
func NewEmbeddingBudget(client redis.UniversalClient, model string, tokenLimit int) *EmbeddingBudget {
	return &EmbeddingBudget{client: client, key: fmt.Sprintf("macro-terminal:embedding-budget:%x", sha256.Sum256([]byte(model))), limit: tokenLimit}
}

func (budget *EmbeddingBudget) Reserve(ctx context.Context, tokens, requests int) (time.Duration, error) {
	if tokens < 1 || tokens > budget.limit || requests < 1 || requests > 80 {
		return 0, fmt.Errorf("embedding input exceeds the local allowance: tokens=%d requests=%d", tokens, requests)
	}
	delay, err := budget.client.Eval(ctx, embeddingWindowScript, []string{budget.key}, tokens, requests, budget.limit, uuid.NewString()).Int64()
	if err != nil {
		return 0, fmt.Errorf("reserve embedding allowance: %w", err)
	}
	return time.Duration(delay) * time.Millisecond, nil
}
