package httpapi

import (
	"atlas/internal/routing"
	"atlas/internal/store"
	"context"
	"fmt"
	"strings"
)

type relationListing struct {
	Candidates []store.BatchTarget `json:"candidates,omitempty"`
	Target     *store.BatchTarget  `json:"target,omitempty"`
}

func relationListIntent(text string) bool {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(text)))
	for len(words) > 0 && (words[0] == "please" || words[0] == "hey") {
		words = words[1:]
	}
	if len(words) >= 2 && (words[0] == "can" || words[0] == "could" || words[0] == "would") && words[1] == "you" {
		words = words[2:]
	}
	if len(words) == 0 {
		return false
	}
	verb := strings.Trim(words[0], "?!.,")
	if verb != "show" && verb != "list" && verb != "find" && verb != "what" && verb != "which" {
		return false
	}
	lower := strings.ToLower(text)
	return strings.Contains(lower, "relation") || strings.Contains(lower, "relationship") || strings.Contains(lower, "related to") || strings.Contains(lower, "linked records")
}

func localRelationListRoute(service routingService, name string, input routing.Request) routing.Result {
	result := localBatchRoute(service, name, input)
	result.Category, result.SelectedTool, result.SelectedChannel, result.ExtractionModel = "lookup", "list_relations", "relation_list", "local_relation_scope"
	result.RoutingToken = service.sign(result)
	return result
}

func (c *routingConversation) prepareRelationList(ctx context.Context, s *store.Store) error {
	c.RelationList = &relationListing{}
	items, err := batchInventory(ctx, s, []string{"task", "reminder", "note"})
	if err != nil {
		return err
	}
	lower := strings.ToLower(c.Route.Input.Text)
	// A record title in the request binds the read to that exact record. If
	// names collide, require a choice instead of returning a misleading subset.
	matches := exactTitleMatches(lower, items)
	if len(matches) == 1 {
		c.RelationList.Target = &matches[0]
		return c.answerRelationList(ctx, s)
	}
	if len(matches) > 1 || strings.Contains(lower, " for ") || strings.Contains(lower, " of ") || strings.Contains(lower, " to ") || strings.Contains(lower, " with ") || strings.Contains(lower, " from ") || strings.Contains(lower, " between ") {
		c.RelationList.Candidates = items
		c.State = "awaiting_relation_target"
		return nil
	}
	return c.answerRelationList(ctx, s)
}

func (c *routingConversation) answerRelationList(ctx context.Context, s *store.Store) error {
	kind, id := "", ""
	if c.RelationList != nil && c.RelationList.Target != nil {
		kind, id = c.RelationList.Target.Kind, c.RelationList.Target.ID
	}
	list, err := s.Relations(ctx, kind, id)
	if err != nil {
		return err
	}
	answer := conversationAnswer{Sources: []conversationSource{}}
	if len(list) == 0 {
		answer.Text = "I found no saved relations."
	} else {
		lines := make([]string, 0, len(list))
		for _, rel := range list {
			lines = append(lines, fmt.Sprintf("%s “%s” (%s) is related to %s “%s” (%s)", rel.From.Type, rel.From.Title, rel.From.Status, rel.To.Type, rel.To.Title, rel.To.Status))
			for _, rec := range []store.RelationRecord{rel.From, rel.To} {
				if rec.Exists {
					answer.Sources = append(answer.Sources, conversationSource{Type: rec.Type, Title: rec.Title, URL: "/#" + rec.Type + "s/" + rec.ID})
				}
			}
		}
		answer.Text = "Saved relations: " + strings.Join(lines, "; ") + "."
	}
	c.Answer, c.State = &answer, "answered"
	return nil
}

func (c *routingConversation) selectRelationTarget(ctx context.Context, s *store.Store, value string) bool {
	if c.RelationList == nil {
		return false
	}
	for _, item := range c.RelationList.Candidates {
		if item.ID == strings.TrimSpace(value) {
			c.RelationList.Target = &item
			c.RelationList.Candidates = nil
			return c.answerRelationList(ctx, s) == nil
		}
	}
	return false
}
