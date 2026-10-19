package httpapi

import (
	"atlas/internal/interpret"
	"atlas/internal/routing"
	"atlas/internal/store"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// A workflow records every explicit action before any step is routed. Each
// step uses the existing conversation machinery and its own write review.
type workflowPlan struct {
	Steps []string `json:"steps"`
	Index int      `json:"index"`
}

var workflowActionStart = regexp.MustCompile(`(?i)^(?:(?:please|hey)\s+|(?:can|could|would)\s+you\s+|i\s+want\s+to\s+)*\b(?:add|create|make|save|note|remind|schedule|find|show|list|search|edit|update|move|rename|complete|finish|dismiss|reopen|delete|remove|clear|link|unlink|attach|detach|relate|unrelate|change|send|email|text|post)\b`)
var workflowConnector = regexp.MustCompile(`(?i)\b(?:and|then|also)\s+(?:(?:then|also|please)\s+)*(?:add|create|make|save|remind|schedule|find|show|list|search|edit|update|move|rename|complete|finish|dismiss|reopen|delete|remove|clear|link|unlink|attach|detach|relate|unrelate|change|send|email|text|post)\b`)
var atomicTaskStart = regexp.MustCompile(`(?i)^(?:(?:please|hey)\s+|(?:can|could|would)\s+you\s+)*(?:(?:add|create|make|save)\s+(?:a\s+|the\s+|my\s+)?task\b|i\s+(?:need|have|want)\s+to\b)`)
var atomicTaskAttachment = regexp.MustCompile(`(?i)(?:\band\s+|,\s*)(?:remind\s+me\b|note\s+that\b|add\s+(?:a\s+)?(?:reminder|note)\b)`)
var workflowPunctuationAction = regexp.MustCompile(`(?i)[.;,!?]\s*(?:(?:and|then|also|please)\s+)*(?:add|create|make|save|remind|schedule|find|show|list|search|edit|update|move|rename|complete|finish|dismiss|reopen|delete|remove|clear|link|unlink|attach|detach|relate|unrelate|change|send|email|text|post)\b`)

func workflowClauses(text string) []string {
	text = strings.TrimSpace(text)
	if !workflowActionStart.MatchString(text) {
		return nil
	}
	if _, explicitNote := interpret.ExplicitNoteBody(text); explicitNote {
		return nil
	}
	// A task with a reminder or note is already one atomic capture. Splitting
	// those attachment clauses would lose the task context and its time.
	if atomicTaskStart.MatchString(text) && atomicTaskAttachment.MatchString(text) {
		return nil
	}
	parts := []string{}
	for _, segment := range strings.FieldsFunc(text, func(r rune) bool { return r == ';' || r == '.' || r == '!' || r == '?' || r == ',' }) {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		matches := workflowConnector.FindAllStringIndex(segment, -1)
		start := 0
		for _, match := range matches {
			before := strings.TrimSpace(segment[start:match[0]])
			if before != "" {
				parts = append(parts, before)
			}
			connector := segment[match[0]:match[1]]
			verb := strings.Fields(connector)
			start = match[1] - len(verb[len(verb)-1])
		}
		parts = append(parts, strings.TrimSpace(segment[start:]))
	}
	if len(parts) < 2 {
		return nil
	}
	for _, part := range parts {
		if !workflowActionStart.MatchString(part) {
			return nil
		}
	}
	return parts
}

func possibleWorkflow(text string) bool {
	if _, explicitNote := interpret.ExplicitNoteBody(text); explicitNote {
		return false
	}
	if atomicTaskStart.MatchString(text) && atomicTaskAttachment.MatchString(text) {
		return false
	}
	if strings.Contains(text, ";") {
		return true
	}
	return workflowConnector.MatchString(text) || workflowPunctuationAction.MatchString(text)
}

func unresolvedWorkflowRoute(service routingService, name string, input routing.Request) routing.Result {
	result := localBatchRoute(service, name, input)
	result.State = "needs_clarification"
	result.SelectedChannel = ""
	result.SelectedTool = ""
	result.Category = "compound"
	result.ExtractionModel = "local_workflow_scope"
	result.Plan = &routing.ActionPlan{Action: "clarify", Clarification: "I see more than one possible action, but I can't safely separate every step. Please restate one action at a time."}
	result.RoutingToken = service.sign(result)
	return result
}

func workflowRoute(service routingService, name string, input routing.Request, steps []string) routing.Result {
	result := localBatchRoute(service, name, input)
	result.State = "needs_clarification"
	result.SelectedChannel = ""
	result.SelectedTool = ""
	result.Category = "compound"
	result.ExtractionModel = "local_workflow_scope"
	result.Plan = &routing.ActionPlan{Action: "clarify", Clarification: "I found " + strconv.Itoa(len(steps)) + " separate actions. Review the ordered steps before I start."}
	result.RoutingToken = service.sign(result)
	return result
}

func (c *routingConversation) workflowProgress(result string) string {
	if c.Workflow == nil {
		return result
	}
	if len(c.Warnings) > 0 {
		result += " " + c.Warnings[len(c.Warnings)-1]
	}
	if c.Workflow.Index+1 < len(c.Workflow.Steps) {
		return result + " Step " + strconv.Itoa(c.Workflow.Index+1) + " of " + strconv.Itoa(len(c.Workflow.Steps)) + " is complete. Say continue to start: " + c.Workflow.Steps[c.Workflow.Index+1] + ". Or say cancel to stop the remaining steps."
	}
	return result + " All " + strconv.Itoa(len(c.Workflow.Steps)) + " planned steps are complete."
}

func workflowMockFixture(step string) string {
	words := strings.Fields(strings.ToLower(step))
	for len(words) > 0 && (words[0] == "please" || words[0] == "then" || words[0] == "also") {
		words = words[1:]
	}
	if len(words) > 0 {
		switch words[0] {
		case "find", "show", "list", "search":
			return "lookup"
		case "edit", "update", "move", "rename", "complete", "finish", "dismiss", "change":
			return "edit"
		case "delete", "remove", "clear":
			return "delete"
		}
	}
	for _, word := range words {
		switch strings.Trim(word, ".,!?;") {
		case "task", "tasks":
			return "task"
		case "reminder", "reminders", "remind":
			return "reminder"
		case "note", "notes":
			return "note"
		}
	}
	return "clarify"
}

func (c *routingConversation) startWorkflowStep(r *http.Request, s *store.Store, service routingService, sessionID string, version int) error {
	if c.Workflow == nil || c.Workflow.Index >= len(c.Workflow.Steps) {
		return routing.ErrContract
	}
	step := c.Workflow.Steps[c.Workflow.Index]
	fixture := ""
	if c.Route.Provider == "mock" {
		fixture = workflowMockFixture(step)
	}
	result, err := evaluateRouting(r.Context(), s, service, c.Route.Provider, routing.Request{Version: routing.Version, RequestID: sessionID + ".step." + strconv.Itoa(c.Workflow.Index) + "." + strconv.Itoa(version), Text: step, Timezone: c.Route.Input.Timezone, RecentConversation: recentConversation(c.Messages), Fixture: fixture})
	if err != nil {
		return err
	}
	// Reviewing the ordered plan does not authorize automatic creation.
	result.Category = ""
	result.RoutingToken = service.sign(result)
	c.Fixture = fixture
	return c.beginRoute(r, s, result)
}
