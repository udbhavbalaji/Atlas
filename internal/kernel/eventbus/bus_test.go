package eventbus_test

import (
	"context"
	"errors"
	"testing"

	"github.com/udbhavbalaji/Atlas/internal/kernel/catalog"
	"github.com/udbhavbalaji/Atlas/internal/kernel/eventbus"
	"github.com/udbhavbalaji/Atlas/internal/kernel/events"
	"github.com/udbhavbalaji/Atlas/internal/kernel/eventstore"
)

func TestPublishPersistsBeforeDispatch(t *testing.T) {
	ctx := context.Background()
	store := &recordingStore{}
	bus := newBusForTest(t, store)
	event := eventForTest(t)

	if err := bus.Subscribe("ArtifactCaptured", func(ctx context.Context, event events.Event) error {
		if !store.appended {
			t.Fatal("handler ran before store append")
		}
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	if err := bus.Publish(ctx, event); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
}

func TestPublishDispatchesMatchingSubscribers(t *testing.T) {
	ctx := context.Background()
	store := &recordingStore{}
	bus := newBusForTest(t, store)
	event := eventForTest(t)
	received := 0

	if err := bus.Subscribe("ArtifactCaptured", func(ctx context.Context, event events.Event) error {
		received++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	if err := bus.Publish(ctx, event); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if received != 1 {
		t.Fatalf("received = %d, want 1", received)
	}
}

func TestReplayIsDeterministicForIdempotentConsumer(t *testing.T) {
	ctx := context.Background()
	first := eventForTest(t)
	second := eventForTest(t)
	second.Metadata.Source = "capture.browser"
	store := &recordingStore{events: []events.Event{first, first, second}}
	bus := newBusForTest(t, store)
	seen := map[string]bool{}
	var order []string

	if err := bus.Replay(ctx, func(ctx context.Context, event events.Event) error {
		if seen[event.Metadata.EventID] {
			return nil
		}
		seen[event.Metadata.EventID] = true
		order = append(order, event.Metadata.EventID)
		return nil
	}); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}

	if len(order) != 2 {
		t.Fatalf("unique replayed events = %d, want 2", len(order))
	}
	if order[0] != first.Metadata.EventID || order[1] != second.Metadata.EventID {
		t.Fatal("replay order did not preserve store order")
	}
}

func newBusForTest(t *testing.T, store eventstore.Store) *eventbus.Bus {
	t.Helper()

	registry := catalog.NewRegistry()
	bus, err := eventbus.New(store, func(event events.Event) error {
		return events.Validate(event, registry)
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return bus
}

func eventForTest(t *testing.T) events.Event {
	t.Helper()

	event, err := events.New("ArtifactCaptured", "capture.manual", events.ActorUser, events.SensitivityPersonal, events.Payload{
		"note": "hello",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return event
}

type recordingStore struct {
	appended bool
	events   []events.Event
	err      error
}

func (s *recordingStore) Append(ctx context.Context, event events.Event) error {
	if s.err != nil {
		return s.err
	}
	s.appended = true
	s.events = append(s.events, event)
	return nil
}

func (s *recordingStore) Replay(ctx context.Context, handler eventstore.Handler) error {
	if s.err != nil {
		return s.err
	}
	if handler == nil {
		return errors.New("handler is required")
	}
	for _, event := range s.events {
		if err := handler(ctx, event); err != nil {
			return err
		}
	}
	return nil
}
