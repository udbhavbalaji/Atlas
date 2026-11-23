package httpapi

import (
	"atlas/internal/provider"
	"atlas/internal/routing"
	"atlas/internal/store"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Live evals are deliberately separate from normal tests and the webpage.
// The caller must provide keys and an explicit request budget. No provider
// fallback or retry is configured here.
type LiveOrchestrationEvalOptions struct {
	Mode          string
	JevAPIKey     string
	GroqAPIKey    string
	MaxRequests   int
	JevEndpoint   string // for simulated-HTTP tests
	GroqEndpoint  string // for simulated-HTTP tests
	HTTPTransport http.RoundTripper
}

var errEvalBudget = errors.New("live evaluation request budget exhausted")

type evalBudgetTransport struct {
	mu         sync.Mutex
	used       int
	max        int
	byProvider map[string]int
	upstream   http.RoundTripper
}

type evalProviderTransport struct {
	budget   *evalBudgetTransport
	provider string
}

func (p evalProviderTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	b := p.budget
	b.mu.Lock()
	if b.used >= b.max {
		b.mu.Unlock()
		return nil, errEvalBudget
	}
	b.used++
	b.byProvider[p.provider]++
	b.mu.Unlock()
	return b.upstream.RoundTrip(r)
}

func (b *evalBudgetTransport) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

func (b *evalBudgetTransport) countProvider(provider string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.byProvider[provider]
}

func RunLiveOrchestrationEval(ctx context.Context, opts LiveOrchestrationEvalOptions) (OrchestrationEvalReport, error) {
	if opts.Mode != "jev" && opts.Mode != "groq" && opts.Mode != "mixed" {
		return OrchestrationEvalReport{}, errors.New("live mode must be jev, groq, or mixed")
	}
	if opts.MaxRequests < 1 || opts.MaxRequests > 20 {
		return OrchestrationEvalReport{}, errors.New("live request budget must be between 1 and 20")
	}
	if (opts.Mode == "jev" || opts.Mode == "mixed") && opts.JevAPIKey == "" {
		return OrchestrationEvalReport{}, errors.New("Jev key is required for this live mode")
	}
	if (opts.Mode == "groq" || opts.Mode == "mixed") && opts.GroqAPIKey == "" {
		return OrchestrationEvalReport{}, errors.New("Groq key is required for this live mode")
	}
	upstream := opts.HTTPTransport
	if upstream == nil {
		upstream = http.DefaultTransport
	}
	budget := &evalBudgetTransport{max: opts.MaxRequests, byProvider: map[string]int{}, upstream: upstream}
	newClient := func(provider string) *http.Client {
		return &http.Client{Transport: evalProviderTransport{budget: budget, provider: provider}, Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	jev := routing.Jev{APIKey: opts.JevAPIKey, Endpoint: opts.JevEndpoint, Client: newClient("jev")}
	groq := routing.FreePlanner{APIKey: opts.GroqAPIKey, Endpoint: opts.GroqEndpoint, Client: newClient("groq"), Service: "groq"}
	report := OrchestrationEvalReport{Version: 1, Mode: opts.Mode, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Cases: []OrchestrationEvalCaseResult{}}
	switch opts.Mode {
	case "jev":
		report.Models = []string{routing.OpenRouterModel}
	case "groq":
		report.Models = []string{routing.GroqModel}
	case "mixed":
		report.Models = []string{routing.OpenRouterModel, routing.GroqModel}
	}
	add := func(c OrchestrationEvalCaseResult) {
		report.Cases = append(report.Cases, c)
		if c.Passed {
			report.Passed++
		} else {
			report.Gaps++
		}
	}
	switch opts.Mode {
	case "jev":
		add(runLiveJevRouting(ctx, jev, "jev/reminder-note-tool", "Remind me to call Rahul and make a note that we need to discuss the battery issue", "create", "reminder", false, budget))
		add(runLiveJevRouting(ctx, jev, "jev/edit-task-tool", "Move my interview with Maya to Friday at 7 PM", "change", "edit_task", true, budget))
		add(runLiveJevRouting(ctx, jev, "jev/unsupported-local-guard", "Send an email to Rahul", "local_unsupported", "", false, budget))
	case "groq":
		add(runLiveGroqPlan(ctx, groq, "groq/reminder-note", routing.State{Text: "Remind me to call Rahul on 2035-01-01 at 10 AM and make a note that we need to discuss the battery issue", Timezone: "UTC", ReferenceAt: "2034-12-30T12:00:00Z", SelectedTool: "reminder"}, "reminder", "reminder", true, budget))
		add(runLiveGroqPlan(ctx, groq, "groq/edit-task", routing.State{Text: "Rename my interview with Maya to Interview with Maya and Rahul", Timezone: "UTC", ReferenceAt: "2034-12-30T12:00:00Z", SelectedTool: "edit_task", Context: []provider.ContextRecord{{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Kind: "task", Title: "Interview with Maya"}}}, "edit", "task", false, budget))
	case "mixed":
		add(runLiveMixedCase(ctx, jev, groq, "mixed/reminder-note", "Remind me to call Rahul on 2035-01-01 at 10 AM and make a note that we need to discuss the battery issue", false, budget))
		add(runLiveMixedCase(ctx, jev, groq, "mixed/lookup", "When is my interview with Maya?", true, budget))
	}
	report.Requests = budget.count()
	report.finish()
	return report, nil
}

type evalChoiceRecorder struct {
	delegate routing.Evaluator
	choices  []string
	usage    routing.Usage
}

func (r *evalChoiceRecorder) Evaluate(ctx context.Context, state routing.State, actions []routing.Action, fixture string) (routing.Evaluation, error) {
	value, err := r.delegate.Evaluate(ctx, state, actions, fixture)
	if err == nil {
		r.choices = append(r.choices, value.Decision.Choice)
		r.usage.InputTokens += value.Usage.InputTokens
		r.usage.OutputTokens += value.Usage.OutputTokens
	}
	return value, err
}

func runLiveJevRouting(ctx context.Context, jev routing.Jev, id, request, wantCategory, wantTool string, seedTask bool, budget *evalBudgetTransport) (result OrchestrationEvalCaseResult) {
	started := time.Now()
	result = OrchestrationEvalCaseResult{ID: id, Slice: "live/jev", Passed: true, Checks: []OrchestrationEvalCheck{}, Stages: []OrchestrationEvalObservation{}}
	defer func() { result.DurationMS = time.Since(started).Milliseconds() }()
	dir, err := os.MkdirTemp("", "atlas-jev-eval-")
	if err != nil {
		result.Error, result.Passed = err.Error(), false
		return
	}
	defer os.RemoveAll(dir)
	s, err := store.Open(filepath.Join(dir, "eval.db"))
	if err != nil {
		result.Error, result.Passed = err.Error(), false
		return
	}
	defer s.Close()
	if seedTask {
		if _, err = s.CreateWithFields(ctx, "Interview with Maya", "", "2035-01-01T10:00:00Z"); err != nil {
			result.Error, result.Passed = err.Error(), false
			return
		}
	}
	recorder := &evalChoiceRecorder{delegate: &routing.Cached{Provider: jev}}
	service := routingService{jev: recorder, free: &evalPlannerScript{fail: true}, configured: true, secret: "live-jev-eval"}
	before := budget.countProvider("jev")
	route, err := evaluateRouting(ctx, s, service, "jev", routing.Request{Version: routing.Version, RequestID: id, Text: request, Timezone: "UTC"})
	result.DecisionCalls = budget.countProvider("jev") - before
	if err != nil {
		result.Error = err.Error()
		result.Passed = false
		return
	}
	result.InputTokens, result.OutputTokens = recorder.usage.InputTokens, recorder.usage.OutputTokens
	result.DecisionChoices = append([]string(nil), recorder.choices...)
	operationCount := 0
	if route.SelectedTool != "" {
		operationCount = 1
	}
	result.Stages = append(result.Stages, OrchestrationEvalObservation{State: route.State, Tool: route.SelectedTool, OperationCount: operationCount})
	if wantCategory == "local_unsupported" {
		result.check("unsupported state", "unsupported", route.State)
		result.check("no Jev request", 0, result.DecisionCalls)
		return
	}
	category, tool := "", ""
	if len(recorder.choices) > 0 {
		category = recorder.choices[0]
	}
	if len(recorder.choices) > 1 {
		tool = recorder.choices[1]
	}
	result.check("category", wantCategory, category)
	result.check("tool", wantTool, tool)
	if wantTool != "" {
		result.Intent = []OrchestrationEvalIntent{{Tool: wantTool}}
	}
	return
}

func runLiveGroqPlan(ctx context.Context, groq routing.FreePlanner, id string, state routing.State, wantAction, wantKind string, wantNote bool, budget *evalBudgetTransport) (result OrchestrationEvalCaseResult) {
	started := time.Now()
	result = OrchestrationEvalCaseResult{ID: id, Slice: "live/groq", Passed: true, Checks: []OrchestrationEvalCheck{}, Stages: []OrchestrationEvalObservation{}}
	result.Intent = []OrchestrationEvalIntent{{Tool: state.SelectedTool}}
	defer func() { result.DurationMS = time.Since(started).Milliseconds() }()
	before := budget.countProvider("groq")
	plan, err := groq.Plan(ctx, state)
	result.PlannerCalls = budget.countProvider("groq") - before
	if err != nil {
		result.Error = err.Error()
		result.Passed = false
		return
	}
	result.InputTokens, result.OutputTokens = plan.Usage.InputTokens, plan.Usage.OutputTokens
	result.check("action", wantAction, plan.Plan.Action)
	result.check("kind", wantKind, plan.Plan.Kind)
	if wantNote {
		result.check("attached note retained", true, plan.Plan.NoteBody != "")
	}
	return
}

func runLiveMixedCase(ctx context.Context, jev routing.Jev, groq routing.FreePlanner, id, request string, lookup bool, budget *evalBudgetTransport) (result OrchestrationEvalCaseResult) {
	started := time.Now()
	result = OrchestrationEvalCaseResult{ID: id, Slice: "live/mixed", Passed: true, Checks: []OrchestrationEvalCheck{}, Stages: []OrchestrationEvalObservation{}}
	if lookup {
		result.Intent = []OrchestrationEvalIntent{{Tool: "lookup_task"}}
	} else {
		result.Intent = []OrchestrationEvalIntent{{Tool: "reminder", Attached: []string{"note"}}}
	}
	defer func() { result.DurationMS = time.Since(started).Milliseconds() }()
	dir, err := os.MkdirTemp("", "atlas-live-eval-")
	if err != nil {
		result.Error, result.Passed = err.Error(), false
		return
	}
	defer os.RemoveAll(dir)
	s, err := store.Open(filepath.Join(dir, "eval.db"))
	if err != nil {
		result.Error, result.Passed = err.Error(), false
		return
	}
	defer s.Close()
	if lookup {
		if _, err = s.CreateWithFields(ctx, "Interview with Maya", "", "2035-01-01T10:00:00Z"); err != nil {
			result.Error, result.Passed = err.Error(), false
			return
		}
	}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{jev: jev, free: groq, configured: true, secret: "live-eval"})
	beforeJev, beforeGroq := budget.countProvider("jev"), budget.countProvider("groq")
	code, body, err := evalSend(ctx, mux, "/api/v1/conversations/routing", map[string]any{"provider": "jev", "version": routing.Version, "request_id": id, "text": request, "timezone": "UTC"})
	result.DecisionCalls = budget.countProvider("jev") - beforeJev
	result.PlannerCalls = budget.countProvider("groq") - beforeGroq
	if err != nil || code != http.StatusCreated {
		result.Error = fmt.Sprintf("conversation HTTP %d: %v %v", code, err, body["error"])
		result.Passed = false
		return
	}
	observed, err := evalObserve(ctx, s, body)
	if err != nil {
		result.Error, result.Passed = err.Error(), false
		return
	}
	result.Stages = append(result.Stages, observed)
	if lookup {
		result.check("answered", "answered", observed.State)
		result.check("no lookup writes", OrchestrationEvalCounts{Tasks: 1}, observed.Counts)
		result.check("answer grounded in saved record", true, strings.Contains(observed.Prompt, "Interview with Maya") && strings.Contains(observed.Prompt, "2035"))
	} else {
		result.check("combined capture requires review", "awaiting_confirmation", observed.State)
		result.check("reminder selected", "reminder", observed.Tool)
		result.check("no write before review", OrchestrationEvalCounts{}, observed.Counts)
		input := map[string]any{}
		if proposal, ok := body["proposal"].(map[string]any); ok {
			input, _ = proposal["input"].(map[string]any)
		}
		noteBody, _ := input["note_body"].(string)
		reminderAt, _ := input["reminder_at"].(string)
		result.check("attached note retained", true, strings.TrimSpace(noteBody) != "")
		result.check("reminder time retained", true, strings.HasPrefix(reminderAt, "2035-01-01T10:00:00"))
	}
	if route, ok := body["route"].(map[string]any); ok {
		if evaluation, ok := route["evaluation"].(map[string]any); ok {
			if usage, ok := evaluation["usage"].(map[string]any); ok {
				result.InputTokens, result.OutputTokens = intFromJSON(usage["input_tokens"]), intFromJSON(usage["output_tokens"])
			}
		}
	}
	return
}

func intFromJSON(value any) int {
	if number, ok := value.(float64); ok {
		return int(number)
	}
	return 0
}
