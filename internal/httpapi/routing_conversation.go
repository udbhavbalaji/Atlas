package httpapi

import (
	"atlas/internal/interpret"
	"atlas/internal/routing"
	"atlas/internal/store"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type routingConversation struct {
	State               string                 `json:"state"`
	Route               routing.Result         `json:"route"`
	Fixture             string                 `json:"fixture,omitempty"`
	Channel             string                 `json:"channel"`
	Draft               dispatchRequest        `json:"draft"`
	Question            *channelQuestion       `json:"question,omitempty"`
	Proposal            *store.CaptureProposal `json:"proposal,omitempty"`
	Warnings            []string               `json:"warnings"`
	Messages            []conversationTurn     `json:"messages"`
	Saved               *store.TaskAction      `json:"saved,omitempty"`
	Answer              *conversationAnswer    `json:"answer,omitempty"`
	Mutation            *recordMutation        `json:"mutation,omitempty"`
	Batch               *batchMutation         `json:"batch,omitempty"`
	BatchEdit           *batchEditMutation     `json:"batch_edit,omitempty"`
	Association         *associationMutation   `json:"association,omitempty"`
	RelationList        *relationListing       `json:"relation_list,omitempty"`
	Occurrence          *occurrenceMutation    `json:"occurrence,omitempty"`
	Workflow            *workflowPlan          `json:"workflow,omitempty"`
	CorrectionAttempted bool                   `json:"correction_attempted,omitempty"`
	ResponseModel       string                 `json:"response_model,omitempty"`
}

func (c *routingConversation) prompt() string {
	switch c.State {
	case "awaiting_workflow_start":
		parts := make([]string, 0, len(c.Workflow.Steps))
		for i, step := range c.Workflow.Steps {
			label := strconv.Itoa(i+1) + ". " + step
			if unsupportedWorkflowReason(step) != "" {
				label += " (Atlas cannot do this step)"
			}
			parts = append(parts, label)
		}
		prompt := "I found these actions in order: " + strings.Join(parts, " ") + " Say start to begin the first step, or cancel. Atlas will pause at each write review and before the next step."
		if len(c.Warnings) > 0 {
			prompt += " " + c.Warnings[len(c.Warnings)-1]
		}
		return prompt
	case "unsupported":
		if c.Route.Plan != nil && c.Route.Plan.Clarification != "" {
			prompt := c.Route.Plan.Clarification + " Reply with a supported action or send a new request."
			if c.Workflow != nil {
				prompt += " Say skip to leave this step undone and continue the planned workflow, or cancel the remaining steps."
			}
			return prompt
		}
		if c.Workflow != nil {
			return "I can't do this planned step in Atlas. Say skip to leave it undone and continue, or cancel the remaining steps."
		}
		return "I can't do that request in Atlas. I can add, find, edit, and delete saved tasks, reminders, and notes. Reply with the action you meant, such as ‘note’, ‘find’, or ‘edit’, or send a new request."
	case "cancelled":
		return "Cancelled. Nothing was saved."
	case "saved":
		if c.Proposal != nil {
			p := c.Proposal.Input
			summary := "Saved " + p.Kind
			if p.Kind == "note" {
				summary += " “" + p.NoteBody + "”"
			} else {
				summary += " “" + p.Title + "”"
			}
			if p.DueAt != "" {
				summary += ", due " + lookupTime(p.DueAt, c.Route.Input.Timezone)
			}
			if p.ReminderAt != "" {
				if p.Kind == "reminder" {
					summary += " for " + lookupTime(p.ReminderAt, c.Route.Input.Timezone)
				} else {
					summary += ", with a linked reminder for " + lookupTime(p.ReminderAt, c.Route.Input.Timezone)
				}
			}
			if p.NoteBody != "" && p.Kind != "note" {
				summary += ", and a linked note: “" + p.NoteBody + "”"
			}
			if c.Workflow != nil {
				return c.workflowProgress(summary + ".")
			}
			return summary + ". What would you like to do next?"
		}
		if c.Workflow != nil {
			return c.workflowProgress("Saved.")
		}
		return "Saved. What would you like to do next?"
	case "answered":
		if c.Answer != nil {
			if c.Workflow != nil {
				return c.workflowProgress(c.Answer.Text)
			}
			return c.Answer.Text
		}
		return "I couldn't read the saved records. Please ask again."
	case "confirming":
		return "Saving. If the connection was interrupted, retry confirmation or resume this conversation."
	case "awaiting_route":
		prompt := "Should I add, find, edit, or delete a saved record?"
		if c.Route.Category == "compound" {
			prompt = "I see a deletion and another action in your request. I can handle them one at a time. Please restate the first action separately, for example, ‘delete all tasks’ or ‘add a reminder’. No records have changed."
		} else if c.Route.Plan != nil && c.Route.Plan.Clarification != "" {
			prompt = c.Route.Plan.Clarification + " I can add, find, edit, or delete a saved task, reminder, or note."
		} else if choices := likelyRouteActions(c.Route); len(choices) == 2 {
			prompt = "I'm not sure whether you want to " + choices[0] + " or " + choices[1] + ". Which should I do? No records have changed."
		}
		if len(c.Warnings) > 0 {
			prompt += " " + c.Warnings[len(c.Warnings)-1]
		}
		return prompt
	case "awaiting_target", "awaiting_field", "awaiting_change":
		if c.Question != nil {
			prompt := c.Question.Prompt + " I haven't changed a record yet."
			if len(c.Warnings) > 0 {
				prompt += " " + c.Warnings[len(c.Warnings)-1]
			}
			return prompt
		}
	case "awaiting_batch_targets":
		prompt := "I'm not sure which records you mean. Choose the saved records to delete. Review the selected list before confirming. Nothing has been deleted."
		if len(c.Warnings) > 0 {
			prompt += " " + c.Warnings[len(c.Warnings)-1]
		}
		return prompt
	case "awaiting_batch_edit_targets":
		return "Choose every " + c.BatchEdit.Kind + " to set to " + c.BatchEdit.Status + ". Atlas will show the exact set before changing anything."
	case "awaiting_batch_edit_confirmation", "applying_batch_edit":
		parts := []string{}
		for _, target := range c.BatchEdit.Targets {
			parts = append(parts, target.Kind+" “"+target.Title+"” ("+target.ID+")")
		}
		change := "Set status to " + c.BatchEdit.Status
		if c.BatchEdit.Kind == "note" {
			change = "Set " + c.BatchEdit.Field + " to “" + lookupExcerpt(c.BatchEdit.Value) + "”"
		}
		return change + " for these records: " + strings.Join(parts, "; ") + ". " + strings.Join(c.BatchEdit.Effects, " ") + " Say yes to apply or cancel."
	case "awaiting_relation_target":
		return "Which saved record's relations should I list? Choose its exact ID from the offered records."
	case "awaiting_occurrence_target":
		return "Which due repeating reminder occurrence should I acknowledge? Choose its exact ID."
	case "awaiting_occurrence_confirmation", "applying_occurrence":
		verb := "Complete"
		if c.Occurrence.Action == "dismiss" {
			verb = "Dismiss"
		}
		return verb + " the current occurrence of reminder “" + c.Occurrence.Target.Title + "” scheduled for " + lookupTime(c.Occurrence.ScheduledAt, c.Route.Input.Timezone) + "? Say yes or cancel."
	case "awaiting_association_targets":
		prompt := "Choose the first and second saved records for this relationship. Atlas will show the exact pair before making a change."
		if c.Association != nil && c.Association.Operation == "dependency" {
			prompt = "Choose the prerequisite task first, then the task that depends on it. Atlas will show the exact pair before making a change."
		}
		if len(c.Warnings) > 0 {
			prompt += " " + c.Warnings[len(c.Warnings)-1]
		}
		return prompt
	case "awaiting_association_confirmation", "applying_association":
		prompt := c.associationSummary() + "? Say yes to apply this relationship change or cancel."
		if len(c.Warnings) > 0 {
			prompt += " " + c.Warnings[len(c.Warnings)-1]
		}
		return prompt
	case "awaiting_delete_confirmation", "deleting", "deleting_batch":
		if c.Batch != nil {
			counts := map[string]int{}
			for _, target := range c.Batch.Targets {
				counts[target.Kind]++
			}
			parts := []string{}
			for _, kind := range c.Batch.Kinds {
				if counts[kind] > 0 {
					parts = append(parts, strconv.Itoa(counts[kind])+" "+kind)
				}
			}
			prompt := "Delete these " + strings.Join(parts, " and ") + " records? This cannot be undone. Only the listed records will be deleted. " + strings.Join(c.Batch.Effects, " ") + " Say yes to delete or cancel."
			if len(c.Warnings) > 0 {
				prompt += " " + c.Warnings[len(c.Warnings)-1]
			}
			return prompt
		}
		if c.Mutation != nil {
			prompt := "Delete " + c.Mutation.Kind + " “" + c.Mutation.Title + "”? This cannot be undone. " + strings.Join(c.Mutation.Effects, " ") + " Say yes to delete or cancel."
			if len(c.Warnings) > 0 {
				prompt += " " + c.Warnings[len(c.Warnings)-1]
			}
			return prompt
		}
	case "applying_edit":
		return "Applying the change. Resume this conversation if the connection was interrupted."
	case "awaiting_confirmation":
		p := c.Proposal.Input
		summary := p.Kind
		if p.Kind == "note" {
			summary += ": " + p.NoteBody
		} else {
			summary += ": " + p.Title
		}
		if p.DueAt != "" {
			summary += "; due " + lookupTime(p.DueAt, c.Route.Input.Timezone)
		}
		if p.ReminderAt != "" {
			summary += "; reminder " + lookupTime(p.ReminderAt, c.Route.Input.Timezone)
		}
		if p.NoteBody != "" && p.Kind != "note" {
			summary += "; note " + p.NoteBody
		}
		prompt := "I have " + summary + ". Say yes to save, or tell me what to change."
		if c.CorrectionAttempted && len(c.Warnings) > 0 {
			prompt += " " + c.Warnings[len(c.Warnings)-1]
		}
		return prompt
	}
	if c.Question != nil {
		prompt := c.Question.Prompt
		if len(c.Warnings) > 0 {
			prompt += " " + c.Warnings[len(c.Warnings)-1]
		}
		return prompt
	}
	return "Tell me what you would like to do with a saved task, reminder, or note."
}

func likelyRouteActions(route routing.Result) []string {
	labels := map[string]string{"task": "add a task", "reminder": "add a reminder", "note": "add a note", "lookup": "find a saved record", "edit": "edit a saved record", "delete": "delete a saved record"}
	probabilities := route.Evaluation.Decision.Probabilities
	choices := []string{}
	used := map[string]bool{}
	for len(choices) < 2 {
		best := ""
		for action := range labels {
			if !used[action] && probabilities[action] >= 0.15 && (best == "" || probabilities[action] > probabilities[best]) {
				best = action
			}
		}
		if best == "" {
			break
		}
		used[best] = true
		choices = append(choices, labels[best])
	}
	return choices
}

func routingConversationResponse(w http.ResponseWriter, status int, d store.SessionDocument, c routingConversation) {
	prompt := c.prompt()
	if n := len(c.Messages); n > 0 && c.Messages[n-1].Role == "assistant" && (c.State == "answered" || c.State == "saved") {
		prompt = c.Messages[n-1].Text
	}
	respond(w, status, map[string]any{"id": d.ID, "version": d.Version, "updated_at": d.UpdatedAt, "state": c.State, "prompt": prompt, "question": c.Question, "route": c.Route, "channel": c.Channel, "draft": c.Draft.Fields, "warnings": c.Warnings, "proposal": c.Proposal, "saved": c.Saved, "answer": c.Answer, "mutation": c.Mutation, "batch": c.Batch, "batch_edit": c.BatchEdit, "association": c.Association, "relation_list": c.RelationList, "occurrence": c.Occurrence, "workflow": c.Workflow, "messages": c.Messages, "response_model": c.ResponseModel, "next_request": map[string]any{"method": "POST", "path": "/api/v1/conversations/routing/" + d.ID + "/reply", "body": map[string]any{"version": d.Version, "text": ""}, "terminal": c.State == "cancelled", "confirmation_required": c.State == "awaiting_workflow_start" || c.State == "awaiting_confirmation" || c.State == "confirming" || c.State == "awaiting_delete_confirmation" || c.State == "deleting" || c.State == "deleting_batch" || c.State == "awaiting_batch_edit_confirmation" || c.State == "applying_batch_edit" || c.State == "awaiting_association_confirmation" || c.State == "applying_association" || c.State == "awaiting_occurrence_confirmation" || c.State == "applying_occurrence"}})
}

func (c *routingConversation) appendAssistant(ctx *http.Request, service routingService) {
	canonical := c.prompt()
	message := canonical
	c.ResponseModel = "local"
	listDetails := verifiedListDetails(canonical)
	if c.Workflow == nil && c.Route.Provider != "mock" && service.responder != nil && c.State == "answered" && listDetails != "" {
		facts := map[string]any{"canonical_answer": canonical, "state": c.State, "selected_tool": c.Route.SelectedTool, "user_request": c.Route.Input.Text, "response_mode": "list_intro", "list_kind": verifiedListKind(canonical)}
		if c.Answer != nil {
			facts["answer"] = c.Answer
		}
		if c.Mutation != nil {
			facts["mutation"] = c.Mutation
		}
		if c.Proposal != nil {
			facts["proposal"] = c.Proposal.Input
		}
		replyCtx, cancel := context.WithTimeout(ctx.Context(), 20*time.Second)
		defer cancel()
		if natural, model, err := service.responder.Respond(replyCtx, facts); err == nil {
			if safeListIntro(natural, canonical) {
				message = strings.TrimSpace(natural) + "\n" + listDetails
				c.ResponseModel = model
			}
		}
		if c.ResponseModel == "local" {
			c.Warnings = append(c.Warnings, "The writing model was unavailable or changed verified facts. Atlas is showing its verified result.")
		}
	}
	c.Messages = append(c.Messages, conversationTurn{Role: "assistant", Text: message})
}

func verifiedListDetails(canonical string) string {
	_, details, ok := strings.Cut(canonical, "\n")
	if !ok || details == "" || verifiedListKind(canonical) == "" {
		return ""
	}
	return details
}

func verifiedListKind(canonical string) string {
	heading, _, _ := strings.Cut(canonical, "\n")
	if !strings.HasPrefix(heading, "Here are ") {
		return ""
	}
	for _, kind := range []string{"tasks", "reminders", "notes"} {
		if strings.HasSuffix(heading, " "+kind+":") {
			return kind
		}
	}
	return ""
}

// Only these introductions can precede a verified list. A model-written
// sentence cannot be proven factual by checking for a few matching words.
func safeListIntro(text, canonical string) bool {
	kind := verifiedListKind(canonical)
	if kind == "" {
		return false
	}
	intro := strings.TrimSpace(text)
	return intro == "Here is what I found:" || intro == "Here's what I found:" ||
		intro == "Here are your "+kind+":" || intro == "Here are the "+kind+" you asked about:" ||
		intro == "Here is your "+strings.TrimSuffix(kind, "s")+" list:" ||
		intro == "Here's your "+strings.TrimSuffix(kind, "s")+" list:"
}

func (c *routingConversation) advance(ctx *http.Request, s *store.Store) error {
	c.Question = nil
	c.Proposal = nil
	c.Answer = nil
	if c.Channel == "" {
		c.State = "awaiting_route"
		return nil
	}
	if c.Channel == "lookup" {
		var answer conversationAnswer
		var err error
		if c.Route.Plan != nil {
			answer, err = answerPlannedLookup(ctx.Context(), s, c.Route.Input, *c.Route.Plan)
		} else {
			answer, err = answerLookup(ctx.Context(), s, c.Route.Input)
		}
		if err != nil {
			return err
		}
		c.Answer = &answer
		c.State = "answered"
		return nil
	}
	if c.Channel == "edit" || c.Channel == "delete" {
		return c.prepareMutation(ctx.Context(), s)
	}
	if c.Channel == "batch_delete" {
		return c.prepareBatch(ctx.Context(), s)
	}
	if c.Channel == "batch_edit" {
		return c.prepareBatchEdit(ctx.Context(), s)
	}
	if c.Channel == "relation_list" {
		return c.prepareRelationList(ctx.Context(), s)
	}
	if c.Channel == "occurrence" {
		return c.prepareOccurrence(ctx.Context(), s)
	}
	if c.Channel == "association" {
		return c.prepareAssociation(ctx.Context(), s)
	}
	out, err := dispatchChannel(ctx.Context(), s, c.Route, c.Channel, c.Draft)
	if err != nil {
		return err
	}
	if len(out.Questions) > 0 {
		c.Question = &out.Questions[0]
		c.State = "awaiting_answer"
		return nil
	}
	if out.Proposal == nil {
		return routing.ErrContract
	}
	c.Proposal = out.Proposal
	c.State = "awaiting_confirmation"
	return nil
}

func (c *routingConversation) beginRoute(r *http.Request, s *store.Store, result routing.Result) error {
	if result.Category == "compound" && c.Workflow == nil {
		if steps := workflowClauses(result.Input.Text); len(steps) > 1 {
			c.Workflow = &workflowPlan{Steps: steps}
		}
	}
	c.Route = result
	c.Channel = ""
	c.Question = nil
	c.Proposal = nil
	c.Answer = nil
	c.Saved = nil
	c.Mutation = nil
	c.Batch = nil
	c.BatchEdit = nil
	c.Association = nil
	c.RelationList = nil
	c.Occurrence = nil
	c.ResponseModel = ""
	c.Warnings = []string{}
	if result.ExtractionModel == "local_parser" {
		c.Warnings = append(c.Warnings, "Atlas used its local parser for this request.")
	}
	c.CorrectionAttempted = false
	c.Draft = dispatchRequest{Version: routing.Version, RoutingToken: result.RoutingToken}
	switch result.State {
	case "unsupported":
		c.State = "unsupported"
	case "needs_clarification":
		if c.Workflow != nil && result.Category == "compound" {
			c.State = "awaiting_workflow_start"
		} else {
			c.State = "awaiting_route"
		}
	default:
		c.Channel = result.SelectedChannel
		if plan := result.Plan; plan != nil {
			switch c.Channel {
			case "tasks", "reminders", "notes":
				c.Draft.Fields.LinkedTaskID = plan.LinkedTaskID
				c.Draft.Fields.NoteBody = plan.NoteBody
				if c.Channel != "notes" {
					c.Draft.Fields.Title = plan.Title
					c.Draft.Fields.ReminderAt = plan.ReminderAt
					c.Draft.Fields.Repeat = plan.Repeat
				}
				if c.Channel == "tasks" {
					c.Draft.Fields.Details = plan.Details
					c.Draft.Fields.DueAt = plan.DueAt
					c.Draft.Reminder = "skip"
					if plan.ReminderAt != "" {
						c.Draft.Reminder = "add"
					}
				}
				if c.Channel != "notes" {
					c.Draft.Note = "skip"
					if plan.NoteBody != "" {
						c.Draft.Note = "add"
					}
				}
			case "edit", "delete":
				c.Mutation = &recordMutation{Action: c.Channel, Kind: plan.Kind, ID: plan.TargetID, TargetQuery: plan.TargetQuery, Field: plan.Field, New: plan.Value}
			}
			return c.advance(r, s)
		}
		if c.Channel != "lookup" && c.Channel != "edit" && c.Channel != "delete" && c.Channel != "association" && c.Channel != "batch_delete" && c.Channel != "batch_edit" && c.Channel != "relation_list" && c.Channel != "occurrence" {
			seed := prepareChannel(result.Input, strings.TrimSuffix(c.Channel, "s"))
			c.Draft = applyChannelPrefill(c.Draft, seed)
			c.Warnings = seed.Warnings
			if c.Draft.Note == "" && c.Channel != "notes" {
				c.Draft.Note = "skip"
			}
			if c.Draft.Reminder == "" && c.Channel == "tasks" {
				c.Draft.Reminder = "skip"
			}
		}
		return c.advance(r, s)
	}
	return nil
}

func normalizedReply(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), ".! ")
}

