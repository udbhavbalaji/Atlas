package httpapi

import (
	"atlas/internal/store"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestConversationReportSnapshotReplayAndCorpusExport(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mux := http.NewServeMux()
	routingRoutesWithService(mux, s, routingService{secret: "reports-test"})
	reportRoutes(mux, s)
	send := batchTestSend(t, mux)
	code, c := send("/api/v1/conversations/routing", map[string]any{"provider": "mock", "fixture": "unsupported", "version": "1", "request_id": "report-conversation", "text": "Send an email", "timezone": "UTC"})
	if code != 201 || c["state"] != "unsupported" {
		t.Fatal(code, c)
	}
	id := c["id"].(string)
	version := int(c["version"].(float64))
	for _, invalid := range []map[string]any{
		{"version": version, "request_id": "é-report-1", "surface": "web", "category": "other", "expected": "Enough detail"},
		{"version": version, "request_id": "invalid-1", "surface": "external", "category": "other", "expected": "Enough detail"},
		{"version": version, "request_id": "invalid-2", "surface": "web", "category": "other", "expected": "No", "message_index": 999},
	} {
		if code, _ := send("/api/v1/conversations/routing/"+id+"/reports", invalid); code != 400 {
			t.Fatalf("invalid report accepted: %#v, HTTP %d", invalid, code)
		}
	}
	input := map[string]any{"version": version, "request_id": "report-request-1", "surface": "web", "category": "missed_action", "expected": "Offer to save a task for sending the email.", "message_index": 0}
	code, report := send("/api/v1/conversations/routing/"+id+"/reports", input)
	if code != 201 || report["session_id"] != id || int(report["session_version"].(float64)) != version {
		t.Fatal(code, report)
	}
	snapshot := report["snapshot"].(map[string]any)
	if snapshot["state"] != "unsupported" || len(snapshot["messages"].([]any)) == 0 || snapshot["provider"] != "mock" {
		t.Fatal(snapshot)
	}
	code, replay := send("/api/v1/conversations/routing/"+id+"/reports", input)
	if code != 200 || replay["id"] != report["id"] {
		t.Fatal(code, replay)
	}
	changed := map[string]any{"version": version, "request_id": "report-request-1", "surface": "web", "category": "missed_action", "expected": "A different expectation"}
	code, conflict := send("/api/v1/conversations/routing/"+id+"/reports", changed)
	if code != 409 || conflict["error"].(map[string]any)["code"] != "report_key_conflict" {
		t.Fatal(code, conflict)
	}
	stale := map[string]any{"version": version + 1, "request_id": "report-request-2", "surface": "web", "category": "other", "expected": "Another outcome"}
	code, conflict = send("/api/v1/conversations/routing/"+id+"/reports", stale)
	if code != 409 || conflict["error"].(map[string]any)["code"] != "session_version_conflict" {
		t.Fatal(code, conflict)
	}
	listResponse := httptest.NewRecorder()
	mux.ServeHTTP(listResponse, httptest.NewRequest(http.MethodGet, "/api/v1/reports", nil))
	var reports []store.ConversationReport
	if listResponse.Code != 200 || json.Unmarshal(listResponse.Body.Bytes(), &reports) != nil || len(reports) != 1 {
		t.Fatal(listResponse.Code, listResponse.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/reports/corpus.jsonl", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != 200 || !strings.Contains(response.Header().Get("Content-Type"), "application/x-ndjson") {
		t.Fatal(response.Code, response.Body.String())
	}
	var candidate map[string]any
	if err = json.Unmarshal(response.Body.Bytes(), &candidate); err != nil {
		t.Fatal(err)
	}
	if candidate["candidate_id"] != report["id"] || candidate["request_text"] != "Send an email" || candidate["expected_behavior"] != input["expected"] {
		t.Fatal(candidate)
	}
	currentResponse := httptest.NewRecorder()
	mux.ServeHTTP(currentResponse, httptest.NewRequest(http.MethodGet, "/api/v1/conversations/routing/"+id, nil))
	var current map[string]any
	if currentResponse.Code != 200 || json.Unmarshal(currentResponse.Body.Bytes(), &current) != nil || int(current["version"].(float64)) != version {
		t.Fatal("report changed the conversation", currentResponse.Code, currentResponse.Body.String())
	}
}
