package eventstore

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/udbhavbalaji/Atlas/internal/kernel/events"
)

type Handler func(context.Context, events.Event) error

type Store interface {
	Append(context.Context, events.Event) error
	Replay(context.Context, Handler) error
}

type JSONLStore struct {
	path string
	mu   sync.Mutex
}

func NewJSONLStore(path string) *JSONLStore {
	return &JSONLStore{path: path}
}

func (s *JSONLStore) Path() string {
	return s.path
}

func (s *JSONLStore) Append(ctx context.Context, event events.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create event store directory: %w", err)
	}

	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open event store: %w", err)
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(event); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	return nil
}

func (s *JSONLStore) Replay(ctx context.Context, handler Handler) error {
	if handler == nil {
		return errors.New("replay handler is required")
	}

	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open event store: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		lineNumber++

		var event events.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode event store line %d: %w", lineNumber, err)
		}
		event.Normalize()

		if err := handler(ctx, event); err != nil {
			return fmt.Errorf("replay event line %d: %w", lineNumber, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan event store: %w", err)
	}

	return nil
}
