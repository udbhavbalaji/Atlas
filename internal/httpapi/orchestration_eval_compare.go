package httpapi

import (
	"fmt"
	"slices"
)

type OrchestrationEvalComparison struct {
	Mode                  string   `json:"mode"`
	BaselineRevision      string   `json:"baseline_revision,omitempty"`
	CurrentRevision       string   `json:"current_revision,omitempty"`
	BaselinePassed        int      `json:"baseline_passed"`
	CurrentPassed         int      `json:"current_passed"`
	BaselineDecisionCalls int      `json:"baseline_decision_calls"`
	CurrentDecisionCalls  int      `json:"current_decision_calls"`
	BaselinePlannerCalls  int      `json:"baseline_planner_calls"`
	CurrentPlannerCalls   int      `json:"current_planner_calls"`
	BaselineRequests      int      `json:"baseline_provider_requests"`
	CurrentRequests       int      `json:"current_provider_requests"`
	BaselineP95DurationMS int64    `json:"baseline_p95_duration_ms"`
	CurrentP95DurationMS  int64    `json:"current_p95_duration_ms"`
	Improved              []string `json:"improved"`
	Regressed             []string `json:"regressed"`
	Added                 []string `json:"added"`
	Removed               []string `json:"removed"`
}

// CompareOrchestrationEvalReports pairs case IDs. A missing case is reported
// separately; it cannot be counted as a behavioral improvement or regression.
func CompareOrchestrationEvalReports(baseline, current OrchestrationEvalReport) (OrchestrationEvalComparison, error) {
	if baseline.Version != current.Version || baseline.Mode != current.Mode {
		return OrchestrationEvalComparison{}, fmt.Errorf("eval reports use different versions or modes: %d/%s versus %d/%s", baseline.Version, baseline.Mode, current.Version, current.Mode)
	}
	if len(baseline.Models) > 0 && len(current.Models) > 0 && !slices.Equal(baseline.Models, current.Models) {
		return OrchestrationEvalComparison{}, fmt.Errorf("eval reports use different models: %v versus %v", baseline.Models, current.Models)
	}
	out := OrchestrationEvalComparison{Mode: current.Mode, BaselineRevision: baseline.Revision, CurrentRevision: current.Revision, BaselinePassed: baseline.Passed, CurrentPassed: current.Passed, BaselineDecisionCalls: baseline.DecisionCalls, CurrentDecisionCalls: current.DecisionCalls, BaselinePlannerCalls: baseline.PlannerCalls, CurrentPlannerCalls: current.PlannerCalls, BaselineRequests: baseline.Requests, CurrentRequests: current.Requests, BaselineP95DurationMS: baseline.P95DurationMS, CurrentP95DurationMS: current.P95DurationMS, Improved: []string{}, Regressed: []string{}, Added: []string{}, Removed: []string{}}
	before := map[string]bool{}
	after := map[string]bool{}
	for _, c := range baseline.Cases {
		before[c.ID] = c.Passed
	}
	for _, c := range current.Cases {
		after[c.ID] = c.Passed
		passed, ok := before[c.ID]
		if !ok {
			out.Added = append(out.Added, c.ID)
		} else if !passed && c.Passed {
			out.Improved = append(out.Improved, c.ID)
		} else if passed && !c.Passed {
			out.Regressed = append(out.Regressed, c.ID)
		}
	}
	for _, c := range baseline.Cases {
		if _, ok := after[c.ID]; !ok {
			out.Removed = append(out.Removed, c.ID)
		}
	}
	return out, nil
}
