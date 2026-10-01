package ingestion

import (
	"sync"
	"time"
)

const (
	// breakerThreshold is the consecutive-failure count that opens a circuit.
	breakerThreshold = 5
	// breakerCooldown is the first open window; every failed half-open probe
	// doubles the next one, capped at breakerMaxCooldown.
	breakerCooldown = time.Minute
	// breakerMaxCooldown caps the escalation so a dead provider is still
	// probed at least every 30 minutes.
	breakerMaxCooldown = 30 * time.Minute
)

// Breaker is a per-configuration consecutive-failure circuit breaker.
// It is safe for concurrent use: the runner fetches due configurations in
// parallel.
type Breaker struct {
	mu          sync.Mutex
	states      map[string]*breakerState
	threshold   int
	cooldown    time.Duration
	maxCooldown time.Duration
	now         func() time.Time
}

type breakerState struct {
	failures int
	openedAt time.Time
	probing  bool
	opens    int
}

// NewBreaker builds a breaker that opens after threshold consecutive
// failures. The first probe happens after cooldown; each failed probe
// doubles the following window up to the maximum.
func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	return &Breaker{
		states:      map[string]*breakerState{},
		threshold:   threshold,
		cooldown:    cooldown,
		maxCooldown: breakerMaxCooldown,
		now:         time.Now,
	}
}

// NewDefaultBreaker builds the breaker the worker uses: 5 consecutive
// failures open the circuit, cooldowns start at one minute and double per
// failed probe up to 30 minutes.
func NewDefaultBreaker() *Breaker {
	return NewBreaker(breakerThreshold, breakerCooldown)
}

// Allow reports whether a fetch for key may proceed.
func (breaker *Breaker) Allow(key string) bool {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()

	state, ok := breaker.states[key]
	if !ok || state.openedAt.IsZero() {
		return true
	}
	if breaker.now().Sub(state.openedAt) < breaker.cooldownFor(state.opens) {
		return false
	}
	if state.probing {
		return false
	}
	state.probing = true
	return true
}

// RecordSuccess closes the circuit for key by resetting its state.
func (breaker *Breaker) RecordSuccess(key string) {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	delete(breaker.states, key)
}

// RecordFailure counts a failure against key, opening the circuit once the
// threshold is reached. A failed half-open probe reopens with a longer
// cooldown.
func (breaker *Breaker) RecordFailure(key string) {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()

	state, ok := breaker.states[key]
	if !ok {
		state = &breakerState{}
		breaker.states[key] = state
	}
	state.probing = false
	state.failures++
	if state.failures >= breaker.threshold {
		state.openedAt = breaker.now()
		state.opens++
	}
}

func (breaker *Breaker) cooldownFor(opens int) time.Duration {
	cooldown := breaker.cooldown
	for range max(opens-1, 0) {
		cooldown *= 2
		if cooldown >= breaker.maxCooldown {
			return breaker.maxCooldown
		}
	}
	return cooldown
}
