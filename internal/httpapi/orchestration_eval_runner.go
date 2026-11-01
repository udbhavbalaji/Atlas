package httpapi

import (
	"atlas/internal/routing"
	"atlas/internal/store"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"
)

// The corpus specifies user outcomes, not the routing states that happen to
// implement them. The small adapter below observes the current conversation
// envelope; future orchestration can keep the corpus and replace that adapter.
//
//go:embed evaldata/orchestration_v1.json
var orchestrationEvalData embed.FS

type OrchestrationEvalSuite struct {
	Version int                     `json:"version"`
	Cases   []OrchestrationEvalCase `json:"cases"`
}

type OrchestrationEvalCase struct {
	ID        string                    `json:"id"`
	Slice     string                    `json:"slice"`
	Text      string                    `json:"text"`
	Timezone  string                    `json:"timezone,omitempty"`
	Seed      []OrchestrationEvalSeed   `json:"seed,omitempty"`
	Decisions []string                  `json:"decisions,omitempty"`
	Plan      routing.ActionPlan        `json:"plan,omitempty"`
	PlanError bool                      `json:"plan_error,omitempty"`
	Intent    []OrchestrationEvalIntent `json:"intent"`
	Expect    OrchestrationEvalExpect   `json:"expect"`
}

type OrchestrationEvalSeed struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	DueAt string `json:"due_at,omitempty"`
}

type OrchestrationEvalIntent struct {
	Tool     string   `json:"tool"`
	Attached []string `json:"attached,omitempty"`
}

type OrchestrationEvalReply struct {
	Text  string `json:"text,omitempty"`
	Field string `json:"field,omitempty"`
	Value string `json:"value,omitempty"`
	State string `json:"state"`
}

type OrchestrationEvalCounts struct {
	Tasks             int `json:"tasks"`
	Reminders         int `json:"reminders"`
	Notes             int `json:"notes"`
	ReminderNoteLinks int `json:"reminder_note_links,omitempty"`
}

type OrchestrationEvalExpect struct {
	FirstState      string                    `json:"first_state"`
	FirstTool       string                    `json:"first_tool,omitempty"`
	FirstQuestion   string                    `json:"first_question,omitempty"`
	OperationCount  int                       `json:"operation_count,omitempty"`
	FirstCounts     OrchestrationEvalCounts   `json:"first_counts"`
	Replies         []OrchestrationEvalReply  `json:"replies,omitempty"`
	FinalCounts     OrchestrationEvalCounts   `json:"final_counts"`
	FinalRecords    []OrchestrationEvalRecord `json:"final_records,omitempty"`
	ContextContains []string                  `json:"context_contains,omitempty"`
	PromptContains  []string                  `json:"prompt_contains,omitempty"`
}

type OrchestrationEvalRecord struct {
	Kind   string   `json:"kind"`
	Title  string   `json:"title,omitempty"`
	Body   string   `json:"body,omitempty"`
	DueAt  string   `json:"due_at,omitempty"`
	Status string   `json:"status,omitempty"`
	Links  []string `json:"links,omitempty"`
}

type OrchestrationEvalCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Want   any    `json:"want"`
	Got    any    `json:"got"`
}

type OrchestrationEvalObservation struct {
	State          string                    `json:"state"`
	Tool           string                    `json:"tool,omitempty"`
	Question       string                    `json:"question,omitempty"`
	OperationCount int                       `json:"operation_count"`
	Counts         OrchestrationEvalCounts   `json:"counts"`
	Records        []OrchestrationEvalRecord `json:"records"`
	ContextTitles  []string                  `json:"context_titles,omitempty"`
	Prompt         string                    `json:"prompt,omitempty"`
}

type OrchestrationEvalCaseResult struct {
	ID              string                         `json:"id"`
	Slice           string                         `json:"slice"`
	Intent          []OrchestrationEvalIntent      `json:"intent"`
	DecisionChoices []string                       `json:"decision_choices,omitempty"`
	Passed          bool                           `json:"passed"`
	Checks          []OrchestrationEvalCheck       `json:"checks"`
	Stages          []OrchestrationEvalObservation `json:"stages"`
	DecisionCalls   int                            `json:"decision_calls"`
	PlannerCalls    int                            `json:"planner_calls"`
	InputTokens     int                            `json:"input_tokens,omitempty"`
	OutputTokens    int                            `json:"output_tokens,omitempty"`
	DurationMS      int64                          `json:"duration_ms"`
	Error           string                         `json:"error,omitempty"`
}

