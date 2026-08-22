package eventbus

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/udbhavbalaji/Atlas/internal/kernel/events"
	"github.com/udbhavbalaji/Atlas/internal/kernel/eventstore"
)

type Handler func(context.Context, events.Event) error

type Validator func(events.Event) error

type Bus struct {
	store     eventstore.Store
	validate  Validator
	mu        sync.RWMutex
	typed     map[events.EventType][]Handler
	broadcast []Handler
}

func New(store eventstore.Store, validate Validator) (*Bus, error) {
	if store == nil {
		return nil, errors.New("event store is required")
	}
	if validate == nil {
		return nil, errors.New("event validator is required")
	}

	return &Bus{
		store:    store,
		validate: validate,
		typed:    make(map[events.EventType][]Handler),
	}, nil
}

func (b *Bus) Subscribe(eventType events.EventType, handler Handler) error {
	if eventType == "" {
		return errors.New("event type is required")
	}
	if handler == nil {
		return errors.New("event handler is required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.typed[eventType] = append(b.typed[eventType], handler)
	return nil
}

func (b *Bus) SubscribeAll(handler Handler) error {
	if handler == nil {
		return errors.New("event handler is required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcast = append(b.broadcast, handler)
	return nil
}

func (b *Bus) Publish(ctx context.Context, event events.Event) error {
	event.Normalize()
	if err := b.validate(event); err != nil {
		return err
	}

	if err := b.store.Append(ctx, event); err != nil {
		return err
	}

	return b.dispatch(ctx, event)
}

func (b *Bus) Replay(ctx context.Context, handler Handler) error {
	if handler == nil {
		return errors.New("event handler is required")
	}

	return b.store.Replay(ctx, func(ctx context.Context, event events.Event) error {
		event.Normalize()
		if err := b.validate(event); err != nil {
			return err
		}
		return handler(ctx, event)
	})
}

func (b *Bus) ReplaySubscribers(ctx context.Context) error {
	return b.store.Replay(ctx, func(ctx context.Context, event events.Event) error {
		event.Normalize()
		if err := b.validate(event); err != nil {
			return err
		}
		return b.dispatch(ctx, event)
	})
}

func (b *Bus) dispatch(ctx context.Context, event events.Event) error {
	b.mu.RLock()
	handlers := append([]Handler{}, b.broadcast...)
	handlers = append(handlers, b.typed[event.Metadata.EventType]...)
	b.mu.RUnlock()

	for _, handler := range handlers {
		if err := handler(ctx, event); err != nil {
			return fmt.Errorf("dispatch %s: %w", event.Metadata.EventType, err)
		}
	}

	return nil
}
