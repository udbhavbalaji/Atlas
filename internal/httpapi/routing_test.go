package httpapi

import (
	"atlas/internal/routing"
	"atlas/internal/store"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRoutingAPIChannelHandoffAndConfirmedPersistence(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := Handler(s)
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	evaluate := func(fixture string) routing.Result {
		w := request("POST", "/api/v1/routing/mock/evaluate", routing.Request{Version: "1", RequestID: "request", Text: "Synthetic input", Timezone: "UTC", Fixture: fixture})
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var result routing.Result
		json.Unmarshal(w.Body.Bytes(), &result)
		return result
	}
	route := evaluate("task")
	w := request("POST", "/api/v1/routing/dispatch", dispatchRequest{Version: "1", RoutingToken: route.RoutingToken})
	var result channelResponse
	json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 200 || result.State != "needs_fields" || len(result.Questions) != 3 {
		t.Fatal(w.Code, w.Body.String())
	}
	body := dispatchRequest{Version: "1", RoutingToken: route.RoutingToken, Reminder: "add", Note: "add", Fields: store.CaptureInput{Title: "Interview at Ather", DueAt: time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339), ReminderAt: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339), NoteBody: "Prepare portfolio"}}
	w = request("POST", "/api/v1/routing/dispatch", body)
	json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 200 || result.State != "awaiting_confirmation" || result.Proposal == nil || result.Channel != "tasks" || result.Persisted {
		t.Fatal(w.Code, w.Body.String())
	}
	tasks, _ := s.Tasks(t.Context())
	if len(tasks) != 0 {
		t.Fatal("preview wrote tasks")
	}
	data, _ := json.Marshal(result.Proposal.Input)
	for i := range 2 {
		r := httptest.NewRequest("POST", "/api/v1/capture/commit", bytes.NewReader(data))
		r.Header.Set("Idempotency-Key", "routing-confirmation")
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 201-i {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	tasks, _ = s.Tasks(t.Context())
	if len(tasks) != 1 {
		t.Fatal(tasks)
	}
	state, err := s.TaskState(t.Context(), tasks[0].ID)
	if err != nil || len(state.Reminders) != 1 {
		t.Fatal(state, err)
	}
	for _, tc := range []struct {
		fixture string
		input   store.CaptureInput
	}{{"note", store.CaptureInput{NoteBody: "Gate code"}}, {"reminder", store.CaptureInput{Title: "Drink water", ReminderAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}}} {
		route = evaluate(tc.fixture)
		body = dispatchRequest{Version: "1", RoutingToken: route.RoutingToken, Fields: tc.input}
		if tc.fixture == "reminder" {
			body.Note = "skip"
		}
		w = request("POST", "/api/v1/routing/dispatch", body)
		json.Unmarshal(w.Body.Bytes(), &result)
		if w.Code != 200 || result.Proposal == nil || result.Proposal.Input.Kind != tc.fixture {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/routing/mock/evaluate", "/api/v1/routing/dispatch"} {
		w = request("GET", path, nil)
		assertErrorCode(t, w, 405, "method_not_allowed")
	}
	w = request("POST", "/api/v1/routing/jev/evaluate", routing.Request{Version: "1", RequestID: "x", Text: "Demo", Timezone: "UTC"})
	assertErrorCode(t, w, 503, "routing_unavailable")
	w = request("GET", "/routing.js", nil)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestRoutingReceiptAmbiguityAndFailures(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	service := routingService{secret: "test-only", configured: false}
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, service)
	send := func(path string, body any) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, bytes.NewReader(data)))
		return w
	}
	evaluate := func(fixture string) routing.Result {
		w := send("/api/v1/routing/mock/evaluate", routing.Request{Version: "1", RequestID: "x", Text: "Demo", Timezone: "UTC", Fixture: fixture})
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var result routing.Result
		json.Unmarshal(w.Body.Bytes(), &result)
		return result
	}
	route := evaluate("ambiguous")
	body := dispatchRequest{Version: "1", RoutingToken: route.RoutingToken, Fields: store.CaptureInput{Title: "Reviewed task"}, Reminder: "skip", Note: "skip"}
	w := send("/api/v1/routing/dispatch", body)
	assertErrorCode(t, w, 422, "routing_unresolved")
	body.Channel = "tasks"
	w = send("/api/v1/routing/dispatch", body)
	assertErrorCode(t, w, 400, "invalid_channel_selection")
	body.ReviewedChannel = true
	w = send("/api/v1/routing/dispatch", body)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	body.Fields.BeforeTaskID = "task_unseen"
	w = send("/api/v1/routing/dispatch", body)
	assertErrorCode(t, w, 400, "invalid_channel_fields")
	body.Fields.BeforeTaskID = ""
	parts := strings.Split(route.RoutingToken, ".")
	decoded, _ := base64.RawURLEncoding.DecodeString(parts[0])
	body.RoutingToken = base64.RawURLEncoding.EncodeToString(bytes.ReplaceAll(decoded, []byte("Demo"), []byte("changed"))) + "." + parts[1]
	w = send("/api/v1/routing/dispatch", body)
	assertErrorCode(t, w, 409, "routing_receipt_invalid")
	other := routingService{secret: "different-server"}
	if _, err = other.verify(route.RoutingToken); err == nil {
		t.Fatal("cross-server token accepted")
	}
	for _, fixture := range []string{"invalid", "unavailable"} {
		w = send("/api/v1/routing/mock/evaluate", routing.Request{Version: "1", RequestID: "x", Text: "Demo", Timezone: "UTC", Fixture: fixture})
		if fixture == "invalid" {
			assertErrorCode(t, w, 422, "routing_evaluation_rejected")
		} else {
			assertErrorCode(t, w, 503, "routing_unavailable")
		}
	}
	route = evaluate("unsupported")
	body.RoutingToken = route.RoutingToken
	w = send("/api/v1/routing/dispatch", body)
	assertErrorCode(t, w, 400, "invalid_channel_selection")
	route = evaluate("task")
	body = dispatchRequest{Version: "1", RoutingToken: route.RoutingToken, Reminder: "skip", Note: "skip", Fields: store.CaptureInput{Title: "Task", NoteBody: "contradiction"}}
	w = send("/api/v1/routing/dispatch", body)
	assertErrorCode(t, w, 400, "invalid_channel_fields")
	for _, raw := range []string{`{"version":"1","version":"2"}`, `{"unknown":true}`} {
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/routing/mock/evaluate", strings.NewReader(raw)))
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	tasks, _ := s.Tasks(t.Context())
	if len(tasks) != 0 {
		t.Fatal("routing failure persisted")
	}
}
