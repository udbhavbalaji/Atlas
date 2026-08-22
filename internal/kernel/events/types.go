package events

import "time"

type EventType string

type Actor string

const (
	ActorUser            Actor = "User"
	ActorSystem          Actor = "System"
	ActorScheduler       Actor = "Scheduler"
	ActorPlugin          Actor = "Plugin"
	ActorAI              Actor = "AI"
	ActorSynchronization Actor = "Synchronization"
)

type Sensitivity string

const (
	SensitivityPublic          Sensitivity = "Public"
	SensitivityInternal        Sensitivity = "Internal"
	SensitivityPersonal        Sensitivity = "Personal"
	SensitivitySensitive       Sensitivity = "Sensitive"
	SensitivityHighlySensitive Sensitivity = "HighlySensitive"
)

type Event struct {
	Metadata   Metadata       `json:"metadata"`
	Payload    Payload        `json:"payload,omitempty"`
	Provenance ProvenanceRefs `json:"provenance,omitempty"`
}

type Metadata struct {
	EventID       string      `json:"event_id"`
	EventType     EventType   `json:"event_type"`
	Version       int         `json:"version"`
	Timestamp     time.Time   `json:"timestamp"`
	Source        string      `json:"source"`
	CorrelationID string      `json:"correlation_id,omitempty"`
	CausationID   string      `json:"causation_id,omitempty"`
	Actor         Actor       `json:"actor"`
	Sensitivity   Sensitivity `json:"sensitivity"`
}

type Payload map[string]any

type ProvenanceRefs []ProvenanceRef

type ProvenanceRef struct {
	Source     string `json:"source,omitempty"`
	Reference  string `json:"reference,omitempty"`
	ArtifactID string `json:"artifact_id,omitempty"`
}

func New(eventType EventType, source string, actor Actor, sensitivity Sensitivity, payload Payload) (Event, error) {
	id, err := NewID()
	if err != nil {
		return Event{}, err
	}

	return Event{
		Metadata: Metadata{
			EventID:     id,
			EventType:   eventType,
			Version:     1,
			Timestamp:   time.Now().UTC(),
			Source:      source,
			Actor:       actor,
			Sensitivity: sensitivity,
		},
		Payload: payload,
	}, nil
}

func (e *Event) Normalize() {
	if !e.Metadata.Timestamp.IsZero() {
		e.Metadata.Timestamp = e.Metadata.Timestamp.UTC()
	}
}
