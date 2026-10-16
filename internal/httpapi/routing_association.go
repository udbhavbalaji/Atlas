package httpapi

import (
	"atlas/internal/provider"
	"atlas/internal/routing"
	"atlas/internal/store"
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
)

type associationMutation struct {
	Operation      string                   `json:"operation"`
	Attach         bool                     `json:"attach"`
	FromKind       string                   `json:"from_kind,omitempty"`
	ToKind         string                   `json:"to_kind,omitempty"`
	FromCandidates []store.BatchTarget      `json:"from_candidates,omitempty"`
	ToCandidates   []store.BatchTarget      `json:"to_candidates,omitempty"`
	Change         *store.AssociationChange `json:"change,omitempty"`
}

func associationIntent(text string) *associationMutation {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) })
	for len(words) > 0 && (words[0] == "please" || words[0] == "hey") {
		words = words[1:]
	}
	if len(words) >= 2 && (words[0] == "can" || words[0] == "could" || words[0] == "would") && words[1] == "you" {
		words = words[2:]
	}
	if len(words) >= 3 && words[0] == "i" && words[1] == "want" && words[2] == "to" {
		words = words[3:]
	}
	if len(words) == 0 {
		return nil
	}
	verb := words[0]
	has := func(targets ...string) bool {
		for _, word := range words {
			for _, target := range targets {
				if word == target {
					return true
				}
			}
		}
		return false
	}
	if (verb == "add" || verb == "remove" || verb == "make" || verb == "set") && has("dependency", "dependencies", "depends") || (verb == "make" || verb == "set") && has("before") && has("task", "tasks") {
		return &associationMutation{Operation: "dependency", Attach: verb != "remove", FromKind: "task", ToKind: "task"}
	}
	if verb == "relate" || verb == "unrelate" || (verb == "add" || verb == "remove" || verb == "set") && has("relation", "relations", "relationship", "relationships") {
		return &associationMutation{Operation: "relation", Attach: verb != "unrelate" && verb != "remove"}
	}
	if (verb == "add" || verb == "remove") && has("link", "links") {
		if has("note", "notes") {
			toKind := ""
			if has("task", "tasks") && !has("reminder", "reminders") {
				toKind = "task"
			} else if has("reminder", "reminders") && !has("task", "tasks") {
				toKind = "reminder"
			}
			return &associationMutation{Operation: "note_link", Attach: verb == "add", FromKind: "note", ToKind: toKind}
		}
		if has("task", "tasks") && has("reminder", "reminders") {
			return &associationMutation{Operation: "reminder_task", Attach: verb == "add", FromKind: "reminder", ToKind: "task"}
		}
	}
	if verb == "remove" && has("from") && has("task", "tasks") && has("reminder", "reminders") && !has("note", "notes") {
		return &associationMutation{Operation: "reminder_task", Attach: false, FromKind: "reminder", ToKind: "task"}
	}
	if verb != "link" && verb != "unlink" && verb != "attach" && verb != "detach" {
		return nil
	}
	attach := verb == "link" || verb == "attach"
	if has("note", "notes") {
		toKind := ""
		if has("task", "tasks") && !has("reminder", "reminders") {
			toKind = "task"
		} else if has("reminder", "reminders") && !has("task", "tasks") {
			toKind = "reminder"
		}
		return &associationMutation{Operation: "note_link", Attach: attach, FromKind: "note", ToKind: toKind}
	}
	if has("reminder", "reminders") && has("task", "tasks") {
		return &associationMutation{Operation: "reminder_task", Attach: attach, FromKind: "reminder", ToKind: "task"}
	}
	return nil
}

func localAssociationRoute(service routingService, name string, input routing.Request) routing.Result {
	result := routing.Result{Version: routing.Version, RegistryVersion: routing.RegistryVersion, RequestID: input.RequestID, Provider: name, Mock: name == "mock", Category: "change", SelectedTool: "association", SelectedChannel: "association", State: "routed", Policy: routing.DefaultPolicy,
		Input: routing.State{Text: input.Text, Timezone: input.Timezone, ReferenceAt: time.Now().UTC().Format(time.RFC3339Nano), RecentConversation: input.RecentConversation, Context: []provider.ContextRecord{}}, ExtractionModel: "local_association_scope"}
	result.RoutingToken = service.sign(result)
	return result
}

