package ratelimit

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrLimitExceeded = errors.New("rate limit exceeded")

const hybridLimitScript = `
local bucket = KEYS[1]
local window = KEYS[2]
local now = tonumber(ARGV[1])
local immediate_limit = tonumber(ARGV[2])
local restoration_period = tonumber(ARGV[3])
local maximum_window = tonumber(ARGV[4])
local maximum_requests = tonumber(ARGV[5])
local member = ARGV[6]

local bucket_values = redis.call("HMGET", bucket, "tokens", "updated_at")
local immediate_remaining = tonumber(bucket_values[1])
local updated_at = tonumber(bucket_values[2])
if immediate_remaining == nil or updated_at == nil then
    immediate_remaining = immediate_limit
    updated_at = now
end

local elapsed = math.max(0, now - updated_at)
immediate_remaining = math.min(immediate_limit, immediate_remaining + (elapsed * immediate_limit / restoration_period))

redis.call("ZREMRANGEBYSCORE", window, "-inf", now - maximum_window)
local count = redis.call("ZCARD", window)
local allowed = immediate_remaining >= 1 and count < maximum_requests

if allowed then
    immediate_remaining = immediate_remaining - 1
    redis.call("ZADD", window, now, member)
end

redis.call("HSET", bucket, "tokens", immediate_remaining, "updated_at", now)
local ttl = math.max(restoration_period, maximum_window) * 2
redis.call("PEXPIRE", bucket, ttl)
redis.call("PEXPIRE", window, ttl)

local token_retry = 0
if immediate_remaining < 1 then
    token_retry = math.ceil((1 - immediate_remaining) * restoration_period / immediate_limit)
end
local window_retry = 0
if count >= maximum_requests then
    local oldest = redis.call("ZRANGE", window, 0, 0, "WITHSCORES")
    if oldest[2] ~= nil then
        window_retry = math.max(0, tonumber(oldest[2]) + maximum_window - now)
    end
end

return {allowed and 1 or 0, math.max(token_retry, window_retry)}
`

type Rule struct {
	// ImmediateRequests is how many requests may happen close together.
	ImmediateRequests int
	// RestoreImmediateRequestsOver is how long it takes to restore the full
	// immediate allowance after it has been used.
	RestoreImmediateRequestsOver time.Duration
	// MaximumRequests is the hard request limit during MaximumWindow.
	MaximumRequests int
	// MaximumWindow is the rolling period used by the hard request limit.
	MaximumWindow time.Duration
}

type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type RedisLimiter struct {
	client redis.UniversalClient
	secret []byte
	prefix string
	now    func() time.Time
}

func NewRedisLimiter(
	client redis.UniversalClient,
	secret []byte,
	keyPrefix string,
) (*RedisLimiter, error) {
	if client == nil {
		return nil, fmt.Errorf("client is required for Redis")
	}
	if len(secret) < 32 {
		return nil, fmt.Errorf("rate-limit key secret must contain at least 32 bytes")
	}
	keyPrefix = strings.TrimSpace(keyPrefix)
	if keyPrefix == "" {
		keyPrefix = "macro_terminal:rate-limit"
	}
	return &RedisLimiter{
		client: client,
		secret: append([]byte(nil), secret...),
		prefix: keyPrefix,
		now:    time.Now,
	}, nil
}

func (limiter *RedisLimiter) Allow(
	ctx context.Context,
	operation string,
	dimension string,
	identity string,
	rule Rule,
) (Decision, error) {
	if err := validateRule(rule); err != nil {
		return Decision{}, err
	}
	operation = strings.TrimSpace(operation)
	dimension = strings.TrimSpace(dimension)
	identity = strings.TrimSpace(identity)
	if operation == "" || dimension == "" || identity == "" {
		return Decision{}, fmt.Errorf("rate-limit operation, dimension, and identity are required")
	}

	identityKey := limiter.identityKey(operation, dimension, identity)
	member, err := randomMember()
	if err != nil {
		return Decision{}, fmt.Errorf("create rate-limit event identifier: %w", err)
	}
	now := limiter.now().UTC().UnixMilli()
	result, err := limiter.client.Eval(
		ctx,
		hybridLimitScript,
		[]string{identityKey + ":bucket", identityKey + ":window"},
		now,
		rule.ImmediateRequests,
		rule.RestoreImmediateRequestsOver.Milliseconds(),
		rule.MaximumWindow.Milliseconds(),
		rule.MaximumRequests,
		member,
	).Int64Slice()
	if err != nil {
		return Decision{}, fmt.Errorf("enforce distributed rate limit: %w", err)
	}
	if len(result) != 2 {
		return Decision{}, fmt.Errorf("invalid distributed rate-limit response")
	}
	decision := Decision{
		Allowed:    result[0] == 1,
		RetryAfter: time.Duration(result[1]) * time.Millisecond,
	}
	if !decision.Allowed {
		return decision, ErrLimitExceeded
	}
	return decision, nil
}

func (limiter *RedisLimiter) identityKey(operation, dimension, identity string) string {
	mac := hmac.New(sha256.New, limiter.secret)
	_, _ = mac.Write([]byte(operation + "\x00" + dimension + "\x00" + identity))
	return limiter.prefix + ":" + operation + ":" + dimension + ":" + hex.EncodeToString(mac.Sum(nil))
}

func validateRule(rule Rule) error {
	if rule.ImmediateRequests < 1 || rule.RestoreImmediateRequestsOver <= 0 ||
		rule.MaximumRequests < 1 || rule.MaximumWindow <= 0 {
		return fmt.Errorf("rate-limit rule values must be positive")
	}
	return nil
}

func randomMember() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
