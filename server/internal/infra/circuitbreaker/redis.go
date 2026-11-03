package circuitbreaker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var ErrOpen = errors.New("circuit breaker is open")

type Policy struct {
	Threshold   int
	Window      time.Duration
	Cooldown    time.Duration
	MaxCooldown time.Duration
	ProbeLease  time.Duration
}

func DefaultPolicy() Policy {
	return Policy{Threshold: 5, Window: 10 * time.Minute, Cooldown: time.Minute, MaxCooldown: 30 * time.Minute, ProbeLease: time.Minute}
}

type Permit struct {
	Scope   string
	Token   string
	Version string
	Probe   bool
	Policy  Policy
}

type Outcome struct {
	Failed    bool
	Neutral   bool
	Immediate bool
	Pause     time.Duration
	Reason    string
}

type Controller interface {
	Acquire(context.Context, string, Policy) (Permit, error)
	Finish(context.Context, Permit, Outcome) error
}

type PauseError struct {
	Scope  string
	Reason string
	After  time.Duration
	Cause  error
}

func (failure *PauseError) Error() string {
	return fmt.Sprintf("dependency paused: %s (%s); retry after %s", failure.Scope, failure.Reason, failure.After.Round(time.Second))
}

func (failure *PauseError) Unwrap() []error {
	if failure.Cause == nil {
		return []error{ErrOpen}
	}
	return []error{ErrOpen, failure.Cause}
}

func Paused(err error) (*PauseError, bool) {
	var paused *PauseError
	ok := errors.As(err, &paused)
	return paused, ok
}

type Breaker struct {
	client redis.UniversalClient
	logger *slog.Logger
}

func New(client redis.UniversalClient, logger *slog.Logger) *Breaker {
	return &Breaker{client: client, logger: logger}
}

const acquireScript = `
local clock = redis.call('TIME')
local now = tonumber(clock[1])*1000 + math.floor(tonumber(clock[2])/1000)
local version = redis.call('HGET', KEYS[1], 'version')
if not version then
  version = ARGV[1]
  redis.call('HSET', KEYS[1], 'version', version)
end
local until_at = tonumber(redis.call('HGET', KEYS[1], 'until') or '0')
if until_at > now then
  return cjson.encode({allowed=false, retry_ms=until_at-now, reason=redis.call('HGET',KEYS[1],'reason') or 'dependency_unavailable'})
end
if until_at > 0 then
  local lease = tonumber(redis.call('HGET',KEYS[1],'lease_until') or '0')
  if lease > now then
    return cjson.encode({allowed=false,retry_ms=lease-now,reason='recovery_probe_busy'})
  end
  version = ARGV[1]
  redis.call('HSET',KEYS[1],'version',version,'lease',ARGV[1],'lease_until',now+tonumber(ARGV[2]))
  redis.call('PEXPIRE',KEYS[1],tonumber(ARGV[3]))
  return cjson.encode({allowed=true,version=version,probe=true})
end
redis.call('PEXPIRE',KEYS[1],tonumber(ARGV[3]))
return cjson.encode({allowed=true,version=version,probe=false})
`

const finishScript = `
local clock = redis.call('TIME')
local now = tonumber(clock[1])*1000 + math.floor(tonumber(clock[2])/1000)
if redis.call('HGET',KEYS[1],'version') ~= ARGV[1] then return '' end
if ARGV[2]=='1' and redis.call('HGET',KEYS[1],'lease') ~= ARGV[3] then return '' end
if ARGV[4]=='neutral' then
  if ARGV[2]=='1' then redis.call('HDEL',KEYS[1],'lease','lease_until') end
  return ''
end
if ARGV[4]=='success' then
  redis.call('HSET',KEYS[1],'failures',0,'failure_start',0,'opens',0)
  if ARGV[2]=='1' then
    redis.call('HSET',KEYS[1],'until',0,'version',ARGV[12])
    redis.call('HDEL',KEYS[1],'lease','lease_until','reason')
    return cjson.encode({event='closed',retry_ms=0,next_probe_at=0})
  end
  return ''
end
local first = tonumber(redis.call('HGET',KEYS[1],'failure_start') or '0')
local failures = tonumber(redis.call('HGET',KEYS[1],'failures') or '0')
if first==0 or now-first > tonumber(ARGV[6]) then first=now; failures=0 end
failures=failures+1
redis.call('HSET',KEYS[1],'failures',failures,'failure_start',first)
if failures < tonumber(ARGV[5]) and ARGV[2]~='1' and ARGV[9]~='1' then return '' end
local opens=tonumber(redis.call('HGET',KEYS[1],'opens') or '0')+1
local delay=math.min(tonumber(ARGV[8]),tonumber(ARGV[7])*2^math.min(opens-1,20))
delay=math.max(delay,tonumber(ARGV[10]))
redis.call('HSET',KEYS[1],'until',now+delay,'opens',opens,'reason',ARGV[11],'version',ARGV[12])
redis.call('HDEL',KEYS[1],'lease','lease_until')
redis.call('PEXPIRE',KEYS[1],math.max(tonumber(ARGV[13]),delay+tonumber(ARGV[6])))
return cjson.encode({event='opened',retry_ms=delay,next_probe_at=now+delay})
`

