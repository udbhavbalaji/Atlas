package interpret

import (
	"atlas/internal/store"
	"context"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var beforeClause = regexp.MustCompile(`(?i)\s+(?:before|ahead of)\s+(.+)$`)
var pronouns = map[string]bool{"that": true, "it": true, "this": true, "that task": true, "this task": true}

func contextTokens(value string) []string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(strings.ReplaceAll(value, "'s", ""), "’s", "")
	words := strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	ignored := map[string]bool{"my": true, "the": true, "a": true, "an": true, "at": true, "for": true, "to": true, "task": true, "with": true, "of": true}
	tokens := []string{}
	for _, w := range words {
		if !ignored[w] {
			tokens = append(tokens, w)
		}
	}
	return tokens
}
func matchesTask(query string, t store.Task) bool {
	needle := contextTokens(query)
	if len(needle) == 0 {
		return false
	}
	hay := map[string]bool{}
	for _, word := range contextTokens(t.Title) {
		hay[word] = true
	}
	for _, word := range needle {
		if !hay[word] {
			return false
		}
	}
	return true
}

// InterpretWithContext resolves only explicit before/ahead-of references. A
// selected context is user input, never implicit conversational memory.
func InterpretWithContext(ctx context.Context, s *store.Store, text, zone, selected string, at time.Time) (Result, error) {
	initial, e := Interpret(text, zone, at)
	if e != nil {
		return initial, e
	}
	command := regexp.MustCompile(`(?i)^(?:can|could|would) you (?:please )?`).ReplaceAllString(strings.TrimSpace(text), "")
	if notePrefix.MatchString(command) {
		return initial, nil
	}
	action := command
	tail := ""
	if m := noteClause.FindStringSubmatchIndex(action); m != nil {
		tail = action[m[0]:]
		action = strings.TrimSpace(action[:m[0]])
	}
	m := beforeClause.FindStringSubmatchIndex(action)
	if m == nil {
		if regexp.MustCompile(`(?i)\s+(?:before|ahead of)[ .]*$`).MatchString(action) {
			initial.Status = "needs_clarification"
			initial.Proposal = nil
			initial.Draft.PreviewID = ""
			initial.Questions = append(initial.Questions, Question{"before_task_id", "missing_context", "Which existing task should this happen before? Include its title or say 'before that' and choose a context task."})
		}
		return initial, nil
	}
	query := strings.Trim(strings.TrimSpace(action[m[2]:m[3]]), ".;,\" ")
	if start := timingStart.FindStringIndex(query); start != nil && start[0] == 0 {
		return initial, nil
	}
	// Explicit reminders after the reference stay independent of its deadline.
	extra := ""
	if split := linkedClause.FindStringIndex(query); split != nil {
		extra = query[split[0]:]
		query = strings.TrimSpace(query[:split[0]])
	}
	prefix := strings.TrimSpace(action[:m[0]])
	offset := regexp.MustCompile(`(?i)\s+(?:(?:[0-9]+|one|two|three|four|five|six|seven|a|an)\s+(?:minute|hour|day|week|month)s?)$`)
	hasOffset := offset.MatchString(prefix)
	prefix = offset.ReplaceAllString(prefix, "")
	r, e := Interpret(prefix+extra+tail, zone, at)
	if e != nil {
		return r, e
	}
	r.Source = strings.TrimSpace(text)
	r.Engine = "local-english-context-v1"
	r.ReferenceQuery = query
	r.Candidates = []store.Task{}
	if hasOffset {
		r.Questions = append(r.Questions, Question{"reminder_at", "unsupported_relative_dependency", "Relative offsets from another task are not supported yet. Enter an explicit deadline or reminder time in the editable fields; the task ordering can still be saved."})
	}
	all, e := s.Tasks(ctx)
	if e != nil {
		return r, e
	}
	candidates := []store.Task{}
	isPronoun := pronouns[strings.ToLower(query)]
	for _, t := range all {
		if t.Status == "open" && (isPronoun || matchesTask(query, t)) {
			candidates = append(candidates, t)
		}
	}
	r.Candidates = candidates
	if len(r.Candidates) > 20 {
		r.Candidates = r.Candidates[:20]
		r.CandidatesTruncated = true
	}
	var target *store.Task
	if selected != "" {
		for _, t := range candidates {
			if t.ID == selected {
				copy := t
				target = &copy
				break
			}
		}
	} else if len(candidates) == 1 {
		target = &candidates[0]
	}
	r.Proposal = nil
	r.Draft.PreviewID = ""
	r.Status = "needs_clarification"
	if target == nil {
		code, message := "context_not_found", "No open task matches that reference. Choose an existing task below or clarify its title."
		if len(candidates) > 1 {
			code, message = "ambiguous_context", "Which existing task should this happen before? Choose a match below."
		}
		if selected != "" {
			code, message = "context_mismatch", "The selected context task is unavailable or does not match the reference. Choose another match."
		}
		r.Questions = append(r.Questions, Question{"before_task_id", code, message})
		return r, nil
	}
	r.Reference = target
	r.Draft.BeforeTaskID = target.ID
	if r.Draft.Kind != "task" {
		r.Questions = append(r.Questions, Question{"kind", "dependency_requires_task", "Choose Task to save ordering. A reminder also needs an explicit delivery time; 'before' alone does not specify one."})
	}
	r.Assumptions = append(r.Assumptions, "'Before' means a prerequisite relationship. It does not copy the target deadline or create a reminder.")
	if len(r.Questions) > 0 {
		return r, nil
	}
	proposal, e := s.CapturePreview(ctx, r.Draft)
	if e != nil {
		return r, e
	}
	r.Reference = proposal.Reference
	r.Proposal = &proposal
	r.Draft = proposal.Input
	r.Status = "ready"
	return r, nil
}
