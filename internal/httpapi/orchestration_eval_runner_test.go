package httpapi

import (
	"atlas/internal/store"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestOfflineOrchestrationEvalReport(t *testing.T) {
	suite, err := LoadOrchestrationEvalSuite()
	if err != nil || len(suite.Cases) < 12 {
		t.Fatal(err, len(suite.Cases))
	}
	report, err := RunOfflineOrchestrationEval(t.Context())
	if err != nil || report.Total != len(suite.Cases) || report.Passed+report.Gaps != report.Total {
		t.Fatal(err, report)
	}
	for _, c := range report.Cases {
		if c.Error != "" {
			t.Errorf("case %s: %s", c.ID, c.Error)
		}
		if len(c.Stages) == 0 || len(c.Checks) == 0 {
			t.Errorf("case %s lacks observations", c.ID)
		}
	}
}

func TestOfflineEvalWebpageAndAPI(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := Handler(s)
	for _, path := range []string{"/orchestration-eval-test", "/api/v1/orchestration-evals"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("%s: HTTP %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/orchestration-evals/offline", nil))
	var report OrchestrationEvalReport
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &report) != nil || report.Total < 12 {
		t.Fatal(w.Code, w.Body.String())
	}
	tasks, err := s.Tasks(t.Context())
	if err != nil || len(tasks) != 0 {
		t.Fatal("eval changed app database", tasks, err)
	}
}
