package planning

import (
	"atlas/internal/routing"
	"context"
	_ "embed"
	"encoding/json"
)

//go:embed testdata/fixtures.json
var fixturesJSON []byte

type Fixture struct {
	Name         string            `json:"name"`
	Text         string            `json:"text"`
	Choices      map[string]string `json:"choices"`
	Capabilities []string          `json:"capabilities"`
	Ready        bool              `json:"ready"`
	Baseline     string            `json:"baseline"`
}

func Fixtures() []Fixture {
	var f []Fixture
	if err := json.Unmarshal(fixturesJSON, &f); err != nil {
		panic(err)
	}
	return f
}

// EvaluateQuestions is fixed-fixture evaluation, never English interpretation.
func (f Fixture) EvaluateQuestions(_ context.Context, _ any, qs map[string]routing.DecisionQuestion) (routing.BatchEvaluation, error) {
	out := routing.BatchEvaluation{Model: "fixture-not-jev", Answers: map[string]routing.Choice{}}
	for id, q := range qs {
		p := map[string]float64{}
		for choice := range q.Criteria {
			p[choice] = 0
		}
		p[f.Choices[id]] = 1
		out.Answers[id] = routing.Choice{Type: "choice", Choice: f.Choices[id], Confidence: 1, Probabilities: p}
	}
	return out, nil
}
func FixturePreview(ctx context.Context, name string) (Result, error) {
	for _, f := range Fixtures() {
		if f.Name == name {
			r, err := Preview(ctx, f, Request{f.Text, "Asia/Kolkata", "2026-10-12T12:00:00Z"})
			r.ModelCalls = 0
			return r, err
		}
	}
	return Result{}, routing.ErrRequest
}