func (c *routingConversation) prepareAssociation(ctx context.Context, s *store.Store) error {
	if c.Association == nil {
		c.Association = associationIntent(c.Route.Input.Text)
	}
	if c.Association == nil {
		return routing.ErrContract
	}
	a := c.Association
	fromKinds := []string{a.FromKind}
	if a.FromKind == "" {
		fromKinds = []string{"task", "reminder", "note"}
	}
	toKinds := []string{a.ToKind}
	if a.ToKind == "" {
		toKinds = []string{"task", "reminder", "note"}
		if a.Operation == "note_link" {
			toKinds = []string{"task", "reminder"}
		}
	}
	var err error
	a.FromCandidates, err = batchInventory(ctx, s, fromKinds)
	if err != nil {
		return err
	}
	a.ToCandidates, err = batchInventory(ctx, s, toKinds)
	if err != nil {
		return err
	}
	a.Change = nil
	if len(a.FromCandidates) == 0 || len(a.ToCandidates) == 0 {
		c.Answer = &conversationAnswer{Text: "I couldn't find the saved records needed for that relationship. Add the records first, then try again.", Sources: []conversationSource{}}
		c.State = "answered"
		return nil
	}
	c.State = "awaiting_association_targets"
	return nil
}

func (c *routingConversation) selectAssociationTargets(value string) bool {
	if c.Association == nil {
		return false
	}
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return false
	}
	find := func(items []store.BatchTarget, id string) (store.BatchTarget, bool) {
		for _, item := range items {
			if item.ID == id {
				return item, true
			}
		}
		return store.BatchTarget{}, false
	}
	from, foundFrom := find(c.Association.FromCandidates, strings.TrimSpace(parts[0]))
	to, foundTo := find(c.Association.ToCandidates, strings.TrimSpace(parts[1]))
	if !foundFrom || !foundTo || from.ID == to.ID {
		return false
	}
	change := store.AssociationChange{Operation: c.Association.Operation, Attach: c.Association.Attach, From: from, To: to}
	c.Association.Change = &change
	c.State = "awaiting_association_confirmation"
	return true
}

func associationVerb(a *associationMutation) string {
	switch a.Operation {
	case "note_link", "reminder_task":
		if a.Attach {
			return "Link"
		}
		return "Unlink"
	case "dependency":
		if a.Attach {
			return "Make the first task a prerequisite for"
		}
		return "Remove the prerequisite from"
	case "relation":
		if a.Attach {
			return "Relate"
		}
		return "Unrelate"
	}
	return "Change"
}

func (c *routingConversation) associationSummary() string {
	a := c.Association
	if a == nil || a.Change == nil {
		return ""
	}
	from, to := a.Change.From, a.Change.To
	if a.Operation == "dependency" {
		if a.Attach {
			return fmt.Sprintf("Make task “%s” a prerequisite for task “%s”", from.Title, to.Title)
		}
		return fmt.Sprintf("Remove task “%s” as a prerequisite for task “%s”", from.Title, to.Title)
	}
	return fmt.Sprintf("%s %s “%s” and %s “%s”", associationVerb(a), from.Kind, from.Title, to.Kind, to.Title)
}

func (c *routingConversation) commitAssociation(ctx context.Context, s *store.Store, key string) error {
	if c.Association == nil || c.Association.Change == nil {
		return routing.ErrContract
	}
	summary := c.associationSummary()
	if err := s.ApplyAssociationRequest(ctx, key, *c.Association.Change); err != nil {
		return err
	}
	c.Answer = &conversationAnswer{Text: summary + ".", Sources: []conversationSource{}}
	c.State = "answered"
	return nil
}
