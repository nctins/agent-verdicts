package signal_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/project"
	"agent-verdicts/internal/signal"
	"agent-verdicts/internal/trust"
)

func (e env) setThreshold(t *testing.T, n int) {
	t.Helper()
	if _, err := project.Update(context.Background(), e.projects, "p", nil, nil, &n, nil); err != nil {
		t.Fatal(err)
	}
}

// Spec 2026-11-15-trust-threshold-sources D2: the threshold counts actors, not signals.
func TestThreshold_CountsDistinctActors(t *testing.T) {
	e := newEnv(t)
	e.setThreshold(t, 2)
	for i := 0; i < 3; i++ { // one actor, three favorable challenges from before D3 (written straight to the repository)
		legacy := signal.Signal{ID: id.New(), ContributionID: e.claim.ID, ActorID: e.actorID, Kind: signal.KindChallenge, Stance: fav, Body: "yes", CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
		if _, err := e.repo.Insert(context.Background(), legacy); err != nil {
			t.Fatal(err)
		}
	}
	c, _ := e.contribs.Get(context.Background(), "p", e.claim.ID)
	if c.Counts.Favorable != 3 || c.Counts.FavorableActors != 1 || *c.State() != trust.StateInsufficientEvidence {
		t.Fatalf("one supporter: %+v state %s", c.Counts, *c.State())
	}
	other := e.newActor(t)
	e.mustCreate(t, signal.NewInput{ActorID: other, Kind: signal.KindAssessment, Stance: fav})
	if got := e.state(t); got != trust.StateFavorable {
		t.Fatalf("two supporters: state = %s", got)
	}
	// the state follows the project's threshold at once, with no rewrite of the claim
	e.setThreshold(t, 3)
	if got := e.state(t); got != trust.StateInsufficientEvidence {
		t.Fatalf("threshold 3: state = %s", got)
	}
	e.setThreshold(t, 1)
	if got := e.state(t); got != trust.StateFavorable {
		t.Fatalf("threshold 1: state = %s", got)
	}
}

func TestThreshold_ActorsFollowWithdrawHideDisable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	other := e.newActor(t)
	a := e.mustCreate(t, signal.NewInput{Kind: signal.KindAssessment, Stance: fav}).Signal
	b := e.mustCreate(t, signal.NewInput{ActorID: other, Kind: signal.KindAssessment, Stance: fav}).Signal
	actors := func() int { return e.counts(t).FavorableActors }
	if actors() != 2 {
		t.Fatalf("actors = %d", actors())
	}
	if err := e.withdraw(e.actorID, a.ID); err != nil || actors() != 1 {
		t.Fatalf("withdraw: %v, actors = %d", err, actors())
	}
	if err := signal.SetHidden(ctx, e.contribs, e.repo, "p", e.claim.ID, b.ID, other, "", true); err != nil || actors() != 0 {
		t.Fatalf("hide: %v, actors = %d", err, actors())
	}
	if err := signal.SetHidden(ctx, e.contribs, e.repo, "p", e.claim.ID, b.ID, other, "", false); err != nil || actors() != 1 {
		t.Fatalf("unhide: %v, actors = %d", err, actors())
	}
	now := time.Now()
	if err := e.actors.SetDisabled(ctx, other, &now); err != nil || actors() != 0 {
		t.Fatalf("disable: %v, actors = %d", err, actors())
	}
	if err := e.actors.SetDisabled(ctx, other, nil); err != nil || actors() != 1 {
		t.Fatalf("enable: %v, actors = %d", err, actors())
	}
	// re-assessing replaces: the actor still counts once
	e.mustCreate(t, signal.NewInput{ActorID: other, Kind: signal.KindAssessment, Stance: fav})
	if actors() != 1 {
		t.Fatalf("re-assess: actors = %d", actors())
	}
	e.mustCreate(t, signal.NewInput{ActorID: other, Kind: signal.KindAssessment, Stance: unfav})
	if actors() != 0 {
		t.Fatalf("changed to unfavorable: actors = %d", actors())
	}
}

func TestSources_StoredOnSignalAndLimited(t *testing.T) {
	e := newEnv(t)
	r := e.mustCreate(t, signal.NewInput{Kind: signal.KindAssessment, Stance: fav, Sources: []string{" https://x.test ", "src/a.go"}})
	got, err := e.repo.Get(context.Background(), r.Signal.ID)
	if err != nil || len(got.Sources) != 2 || got.Sources[0] != "https://x.test" {
		t.Fatalf("stored = %+v, %v", got.Sources, err)
	}
	plain := e.mustCreate(t, signal.NewInput{ActorID: e.newActor(t), Kind: signal.KindAssessment, Stance: fav})
	if got, _ := e.repo.Get(context.Background(), plain.Signal.ID); got.Sources == nil || len(got.Sources) != 0 {
		t.Fatalf("no sources = %#v", got.Sources)
	}
	for name, src := range map[string][]string{
		"too many":     make([]string, contribution.MaxSources+1),
		"empty":        {"  "},
		"too long":     {strings.Repeat("x", 501)},
		"control char": {"a\tb"},
		"newline":      {"a\nb"},
	} {
		if name == "too many" {
			for i := range src {
				src[i] = "s"
			}
		}
		if _, err := e.create(signal.NewInput{Kind: signal.KindAssessment, Stance: fav, Sources: src}); !errors.Is(err, signal.ErrInvalidArgument) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	e.mustCreate(t, signal.NewInput{Kind: signal.KindAssessment, Stance: fav, Sources: []string{strings.Repeat("é", 500)}})
}
