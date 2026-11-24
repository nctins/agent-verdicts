package mcp_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"agent-verdicts/internal/reads"
)

// flakyReads fails every Record while fail is set.
type flakyReads struct {
	reads.Repository
	fail atomic.Bool
}

func (f *flakyReads) Record(ctx context.Context, projectID, actorID string, k reads.Kind, ids []string) error {
	if f.fail.Load() {
		return errors.New("disk full")
	}
	return f.Repository.Record(ctx, projectID, actorID, k, ids)
}

func (e *env) readStats(t *testing.T) map[string]reads.Item {
	t.Helper()
	today, _ := time.Parse(time.DateOnly, time.Now().UTC().Format(time.DateOnly))
	s, err := e.reads.Stats(context.Background(), "p", today, today)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]reads.Item{}
	for _, it := range s.Items {
		out[it.ID] = it
	}
	return out
}

func TestReads_CountedForOthersNotAuthor(t *testing.T) {
	e := newEnv(t)
	id := e.call(t, "create_contribution", map[string]any{"title": "Pool size is 20 connections"})["id"].(string)
	// The author's own reads do not count.
	e.call(t, "observe_trust_signal", map[string]any{"contribution_id": id})
	e.call(t, "reference_contribution", map[string]any{"contribution_id": id})
	e.call(t, "search", map[string]any{"query": "pool size"})
	if got := e.readStats(t); len(got) != 0 {
		t.Fatalf("author reads counted: %+v", got)
	}

	other, token := e.agent(t, "reader")
	e.grant(t, other, "contributor", "")
	r := e.as(t, token)
	r.call(t, "observe_trust_signal", map[string]any{"contribution_id": id})
	r.call(t, "observe_trust_signal", map[string]any{"contribution_id": id})
	r.call(t, "reference_contribution", map[string]any{"contribution_id": id})
	if res := r.call(t, "search", map[string]any{"query": "pool size"}); len(res["results"].([]any)) != 1 {
		t.Fatalf("search = %+v", res)
	}
	r.call(t, "list_contributions", map[string]any{}) // browsing is not counted
	r.callErr(t, "observe_trust_signal", map[string]any{"contribution_id": "00000000-0000-4000-8000-000000000000"})

	got := e.readStats(t)[id]
	if got.Observe != (reads.Count{Reads: 2, Readers: 1}) || got.Reference != (reads.Count{Reads: 1, Readers: 1}) || got.SearchHits != 1 {
		t.Fatalf("counts = %+v", got)
	}
}

func TestReads_RecordFailureKeepsReadWorking(t *testing.T) {
	e := newEnv(t)
	id := e.call(t, "create_contribution", map[string]any{"title": "Cache TTL is 60 seconds"})["id"].(string)
	other, token := e.agent(t, "reader")
	e.grant(t, other, "contributor", "")
	r := e.as(t, token)
	e.reads.fail.Store(true)
	if out := r.call(t, "observe_trust_signal", map[string]any{"contribution_id": id}); out["state"] != "InsufficientEvidence" {
		t.Fatalf("observe = %+v", out)
	}
	r.call(t, "reference_contribution", map[string]any{"contribution_id": id})
	if res := r.call(t, "search", map[string]any{"query": "cache TTL"}); len(res["results"].([]any)) != 1 {
		t.Fatalf("search = %+v", res)
	}
	e.reads.fail.Store(false)
	if got := e.readStats(t); len(got) != 0 {
		t.Fatalf("counts = %+v", got)
	}
}
