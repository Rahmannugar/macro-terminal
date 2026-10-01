package ingestion

import (
	"sync"
	"time"
)

const (
	// breakerThreshold is how many consecutive failures open the circuit.
	breakerThreshold = 5
	// breakerCooldown is the first cooldown. Every failed retry doubles the
	// next one, up to breakerMaxCooldown.
	breakerCooldown = time.Minute
	// breakerMaxCooldown caps the cooldown so a dead provider is still tried
	// again every 30 minutes.
	breakerMaxCooldown = 30 * time.Minute
)

// Breaker stops calling a source that keeps failing: after too many
// failures in a row, its fetches wait for a cooldown before trying again.
// Every source configuration has its own state, and the methods are safe to
// call from the runner's parallel fetches.
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
// failures and lets one test request through after each cooldown.
func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	return &Breaker{
		states:      map[string]*breakerState{},
		threshold:   threshold,
		cooldown:    cooldown,
		maxCooldown: breakerMaxCooldown,
		now:         time.Now,
	}
}

// NewDefaultBreaker is the worker's breaker, built from the constants
// above.
func NewDefaultBreaker() *Breaker {
	return NewBreaker(breakerThreshold, breakerCooldown)
}

// Allow reports whether a fetch for key may run right now. The first call
// after a cooldown is let through as a test; if that fails, the wait
// doubles.
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

// RecordSuccess clears the source's failure state after a successful
// fetch.
func (breaker *Breaker) RecordSuccess(key string) {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	delete(breaker.states, key)
}

// RecordFailure counts a failed fetch against key. Once the threshold is
// reached the circuit opens and fetches are blocked until the cooldown
// passes.
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

// cooldownFor doubles the cooldown for every time the circuit has opened:
// 1m, 2m, 4m … capped at breakerMaxCooldown.
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
