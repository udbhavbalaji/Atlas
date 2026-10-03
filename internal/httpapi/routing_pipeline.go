package httpapi

import (
	"atlas/internal/provider"
	"atlas/internal/routing"
	"atlas/internal/store"
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

// The conversation path retrieves likely saved records before Jev decides the
// intent and Atlas operation. Jev can make a focused extra choice when the
// first category is unclear. The language model only supplies arguments.
func evaluateJevPipeline(ctx context.Context, s *store.Store, service routingService, input routing.Request) (routing.Result, error) {
	if service.free == nil {
		return routing.Result{}, errRoutingConfiguration
	}
	ctx, cancel := context.WithTimeout(ctx, 80*time.Second)
	defer cancel()
	state := routing.State{Text: input.Text, Timezone: input.Timezone, ReferenceAt: time.Now().UTC().Format(time.RFC3339Nano), RecentConversation: input.RecentConversation, RecentRecordID: input.RecentRecordID, RecentRecordKind: input.RecentRecordKind, Context: []provider.ContextRecord{}}
	var err error
	state.Context, state.ContextTruncated, err = pipelineContext(ctx, s, input.Text, input.RecentConversation, "", 8)
	if err != nil {
		return routing.Result{}, err
	}
	state.Context = includeRecentRecord(ctx, s, state.Context, input.RecentRecordID, input.RecentRecordKind)
	categories := []routing.Action{
		{ID: "create", Description: "Save a new task, reminder, note, or linked combination of them."},
		{ID: "read", Description: "Answer a question about saved tasks, reminders, or notes, including questions beginning when, what, where, which, or did I. Use the recent conversation to resolve references such as it, that, or my friend. Includes completed and old records."},
		{ID: "change", Description: "Change an existing task, reminder, or note, including its date, status, title, or text."},
		{ID: "remove", Description: "Delete an existing saved record."},
		{ID: "clarify", Description: "The user's intent cannot be determined from this request."},
		{ID: "unsupported", Description: "The user requests an external action or a capability Atlas does not have."},
	}
	first, err := service.jev.Evaluate(ctx, state, categories, "")
	if err != nil {
		return routing.Result{}, fmt.Errorf("category choice: %w", err)
	}
	category := pipelineChoice(first.Decision)
	if category == "clarify" {
		focused := []routing.Action{
			{ID: "read", Description: "The user asks for information from Atlas, especially when or what about an existing or recently discussed task, reminder, or note."},
			{ID: "write", Description: "The user wants Atlas to save, change, or remove a record."},
			{ID: "other", Description: "There is no understandable request to read or write an Atlas record."},
		}
		decision, focusErr := service.jev.Evaluate(ctx, state, focused, "")
		if focusErr != nil {
			return routing.Result{}, fmt.Errorf("focused intent choice: %w", focusErr)
		}
		first.ModelCalls += decision.ModelCalls
		first.Usage.InputTokens += decision.Usage.InputTokens
		first.Usage.OutputTokens += decision.Usage.OutputTokens
		switch pipelineChoice(decision.Decision) {
		case "read":
			category = "read"
		case "write":
			writes := []routing.Action{
				{ID: "create", Description: "Save a new task, reminder, or note."},
				{ID: "change", Description: "Change an existing task, reminder, or note."},
				{ID: "remove", Description: "Delete an existing saved task, reminder, or note."},
				{ID: "clarify", Description: "The requested write cannot be determined."},
			}
			writeDecision, writeErr := service.jev.Evaluate(ctx, state, writes, "")
			if writeErr != nil {
				return routing.Result{}, fmt.Errorf("focused write choice: %w", writeErr)
			}
			first.ModelCalls += writeDecision.ModelCalls
			first.Usage.InputTokens += writeDecision.Usage.InputTokens
			first.Usage.OutputTokens += writeDecision.Usage.OutputTokens
			category = pipelineChoice(writeDecision.Decision)
		}
	}
	if category == "clarify" && groundedRecentLookupKind(state) != "" {
		category = "read"
	}
	result := routing.Result{Version: routing.Version, RegistryVersion: routing.RegistryVersion, RequestID: input.RequestID, Provider: "jev", Input: state, Evaluation: first, Policy: routing.DefaultPolicy, State: "needs_clarification"}
	result.Category = category
	if category == "unsupported" {
		result.State = "unsupported"
	} else if category != "clarify" {
		tools := pipelineTools(category)
		second, toolErr := service.jev.Evaluate(ctx, state, tools, "")
		if toolErr != nil {
			return routing.Result{}, fmt.Errorf("tool choice: %w", toolErr)
		}
		result.Evaluation.ModelCalls += second.ModelCalls
		result.Evaluation.Usage.InputTokens += second.Usage.InputTokens
		result.Evaluation.Usage.OutputTokens += second.Usage.OutputTokens
		tool := pipelineChoice(second.Decision)
		if (tool == "clarify" || tool == "") && category == "read" {
			if kind := groundedRecentLookupKind(state); kind != "" {
				tool = "lookup_" + kind
			}
		}
		if tool != "clarify" && tool != "" {
			result.State = "routed"
			result.SelectedTool = tool
			result.SelectedChannel = pipelineChannel(tool)
			if result.SelectedChannel == "" {
				return routing.Result{}, routing.ErrContract
			}
			state.SelectedTool = tool
			state.Context, state.ContextTruncated, err = pipelineContext(ctx, s, input.Text, input.RecentConversation, tool, 20)
			if err != nil {
				return routing.Result{}, err
			}
			state.Context = includeRecentRecord(ctx, s, state.Context, input.RecentRecordID, input.RecentRecordKind)
			result.Input = state
			planned, planErr := service.free.Plan(ctx, state)
			if planErr != nil {
				if category == "read" {
					result.ExtractionModel = "local_lookup"
				} else if fallback, ok := localReminderPlan(state); ok {
					result.Plan = &fallback
					result.ExtractionModel = "local_parser"
				} else {
					return routing.Result{}, fmt.Errorf("argument extraction: %w", planErr)
				}
			} else if !pipelinePlanMatches(tool, planned.Plan) {
				if category == "read" {
					result.ExtractionModel = "local_lookup"
				} else if fallback, ok := localReminderPlan(state); ok {
					result.Plan = &fallback
					result.ExtractionModel = "local_parser"
				} else {
					return routing.Result{}, fmt.Errorf("extracted tool mismatch: %w", routing.ErrContract)
				}
			} else {
				planned.Plan = routing.NormalizePlanTiming(planned.Plan, state)
				// A task with a stated time should retain that time as its
				// deadline as well as its linked reminder. This keeps later
				// reschedules synchronized even when extraction only filled
				// the reminder field.
				normalizeTimedTaskPlan(tool, input.Text, &planned.Plan)
				if tool == "reminder" && planned.Plan.ReminderAt == "" {
					seed := prepareChannel(state, "reminder")
					if !seedHasDefaultTime(seed) {
						planned.Plan.ReminderAt = seed.Fields["reminder_at"]
					}
				}
				if err = routing.ValidatePlan(planned.Plan, state); err != nil {
					return routing.Result{}, err
				}
				result.Plan = &planned.Plan
				result.ExtractionModel = planned.Model
				result.Evaluation.ModelCalls++
				result.Evaluation.Usage.InputTokens += planned.Usage.InputTokens
				result.Evaluation.Usage.OutputTokens += planned.Usage.OutputTokens
			}
		}
	}
	result.RoutingToken = service.sign(result)
	return result, nil
}

// When every free extractor fails, a fully specified standalone reminder can
// still use Atlas's existing English time parser. Ambiguous times and extra
// attached content stay in the normal clarification path.
func localReminderPlan(state routing.State) (routing.ActionPlan, bool) {
	if state.SelectedTool != "reminder" || !explicitReminderRequest.MatchString(state.Text) {
		return routing.ActionPlan{}, false
	}
	seed := prepareChannel(state, "reminder")
	if seed.Fields["title"] == "" || seed.Fields["reminder_at"] == "" || seed.Fields["note_body"] != "" || seedHasDefaultTime(seed) {
		return routing.ActionPlan{}, false
	}
	plan := routing.ActionPlan{Action: "reminder", Kind: "reminder", Title: seed.Fields["title"], ReminderAt: seed.Fields["reminder_at"], Repeat: seed.Fields["repeat"]}
	if routing.ValidatePlan(plan, state) != nil {
		return routing.ActionPlan{}, false
	}
	return plan, true
}

func seedHasDefaultTime(seed channelPrefill) bool {
	for _, warning := range seed.Warnings {
		if strings.Contains(warning, "review this default") {
			return true
		}
	}
	return false
}

func groundedRecentLookupKind(state routing.State) string {
	if state.RecentRecordID == "" || !referentialLookup(state.Text) || collectionLookup(state.Text, lookupScope(state.Text)) || len(lookupTerms(state.Text)) > 0 {
		return ""
	}
	scope := lookupScope(state.Text)
	if scope != "" && scope != state.RecentRecordKind {
		return ""
	}
	for _, record := range state.Context {
		if record.ID == state.RecentRecordID && record.Kind == state.RecentRecordKind {
			return record.Kind
		}
	}
	return ""
}

func includeRecentRecord(ctx context.Context, s *store.Store, records []provider.ContextRecord, id, kind string) []provider.ContextRecord {
	if id == "" {
		return records
	}
	for _, record := range records {
		if record.ID == id && record.Kind == kind {
			return records
		}
	}
	var record provider.ContextRecord
	switch kind {
	case "task":
		v, err := s.TaskState(ctx, id)
		if err != nil {
			return records
		}
		record = provider.ContextRecord{ID: id, Kind: kind, Title: v.Task.Title, Body: lookupExcerpt(v.Task.Details), Status: v.Task.Status, DueAt: v.Task.DueAt, UpdatedAt: v.Task.UpdatedAt}
	case "reminder":
		v, err := s.ReminderState(ctx, id)
		if err != nil {
			return records
		}
		record = provider.ContextRecord{ID: id, Kind: kind, Title: v.Reminder.Title, Status: v.Reminder.Status, DueAt: v.Reminder.ScheduledAt, UpdatedAt: v.Reminder.UpdatedAt, TaskID: v.Reminder.TaskID, TaskTitle: v.Reminder.TaskTitle}
	case "note":
		v, err := s.NoteState(ctx, id)
		if err != nil {
			return records
		}
		record = provider.ContextRecord{ID: id, Kind: kind, Title: noteLabel(*v.Note), Body: lookupExcerpt(v.Note.Body), UpdatedAt: v.Note.UpdatedAt}
	default:
		return records
	}
	return append([]provider.ContextRecord{record}, records...)
}

func normalizeTimedTaskPlan(tool, text string, plan *routing.ActionPlan) {
	if tool == "task" && plan.DueAt == "" && plan.ReminderAt != "" && !strings.Contains(strings.ToLower(text), "remind") {
		plan.DueAt = plan.ReminderAt
	}
}

func pipelineChoice(choice routing.Choice) string {
	selected := choice.Probabilities[choice.Choice]
	second := 0.0
	for id, probability := range choice.Probabilities {
		if id != choice.Choice && probability > second {
			second = probability
		}
	}
	if selected < .35 || selected-second < .04 {
		return "clarify"
	}
	return choice.Choice
}

func pipelineTools(category string) []routing.Action {
	switch category {
	case "create":
		return []routing.Action{
			{ID: "task", Description: "Create a task or event; this tool can also save a linked note and reminder in one operation."},
			{ID: "reminder", Description: "Create a reminder, optionally linked to a task that already exists."},
			{ID: "note", Description: "Save a standalone note or a note linked to an existing task."},
			{ID: "clarify", Description: "The record type is genuinely unclear."},
		}
	case "read":
		return []routing.Action{
			{ID: "lookup_task", Description: "Read task information, including linked notes and reminders."},
			{ID: "lookup_reminder", Description: "Read reminders, including old, completed, and dismissed reminders."},
			{ID: "lookup_note", Description: "Read notes and their task links."},
			{ID: "lookup_all", Description: "Read across tasks, reminders, and notes."},
			{ID: "clarify", Description: "The requested information is genuinely unclear."},
		}
	case "change", "remove":
		verb := "edit"
		if category == "remove" {
			verb = "delete"
		}
		return []routing.Action{
			{ID: verb + "_task", Description: verb + " an existing task, including related records when the store's workflow requires it."},
			{ID: verb + "_reminder", Description: verb + " an existing reminder."},
			{ID: verb + "_note", Description: verb + " an existing note."},
			{ID: "clarify", Description: "The target record type is genuinely unclear."},
		}
	}
	return nil
}

func pipelineChannel(tool string) string {
	switch {
	case tool == "task":
		return "tasks"
	case tool == "reminder":
		return "reminders"
	case tool == "note":
		return "notes"
	case strings.HasPrefix(tool, "lookup_"):
		return "lookup"
	case strings.HasPrefix(tool, "edit_"):
		return "edit"
	case strings.HasPrefix(tool, "delete_"):
		return "delete"
	}
	return ""
}

func pipelinePlanMatches(tool string, plan routing.ActionPlan) bool {
	switch {
	case tool == "task", tool == "reminder", tool == "note":
		return plan.Action == tool && (plan.Kind == "" || plan.Kind == tool)
	case strings.HasPrefix(tool, "lookup_"):
		kind := strings.TrimPrefix(tool, "lookup_")
		return plan.Action == "lookup" && (kind == "all" || plan.Kind == "" || plan.Kind == kind)
	case strings.HasPrefix(tool, "edit_"):
		return plan.Action == "edit" && plan.Kind == strings.TrimPrefix(tool, "edit_")
	case strings.HasPrefix(tool, "delete_"):
		return plan.Action == "delete" && plan.Kind == strings.TrimPrefix(tool, "delete_")
	}
	return false
}

func pipelineContext(ctx context.Context, s *store.Store, text, recent, tool string, limit int) ([]provider.ContextRecord, bool, error) {
	all := []provider.ContextRecord{}
	tasks, err := s.Tasks(ctx)
	if err != nil {
		return nil, false, err
	}
	reminders, err := s.Reminders(ctx)
	if err != nil {
		return nil, false, err
	}
	notes, err := s.Notes(ctx)
	if err != nil {
		return nil, false, err
	}
	for _, task := range tasks {
		all = append(all, provider.ContextRecord{ID: task.ID, Kind: "task", Title: task.Title, Body: lookupExcerpt(task.Details), Status: task.Status, DueAt: task.DueAt, UpdatedAt: task.UpdatedAt})
	}
	for _, reminder := range reminders {
		all = append(all, provider.ContextRecord{ID: reminder.ID, Kind: "reminder", Title: reminder.Title, Status: reminder.Status, DueAt: reminder.ScheduledAt, UpdatedAt: reminder.UpdatedAt, TaskID: reminder.TaskID, TaskTitle: reminder.TaskTitle})
	}
	for _, note := range notes {
		record := provider.ContextRecord{ID: note.ID, Kind: "note", Title: noteLabel(note), Body: lookupExcerpt(note.Body), UpdatedAt: note.UpdatedAt}
		for _, link := range note.Links {
			if link.TargetType == "task" {
				record.TaskID, record.TaskTitle = link.TargetID, link.TargetTitle
				break
			}
		}
		all = append(all, record)
	}
	terms := contextKeywords(text, recent, all)
	preferred := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(tool, "lookup_"), "edit_"), "delete_")
	score := func(r provider.ContextRecord) int {
		n := 0
		if preferred == r.Kind {
			n += 3
		}
		body := strings.ToLower(r.Title + " " + r.Body + " " + r.TaskTitle)
		for _, term := range terms {
			if strings.Contains(body, term.word) {
				n += term.weight
			}
		}
		return n
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := score(all[i]), score(all[j])
		if a != b {
			return a > b
		}
		return all[i].UpdatedAt > all[j].UpdatedAt
	})
	if len(all) > limit {
		return all[:limit], true, nil
	}
	return all, false, nil
}

