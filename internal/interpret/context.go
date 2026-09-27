package interpret

import (
	"atlas/internal/store"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var ErrLead = errors.New("reminder_lead_minutes must be 0 (default), 15, 60, 180 or 1440")

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

// InterpretWithContext resolves explicit references and suggests related task
// context for reminder requests. Selected context is visible user input.
func InterpretWithContext(ctx context.Context, s *store.Store, text, zone, selected string, at time.Time) (Result, error) {
	return InterpretWithContextLead(ctx, s, text, zone, selected, at, 60)
}

// InterpretWithContextLead proposes a visible lead time; it never saves implicitly.
func InterpretWithContextLead(ctx context.Context, s *store.Store, text, zone, selected string, at time.Time, lead int) (Result, error) {
	if lead == 0 {
		lead = 60
	}
	if lead != 15 && lead != 60 && lead != 180 && lead != 1440 {
		return Result{}, ErrLead
	}
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
			return initial, nil
		}
		return relatedReminder(ctx, s, initial, selected, at, lead)
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
	offset := regexp.MustCompile(`(?i)\s+([0-9]+|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|a|an)\s+(minute|hour|day|week|month)s?$`)
	offsetParts := offset.FindStringSubmatch(prefix)
	hasOffset := len(offsetParts) > 0
	unsupportedOffset := !hasOffset && regexp.MustCompile(`(?i)\b(?:minutes?|hours?|days?|weeks?|months?)$`).MatchString(prefix)
	prefix = offset.ReplaceAllString(prefix, "")
	r, e := Interpret(prefix+extra+tail, zone, at)
	if e != nil {
		return r, e
	}
	r.Source = strings.TrimSpace(text)
	r.Engine = "local-english-context-v1"
	r.ReferenceQuery = query
	r.Candidates = []store.Task{}
	if unsupportedOffset {
		r.Questions = append(r.Questions, Question{"reminder_at", "unsupported_relative_dependency", "Specify a whole-number offset such as one day or two hours before the task, or enter an explicit reminder time."})
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
	if hasOffset || r.Draft.Kind == "reminder" {
		r.Draft.Kind = "task"
		r.Assumptions = append(r.Assumptions, "This reminder tracks a prerequisite task, linked to the referenced task.")
		r = proposeContextTime(r, *target, at, lead, offsetParts)
	} else {
		r.Assumptions = append(r.Assumptions, "'Before' saves prerequisite ordering without creating a reminder.")
	}
	if len(r.Questions) > 0 {
		return r, nil
	}
	proposal, e := s.CapturePreview(ctx, r.Draft)
	if e != nil {
		return r, e
	}
	if proposal.Reference.UpdatedAt != target.UpdatedAt {
		r.Questions = append(r.Questions, Question{"before_task_id", "context_lookup_changed", "The context task changed while calculating this proposal. Interpret again to refresh its timing."})
		return r, nil
	}
	r.Reference = proposal.Reference
	r.Proposal = &proposal
	r.Draft = proposal.Input
	r.Status = "ready"
	return r, nil
}

// Topic overlap suggests context for reminder actions, rather than requiring the
// complete target title. Generic action words are excluded. Suggestions remain
// visible in the review and ambiguous candidates require a user choice.
func relatedTopic(action string, t store.Task) bool {
	ignored := map[string]bool{"book": true, "get": true, "go": true, "need": true, "buy": true, "print": true, "printed": true, "prepare": true, "call": true, "send": true, "do": true, "remind": true, "me": true, "please": true, "make": true, "take": true, "ticket": true}
	tokens := func(value string) map[string]bool {
		out := map[string]bool{}
		for _, w := range contextTokens(value) {
			if strings.HasSuffix(w, "s") && len(w) > 4 {
				w = strings.TrimSuffix(w, "s")
			}
			if !ignored[w] {
				out[w] = true
			}
		}
		return out
	}
	a, b := tokens(action), tokens(t.Title)
	for w := range a {
		if b[w] {
			return true
		}
	}
	// A small explicit vocabulary bridge; this is not semantic model inference.
	return a["resume"] && b["interview"]
}
func relatedReminder(ctx context.Context, s *store.Store, r Result, selected string, at time.Time, lead int) (Result, error) {
	if r.Draft.Kind != "reminder" {
		return r, nil
	}
	all, e := s.Tasks(ctx)
	if e != nil {
		return r, e
	}
	candidates := []store.Task{}
	var target *store.Task
	for _, t := range all {
		if t.Status != "open" {
			continue
		}
		if relatedTopic(r.Draft.Title, t) || t.ID == selected {
			candidates = append(candidates, t)
			if t.ID == selected {
				copy := t
				target = &copy
			}
		}
	}
	if len(candidates) == 0 && selected == "" {
		return r, nil
	}
	if selected == "" && len(candidates) == 1 {
		target = &candidates[0]
	}
	r.Engine = "local-english-context-v2"
	r.Candidates = candidates
	r.ReferenceQuery = r.Draft.Title
	r.Proposal = nil
	r.Draft.PreviewID = ""
	r.Status = "needs_clarification"
	if len(r.Candidates) > 20 {
		r.Candidates = r.Candidates[:20]
		r.CandidatesTruncated = true
	}
	if target == nil {
		code, message := "ambiguous_context", "Several open tasks could be related. Choose the intended task or refine the sentence."
		if selected != "" {
			code, message = "context_mismatch", "The selected context task is no longer open or available."
		}
		r.Questions = append(r.Questions, Question{"before_task_id", code, message})
		return r, nil
	}
	r.Reference = target
	r.Draft.BeforeTaskID = target.ID
	r.Draft.Kind = "task"
	r.Assumptions = append(r.Assumptions, "Related context: "+target.Title+". This proposal creates a prerequisite task and a reminder linked to that new task. Change the context or edit the relationship if this is not intended.")
	r = proposeContextTime(r, *target, at, lead, nil)
	if len(r.Questions) > 0 {
		return r, nil
	}
	p, e := s.CapturePreview(ctx, r.Draft)
	if e != nil {
		return r, e
	}
	if p.Reference.UpdatedAt != target.UpdatedAt {
		r.Questions = append(r.Questions, Question{"before_task_id", "context_lookup_changed", "The context task changed while calculating this proposal. Interpret again to refresh its timing."})
		return r, nil
	}
	r.Proposal = &p
	r.Draft = p.Input
	r.Reference = p.Reference
	r.Status = "ready"
	return r, nil
}
func proposeContextTime(r Result, target store.Task, at time.Time, lead int, parts []string) Result {
	r.Engine = "local-english-context-v2"
	// Replace the generic missing-time question with the precise context reason.
	questions := []Question{}
	for _, q := range r.Questions {
		if q.Code != "missing_time" {
			questions = append(questions, q)
		}
	}
	r.Questions = questions
	if r.Draft.ReminderAt != "" {
		if len(parts) > 0 {
			r.Questions = append(r.Questions, Question{"reminder_at", "multiple_reminders", "Both an explicit reminder time and a relative offset were supplied. Choose one delivery time in the fields."})
		}
		return r
	}
	r.Draft.Timezone = r.Timezone
	if target.DueAt == "" {
		r.Questions = append(r.Questions, Question{"reminder_at", "context_missing_deadline", "Context found: " + target.Title + ". This task has no deadline, so Atlas cannot derive a reminder time. Add a deadline to it and interpret again, enter a reminder time below, or uncheck the reminder to save ordering only."})
		return r
	}
	due, e := time.Parse(time.RFC3339Nano, target.DueAt)
	if e != nil {
		return r
	}
	reminder := due.Add(-time.Duration(lead) * time.Minute)
	description := fmt.Sprintf("%d minutes", lead)
	if lead == 60 {
		description = "1 hour"
	}
	if lead == 180 {
		description = "3 hours"
	}
	if lead == 1440 && len(parts) == 0 {
		parts = []string{"", "1", "day"}
	}
	if len(parts) > 0 {
		count, e := strconv.Atoi(parts[1])
		if e != nil {
			count = map[string]int{"a": 1, "an": 1, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12}[strings.ToLower(parts[1])]
		}
		unit := strings.ToLower(parts[2])
		if count < 1 || count > 365 || unit == "month" {
			r.Questions = append(r.Questions, Question{"reminder_at", "unsupported_relative_dependency", "Use 1–365 minutes, hours, days or weeks before the task, or enter an explicit reminder time."})
			return r
		}
		if unit == "day" || unit == "week" {
			if unit == "week" {
				count *= 7
			}
			loc, _ := time.LoadLocation(r.Timezone)
			local := due.In(loc)
			y, m, d := local.Date()
			h, min, sec := local.Clock()
			calendar := time.Date(y, m, d-count, 12, 0, 0, 0, time.UTC)
			y, m, d = calendar.Date()
			matches := wallTimes(y, m, d, h, min, sec, due.Nanosecond(), loc)
			if len(matches) != 1 {
				r.Questions = append(r.Questions, Question{"reminder_at", "dst_context_time", "The relative reminder falls in a missing or repeated local time. Choose an explicit reminder time."})
				return r
			}
			reminder = matches[0]
		} else {
			duration := time.Minute
			if unit == "hour" {
				duration = time.Hour
			}
			reminder = due.Add(-time.Duration(count) * duration)
		}
		description = parts[1] + " " + parts[2]
	}
	if !reminder.After(at) {
		r.Questions = append(r.Questions, Question{"reminder_at", "context_time_past", "The proposed time before " + target.Title + " is already past. Choose a future reminder time or a shorter lead time."})
		return r
	}
	r.Draft.ReminderAt = reminder.UTC().Format(time.RFC3339Nano)
	r.Assumptions = append(r.Assumptions, "Proposed reminder: "+description+" before the deadline of "+target.Title+". Review the exact time before saving. This is a fixed time; later deadline edits do not move a saved reminder.")
	return r
}