func stateKey(scope string) string {
	return fmt.Sprintf("macro-terminal:circuit:%x", sha256.Sum256([]byte(scope)))
}

func (breaker *Breaker) Acquire(ctx context.Context, scope string, policy Policy) (Permit, error) {
	if err := ctx.Err(); err != nil {
		return Permit{}, err
	}
	if scope == "" || policy.Threshold < 1 || policy.Window <= 0 || policy.Cooldown <= 0 || policy.MaxCooldown < policy.Cooldown || policy.ProbeLease <= 0 {
		return Permit{}, errors.New("invalid circuit policy or scope")
	}
	token := uuid.NewString()
	raw, err := breaker.client.Eval(ctx, acquireScript, []string{stateKey(scope)}, token, policy.ProbeLease.Milliseconds(), ttl(policy).Milliseconds()).Text()
	if err != nil {
		breaker.logger.ErrorContext(ctx, "Circuit state unavailable", "event", "circuit.state_unavailable", "dependency_scope", scope, "error", err)
		return Permit{}, &PauseError{Scope: scope, Reason: "circuit_store_unavailable", After: 30 * time.Second, Cause: err}
	}
	var decision struct {
		Allowed bool   `json:"allowed"`
		Version string `json:"version"`
		Probe   bool   `json:"probe"`
		RetryMS int64  `json:"retry_ms"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		return Permit{}, fmt.Errorf("decode circuit decision: %w", err)
	}
	if !decision.Allowed {
		return Permit{}, &PauseError{Scope: scope, Reason: decision.Reason, After: time.Duration(decision.RetryMS) * time.Millisecond}
	}
	if decision.Probe {
		breaker.logger.InfoContext(ctx, "Testing a paused dependency", "event", "circuit.probing", "dependency_scope", scope)
	}
	return Permit{Scope: scope, Token: token, Version: decision.Version, Probe: decision.Probe, Policy: policy}, nil
}

func ttl(policy Policy) time.Duration {
	return policy.Window + policy.MaxCooldown + policy.ProbeLease + time.Hour
}

func (breaker *Breaker) Finish(ctx context.Context, permit Permit, outcome Outcome) error {
	kind := "success"
	if outcome.Neutral {
		kind = "neutral"
	} else if outcome.Failed {
		kind = "failure"
	}
	probe, immediate := 0, 0
	if permit.Probe {
		probe = 1
	}
	if outcome.Immediate {
		immediate = 1
	}
	event, err := breaker.client.Eval(ctx, finishScript, []string{stateKey(permit.Scope)}, permit.Version, probe, permit.Token, kind,
		permit.Policy.Threshold, permit.Policy.Window.Milliseconds(), permit.Policy.Cooldown.Milliseconds(), permit.Policy.MaxCooldown.Milliseconds(),
		immediate, outcome.Pause.Milliseconds(), outcome.Reason, uuid.NewString(), ttl(permit.Policy).Milliseconds()).Text()
	if err != nil {
		return fmt.Errorf("update circuit state: %w", err)
	}
	if event != "" {
		var transition struct {
			Event       string `json:"event"`
			RetryMS     int64  `json:"retry_ms"`
			NextProbeAt int64  `json:"next_probe_at"`
		}
		if err := json.Unmarshal([]byte(event), &transition); err != nil {
			return fmt.Errorf("decode circuit transition: %w", err)
		}
		breaker.logger.InfoContext(ctx, "Dependency circuit changed", "event", "circuit."+transition.Event, "dependency_scope", permit.Scope, "reason", outcome.Reason, "retry_after_seconds", float64(transition.RetryMS)/1000, "next_probe_unix_ms", transition.NextProbeAt)
	}
	return nil
}