func recentConversation(messages []conversationTurn) string {
	if len(messages) < 2 {
		return ""
	}
	start := len(messages) - 5
	if start < 0 {
		start = 0
	}
	parts := []string{}
	for _, turn := range messages[start : len(messages)-1] {
		label := "User"
		if turn.Role == "assistant" {
			label = "Atlas"
		}
		parts = append(parts, label+": "+turn.Text)
	}
	value := strings.Join(parts, "\n")
	runes := []rune(value)
	if len(runes) > 1800 {
		value = string(runes[len(runes)-1800:])
	}
	return value
}

func recentAnswerRecord(answer *conversationAnswer) (string, string) {
	if answer == nil || len(answer.Sources) != 1 {
		return "", ""
	}
	source := answer.Sources[0]
	plural := map[string]string{"task": "tasks", "reminder": "reminders", "note": "notes"}[source.Type]
	prefix := "/#" + plural + "/"
	if plural == "" || !strings.HasPrefix(source.URL, prefix) {
		return "", ""
	}
	id := strings.TrimPrefix(source.URL, prefix)
	if id == "" || strings.Contains(id, "/") {
		return "", ""
	}
	return id, source.Type
}

// conversationFocusRecord returns the entity the user most recently acted on,
// not merely the entity returned by a lookup. Keeping saves and edits in the
// same focus path is what lets ordinary follow-ups such as "move it", "what
// does that say?", or "delete that reminder" cross workflow boundaries
// without forcing the user to repeat a title.
func conversationFocusRecord(c routingConversation) (string, string) {
	if c.Mutation != nil && c.Mutation.ID != "" && c.Mutation.Kind != "" && c.Mutation.Action != "delete" {
		return c.Mutation.ID, c.Mutation.Kind
	}
	if id, kind := recentAnswerRecord(c.Answer); id != "" {
		return id, kind
	}
	if c.Saved == nil || c.Proposal == nil {
		return "", ""
	}
	switch c.Proposal.Input.Kind {
	case "task":
		if c.Saved.TaskID != "" {
			return c.Saved.TaskID, "task"
		}
	case "reminder":
		if len(c.Saved.Reminders) == 1 {
			return c.Saved.Reminders[0].ID, "reminder"
		}
	case "note":
		if len(c.Saved.Notes) == 1 {
			return c.Saved.Notes[0].ID, "note"
		}
	}
	return "", ""
}
func isConfirm(value string) bool {
	switch normalizedReply(value) {
	case "yes", "yes save it", "save it", "confirm", "confirm and save":
		return true
	}
	return false
}
func isCancel(value string) bool {
	switch normalizedReply(value) {
	case "cancel", "never mind", "nevermind", "stop":
		return true
	}
	return false
}
func selectedChannel(value string) string {
	switch normalizedReply(value) {
	case "task", "a task", "tasks", "todo", "to do", "action":
		return "tasks"
	case "reminder", "a reminder", "reminders", "remind me":
		return "reminders"
	case "note", "a note", "notes":
		return "notes"
	case "lookup", "look up", "find", "search", "find existing records":
		return "lookup"
	case "edit", "change", "update":
		return "edit"
	case "delete", "remove":
		return "delete"
	}
	return ""
}
func choiceReply(value string) string {
	switch normalizedReply(value) {
	case "yes", "yes please", "add", "add it", "sure", "include it", "add a reminder", "add a note":
		return "add"
	case "no", "no thanks", "skip", "none", "without it", "don't add it", "do not add it", "no reminder", "no note", "skip reminder", "skip note":
		return "skip"
	}
	return ""
}