type OrchestrationEvalReport struct {
	Version       int                               `json:"version"`
	Mode          string                            `json:"mode"`
	Models        []string                          `json:"models"`
	Revision      string                            `json:"revision,omitempty"`
	GeneratedAt   string                            `json:"generated_at"`
	Total         int                               `json:"total"`
	Passed        int                               `json:"passed"`
	Gaps          int                               `json:"gaps"`
	Requests      int                               `json:"provider_requests"`
	DecisionCalls int                               `json:"decision_calls"`
	PlannerCalls  int                               `json:"planner_calls"`
	P50DurationMS int64                             `json:"p50_duration_ms"`
	P95DurationMS int64                             `json:"p95_duration_ms"`
	Slices        map[string]OrchestrationEvalSlice `json:"slices"`
	Cases         []OrchestrationEvalCaseResult     `json:"cases"`
}

type OrchestrationEvalSlice struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Gaps   int `json:"gaps"`
}

func LoadOrchestrationEvalSuite() (OrchestrationEvalSuite, error) {
	data, err := orchestrationEvalData.ReadFile("evaldata/orchestration_v1.json")
	if err != nil {
		return OrchestrationEvalSuite{}, err
	}
	var suite OrchestrationEvalSuite
	if err := json.Unmarshal(data, &suite); err != nil {
		return OrchestrationEvalSuite{}, err
	}
	if suite.Version != 1 || len(suite.Cases) == 0 {
		return OrchestrationEvalSuite{}, errors.New("unsupported or empty orchestration eval suite")
	}
	seen := map[string]bool{}
	for _, c := range suite.Cases {
		if c.ID == "" || seen[c.ID] || c.Text == "" || c.Slice == "" {
			return OrchestrationEvalSuite{}, fmt.Errorf("invalid or duplicate eval case %q", c.ID)
		}
		seen[c.ID] = true
	}
	return suite, nil
}

// RunOfflineOrchestrationEval uses fresh SQLite state and scripted model
// boundaries for every case. It never uses configured provider credentials.
func RunOfflineOrchestrationEval(ctx context.Context) (OrchestrationEvalReport, error) {
	suite, err := LoadOrchestrationEvalSuite()
	if err != nil {
		return OrchestrationEvalReport{}, err
	}
	report := OrchestrationEvalReport{Version: suite.Version, Mode: "offline-scripted", Models: []string{"scripted-jev", "scripted-extractor"}, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Total: len(suite.Cases), Cases: make([]OrchestrationEvalCaseResult, 0, len(suite.Cases))}
	for _, c := range suite.Cases {
		result := runOfflineOrchestrationCase(ctx, c)
		report.Cases = append(report.Cases, result)
		if result.Passed {
			report.Passed++
		} else {
			report.Gaps++
		}
	}
	report.finish()
	return report, nil
}

