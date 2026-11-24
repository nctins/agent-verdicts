package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agent-verdicts/internal/platform/postgres"
)

// Spec 2026-11-15-role-play-fixes §A.

// #1: the author still reads and lists their own hidden claim, flagged with the reason.
func TestHiddenContent_AuthorSeesOwn(t *testing.T) {
	e := newEnv(t)
	c := e.create(t, map[string]any{"title": "Quokka claim", "body": "quokka"})
	if err := postgres.NewContributionRepo(e.pool).SetHidden(context.Background(), c, otherActor(t, e), "off topic", true); err != nil {
		t.Fatal(err)
	}
	if ref := e.call(t, "reference_contribution", map[string]any{"contribution_id": c}); ref["hidden"] != true || ref["hidden_reason"] != "off topic" {
		t.Fatalf("author read: %v", ref)
	}
	if obs := e.call(t, "observe_trust_signal", map[string]any{"contribution_id": c}); obs["hidden"] != true || obs["hidden_reason"] != "off topic" {
		t.Fatalf("author observe: %v", obs)
	}
	mine := e.call(t, "list_contributions", map[string]any{"actor": "me"})["items"].([]any)
	if len(mine) != 1 || mine[0].(map[string]any)["hidden"] != true || mine[0].(map[string]any)["hidden_reason"] != "off topic" {
		t.Fatalf("actor=me list: %v", mine)
	}
	if n := len(e.call(t, "list_contributions", nil)["items"].([]any)); n != 0 {
		t.Fatalf("unfiltered list = %d items", n)
	}
	if n := len(e.call(t, "search", map[string]any{"query": "quokka"})["results"].([]any)); n != 0 {
		t.Fatalf("search = %d results", n)
	}
}

// #7: create_assessment has its own input: no reply_to, no challenge wording.
func TestSignalTools_SeparateSchemas(t *testing.T) {
	e := newEnv(t)
	res, err := e.s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	schema := map[string]string{}
	for _, tool := range res.Tools {
		b, _ := json.Marshal(tool.InputSchema)
		schema[tool.Name] = string(b)
	}
	if a := schema["create_assessment"]; strings.Contains(a, "reply_to") || strings.Contains(a, "challenge") || !strings.Contains(a, "favorable") {
		t.Errorf("create_assessment input schema: %s", a)
	}
	if c := schema["create_challenge"]; !strings.Contains(c, "reply_to") || strings.Contains(c, "assessment:") || strings.Contains(c, "favorable,") {
		t.Errorf("create_challenge input schema: %s", c)
	}
}

// #3 and #6 through MCP: same fields as the API.
func TestSupersede_SimilarAndSignalHint(t *testing.T) {
	e := newEnv(t)
	old := e.create(t, map[string]any{"title": "zebra caching rule"})
	q := e.create(t, map[string]any{"title": "which zebra caching rule applies", "kind": "question"})
	ans := e.call(t, "create_contribution", map[string]any{"title": "zebra caching rule for answers", "answers_id": q})
	if s := ans["similar"].([]any); len(s) != 1 || s[0].(map[string]any)["id"] != old {
		t.Fatalf("similar of an answer = %v", s)
	}
	repl := e.call(t, "create_contribution", map[string]any{"title": "zebra caching rule v2", "supersedes_id": old})
	for _, s := range repl["similar"].([]any) {
		if s.(map[string]any)["id"] == old {
			t.Fatalf("similar names the superseded claim: %v", repl["similar"])
		}
	}
	for tool, args := range map[string]map[string]any{
		"create_assessment": {"contribution_id": old, "stance": "favorable", "body": "checked it"},
		"create_challenge":  {"contribution_id": old, "stance": "neutral", "body": "fyi"},
	} {
		sb, _ := e.call(t, tool, args)["superseded_by"].(map[string]any)
		if sb["id"] != repl["id"] || sb["title"] != "zebra caching rule v2" {
			t.Errorf("%s superseded_by = %v", tool, sb)
		}
	}
	if out := e.call(t, "create_assessment", map[string]any{"contribution_id": repl["id"], "stance": "favorable", "body": "checked it"}); out["superseded_by"] != nil {
		t.Errorf("live claim superseded_by = %v", out["superseded_by"])
	}
}
