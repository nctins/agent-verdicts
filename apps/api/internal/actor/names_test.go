package actor_test

import (
	"context"
	"errors"
	"testing"

	"agent-verdicts/internal/actor"
)

type countingRepo struct {
	actor.Repository
	gets  int
	byID  map[string]actor.Actor
	fails bool
}

func (c *countingRepo) Get(_ context.Context, id string) (actor.Actor, error) {
	c.gets++
	if c.fails {
		return actor.Actor{}, errors.New("boom")
	}
	a, ok := c.byID[id]
	if !ok {
		return actor.Actor{}, actor.ErrNotFound
	}
	return a, nil
}

func TestNameResolver_CachesAndFallsBack(t *testing.T) {
	repo := &countingRepo{byID: map[string]actor.Actor{
		"a1": {ID: "a1", Name: "Alice"},
		"a2": {ID: "a2", Name: ""},
	}}
	r := actor.NewNameResolver(repo)
	ctx := context.Background()

	if got := r.Name(ctx, "a1"); got != "Alice" {
		t.Fatalf("Name(a1) = %q, want Alice", got)
	}
	if got := r.Name(ctx, "a1"); got != "Alice" {
		t.Fatalf("second Name(a1) = %q, want Alice", got)
	}
	if got := r.Name(ctx, "a2"); got != "" {
		t.Fatalf("Name(a2) = %q, want empty (legacy actor)", got)
	}
	if got := r.Name(ctx, "missing"); got != "" {
		t.Fatalf("Name(missing) = %q, want empty", got)
	}
	r.Name(ctx, "missing")
	if repo.gets != 3 {
		t.Fatalf("repo.Get called %d times, want 3 (one per distinct id)", repo.gets)
	}
}

func TestNameResolver_RepoErrorYieldsEmpty(t *testing.T) {
	r := actor.NewNameResolver(&countingRepo{fails: true})
	if got := r.Name(context.Background(), "x"); got != "" {
		t.Fatalf("Name() on failing repo = %q, want empty", got)
	}
}
