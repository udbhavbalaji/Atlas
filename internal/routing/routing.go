// Package routing selects a primary channel. Channels own field collection,
// enrichment and proposals; selecting a channel never authorizes a write.
package routing

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"atlas/internal/provider"
)

const Version = "1"
const RegistryVersion = "1"

var ErrRequest = errors.New("invalid routing request")
var ErrContract = errors.New("invalid routing evaluation")
var ErrUnavailable = errors.New("routing provider unavailable; no fallback was used")

type Action struct {
	ID          string `json:"id"`
	Channel     string `json:"channel"`
	Description string `json:"description"`
}

// Registry is the single source of route criteria. Only implemented channels
// belong here. Add capabilities together with their subsystem handler.
func Registry() []Action {
	return []Action{
		{"task", "tasks", "Capture a new action, obligation, plan or scheduled event, including an interview or movie outing. The task channel may offer linked reminders and notes."},
		{"reminder", "reminders", "Capture an explicit request to be reminded or notified. Timing and linkage to an existing task are collected by the reminder channel."},
		{"note", "notes", "Capture information, observations or reference material to retain, without an action or notification as the primary intent."},
		{"clarify", "", "The primary intent is unclear, or several independent actions cannot be represented by one capture. Ask the user before routing."},
		{"unsupported", "", "The request needs a capability not implemented here: search, editing/deleting existing records, external actions, or unrelated conversation."},
	}
}

type Request struct {
	Version      string `json:"version"`
	RequestID    string `json:"request_id"`
	Text         string `json:"text"`
	Timezone     string `json:"timezone"`
	ContextQuery string `json:"context_query"`
	Fixture      string `json:"fixture"`
}
type State struct {
	Text             string                   `json:"text"`
	Timezone         string                   `json:"timezone"`
	ReferenceAt      string                   `json:"reference_at"`
	Context          []provider.ContextRecord `json:"context"`
	ContextTruncated bool                     `json:"context_truncated"`
}
type Choice struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}
type Usage struct {
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	CostUSD      string `json:"cost_usd,omitempty"`
}
type Evaluation struct {
	Decision    Choice `json:"decision"`
	Usage       Usage  `json:"usage"`
	Model       string `json:"model"`
	CacheHit    bool   `json:"cache_hit"`
	ModelCalls  int    `json:"model_calls"`
	EvaluatedAt string `json:"evaluated_at,omitempty"`
}
type Evaluator interface {
	Evaluate(context.Context, State, []Action, string) (Evaluation, error)
}
type Policy struct {
	MinProbability float64 `json:"min_probability"`
	MinMargin      float64 `json:"min_margin"`
}
type Result struct {
	Version         string     `json:"version"`
	RegistryVersion string     `json:"registry_version"`
	RequestID       string     `json:"request_id"`
	Provider        string     `json:"provider"`
	Mock            bool       `json:"mock"`
	State           string     `json:"state"`
	SelectedChannel string     `json:"selected_channel"`
	Input           State      `json:"input"`
	Evaluation      Evaluation `json:"evaluation"`
	Policy          Policy     `json:"policy"`
	RoutingToken    string     `json:"routing_token,omitempty"`
	Persisted       bool       `json:"persisted"`
}

func ValidateRequest(r Request) error {
	if r.Version != Version || r.RequestID == "" || len(r.RequestID) > 128 || !utf8.ValidString(r.Text) || strings.TrimSpace(r.Text) == "" || len(r.Text) > 12000 || len([]rune(r.ContextQuery)) > 200 || !utf8.ValidString(r.ContextQuery) || r.Timezone == "" || r.Timezone == "Local" {
		return ErrRequest
	}
	if _, err := time.LoadLocation(r.Timezone); err != nil {
		return ErrRequest
	}
	return nil
}

func ValidateChoice(c Choice, actions []Action) error {
	if c.Type != "choice" || len(c.Probabilities) != len(actions) || !finiteProbability(c.Confidence) {
		return ErrContract
	}
	sum := 0.0
	selected, ok := c.Probabilities[c.Choice]
	if !ok {
		return ErrContract
	}
	for _, a := range actions {
		p, ok := c.Probabilities[a.ID]
		if !ok || !finiteProbability(p) || p > selected {
			return ErrContract
		}
		sum += p
	}
	if math.Abs(sum-1) > 0.001 {
		return ErrContract
	}
	return nil
}
func finiteProbability(p float64) bool {
	return !math.IsNaN(p) && !math.IsInf(p, 0) && p >= 0 && p <= 1
}

// Decide uses a visible, provisional policy. Probabilities are routing evidence,
// not a guarantee of accuracy or a permission to execute tools.
func Decide(req Request, state State, e Evaluation, name string, mock bool) (Result, error) {
	policy := Policy{0.65, 0.15}
	r := Result{Version: Version, RegistryVersion: RegistryVersion, RequestID: req.RequestID, Provider: name, Mock: mock, Input: state, Evaluation: e, Policy: policy, State: "needs_clarification"}
	if err := ValidateChoice(e.Decision, Registry()); err != nil {
		return r, err
	}
	if e.Usage.InputTokens < 0 || e.Usage.OutputTokens < 0 || e.Model == "" {
		return r, ErrContract
	}
	if e.Decision.Choice == "unsupported" {
		r.State = "unsupported"
		return r, nil
	}
	second := 0.0
	for id, p := range e.Decision.Probabilities {
		if id != e.Decision.Choice && p > second {
			second = p
		}
	}
	winning := e.Decision.Probabilities[e.Decision.Choice]
	if e.Decision.Choice == "clarify" || winning < policy.MinProbability || winning-second < policy.MinMargin {
		return r, nil
	}
	for _, a := range Registry() {
		if a.ID == e.Decision.Choice {
			r.SelectedChannel = a.Channel
		}
	}
	r.State = "routed"
	return r, nil
}
