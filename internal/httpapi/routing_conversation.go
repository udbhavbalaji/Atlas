package httpapi

import (
	"atlas/internal/interpret"
	"atlas/internal/routing"
	"atlas/internal/store"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

type routingConversation struct {
	State    string                 `json:"state"`
	Route    routing.Result         `json:"route"`
	Fixture  string                 `json:"fixture,omitempty"`
	Channel  string                 `json:"channel"`
	Draft    dispatchRequest        `json:"draft"`
	Question *channelQuestion       `json:"question,omitempty"`
	Proposal *store.CaptureProposal `json:"proposal,omitempty"`
	Warnings []string               `json:"warnings"`
	Messages []conversationTurn     `json:"messages"`
	Saved    *store.TaskAction      `json:"saved,omitempty"`
	Answer   *conversationAnswer    `json:"answer,omitempty"`
	Mutation *recordMutation        `json:"mutation,omitempty"`
}

func (c *routingConversation) prompt() string {
	switch c.State {
	case "unsupported":
		return "I can add, find, edit, and delete Atlas tasks, reminders, and notes. That request needs another capability."
	case "cancelled":
		return "Cancelled. Nothing was saved."
	case "saved":
		return "Saved."
	case "answered":
		if c.Answer != nil {
			return c.Answer.Text
		}
		return "I couldn't read the saved records. Please ask again."
	case "confirming":
		return "Saving. If the connection was interrupted, retry confirmation or resume this conversation."
	case "awaiting_route":
		return "Should I add, find, edit, or delete a saved record?"
	case "awaiting_target", "awaiting_field", "awaiting_change":
		if c.Question != nil {
			return c.Question.Prompt
		}
	case "awaiting_delete_confirmation", "deleting":
		if c.Mutation != nil {
			return "Delete " + c.Mutation.Kind + " “" + c.Mutation.Title + "”? This cannot be undone. " + strings.Join(c.Mutation.Effects, " ") + " Say yes to delete or cancel."
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
			summary += "; due " + p.DueAt
		}
		if p.ReminderAt != "" {
			summary += "; reminder " + p.ReminderAt
		}
		if p.NoteBody != "" && p.Kind != "note" {
			summary += "; note " + p.NoteBody
		}
		return "I have " + summary + ". Say yes to save, or tell me what to change."
	}
	if c.Question != nil {
		return c.Question.Prompt
	}
	return "Tell me what you would like to save."
}

func routingConversationResponse(w http.ResponseWriter, status int, d store.SessionDocument, c routingConversation) {
	prompt := c.prompt()
	respond(w, status, map[string]any{"id": d.ID, "version": d.Version, "updated_at": d.UpdatedAt, "state": c.State, "prompt": prompt, "question": c.Question, "route": c.Route, "channel": c.Channel, "draft": c.Draft.Fields, "warnings": c.Warnings, "proposal": c.Proposal, "saved": c.Saved, "answer": c.Answer, "mutation": c.Mutation, "messages": c.Messages, "next_request": map[string]any{"method": "POST", "path": "/api/v1/conversations/routing/" + d.ID + "/reply", "body": map[string]any{"version": d.Version, "text": ""}, "terminal": c.State == "saved" || c.State == "cancelled" || c.State == "unsupported", "confirmation_required": c.State == "awaiting_confirmation" || c.State == "confirming" || c.State == "awaiting_delete_confirmation" || c.State == "deleting"}})
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
		answer, err := answerLookup(ctx.Context(), s, c.Route.Input)
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
	c.Route = result
	c.Channel = ""
	c.Question = nil
	c.Proposal = nil
	c.Answer = nil
	c.Mutation = nil
	c.Warnings = []string{}
	c.Draft = dispatchRequest{Version: routing.Version, RoutingToken: result.RoutingToken}
	switch result.State {
	case "unsupported":
		c.State = "unsupported"
	case "needs_clarification":
		c.State = "awaiting_route"
	default:
		c.Channel = result.SelectedChannel
		if c.Channel != "lookup" && c.Channel != "edit" && c.Channel != "delete" {
			seed := prepareChannel(result.Input, strings.TrimSuffix(c.Channel, "s"))
			c.Draft = applyChannelPrefill(c.Draft, seed)
			c.Warnings = seed.Warnings
		}
		return c.advance(r, s)
	}
	return nil
}

func normalizedReply(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), ".! ")
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
		c.Messages = append(c.Messages, conversationTurn{Role: "assistant", Text: c.prompt()})
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
			c.Messages = append(c.Messages, conversationTurn{Role: "assistant", Text: c.prompt()})
			d, err = s.SaveSession(r.Context(), d.ID, d.Version, c)
			if err != nil {
				failure(w, err)
				return
			}
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
		if c.State == "saved" && isConfirm(value) {
			routingConversationResponse(w, 200, d, c)
			return
		}
		if in.Version != d.Version {
			failure(w, store.ErrSessionConflict)
			return
		}
		if c.State == "saved" || c.State == "cancelled" || c.State == "unsupported" {
			apiError(w, 409, "session_closed", "Start a new routing conversation.", false)
			return
		}
		if (c.State == "confirming" || c.State == "deleting" || c.State == "applying_edit") && !isConfirm(value) {
			apiError(w, 409, "confirmation_pending", "Retry confirmation to recover the result.", false)
			return
		}
		if c.State != "confirming" {
			c.Messages = append(c.Messages, conversationTurn{Role: "user", Text: value})
			if isCancel(value) {
				c.State = "cancelled"
				c.Question = nil
				c.Proposal = nil
			} else {
				switch c.State {
				case "answered":
					result, routeErr := evaluateRouting(r.Context(), s, service, c.Route.Provider, routing.Request{Version: routing.Version, RequestID: d.ID + "." + strconv.Itoa(d.Version+1), Text: value, Timezone: c.Route.Input.Timezone, Fixture: c.Fixture})
					if routeErr != nil {
						routingFailure(w, routeErr)
						return
					}
					if err = c.beginRoute(r, s, result); err != nil {
						failure(w, err)
						return
					}
				case "awaiting_route":
					channel := selectedChannel(value)
					if channel != "" {
						c.Channel = channel
						c.Draft.Channel = channel
						c.Draft.ReviewedChannel = true
						if channel != "lookup" && channel != "edit" && channel != "delete" {
							seed := prepareChannel(c.Route.Input, strings.TrimSuffix(channel, "s"))
							c.Draft = applyChannelPrefill(c.Draft, seed)
							c.Warnings = seed.Warnings
						}
						if err = c.advance(r, s); err != nil {
							failure(w, err)
							return
						}
					}
				case "awaiting_answer":
					if c.answer(in.Field, value) {
						if err = c.advance(r, s); err != nil {
							failure(w, err)
							return
						}
					}
				case "awaiting_confirmation":
					if isConfirm(value) {
						c.State = "confirming"
					} else if c.correction(in.Field, value) {
						if err = c.advance(r, s); err != nil {
							failure(w, err)
							return
						}
					}
				case "awaiting_target", "awaiting_field", "awaiting_change":
					c.mutationReply(r.Context(), s, value)
				case "awaiting_delete_confirmation":
					if isConfirm(value) {
						c.State = "deleting"
					}
				}
			}
			if c.State == "applying_edit" {
				if err = c.snapshotMutation(r.Context(), s); err != nil {
					failure(w, err)
					return
				}
			}
			c.Messages = append(c.Messages, conversationTurn{Role: "assistant", Text: c.prompt()})
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
			c.Messages = append(c.Messages, conversationTurn{Role: "assistant", Text: c.prompt()})
			d, err = s.SaveSession(r.Context(), d.ID, d.Version, c)
			if err != nil {
				failure(w, err)
				return
			}
		}
		if c.State == "confirming" {
			saved, _, commitErr := s.CommitCapture(r.Context(), "routing.session."+d.ID, c.Proposal.Input)
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
			c.Messages = append(c.Messages, conversationTurn{Role: "assistant", Text: c.prompt()})
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
