package authentication_test

import (
	"context"
	"strings"
	"testing"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/postgres"
)

func TestBootstrap_CreatesActorAndIssuesWorkingCredential(t *testing.T) {
	db, repo := newTestRepos(t)
	actors := postgres.NewActorRepo(db)
	ctx := context.Background()

	actorID, token, err := authentication.Bootstrap(ctx, actors, repo, actor.TypeHuman, "Alice")
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if actorID == "" || token == "" {
		t.Fatalf("Bootstrap() = %q, %q, want both non-empty", actorID, token)
	}

	a, err := actors.Get(ctx, actorID)
	if err != nil {
		t.Fatalf("actors.Get(bootstrapped actorID) error = %v", err)
	}
	if a.Type != actor.TypeHuman {
		t.Fatalf("bootstrapped Actor.Type = %q, want %q", a.Type, actor.TypeHuman)
	}
	if a.Name != "Alice" {
		t.Fatalf("bootstrapped Actor.Name = %q, want %q", a.Name, "Alice")
	}

	gotActorID, err := authentication.Verify(ctx, actors, repo, token)
	if err != nil {
		t.Fatalf("Verify(bootstrap token) error = %v", err)
	}
	if gotActorID != actorID {
		t.Fatalf("Verify(bootstrap token) = %q, want %q", gotActorID, actorID)
	}
}

func TestBootstrap_TwoCalls_CreateTwoIndependentActors(t *testing.T) {
	db, repo := newTestRepos(t)
	actors := postgres.NewActorRepo(db)
	ctx := context.Background()

	id1, _, err := authentication.Bootstrap(ctx, actors, repo, actor.TypeAgent, "bot")
	if err != nil {
		t.Fatalf("Bootstrap() #1 error = %v", err)
	}
	id2, _, err := authentication.Bootstrap(ctx, actors, repo, actor.TypeAgent, "bot 2")
	if err != nil {
		t.Fatalf("Bootstrap() #2 error = %v", err)
	}
	if id1 == id2 {
		t.Fatal("two Bootstrap() calls produced the same Actor ID")
	}
}

func TestBootstrap_RejectsInvalidName(t *testing.T) {
	db, repo := newTestRepos(t)
	actors := postgres.NewActorRepo(db)
	for _, name := range []string{"", "   ", strings.Repeat("x", actor.MaxNameLen+1)} {
		if _, _, err := authentication.Bootstrap(context.Background(), actors, repo, actor.TypeAgent, name); err == nil {
			t.Errorf("Bootstrap(name=%q) error = nil, want validation error", name)
		}
	}
	all, err := actors.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("actors created despite invalid names: %d", len(all))
	}
}

func TestBootstrap_TrimsName(t *testing.T) {
	db, repo := newTestRepos(t)
	actors := postgres.NewActorRepo(db)
	id, _, err := authentication.Bootstrap(context.Background(), actors, repo, actor.TypeAgent, "  Bot  ")
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	a, _ := actors.Get(context.Background(), id)
	if a.Name != "Bot" {
		t.Fatalf("Name = %q, want %q", a.Name, "Bot")
	}
}
