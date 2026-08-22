package eventstore_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/udbhavbalaji/Atlas/internal/kernel/events"
	"github.com/udbhavbalaji/Atlas/internal/kernel/eventstore"
)

func TestJSONLStoreAppendsAndReplaysInOrder(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	store := eventstore.NewJSONLStore(path)
	first := eventForTest(t, "capture.manual")
	second := eventForTest(t, "capture.browser")

	if err := store.Append(ctx, first); err != nil {
		t.Fatalf("Append(first) error = %v", err)
	}
	if err := store.Append(ctx, second); err != nil {
		t.Fatalf("Append(second) error = %v", err)
	}

	var got []events.Event
	if err := store.Replay(ctx, func(ctx context.Context, event events.Event) error {
		got = append(got, event)
		return nil
	}); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("replayed %d events, want 2", len(got))
	}
	if got[0].Metadata.EventID != first.Metadata.EventID {
		t.Fatalf("first replayed ID = %s, want %s", got[0].Metadata.EventID, first.Metadata.EventID)
	}
	if got[1].Metadata.EventID != second.Metadata.EventID {
		t.Fatalf("second replayed ID = %s, want %s", got[1].Metadata.EventID, second.Metadata.EventID)
	}
}

func TestJSONLStoreReplayFailsOnMalformedJSONL(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("{not-json}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store := eventstore.NewJSONLStore(path)
	err := store.Replay(ctx, func(ctx context.Context, event events.Event) error {
		return nil
	})
	if err == nil {
		t.Fatal("Replay() error = nil, want malformed JSONL error")
	}
}

func eventForTest(t *testing.T, source string) events.Event {
	t.Helper()

	event, err := events.New("ArtifactCaptured", source, events.ActorUser, events.SensitivityPersonal, events.Payload{
		"source": source,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return event
}
