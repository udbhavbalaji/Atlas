package events_test

import (
	"testing"
	"time"

	"github.com/udbhavbalaji/Atlas/internal/kernel/catalog"
	"github.com/udbhavbalaji/Atlas/internal/kernel/events"
)

func TestValidateAcceptsValidCatalogEvent(t *testing.T) {
	event := validEvent(t)

	if err := events.Validate(event, catalog.NewRegistry()); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsUnknownEventType(t *testing.T) {
	event := validEvent(t)
	event.Metadata.EventType = "ImportDocument"

	if err := events.Validate(event, catalog.NewRegistry()); err == nil {
		t.Fatal("Validate() error = nil, want unknown event type error")
	}
}

func TestValidateRejectsMissingRequiredMetadata(t *testing.T) {
	event := validEvent(t)
	event.Metadata.Source = ""

	if err := events.Validate(event, catalog.NewRegistry()); err == nil {
		t.Fatal("Validate() error = nil, want missing source error")
	}
}

func TestNormalizeConvertsTimestampToUTC(t *testing.T) {
	event := validEvent(t)
	event.Metadata.Timestamp = time.Date(2026, 7, 5, 12, 0, 0, 0, time.FixedZone("IST", 5*60*60+30*60))

	event.Normalize()

	if event.Metadata.Timestamp.Location() != time.UTC {
		t.Fatalf("timestamp location = %v, want UTC", event.Metadata.Timestamp.Location())
	}
}

func validEvent(t *testing.T) events.Event {
	t.Helper()

	event, err := events.New("ArtifactCaptured", "capture.manual", events.ActorUser, events.SensitivityPersonal, events.Payload{
		"note": "hello",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return event
}
