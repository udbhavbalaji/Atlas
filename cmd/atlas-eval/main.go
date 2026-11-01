package main

import (
	"atlas/internal/httpapi"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

func main() {
	mode := flag.String("mode", "offline", "offline, jev, groq, or mixed")
	allowLive := flag.Bool("allow-live", false, "explicitly allow bounded provider requests")
	maxRequests := flag.Int("max-requests", 0, "maximum provider requests for a live run (default: 4 Jev, 2 Groq, 8 mixed)")
	output := flag.String("out", "", "write the JSON report to this file instead of stdout")
	baselinePath := flag.String("baseline", "", "compare this run with a previous JSON report")
	comparisonOutput := flag.String("comparison-out", "", "write paired comparison JSON to this file")
	revision := flag.String("revision", "", "label this run with a commit or branch name")
	strict := flag.Bool("strict", false, "exit with status 1 when target gaps remain")
	flag.Parse()
	var report httpapi.OrchestrationEvalReport
	var err error
	if *mode == "offline" {
		report, err = httpapi.RunOfflineOrchestrationEval(context.Background())
	} else {
		if !*allowLive {
			fmt.Fprintln(os.Stderr, "live modes require -allow-live; requests have a bounded default budget")
			os.Exit(2)
		}
		budget := *maxRequests
		if budget == 0 {
			budget = map[string]int{"jev": 4, "groq": 2, "mixed": 8}[*mode]
		}
		report, err = httpapi.RunLiveOrchestrationEval(context.Background(), httpapi.LiveOrchestrationEvalOptions{Mode: *mode, JevAPIKey: os.Getenv("OPENROUTER_API_KEY"), GroqAPIKey: os.Getenv("GROQ_API_KEY"), MaxRequests: budget})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	report.Revision = *revision
	if *baselinePath != "" {
		data, readErr := os.ReadFile(*baselinePath)
		if readErr != nil {
			fmt.Fprintln(os.Stderr, readErr)
			os.Exit(2)
		}
		var baseline httpapi.OrchestrationEvalReport
		if readErr = json.Unmarshal(data, &baseline); readErr != nil {
			fmt.Fprintln(os.Stderr, readErr)
			os.Exit(2)
		}
		comparison, compareErr := httpapi.CompareOrchestrationEvalReports(baseline, report)
		if compareErr != nil {
			fmt.Fprintln(os.Stderr, compareErr)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "paired comparison: %d improved, %d regressed, %d added, %d removed\n", len(comparison.Improved), len(comparison.Regressed), len(comparison.Added), len(comparison.Removed))
		if *comparisonOutput != "" {
			comparisonData, marshalErr := json.MarshalIndent(comparison, "", "  ")
			if marshalErr == nil {
				marshalErr = os.WriteFile(*comparisonOutput, append(comparisonData, '\n'), 0o600)
			}
			if marshalErr != nil {
				fmt.Fprintln(os.Stderr, marshalErr)
				os.Exit(2)
			}
		}
	} else if *comparisonOutput != "" {
		fmt.Fprintln(os.Stderr, "-comparison-out requires -baseline")
		os.Exit(2)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	data = append(data, '\n')
	if *output == "" {
		_, err = os.Stdout.Write(data)
	} else {
		err = os.WriteFile(*output, data, 0o600)
		if err == nil {
			fmt.Fprintf(os.Stderr, "%d/%d passed; %d target gaps; report: %s\n", report.Passed, report.Total, report.Gaps, *output)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *strict && report.Gaps > 0 {
		os.Exit(1)
	}
}
