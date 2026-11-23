package httpapi

import "testing"

func TestCompareOrchestrationEvalReportsPairsCases(t *testing.T) {
	baseline := OrchestrationEvalReport{Version: 1, Mode: "offline-scripted", Cases: []OrchestrationEvalCaseResult{{ID: "fixed", Passed: true}, {ID: "gap", Passed: false}, {ID: "removed", Passed: true}}}
	current := OrchestrationEvalReport{Version: 1, Mode: "offline-scripted", Cases: []OrchestrationEvalCaseResult{{ID: "fixed", Passed: false}, {ID: "gap", Passed: true}, {ID: "added", Passed: true}}}
	baseline.finish()
	current.finish()
	got, err := CompareOrchestrationEvalReports(baseline, current)
	if err != nil || len(got.Improved) != 1 || got.Improved[0] != "gap" || len(got.Regressed) != 1 || got.Regressed[0] != "fixed" || len(got.Added) != 1 || got.Added[0] != "added" || len(got.Removed) != 1 || got.Removed[0] != "removed" {
		t.Fatal(err, got)
	}
	current.Mode = "groq"
	if _, err = CompareOrchestrationEvalReports(baseline, current); err == nil {
		t.Fatal("different modes were compared")
	}
}