type contextKeyword struct {
	word   string
	weight int
}

// Distinctive words in the current request outrank generic verbs. Prior chat
// supplies weaker carry-over terms for pronouns and short follow-up questions.
func contextKeywords(text, recent string, records []provider.ContextRecord) []contextKeyword {
	words := func(value string) []string {
		return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	}
	stop := map[string]bool{"need": true, "want": true, "please": true, "could": true, "would": true, "should": true, "know": true, "time": true, "date": true, "due": true, "deadline": true, "schedule": true, "scheduled": true, "update": true, "change": true, "create": true, "add": true, "make": true, "set": true, "get": true, "find": true, "look": true, "right": true, "now": true, "minutes": true, "minute": true, "hours": true, "hour": true}
	weights := map[string]int{}
	for _, source := range []struct {
		value  string
		weight int
	}{{text, 12}, {recent, 3}} {
		for _, word := range words(source.value) {
			if len([]rune(word)) < 3 || lookupStopWords[word] || stop[word] {
				continue
			}
			if source.weight > weights[word] {
				weights[word] = source.weight
			}
		}
	}
	keywords := make([]contextKeyword, 0, len(weights))
	for word, weight := range weights {
		frequency := 0
		for _, record := range records {
			if strings.Contains(strings.ToLower(record.Title+" "+record.Body+" "+record.TaskTitle), word) {
				frequency++
			}
		}
		if frequency == 0 {
			continue
		}
		keywords = append(keywords, contextKeyword{word: word, weight: weight + 6/(frequency+1)})
	}
	sort.SliceStable(keywords, func(i, j int) bool {
		if keywords[i].weight != keywords[j].weight {
			return keywords[i].weight > keywords[j].weight
		}
		return len(keywords[i].word) > len(keywords[j].word)
	})
	if len(keywords) > 8 {
		keywords = keywords[:8]
	}
	return keywords
}
