package routing

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJevBatchWireAndMalformedAnswers(t *testing.T) {
	qs := map[string]DecisionQuestion{"a1_action": {Type: "choice", Instructions: "classify", Criteria: map[string]string{"task": "task", "note": "note"}}, "a2_action": {Type: "choice", Instructions: "classify", Criteria: map[string]string{"task": "task", "note": "note"}}}
	answer := func(choice string) Choice {
		p := map[string]float64{"task": 0, "note": 0}
		p[choice] = 1
		return Choice{"choice", choice, 1, p}
	}
	valid := map[string]any{"model": OpenRouterModel, "answers": map[string]Choice{"a1_action": answer("task"), "a2_action": answer("note")}}
	b, _ := json.Marshal(valid)
	body := string(b)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var in struct {
			Questions map[string]DecisionQuestion `json:"questions"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || len(in.Questions) != 2 || r.Header.Get("Authorization") != "Bearer test" {
			t.Error("invalid batch request")
		}
		w.Write([]byte(body))
	}))
	defer server.Close()
	j := Jev{APIKey: "test", Endpoint: server.URL}
	out, err := j.EvaluateQuestions(t.Context(), map[string]string{"text": "task and note"}, qs)
	if err != nil || calls != 1 || out.Answers["a2_action"].Choice != "note" {
		t.Fatal(out, err, calls)
	}
	for _, bad := range []string{"not json", `{"model":"x","answers":{}}`, `{"model":"x","answers":{"a1_action":42,"a2_action":{}}}`, strings.Replace(string(b), "a2_action", "a9_action", 1), strings.Repeat("x", 262145)} {
		body = bad
		before := calls
		_, err := j.EvaluateQuestions(t.Context(), nil, qs)
		if !errors.Is(err, ErrContract) || calls != before+1 {
			t.Fatal(err, calls)
		}
	}
	for _, status := range []int{429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			n := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n++; w.WriteHeader(status); w.Write([]byte("secret")) }))
			defer s.Close()
			_, err := (Jev{APIKey: "test", Endpoint: s.URL}).EvaluateQuestions(t.Context(), nil, qs)
			if !errors.Is(err, ErrUnavailable) || n != 1 || strings.Contains(err.Error(), "secret") {
				t.Fatal(err, n)
			}
		})
	}
}