func (r *OrchestrationEvalReport) finish() {
	r.Total, r.Passed, r.Gaps, r.DecisionCalls, r.PlannerCalls = len(r.Cases), 0, 0, 0, 0
	r.Slices = map[string]OrchestrationEvalSlice{}
	durations := make([]int64, 0, len(r.Cases))
	for _, c := range r.Cases {
		slice := r.Slices[c.Slice]
		slice.Total++
		if c.Passed {
			r.Passed++
			slice.Passed++
		} else {
			r.Gaps++
			slice.Gaps++
		}
		r.Slices[c.Slice] = slice
		r.DecisionCalls += c.DecisionCalls
		r.PlannerCalls += c.PlannerCalls
		durations = append(durations, c.DurationMS)
	}
	if len(durations) == 0 {
		return
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	r.P50DurationMS = durations[(len(durations)-1)/2]
	r.P95DurationMS = durations[(95*len(durations)+99)/100-1]
}

type evalDecisionScript struct {
	choices []string
	calls   int
}

func (s *evalDecisionScript) Evaluate(ctx context.Context, state routing.State, actions []routing.Action, _ string) (routing.Evaluation, error) {
	if s.calls >= len(s.choices) {
		return routing.Evaluation{}, fmt.Errorf("unexpected Jev decision %d", s.calls+1)
	}
	choice := s.choices[s.calls]
	s.calls++
	evaluation, err := (routing.Mock{}).Evaluate(ctx, state, actions, choice)
	if err == nil {
		evaluation.Model = "scripted-jev"
		evaluation.ModelCalls = 1
	}
	return evaluation, err
}

type evalPlannerScript struct {
	plan  routing.ActionPlan
	fail  bool
	calls int
}

func (s *evalPlannerScript) Plan(context.Context, routing.State) (routing.PlanResult, error) {
	s.calls++
	if s.fail {
		return routing.PlanResult{}, routing.ErrContract
	}
	return routing.PlanResult{Plan: s.plan, Model: "scripted-extractor"}, nil
}

func runOfflineOrchestrationCase(ctx context.Context, c OrchestrationEvalCase) (result OrchestrationEvalCaseResult) {
	started := time.Now()
	result = OrchestrationEvalCaseResult{ID: c.ID, Slice: c.Slice, Intent: c.Intent, Passed: true, Stages: []OrchestrationEvalObservation{}, Checks: []OrchestrationEvalCheck{}}
	defer func() {
		result.DurationMS = time.Since(started).Milliseconds()
		if result.Error != "" {
			result.Passed = false
		}
	}()
	dir, err := os.MkdirTemp("", "atlas-orchestration-eval-")
	if err != nil {
		result.Error = err.Error()
		return
	}
	defer os.RemoveAll(dir)
	s, err := store.Open(filepath.Join(dir, "eval.db"))
	if err != nil {
		result.Error = err.Error()
		return
	}
	defer s.Close()
	for _, seed := range c.Seed {
		switch seed.Kind {
		case "task":
			_, err = s.CreateWithFields(ctx, seed.Title, "", seed.DueAt)
		default:
			err = fmt.Errorf("unsupported seed kind %q", seed.Kind)
		}
		if err != nil {
			result.Error = err.Error()
			return
		}
	}
	decisions := &evalDecisionScript{choices: c.Decisions}
	planner := &evalPlannerScript{plan: c.Plan, fail: c.PlanError}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{jev: decisions, free: planner, configured: true, secret: "offline-eval"})
	timezone := c.Timezone
	if timezone == "" {
		timezone = "UTC"
	}
	code, body, err := evalSend(ctx, mux, "/api/v1/conversations/routing", map[string]any{"provider": "jev", "version": routing.Version, "request_id": "eval-" + c.ID, "text": c.Text, "timezone": timezone})
	if err != nil || code != http.StatusCreated {
		result.Error = fmt.Sprintf("start HTTP %d: %v %v", code, err, body["error"])
		return
	}
	first, err := evalObserve(ctx, s, body)
	if err != nil {
		result.Error = err.Error()
		return
	}
	result.Stages = append(result.Stages, first)
	result.check("first state", c.Expect.FirstState, first.State)
	if c.Expect.FirstTool != "" {
		result.check("selected tool", c.Expect.FirstTool, first.Tool)
	}
	if c.Expect.FirstQuestion != "" {
		result.check("question", c.Expect.FirstQuestion, first.Question)
	}
	if c.Expect.OperationCount > 0 {
		result.check("operation count", c.Expect.OperationCount, first.OperationCount)
	}
	result.check("before replies", c.Expect.FirstCounts, first.Counts)
	for _, title := range c.Expect.ContextContains {
		result.check("context contains "+title, true, slices.Contains(first.ContextTitles, title))
	}
	for _, phrase := range c.Expect.PromptContains {
		result.check("prompt contains "+phrase, true, strings.Contains(first.Prompt, phrase))
	}
	for index, reply := range c.Expect.Replies {
		id, _ := body["id"].(string)
		request := map[string]any{"version": body["version"]}
		if reply.Field != "" {
			request["field"], request["value"] = reply.Field, reply.Value
		} else {
			request["text"] = reply.Text
		}
		code, body, err = evalSend(ctx, mux, "/api/v1/conversations/routing/"+id+"/reply", request)
		if err != nil || code != http.StatusOK {
			result.Error = fmt.Sprintf("reply %d HTTP %d: %v %v", index+1, code, err, body["error"])
			break
		}
		observed, observeErr := evalObserve(ctx, s, body)
		if observeErr != nil {
			result.Error = observeErr.Error()
			break
		}
		result.Stages = append(result.Stages, observed)
		result.check(fmt.Sprintf("reply %d state", index+1), reply.State, observed.State)
	}
	if len(result.Stages) > 0 {
		last := result.Stages[len(result.Stages)-1]
		result.check("final counts", c.Expect.FinalCounts, last.Counts)
		if len(c.Expect.FinalRecords) > 0 {
			result.check("final record facts", c.Expect.FinalRecords, last.Records)
		}
	}
	result.DecisionCalls, result.PlannerCalls = decisions.calls, planner.calls
	result.DecisionChoices = append([]string(nil), c.Decisions[:decisions.calls]...)
	result.check("scripted decision budget", true, decisions.calls <= 3)
	result.check("scripted extraction budget", true, planner.calls <= 1)
	return
}