// Answers are bound to the one pending question. Structured field/value input
// lets a speech model supply a parsed value while the server retains control
// of state, validation, confirmation and persistence.
func (c *routingConversation) answer(field, value string) bool {
	q := c.Question
	if q == nil || field != "" && field != q.Field {
		return false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	switch q.Field {
	case "reminder":
		choice := choiceReply(value)
		if choice == "" {
			return false
		}
		c.Draft.Reminder = choice
	case "note":
		choice := choiceReply(value)
		if choice == "" {
			return false
		}
		c.Draft.Note = choice
	case "fields.title":
		c.Draft.Fields.Title = value
	case "fields.note_body":
		c.Draft.Fields.NoteBody = value
	case "fields.reminder_at":
		instant, repeat, ok := interpret.ResolveTime(value, c.Route.Input.Timezone, c.Route.Input.ReferenceAt, "reminder_at")
		if !ok {
			return false
		}
		c.Draft.Fields.ReminderAt = instant
		if repeat != "" {
			c.Draft.Fields.Repeat = repeat
		}
	default:
		return false
	}
	return true
}

func (c *routingConversation) correction(field, value string) bool {
	field = strings.TrimSpace(strings.TrimPrefix(field, "fields."))
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if field == "" {
		text := strings.TrimSpace(value)
		if strings.HasPrefix(strings.ToLower(text), "actually ") {
			field, value = "channel", strings.TrimSpace(text[9:])
		} else {
			if !strings.HasPrefix(strings.ToLower(text), "change ") {
				return false
			}
			parts := strings.SplitN(text[7:], " to ", 2)
			if len(parts) != 2 {
				return false
			}
			field, value = strings.ToLower(strings.TrimSpace(parts[0])), strings.TrimSpace(parts[1])
		}
	}
	switch field {
	case "channel":
		channel := selectedChannel(value)
		if channel == "" {
			return false
		}
		c.Channel = channel
		c.Draft = dispatchRequest{Version: routing.Version, RoutingToken: c.Route.RoutingToken, Channel: channel, ReviewedChannel: true}
		if channel != "lookup" {
			seed := prepareChannel(c.Route.Input, strings.TrimSuffix(channel, "s"))
			c.Draft = applyChannelPrefill(c.Draft, seed)
			c.Warnings = seed.Warnings
			if c.Draft.Note == "" && channel != "notes" {
				c.Draft.Note = "skip"
			}
			if c.Draft.Reminder == "" && channel == "tasks" {
				c.Draft.Reminder = "skip"
			}
		}
		return true
	case "title":
		if c.Channel == "notes" {
			return false
		}
		c.Draft.Fields.Title = value
	case "details":
		if c.Channel != "tasks" {
			return false
		}
		c.Draft.Fields.Details = value
	case "note_body":
		c.Draft.Fields.NoteBody = value
		if c.Channel != "notes" {
			c.Draft.Note = "add"
		}
	case "note":
		if c.Channel == "notes" {
			c.Draft.Fields.NoteBody = value
			break
		}
		choice := choiceReply(value)
		if choice == "skip" {
			c.Draft.Note = "skip"
			c.Draft.Fields.NoteBody = ""
		} else {
			c.Draft.Note = "add"
			if choice != "add" {
				c.Draft.Fields.NoteBody = value
			}
		}
	case "due_at", "deadline":
		if c.Channel != "tasks" {
			return false
		}
		instant, _, ok := interpret.ResolveTime(value, c.Route.Input.Timezone, c.Route.Input.ReferenceAt, "due_at")
		if !ok {
			return false
		}
		c.Draft.Fields.DueAt = instant
	case "reminder_at", "reminder time":
		if c.Channel == "notes" {
			return false
		}
		instant, repeat, ok := interpret.ResolveTime(value, c.Route.Input.Timezone, c.Route.Input.ReferenceAt, "reminder_at")
		if !ok {
			return false
		}
		c.Draft.Fields.ReminderAt = instant
		c.Draft.Fields.Repeat = repeat
		if c.Channel == "tasks" {
			c.Draft.Reminder = "add"
		}
	case "reminder":
		if c.Channel != "tasks" {
			return false
		}
		choice := choiceReply(value)
		if choice == "" {
			return false
		}
		c.Draft.Reminder = choice
		if choice == "skip" {
			c.Draft.Fields.ReminderAt = ""
			c.Draft.Fields.ReminderTitle = ""
			c.Draft.Fields.Repeat = ""
		}
	case "repeat":
		if value != "daily" && value != "weekly" && value != "none" {
			return false
		}
		if value == "none" {
			value = ""
		}
		c.Draft.Fields.Repeat = value
	case "before_task_id", "prerequisite":
		if c.Channel != "tasks" {
			return false
		}
		for _, record := range c.Route.Input.Context {
			if record.ID == value || strings.EqualFold(record.Title, value) {
				c.Draft.Fields.BeforeTaskID = record.ID
				return true
			}
		}
		return false
	default:
		return false
	}
	c.Draft.Fields.PreviewID = ""
	c.Draft.Fields.BeforeTaskVersion = ""
	return true
}

func routingConversationRoutes(mux *http.ServeMux, s *store.Store, service routingService) {
	mux.HandleFunc("POST /api/v1/conversations/routing", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Provider     string `json:"provider"`
			Version      string `json:"version"`
			RequestID    string `json:"request_id"`
			Text         string `json:"text"`
			Timezone     string `json:"timezone"`
			ContextQuery string `json:"context_query"`
			Fixture      string `json:"fixture"`
		}
		if !decode(w, r, &in) {
			return
		}
		result, err := evaluateRouting(r.Context(), s, service, in.Provider, routing.Request{Version: in.Version, RequestID: in.RequestID, Text: in.Text, Timezone: in.Timezone, ContextQuery: in.ContextQuery, Fixture: in.Fixture})
		if err != nil {
			routingFailure(w, err)
			return
		}
		c := routingConversation{Fixture: in.Fixture, Messages: []conversationTurn{{Role: "user", Text: in.Text}}}
		if err = c.beginRoute(r, s, result); err != nil {
			failure(w, err)
			return
		}
		if c.State == "applying_edit" {
			if err = c.snapshotMutation(r.Context(), s); err != nil {
				failure(w, err)
				return
			}
		}
		if c.Route.Category == "" || c.State != "awaiting_confirmation" {
			c.appendAssistant(r, service)
		}
		d, err := s.CreateSession(r.Context(), c)
		if err != nil {
			failure(w, err)
			return
		}
		if c.State == "applying_edit" {
			if err = c.commitMutation(r.Context(), s); err != nil {
				failure(w, err)
				return
			}
			if c.Route.Category == "" || c.State != "awaiting_confirmation" {
				c.appendAssistant(r, service)
			}
			d, err = s.SaveSession(r.Context(), d.ID, d.Version, c)
			if err != nil {
				failure(w, err)
				return
			}
		}
		if d, c, err = autoCapture(r, s, service, d, c); err != nil {
			failure(w, err)
			return
		}
		w.Header().Set("Location", "/api/v1/conversations/routing/"+d.ID)
		routingConversationResponse(w, 201, d, c)
	})
	mux.HandleFunc("GET /api/v1/conversations/routing/{id}", func(w http.ResponseWriter, r *http.Request) {
		d, c, err := loadRoutingConversation(r, s)
		if err != nil {
			failure(w, err)
			return
		}
		routingConversationResponse(w, 200, d, c)
	})
	mux.HandleFunc("POST /api/v1/conversations/routing/{id}/reply", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Version int    `json:"version"`
			Text    string `json:"text"`
			Field   string `json:"field"`
			Value   string `json:"value"`
		}
		if !decode(w, r, &in) {
			return
		}
		if len([]rune(in.Text)) > 12000 || len([]rune(in.Value)) > 12000 || strings.TrimSpace(in.Text) == "" && strings.TrimSpace(in.Value) == "" {
			apiError(w, 400, "invalid_reply", "Provide a short text or field value.", false)
			return
		}
		d, c, err := loadRoutingConversation(r, s)
		if err != nil {
			failure(w, err)
			return
		}
		value := in.Text
		if in.Value != "" {
			value = in.Value
		}
		if (c.State == "saved" || c.State == "answered" && (c.BatchEdit != nil || c.Occurrence != nil && c.Occurrence.Acknowledged)) && isConfirm(value) {
			routingConversationResponse(w, 200, d, c)
			return
		}
		if in.Version != d.Version {
			failure(w, store.ErrSessionConflict)
			return
		}
		if c.State == "cancelled" {
			apiError(w, 409, "session_closed", "Start a new routing conversation.", false)
			return
		}
		if (c.State == "confirming" || c.State == "deleting" || c.State == "deleting_batch" || c.State == "applying_edit" || c.State == "applying_association" || c.State == "applying_batch_edit" || c.State == "applying_occurrence") && !isConfirm(value) {
			apiError(w, 409, "confirmation_pending", "Retry confirmation to recover the result.", false)
			return
		}
		if c.State != "confirming" && c.State != "deleting_batch" && c.State != "applying_association" && c.State != "applying_batch_edit" && c.State != "applying_occurrence" {
			c.Messages = append(c.Messages, conversationTurn{Role: "user", Text: value})
			if isCancel(value) {
				c.State = "cancelled"
				c.Question = nil
				c.Proposal = nil
			} else {
				switch c.State {
				case "awaiting_workflow_start":
					if normalizedReply(value) == "start" || normalizedReply(value) == "begin" || isConfirm(value) {
						if err = c.startWorkflowStep(r, s, service, d.ID, d.Version+1); err != nil {
							routingFailure(w, err)
							return
						}
					} else {
						c.Warnings = []string{"Say start to begin the reviewed plan, or cancel it."}
					}
				case "answered", "saved", "unsupported":
					if c.Workflow != nil && c.State == "unsupported" && normalizedReply(value) == "skip" && c.Workflow.Index+1 == len(c.Workflow.Steps) {
						c.State = "answered"
						c.Answer = &conversationAnswer{Text: "Skipped the final unsupported step. No records were changed by it.", Sources: []conversationSource{}}
						break
					}
					if c.Workflow != nil && c.Workflow.Index+1 < len(c.Workflow.Steps) {
						if normalizedReply(value) == "continue" && c.State != "unsupported" || normalizedReply(value) == "skip" && c.State == "unsupported" {
							c.Workflow.Index++
							if err = c.startWorkflowStep(r, s, service, d.ID, d.Version+1); err != nil {
								routingFailure(w, err)
								return
							}
						} else if c.State != "unsupported" {
							c.Warnings = []string{"The remaining planned steps have not started. Say continue or cancel before a new request."}
						}
						if c.State != "unsupported" || normalizedReply(value) == "skip" {
							break
						}
					}
					if c.Workflow != nil && c.Workflow.Index+1 == len(c.Workflow.Steps) && c.State != "unsupported" {
						c.Workflow = nil
					}
					previousID, previousKind := conversationFocusRecord(c)
					fixture := c.Fixture
					if c.State == "unsupported" {
						fixture = ""
						if c.Route.Provider == "mock" {
							fixture = map[string]string{"tasks": "task", "reminders": "reminder", "notes": "note", "lookup": "lookup", "edit": "edit", "delete": "delete"}[selectedChannel(value)]
						}
					}
					result, routeErr := evaluateRouting(r.Context(), s, service, c.Route.Provider, routing.Request{Version: routing.Version, RequestID: d.ID + "." + strconv.Itoa(d.Version+1), Text: value, Timezone: c.Route.Input.Timezone, RecentConversation: recentConversation(c.Messages), RecentRecordID: previousID, RecentRecordKind: previousKind, Fixture: fixture})
					if routeErr != nil {
						routingFailure(w, routeErr)
						return
					}
					c.Fixture = fixture
					if err = c.beginRoute(r, s, result); err != nil {
						failure(w, err)
						return
					}
				case "awaiting_route":
					if c.Route.Category == "compound" {
						result, routeErr := evaluateRouting(r.Context(), s, service, c.Route.Provider, routing.Request{Version: routing.Version, RequestID: d.ID + "." + strconv.Itoa(d.Version+1), Text: value, Timezone: c.Route.Input.Timezone, RecentConversation: recentConversation(c.Messages)})
						if routeErr != nil {
							routingFailure(w, routeErr)
							return
						}
						if err = c.beginRoute(r, s, result); err != nil {
							failure(w, err)
							return
						}
						break
					}
					channel := selectedChannel(value)
					if channel == "" {
						channel = c.naturalRouteChoice(r.Context(), service, value)
					}
					if channel != "" {
						c.Warnings = nil
						c.Channel = channel
						c.Draft.Channel = channel
						c.Draft.ReviewedChannel = true
						if channel != "lookup" && channel != "edit" && channel != "delete" {
							seed := prepareChannel(c.Route.Input, strings.TrimSuffix(channel, "s"))
							c.Draft = applyChannelPrefill(c.Draft, seed)
							c.Warnings = seed.Warnings
							if c.Draft.Note == "" && channel != "notes" {
								c.Draft.Note = "skip"
							}
							if c.Draft.Reminder == "" && channel == "tasks" {
								c.Draft.Reminder = "skip"
							}
						}
						if err = c.advance(r, s); err != nil {
							failure(w, err)
							return
						}
					} else {
						c.Warnings = []string{"I couldn't match that to a saved-record action. Reply with task, reminder, note, find, edit, or delete."}
					}
				case "awaiting_answer":
					if c.answer(in.Field, value) || in.Field == "" && c.naturalChoiceAnswer(r.Context(), service, value) {
						c.Warnings = nil
						if err = c.advance(r, s); err != nil {
							failure(w, err)
							return
						}
					} else {
						c.Warnings = []string{"I couldn't use that answer. Please choose one of the offered options or answer the question directly."}
					}
				case "awaiting_confirmation":
					if isConfirm(value) {
						c.State = "confirming"
					} else if c.correction(in.Field, value) {
						c.Warnings = nil
						c.CorrectionAttempted = false
						if err = c.advance(r, s); err != nil {
							failure(w, err)
							return
						}
					} else if in.Field == "" && c.naturalCorrection(r.Context(), service, value) && c.State == "awaiting_confirmation" {
						if err = c.advance(r, s); err != nil {
							failure(w, err)
							return
						}
					} else if c.State == "awaiting_confirmation" && len(c.Warnings) == 0 {
						c.Warnings = []string{"I haven't applied that request. Change this draft, confirm it, or cancel it before starting a new request."}
					}
				case "awaiting_batch_targets":
					if c.selectBatchTargets(value) {
						c.Warnings = nil
						if err = c.batchEffects(r.Context(), s); err != nil {
							failure(w, err)
							return
						}
					} else {
						c.Warnings = []string{"I couldn't match that selection. Select one or more records from the list, or cancel."}
					}
				case "awaiting_batch_edit_targets":
					if c.selectBatchEditTargets(r.Context(), s, value) {
						c.Warnings = nil
					} else {
						c.Warnings = []string{"Choose one or more listed record IDs, or cancel."}
					}
				case "awaiting_batch_edit_confirmation":
					if isConfirm(value) {
						c.State = "applying_batch_edit"
					} else {
						c.Warnings = []string{"The reviewed bulk edit has not started. Confirm it or cancel."}
					}
				case "awaiting_relation_target":
					if c.selectRelationTarget(r.Context(), s, value) {
						c.Warnings = nil
					} else {
						c.Warnings = []string{"Choose a listed record ID."}
					}
				case "awaiting_occurrence_target":
					if c.selectOccurrence(r.Context(), s, value) == nil {
						c.Warnings = nil
					} else {
						c.Warnings = []string{"Choose a due repeating reminder ID."}
					}
				case "awaiting_occurrence_confirmation":
					if isConfirm(value) {
						c.State = "applying_occurrence"
					} else {
						c.Warnings = []string{"The occurrence has not been acknowledged. Confirm it or cancel."}
					}
				case "awaiting_association_targets":
					if c.selectAssociationTargets(value) {
						c.Warnings = nil
					} else {
						c.Warnings = []string{"I couldn't match that pair. Choose one record from each list, or cancel."}
					}
				case "awaiting_association_confirmation":
					if isConfirm(value) {
						c.State = "applying_association"
					} else {
						c.Warnings = []string{"The relationship has not changed. Confirm the reviewed pair or cancel it."}
					}
				case "awaiting_target", "awaiting_field", "awaiting_change":
					before := c.State
					handled, mutationErr := c.mutationReply(r.Context(), s, value)
					if mutationErr != nil {
						failure(w, mutationErr)
						return
					}
					if !handled || before == "awaiting_change" && c.State == before {
						c.Warnings = []string{"I couldn't apply that answer. Please provide the requested record, field, or value."}
					} else {
						c.Warnings = nil
					}
				case "awaiting_delete_confirmation":
					if isConfirm(value) {
						if c.Batch != nil {
							c.State = "deleting_batch"
						} else {
							c.State = "deleting"
						}
					} else {
						c.Warnings = []string{"I have not started that new request. Confirm this deletion or cancel it, then send the new request."}
					}
				}
			}
			if c.State == "applying_edit" {
				if err = c.snapshotMutation(r.Context(), s); err != nil {
					failure(w, err)
					return
				}
			}
			if c.Route.Category == "" || c.State != "awaiting_confirmation" {
				c.appendAssistant(r, service)
			}
			d, err = s.SaveSession(r.Context(), d.ID, d.Version, c)
			if err != nil {
				failure(w, err)
				return
			}
			if d, c, err = autoCapture(r, s, service, d, c); err != nil {
				failure(w, err)
				return
			}
		}
		if c.State == "deleting_batch" {
			if err = c.commitBatch(r.Context(), s, "routing.batch."+d.ID+"."+strconv.Itoa(d.Version)); errors.Is(err, store.ErrBatchChanged) {
				if err = c.prepareBatch(r.Context(), s); err != nil {
					failure(w, err)
					return
				}
				c.Warnings = append(c.Warnings, "The reviewed records changed. Review the current list and confirm again.")
			} else if err != nil {
				failure(w, err)
				return
			}
			c.appendAssistant(r, service)
			d, err = s.SaveSession(r.Context(), d.ID, d.Version, c)
			if err != nil {
				failure(w, err)
				return
			}
		}
		if c.State == "applying_batch_edit" {
			if err = c.commitBatchEdit(r.Context(), s, "routing.batch.edit."+d.ID+"."+strconv.Itoa(d.Version)); errors.Is(err, store.ErrBatchEditChanged) {
				if err = c.prepareBatchEdit(r.Context(), s); err != nil {
					failure(w, err)
					return
				}
				c.Warnings = append(c.Warnings, "The reviewed records changed. Review the current set and confirm again.")
			} else if err != nil {
				failure(w, err)
				return
			}
			c.appendAssistant(r, service)
			d, err = s.SaveSession(r.Context(), d.ID, d.Version, c)
			if err != nil {
				failure(w, err)
				return
			}
		}
		if c.State == "applying_occurrence" {
			if err = c.commitOccurrence(r.Context(), s); errors.Is(err, store.ErrOccurrenceConflict) {
				if err = c.prepareOccurrence(r.Context(), s); err != nil {
					failure(w, err)
					return
				}
				c.Warnings = append(c.Warnings, "The reviewed occurrence changed. Review the current due occurrence and confirm again.")
			} else if err != nil {
				failure(w, err)
				return
			}
			c.appendAssistant(r, service)
			d, err = s.SaveSession(r.Context(), d.ID, d.Version, c)
			if err != nil {
				failure(w, err)
				return
			}
		}
		if c.State == "applying_association" {
			if err = c.commitAssociation(r.Context(), s, "routing.association."+d.ID+"."+strconv.Itoa(d.Version)); errors.Is(err, store.ErrAssociationChanged) {
				if err = c.prepareAssociation(r.Context(), s); err != nil {
					failure(w, err)
					return
				}
				c.Warnings = append(c.Warnings, "The reviewed records changed. Select the current pair and confirm again.")
			} else if err != nil {
				failure(w, err)
				return
			}
			c.appendAssistant(r, service)
			d, err = s.SaveSession(r.Context(), d.ID, d.Version, c)
			if err != nil {
				failure(w, err)
				return
			}
		}
		if c.State == "applying_edit" || c.State == "deleting" {
			if err = c.commitMutation(r.Context(), s); err != nil {
				failure(w, err)
				return
			}
			c.appendAssistant(r, service)
			d, err = s.SaveSession(r.Context(), d.ID, d.Version, c)
			if err != nil {
				failure(w, err)
				return
			}
		}
		if c.State == "confirming" {
			saved, _, commitErr := s.CommitCapture(r.Context(), routingCaptureRequestID(d), c.Proposal.Input)
			if errors.Is(commitErr, store.ErrCaptureContext) {
				if err = c.advance(r, s); err != nil {
					failure(w, err)
					return
				}
				c.Messages = append(c.Messages, conversationTurn{Role: "assistant", Text: "The referenced task changed. Review this proposal again. " + c.prompt()})
				d, err = s.SaveSession(r.Context(), d.ID, d.Version, c)
				if err != nil {
					failure(w, err)
					return
				}
				routingConversationResponse(w, 200, d, c)
				return
			}
			if commitErr != nil {
				failure(w, commitErr)
				return
			}
			c.State = "saved"
			c.Saved = &saved
			c.appendAssistant(r, service)
			d, err = s.SaveSession(r.Context(), d.ID, d.Version, c)
			if errors.Is(err, store.ErrSessionConflict) {
				d, c, err = loadRoutingConversation(r, s)
			}
			if err != nil {
				failure(w, err)
				return
			}
		}
		routingConversationResponse(w, 200, d, c)
	})
}

