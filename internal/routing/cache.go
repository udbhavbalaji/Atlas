package routing

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sync"
	"time"
)

type cachedEvaluation struct {
	value   Evaluation
	expires time.Time
}
type pendingEvaluation struct {
	done  chan struct{}
	value Evaluation
	err   error
}

// Cached shares successful routing decisions for five minutes, capped at 128
// entries. Keys are hashes, not retained user text. Concurrent identical calls
// share one request. Failures are never cached or retried automatically.
// ReferenceAt is excluded because routing only classifies primary intent; date
// resolution belongs to channels and must not use this cache.
type Cached struct {
	Provider Evaluator
	mu       sync.Mutex
	entries  map[[32]byte]cachedEvaluation
	pending  map[[32]byte]*pendingEvaluation
}

func (c *Cached) Evaluate(ctx context.Context, state State, actions []Action, fixture string) (Evaluation, error) {
	keyState := state
	keyState.ReferenceAt = ""
	encoded, err := json.Marshal(struct {
		State   State
		Actions []Action
		Fixture string
	}{keyState, actions, fixture})
	if err != nil {
		return Evaluation{}, ErrRequest
	}
	key := sha256.Sum256(encoded)
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[[32]byte]cachedEvaluation{}
		c.pending = map[[32]byte]*pendingEvaluation{}
	}
	if entry, ok := c.entries[key]; ok && time.Now().Before(entry.expires) {
		c.mu.Unlock()
		return reuseEvaluation(entry.value), nil
	}
	if pending, ok := c.pending[key]; ok {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return Evaluation{}, ErrUnavailable
		case <-pending.done:
			if pending.err != nil {
				return Evaluation{}, pending.err
			}
			return reuseEvaluation(pending.value), nil
		}
	}
	pending := &pendingEvaluation{done: make(chan struct{})}
	c.pending[key] = pending
	c.mu.Unlock()
	value, err := c.Provider.Evaluate(ctx, state, actions, fixture)
	if err == nil {
		err = ValidateChoice(value.Decision, actions)
		if value.Model == "" || value.Usage.InputTokens < 0 || value.Usage.OutputTokens < 0 {
			err = ErrContract
		}
	}
	value.ModelCalls = 1
	value.CacheHit = false
	value.EvaluatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	c.mu.Lock()
	if err == nil {
		for k, e := range c.entries {
			if !time.Now().Before(e.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= 128 {
			var oldest [32]byte
			var expires time.Time
			for k, e := range c.entries {
				if expires.IsZero() || e.expires.Before(expires) {
					oldest = k
					expires = e.expires
				}
			}
			delete(c.entries, oldest)
		}
		c.entries[key] = cachedEvaluation{cloneEvaluation(value), time.Now().Add(5 * time.Minute)}
	}
	pending.value = cloneEvaluation(value)
	pending.err = err
	delete(c.pending, key)
	close(pending.done)
	c.mu.Unlock()
	return value, err
}
func cloneEvaluation(value Evaluation) Evaluation {
	original := value.Decision.Probabilities
	value.Decision.Probabilities = map[string]float64{}
	for key, p := range original {
		value.Decision.Probabilities[key] = p
	}
	return value
}
func reuseEvaluation(value Evaluation) Evaluation {
	value = cloneEvaluation(value)
	value.CacheHit = true
	value.ModelCalls = 0
	return value
}
