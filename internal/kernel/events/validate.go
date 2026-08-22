package events

import (
	"errors"
	"fmt"
	"time"
)

type Catalog interface {
	Contains(EventType) bool
}

func Validate(e Event, catalog Catalog) error {
	if catalog == nil {
		return errors.New("event catalog is required")
	}
	if e.Metadata.EventID == "" {
		return errors.New("event metadata event_id is required")
	}
	if e.Metadata.EventType == "" {
		return errors.New("event metadata event_type is required")
	}
	if !catalog.Contains(e.Metadata.EventType) {
		return fmt.Errorf("event type %q is not registered in the event catalog", e.Metadata.EventType)
	}
	if e.Metadata.Version < 1 {
		return errors.New("event metadata version must be greater than zero")
	}
	if e.Metadata.Timestamp.IsZero() {
		return errors.New("event metadata timestamp is required")
	}
	if !e.Metadata.Timestamp.Equal(e.Metadata.Timestamp.UTC()) || e.Metadata.Timestamp.Location() != time.UTC {
		return errors.New("event metadata timestamp must be UTC")
	}
	if e.Metadata.Source == "" {
		return errors.New("event metadata source is required")
	}
	if e.Metadata.Actor == "" {
		return errors.New("event metadata actor is required")
	}
	if e.Metadata.Sensitivity == "" {
		return errors.New("event metadata sensitivity is required")
	}
	if !validActor(e.Metadata.Actor) {
		return fmt.Errorf("event metadata actor %q is not supported", e.Metadata.Actor)
	}
	if !validSensitivity(e.Metadata.Sensitivity) {
		return fmt.Errorf("event metadata sensitivity %q is not supported", e.Metadata.Sensitivity)
	}

	return nil
}

func validActor(actor Actor) bool {
	switch actor {
	case ActorUser, ActorSystem, ActorScheduler, ActorPlugin, ActorAI, ActorSynchronization:
		return true
	default:
		return false
	}
}

func validSensitivity(sensitivity Sensitivity) bool {
	switch sensitivity {
	case SensitivityPublic, SensitivityInternal, SensitivityPersonal, SensitivitySensitive, SensitivityHighlySensitive:
		return true
	default:
		return false
	}
}