func loadRoutingConversation(r *http.Request, s *store.Store) (store.SessionDocument, routingConversation, error) {
	d, err := s.Session(r.Context(), r.PathValue("id"))
	if err != nil {
		return d, routingConversation{}, err
	}
	var c routingConversation
	if err = json.Unmarshal(d.Data, &c); err != nil {
		return d, c, err
	}
	if c.Route.Version != routing.Version || c.Route.RegistryVersion != routing.RegistryVersion {
		return d, c, routing.ErrRequest
	}
	return d, c, nil
}

func autoCapture(r *http.Request, s *store.Store, service routingService, d store.SessionDocument, c routingConversation) (store.SessionDocument, routingConversation, error) {
	if c.Route.Provider != "jev" || c.Route.Category == "" || c.State != "awaiting_confirmation" || c.Proposal == nil {
		return d, c, nil
	}
	c.State = "confirming"
	var err error
	d, err = s.SaveSession(r.Context(), d.ID, d.Version, c)
	if err != nil {
		return d, c, err
	}
	saved, _, err := s.CommitCapture(r.Context(), routingCaptureRequestID(d), c.Proposal.Input)
	if errors.Is(err, store.ErrCaptureContext) {
		if err = c.advance(r, s); err != nil {
			return d, c, err
		}
		c.appendAssistant(r, service)
		d, err = s.SaveSession(r.Context(), d.ID, d.Version, c)
		return d, c, err
	}
	if err != nil {
		return d, c, err
	}
	c.State = "saved"
	c.Saved = &saved
	c.appendAssistant(r, service)
	d, err = s.SaveSession(r.Context(), d.ID, d.Version, c)
	return d, c, err
}

func routingCaptureRequestID(d store.SessionDocument) string {
	return "routing.session." + d.ID + "." + strconv.Itoa(d.Version)
}
