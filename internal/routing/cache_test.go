package routing

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type evaluateFunc func(context.Context, State, []Action, string) (Evaluation, error)

func (f evaluateFunc) Evaluate(ctx context.Context, s State, a []Action, fixture string) (Evaluation, error) {
	return f(ctx, s, a, fixture)
}

func TestRoutingCacheReusesOnlyMatchingSuccessfulEvaluations(t *testing.T) {
	var calls atomic.Int32
	c := &Cached{Provider: evaluateFunc(func(ctx context.Context, s State, a []Action, _ string) (Evaluation, error) {
		calls.Add(1)
		return (Mock{}).Evaluate(ctx, s, a, "task")
	})}
	state := State{Text: "interview", Timezone: "UTC"}
	first, err := c.Evaluate(t.Context(), state, Registry(), "")
	if err != nil || first.ModelCalls != 1 || first.CacheHit {
		t.Fatal(first, err)
	}
	state.ReferenceAt = "a later reference time"
	second, err := c.Evaluate(t.Context(), state, Registry(), "")
	if err != nil || !second.CacheHit || second.ModelCalls != 0 || calls.Load() != 1 {
		t.Fatal(second, err, calls.Load())
	}
	second.Decision.Probabilities["task"] = 0
	third, _ := c.Evaluate(t.Context(), state, Registry(), "")
	if third.Decision.Probabilities["task"] != 1-0.01*float64(len(Registry())-1) {
		t.Fatal("caller mutated cache")
	}
	state.Text = "changed"
	c.Evaluate(t.Context(), state, Registry(), "")
	if calls.Load() != 2 {
		t.Fatal(calls.Load())
	}
	state.ContextTruncated = true
	c.Evaluate(t.Context(), state, Registry(), "")
	if calls.Load() != 3 {
		t.Fatal("changed context reused")
	}
	for key, e := range c.entries {
		e.expires = time.Now().Add(-time.Second)
		c.entries[key] = e
	}
	c.Evaluate(t.Context(), state, Registry(), "")
	if calls.Load() != 4 {
		t.Fatal("expired entry reused")
	}
	c.Provider = evaluateFunc(func(context.Context, State, []Action, string) (Evaluation, error) {
		calls.Add(1)
		return Evaluation{}, ErrUnavailable
	})
	state.Text = "failure"
	for range 2 {
		_, err = c.Evaluate(t.Context(), state, Registry(), "")
		if !errors.Is(err, ErrUnavailable) {
			t.Fatal(err)
		}
	}
	if calls.Load() != 6 {
		t.Fatal("failure cached", calls.Load())
	}
}
func TestRoutingCacheCoalescesConcurrentCallsAndBoundsRetention(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	c := &Cached{Provider: evaluateFunc(func(ctx context.Context, s State, a []Action, _ string) (Evaluation, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return (Mock{}).Evaluate(ctx, s, a, "task")
	})}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Evaluate(t.Context(), State{Text: "same"}, Registry(), "")
			if err != nil {
				t.Error(err)
			}
		}()
	}
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := c.Evaluate(ctx, State{Text: "same"}, Registry(), "")
	if err != ErrUnavailable {
		t.Fatal(err)
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	for i := range 130 {
		c.Evaluate(t.Context(), State{Text: string(rune(i + 100))}, Registry(), "")
	}
	if len(c.entries) != 128 {
		t.Fatal(len(c.entries))
	}
}
