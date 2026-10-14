package planning

import (
	"atlas/internal/routing"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func linked(t *testing.T) Result {
	t.Helper()
	p, e := FixturePreview(t.Context(), "linked")
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func clone(p Plan) Plan { b, _ := json.Marshal(p); var c Plan; json.Unmarshal(b, &c); return c }
func TestFixturesAndSingleRouterComparison(t *testing.T) {
	for _, f := range Fixtures() {
		t.Run(f.Name, func(t *testing.T) {
			out, err := FixturePreview(t.Context(), f.Name)
			if f.Name == "malformed" {
				if !errors.Is(err, routing.ErrContract) {
					t.Fatal(err)
				}
				return
			}
			if err != nil || out.Ready != f.Ready || out.Persisted || !out.PreviewOnly || out.ModelCalls != 0 {
				t.Fatal(out, err)
			}
			caps := []string{}
			for _, a := range out.Plan.Actions {
				caps = append(caps, a.Capability)
			}
			if !reflect.DeepEqual(caps, f.Capabilities) {
				t.Fatal(caps)
			}
			e, err := (routing.Mock{}).Evaluate(t.Context(), routing.State{}, routing.Registry(), f.Baseline)
			if err != nil {
				t.Fatal(err)
			}
			baseline, err := routing.Decide(routing.Request{}, routing.State{}, e, "mock", true)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("current single-router fixture: %s/%s; preview actions: %v; ready: %v", baseline.State, baseline.SelectedChannel, caps, out.Ready)
		})
	}
}
func TestAcceptanceAndRevisionPreservesSiblings(t *testing.T) {
	out := linked(t)
	p := out.Plan
	if p.Actions[0].Arguments.DueAt == nil || p.Actions[0].Arguments.DueAt.At != "" || len(out.Issues) != 2 {
		t.Fatal(out)
	}
	ref := p.Actions[1].Arguments.ScheduledAt.Reference
	if ref == nil || ref.ActionID != "a1" || ref.Field != "due_at" || ref.OffsetMinutes != -120 {
		t.Fatal(ref)
	}
	revised, err := Revise(p, "a1", "due_at", "2026-10-13T18:00:00+05:30", "2026-10-13T18:00:00+05:30")
	if err != nil || !revised.Ready || revised.Plan.Revision != 2 || revised.ChangedActionID != "a1" || revised.ModelCalls != 0 {
		t.Fatal(revised, err)
	}
	if !reflect.DeepEqual(p.Actions[1:], revised.Plan.Actions[1:]) || p.Actions[0].Arguments.DueAt.At != "" {
		t.Fatal("revision mutated siblings or original")
	}
	// A reminder correction clears only its own relative reference.
	changed, err := Revise(revised.Plan, "a2", "scheduled_at", "2026-10-13T12:00:00Z", "2026-10-13T12:00:00Z")
	if err != nil || changed.Plan.Actions[1].Arguments.ScheduledAt.Reference != nil || len(changed.Plan.Actions[1].DependsOn) != 0 || !reflect.DeepEqual(changed.Plan.Actions[0], revised.Plan.Actions[0]) {
		t.Fatal(changed, err)
	}
}
func TestPlanRejectsMalformedAndInventedValues(t *testing.T) {
	p := linked(t).Plan
	for name, mutate := range map[string]func(*Plan){
		"unknown capability": func(p *Plan) { p.Actions[0].Capability = "task.delete" },
		"duplicate id":       func(p *Plan) { p.Actions[1].ID = "a1" },
		"missing reference":  func(p *Plan) { p.Actions[1].DependsOn = []string{"a6"} },
		"cycle":              func(p *Plan) { p.Actions[0].DependsOn = []string{"a2"} },
		"wrong type reference": func(p *Plan) {
			p.Actions[1].DependsOn = []string{"a3"}
			p.Actions[1].Arguments.ScheduledAt.Reference.ActionID = "a3"
		},
		"wrong field reference":        func(p *Plan) { p.Actions[1].Arguments.ScheduledAt.Reference.Field = "body" },
		"invented offset":              func(p *Plan) { p.Actions[1].Arguments.ScheduledAt.Reference.OffsetMinutes = -60 },
		"invented time":                func(p *Plan) { p.Actions[0].Arguments.DueAt.At = "2026-10-13T12:00:00Z" },
		"invented text":                func(p *Plan) { p.Actions[2].Arguments.Body = "secret invented note" },
		"invalid timezone":             func(p *Plan) { p.Timezone = "Local" },
		"argument mismatch":            func(p *Plan) { p.Actions[0].Arguments.Body = p.Actions[0].Source },
		"dependency and absolute time": func(p *Plan) { p.Actions[1].Arguments.ScheduledAt.At = "2026-10-13T12:00:00Z" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := clone(p)
			mutate(&bad)
			if _, err := Inspect(bad); err == nil {
				t.Fatal("accepted malformed plan")
			}
		})
	}
	// Go's JSON types reject argument type mismatches before validation.
	var a Action
	if json.Unmarshal([]byte(`{"arguments":{"title":42}}`), &a) == nil {
		t.Fatal("accepted numeric title")
	}
}
func TestAmbiguitiesRemainUnresolved(t *testing.T) {
	for _, value := range []string{"tomorrow", "2026-10-13", "03/04/2027 at 8", "2026-11-01T01:30:00", "next week", "tomorrow afternoon", "2026-02-30T12:00:00Z"} {
		out, err := Revise(linked(t).Plan, "a1", "due_at", value, value)
		if err != nil || out.Ready || out.Plan.Actions[0].Arguments.DueAt.At != "" {
			t.Fatal(value, out, err)
		}
	}
	p := linked(t).Plan
	p.Actions[0].Arguments.Title = ""
	p.Actions[2].Arguments.Body = ""
	p.Actions[0].Confidence = 0.4
	p.CoverageConfirmed = false
	out, err := Inspect(p)
	if err != nil || out.Ready || len(out.Issues) < 5 {
		t.Fatal(out, err)
	}
}
func TestRevisionTargetAndRequestLimits(t *testing.T) {
	p := linked(t).Plan
	choices := RevisionChoices(p)
	for _, target := range []string{"a1.due_at", "a3.body", "clarify"} {
		e, err := (routing.Mock{}).Evaluate(t.Context(), routing.State{}, choices, target)
		if err != nil {
			t.Fatal(err)
		}
		id, field, ok := RevisionTarget(e.Decision, p)
		if target == "clarify" {
			if ok {
				t.Fatal("unclear target accepted")
			}
		} else if !ok || id+"."+field != target {
			t.Fatal(id, field, ok)
		}
	}
	for _, tc := range []struct{ id, field, value, source string }{{"a9", "title", "x", "x"}, {"a1", "body", "x", "x"}, {"a1", "title", "invented", "said something else"}, {"a1", "title", "", ""}} {
		if _, err := Revise(p, tc.id, tc.field, tc.value, tc.source); err == nil {
			t.Fatal(tc)
		}
	}
	for _, r := range []Request{{"", "UTC", "2026-10-12T12:00:00Z"}, {"task", "Local", "2026-10-12T12:00:00Z"}, {"task", "UTC", "invalid"}, {strings.Repeat("task;", 7), "UTC", "2026-10-12T12:00:00Z"}} {
		if _, _, err := Questions(r); err == nil {
			t.Fatal(r)
		}
	}
}

type alteredFixture struct {
	Fixture
	change func(*routing.BatchEvaluation)
}

func (f alteredFixture) EvaluateQuestions(ctx context.Context, state any, q map[string]routing.DecisionQuestion) (routing.BatchEvaluation, error) {
	v, e := f.Fixture.EvaluateQuestions(ctx, state, q)
	f.change(&v)
	return v, e
}
func TestLowConfidenceAndMalformedBatch(t *testing.T) {
	f := Fixtures()[0]
	r := Request{f.Text, "UTC", "2026-10-12T12:00:00Z"}
	low := alteredFixture{f, func(v *routing.BatchEvaluation) {
		a := v.Answers["a1_action"]
		a.Confidence = 0.2
		v.Answers["a1_action"] = a
	}}
	out, err := Preview(t.Context(), low, r)
	if err != nil || out.Ready || len(out.Issues) == 0 {
		t.Fatal(out, err)
	}
	for _, change := range []func(*routing.BatchEvaluation){func(v *routing.BatchEvaluation) { delete(v.Answers, "a1_action") }, func(v *routing.BatchEvaluation) { v.Model = "" }, func(v *routing.BatchEvaluation) {
		a := v.Answers["a1_action"]
		a.Probabilities["task.create"] = -1
		v.Answers["a1_action"] = a
	}} {
		if _, err := Preview(t.Context(), alteredFixture{f, change}, r); err == nil {
			t.Fatal("malformed response accepted")
		}
	}
}

// Explicit opt-in only: at most two live requests for one chosen fixture. Never
// reads local key files; ordinary go test ./... does not incur model calls.
func TestLiveJevPlanningComparison(t *testing.T) {
	name := os.Getenv("ATLAS_LIVE_PLAN_FIXTURE")
	if name == "" {
		t.Skip("set ATLAS_LIVE_PLAN_FIXTURE and OPENROUTER_API_KEY for two live requests")
	}
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Fatal("OPENROUTER_API_KEY is required")
	}
	var f Fixture
	for _, x := range Fixtures() {
		if x.Name == name {
			f = x
		}
	}
	if f.Name == "" || name == "malformed" {
		t.Fatal("choose a valid semantic fixture; malformed output is tested locally")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	j := routing.Jev{APIKey: key}
	reference := time.Now().UTC().Format(time.RFC3339Nano)
	out, err := Preview(ctx, j, Request{f.Text, "Asia/Kolkata", reference})
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := j.Evaluate(ctx, routing.State{Text: f.Text, Timezone: "Asia/Kolkata", ReferenceAt: reference}, routing.Registry(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("baseline=%s preview=%+v usage=%+v", baseline.Decision.Choice, out.Plan.Actions, out.Usage)
	caps := []string{}
	for _, a := range out.Plan.Actions {
		caps = append(caps, a.Capability)
	}
	if !reflect.DeepEqual(caps, f.Capabilities) || out.Ready != f.Ready {
		t.Fatalf("rollout evaluation failed: capabilities=%v ready=%v issues=%v", caps, out.Ready, out.Issues)
	}
}

func TestLiveJevPlanCorrection(t *testing.T) {
	if os.Getenv("ATLAS_LIVE_PLAN_CORRECTION") != "1" {
		t.Skip("set ATLAS_LIVE_PLAN_CORRECTION=1 for one live correction decision")
	}
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Fatal("OPENROUTER_API_KEY is required")
	}
	p := linked(t).Plan
	data, _ := json.Marshal(p)
	text := "Change the task deadline to 2026-10-13T18:00:00+05:30"
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	e, err := (routing.Jev{APIKey: key}).Evaluate(ctx, routing.State{Text: text, Timezone: p.Timezone, ReferenceAt: p.ReferenceAt, RecentConversation: string(data)}, RevisionChoices(p), "")
	if err != nil {
		t.Fatal(err)
	}
	id, field, ok := RevisionTarget(e.Decision, p)
	if !ok || id != "a1" || field != "due_at" {
		t.Fatal("wrong or unresolved correction target", e.Decision)
	}
	out, err := Revise(p, id, field, "2026-10-13T18:00:00+05:30", text)
	if err != nil || !out.Ready || !reflect.DeepEqual(p.Actions[1:], out.Plan.Actions[1:]) {
		t.Fatal(out, err)
	}
	t.Logf("correction=%s usage=%+v", e.Decision.Choice, e.Usage)
}
