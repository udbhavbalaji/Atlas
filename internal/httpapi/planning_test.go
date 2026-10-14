package httpapi

import (
	"atlas/internal/planning"
	"atlas/internal/routing"
	"atlas/internal/store"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func planRequest(h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewReader(b)))
	return w
}

type previewResponse struct {
	planning.Result
	PreviewToken string `json:"preview_token"`
}

func readPreview(t *testing.T, w *httptest.ResponseRecorder) previewResponse {
	t.Helper()
	var p previewResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	return p
}
func TestPlanningFlagPageAndNoPersistence(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("ATLAS_PLAN_PREVIEW", "")
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := Handler(s)
	w := planRequest(h, "POST", "/api/v1/planning/fixtures/linked", map[string]any{})
	assertErrorCode(t, w, 404, "planning_disabled")
	w = planRequest(h, "GET", "/planning-test", nil)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), "Plan preview lab") {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, path := range []string{"/planning-test.js", "/api/v1/planning", "/api/v1/planning/fixtures"} {
		w = planRequest(h, "GET", path, nil)
		if w.Code != 200 {
			t.Fatal(path, w.Code)
		}
	}
	// Existing single-action route still works with planning disabled.
	w = planRequest(h, "POST", "/api/v1/routing/mock/evaluate", routing.Request{Version: "1", RequestID: "x", Text: "task", Timezone: "UTC", Fixture: "task"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	t.Setenv("ATLAS_PLAN_PREVIEW", "1")
	h = Handler(s)
	before := map[string]string{}
	for _, path := range []string{"/api/v1/tasks", "/api/v1/reminders", "/api/v1/notes", "/api/v1/activity", "/api/v1/deliveries"} {
		w = planRequest(h, "GET", path, nil)
		if w.Code != 200 {
			t.Fatal(path, w.Code)
		}
		before[path] = w.Body.String()
	}
	p := readPreview(t, planRequest(h, "POST", "/api/v1/planning/fixtures/linked", map[string]any{}))
	if p.Persisted || !p.PreviewOnly || p.Ready || p.ModelCalls != 0 || p.PreviewToken == "" {
		t.Fatal(p)
	}
	changed := readPreview(t, planRequest(h, "POST", "/api/v1/planning/revise", map[string]any{"preview_token": p.PreviewToken, "action_id": "a1", "field": "due_at", "value": "2026-10-13T18:00:00+05:30"}))
	if !changed.Ready || changed.Plan.Revision != 2 || !reflect.DeepEqual(p.Plan.Actions[1:], changed.Plan.Actions[1:]) {
		t.Fatal(changed)
	}
	// A preview receipt cannot authorize either existing persistence path.
	w = planRequest(h, "POST", "/api/v1/routing/dispatch", map[string]any{"version": "1", "routing_token": changed.PreviewToken})
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = planRequest(h, "POST", "/api/v1/capture/commit", map[string]any{"preview_token": changed.PreviewToken})
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = planRequest(h, "POST", "/api/v1/planning/commit", map[string]any{"preview_token": changed.PreviewToken})
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	for path, body := range before {
		w = planRequest(h, "GET", path, nil)
		if w.Body.String() != body {
			t.Fatalf("preview wrote data at %s", path)
		}
	}
}

type countedBatch struct {
	calls   int
	fixture planning.Fixture
}

func (c *countedBatch) EvaluateQuestions(ctx context.Context, state any, qs map[string]routing.DecisionQuestion) (routing.BatchEvaluation, error) {
	c.calls++
	return c.fixture.EvaluateQuestions(ctx, state, qs)
}

type countedCorrection struct {
	calls  int
	choice string
}

func (c *countedCorrection) Evaluate(ctx context.Context, state routing.State, actions []routing.Action, fixture string) (routing.Evaluation, error) {
	c.calls++
	return correctionFixture{c.choice}.Evaluate(ctx, state, actions, fixture)
}
func TestPlanningOneCallRevisionAndReceiptIsolation(t *testing.T) {
	fixture := planning.Fixtures()[2]
	batch := &countedBatch{fixture: fixture}
	correction := &countedCorrection{choice: "a1.due_at"}
	service := planningService{enabled: true, batch: batch, correction: correction, secret: "plan-test"}
	mux := http.NewServeMux()
	planningRoutesWithService(mux, service)
	p := readPreview(t, planRequest(mux, "POST", "/api/v1/planning/preview", map[string]any{"text": fixture.Text, "timezone": "Asia/Kolkata"}))
	if batch.calls != 1 || p.ModelCalls != 1 {
		t.Fatal(batch.calls, p.ModelCalls)
	}
	changed := readPreview(t, planRequest(mux, "POST", "/api/v1/planning/revise", map[string]any{"preview_token": p.PreviewToken, "text": "Change the task deadline to 2026-10-13T18:00:00+05:30"}))
	if batch.calls != 1 || correction.calls != 1 || changed.ModelCalls != 1 || !changed.Ready || !reflect.DeepEqual(p.Plan.Actions[1:], changed.Plan.Actions[1:]) {
		t.Fatal(changed, batch.calls, correction.calls)
	}
	correction.choice = "clarify"
	w := planRequest(mux, "POST", "/api/v1/planning/revise", map[string]any{"preview_token": p.PreviewToken, "text": "Change it to tomorrow"})
	assertErrorCode(t, w, 422, "plan_correction_unresolved")
	for _, body := range []map[string]any{{"preview_token": p.PreviewToken, "text": "yes"}, {"preview_token": p.PreviewToken, "text": "Change it to tomorrow", "action_id": "a1"}, {"preview_token": p.PreviewToken, "action_id": "a1", "field": "execute", "value": "yes"}} {
		before := correction.calls
		w = planRequest(mux, "POST", "/api/v1/planning/revise", body)
		if w.Code < 400 || correction.calls != before {
			t.Fatal(w.Code, correction.calls)
		}
	}
	if _, err := (planningService{secret: "other"}).verify(p.PreviewToken); err == nil {
		t.Fatal("cross-server receipt accepted")
	}
	if _, err := service.verify(p.PreviewToken + "x"); err == nil {
		t.Fatal("tamper accepted")
	}
	other := routingService{secret: service.secret}
	if _, err := other.verify(p.PreviewToken); err == nil {
		t.Fatal("plan receipt accepted by router")
	}
	expired := planningReceipt{"plan-preview-v1", p.Plan, time.Now().Add(-time.Hour).Unix()}
	data, _ := json.Marshal(expired)
	// Even a correctly signed expired receipt is rejected.
	token := signTestPlanReceipt(service.secret, data)
	if _, err := service.verify(token); err == nil {
		t.Fatal("expired accepted")
	}
}
func TestPlanningInvalidRequestsDoNotCallModel(t *testing.T) {
	b := &countedBatch{fixture: planning.Fixtures()[0]}
	m := http.NewServeMux()
	planningRoutesWithService(m, planningService{enabled: true, batch: b, secret: "test"})
	for _, body := range []map[string]any{{"text": "task", "timezone": "Local"}, {"text": "", "timezone": "UTC"}, {"text": 42, "timezone": "UTC"}, {"text": "task", "timezone": "UTC", "execute": true}} {
		w := planRequest(m, "POST", "/api/v1/planning/preview", body)
		if w.Code != 400 || b.calls != 0 {
			t.Fatal(w.Code, b.calls)
		}
	}
	w := planRequest(m, "POST", "/api/v1/planning/fixtures/malformed", map[string]any{})
	assertErrorCode(t, w, 422, "routing_evaluation_rejected")
}

func signTestPlanReceipt(secret string, b []byte) string {
	encoded := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