func (r *OrchestrationEvalCaseResult) check(name string, want, got any) {
	passed := reflect.DeepEqual(want, got)
	r.Checks = append(r.Checks, OrchestrationEvalCheck{Name: name, Passed: passed, Want: want, Got: got})
	if !passed {
		r.Passed = false
	}
}

func evalSend(ctx context.Context, mux *http.ServeMux, path string, input any) (int, map[string]any, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return 0, nil, err
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequestWithContext(ctx, http.MethodPost, path, bytes.NewReader(data)))
	var out map[string]any
	if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		return w.Code, nil, err
	}
	return w.Code, out, nil
}

func evalObserve(ctx context.Context, s *store.Store, body map[string]any) (OrchestrationEvalObservation, error) {
	state, _ := body["state"].(string)
	var tool, question string
	contextTitles := []string{}
	if route, ok := body["route"].(map[string]any); ok {
		tool, _ = route["selected_tool"].(string)
		if input, ok := route["input"].(map[string]any); ok {
			if records, ok := input["context"].([]any); ok {
				for _, value := range records {
					if record, ok := value.(map[string]any); ok {
						if title, ok := record["title"].(string); ok {
							contextTitles = append(contextTitles, title)
						}
					}
				}
			}
		}
	}
	if q, ok := body["question"].(map[string]any); ok {
		question, _ = q["field"].(string)
	}
	operationCount := 0
	if workflow, ok := body["workflow"].(map[string]any); ok {
		if steps, ok := workflow["steps"].([]any); ok {
			operationCount = len(steps)
		}
	} else if tool != "" {
		operationCount = 1
	}
	tasks, err := s.Tasks(ctx)
	if err != nil {
		return OrchestrationEvalObservation{}, err
	}
	reminders, err := s.Reminders(ctx)
	if err != nil {
		return OrchestrationEvalObservation{}, err
	}
	notes, err := s.Notes(ctx)
	if err != nil {
		return OrchestrationEvalObservation{}, err
	}
	counts := OrchestrationEvalCounts{Tasks: len(tasks), Reminders: len(reminders), Notes: len(notes)}
	records := make([]OrchestrationEvalRecord, 0, len(tasks)+len(reminders)+len(notes))
	for _, task := range tasks {
		records = append(records, OrchestrationEvalRecord{Kind: "task", Title: task.Title, Body: task.Details, DueAt: evalNormalizeTime(task.DueAt), Status: task.Status})
	}
	for _, reminder := range reminders {
		records = append(records, OrchestrationEvalRecord{Kind: "reminder", Title: reminder.Title, DueAt: evalNormalizeTime(reminder.ScheduledAt), Status: reminder.Status})
	}
	for _, note := range notes {
		links := []string{}
		for _, link := range note.Links {
			links = append(links, link.TargetType)
			if link.TargetType == "reminder" {
				counts.ReminderNoteLinks++
			}
		}
		sort.Strings(links)
		records = append(records, OrchestrationEvalRecord{Kind: "note", Title: note.Title, Body: note.Body, Links: links})
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Kind != records[j].Kind {
			return records[i].Kind < records[j].Kind
		}
		if records[i].Title != records[j].Title {
			return records[i].Title < records[j].Title
		}
		return records[i].Body < records[j].Body
	})
	prompt, _ := body["prompt"].(string)
	return OrchestrationEvalObservation{State: state, Tool: tool, Question: question, OperationCount: operationCount, Counts: counts, Records: records, ContextTitles: contextTitles, Prompt: prompt}, nil
}

func evalNormalizeTime(value string) string {
	if value == "" {
		return ""
	}
	instant, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return instant.UTC().Format(time.RFC3339)
}
