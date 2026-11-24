package signal_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/project"
	"agent-verdicts/internal/signal"
	"agent-verdicts/internal/trust"
)

type env struct {
	contribs contribution.Repository
	projects project.Repository
	actors   *postgres.ActorRepo
	repo     signal.Repository
	actorID  string
	claim    contribution.Contribution
	question contribution.Contribution
}

func newEnv(t *testing.T) env {
	t.Helper()
	ctx := context.Background()
	pool := pgtest.NewMigrated(t)
	e := env{contribs: postgres.NewContributionRepo(pool), actors: postgres.NewActorRepo(pool), repo: postgres.NewSignalRepo(pool)}
	e.actorID = e.newActor(t)
	projects := project.Repository(postgres.NewProjectRepo(pool))
	e.projects = projects
	if _, err := project.Create(ctx, projects, "p", "", ""); err != nil {
		t.Fatal(err)
	}
	// Another actor wrote them: the author's own support would not count (spec 2026-11-21-ignore-self-support).
	author := e.newActor(t)
	mk := func(kind contribution.Kind) contribution.Contribution {
		c, err := contribution.Create(ctx, projects, e.actors, e.contribs, contribution.NewInput{ProjectID: "p", ActorID: author, Kind: kind, Title: "t"})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	e.claim, e.question = mk(contribution.KindClaim), mk(contribution.KindQuestion)
	return e
}

func (e env) newActor(t *testing.T) string {
	t.Helper()
	a := actor.New(actor.TypeAgent)
	a.Name = "a-" + a.ID
	if err := e.actors.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a.ID
}

func (e env) create(in signal.NewInput) (signal.Result, error) {
	if in.ProjectID == "" {
		in.ProjectID = "p"
	}
	if in.ContributionID == "" {
		in.ContributionID = e.claim.ID
	}
	if in.ActorID == "" {
		in.ActorID = e.actorID
	}
	return signal.Create(context.Background(), e.contribs, e.actors, e.repo, in)
}

func (e env) mustCreate(t *testing.T, in signal.NewInput) signal.Result {
	t.Helper()
	r, err := e.create(in)
	if err != nil {
		t.Fatalf("Create(%+v): %v", in, err)
	}
	return r
}

func (e env) counts(t *testing.T) trust.Counts {
	t.Helper()
	c, err := e.contribs.Get(context.Background(), "p", e.claim.ID)
	if err != nil {
		t.Fatal(err)
	}
	return c.Counts
}

const (
	fav   = signal.StanceFavorable
	unfav = signal.StanceUnfavorable
)

func TestCreate_Validation(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name string
		in   signal.NewInput
		want error
	}{
		{"unknown stance", signal.NewInput{Kind: signal.KindAssessment, Stance: "maybe"}, signal.ErrInvalidArgument},
		{"unknown kind", signal.NewInput{Kind: "vote", Stance: fav, Body: "b"}, signal.ErrInvalidArgument},
		{"empty challenge", signal.NewInput{Kind: signal.KindChallenge, Stance: unfav}, signal.ErrInvalidArgument},
		{"body too long", signal.NewInput{Kind: signal.KindChallenge, Stance: unfav, Body: strings.Repeat("x", 65537)}, signal.ErrInvalidArgument},
		{"invalid utf-8", signal.NewInput{Kind: signal.KindChallenge, Stance: unfav, Body: "\xff"}, signal.ErrInvalidArgument},
		{"NUL in body", signal.NewInput{Kind: signal.KindChallenge, Stance: unfav, Body: "a\x00b"}, signal.ErrInvalidArgument},
		{"missing contribution", signal.NewInput{ContributionID: id.New(), Kind: signal.KindAssessment, Stance: fav}, contribution.ErrNotFound},
		{"malformed contribution id", signal.NewInput{ContributionID: "nope", Kind: signal.KindAssessment, Stance: fav}, contribution.ErrNotFound},
		{"other project", signal.NewInput{ProjectID: "other", Kind: signal.KindAssessment, Stance: fav}, contribution.ErrNotFound},
		{"unknown actor", signal.NewInput{ActorID: id.New(), Kind: signal.KindAssessment, Stance: fav}, signal.ErrActorNotFound},
	}
	for _, tc := range cases {
		if _, err := e.create(tc.in); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	_, err := e.create(signal.NewInput{ContributionID: e.question.ID, Kind: signal.KindAssessment, Stance: fav})
	if !errors.Is(err, signal.ErrInvalidArgument) || !strings.Contains(err.Error(), "answer it instead") {
		t.Errorf("question: err = %v", err)
	}
	e.mustCreate(t, signal.NewInput{Kind: signal.KindAssessment, Stance: signal.StanceNeutral}) // empty assessment body is fine
	e.mustCreate(t, signal.NewInput{Kind: signal.KindChallenge, Stance: unfav, Body: strings.Repeat("x", 65536)})
	if got := e.counts(t); got != (trust.Counts{Unfavorable: 1, Neutral: 1}) {
		t.Fatalf("counts = %+v", got)
	}
}

func TestCreate_DisabledActor(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	if err := e.actors.SetDisabled(context.Background(), e.actorID, &now); err != nil {
		t.Fatal(err)
	}
	if _, err := e.create(signal.NewInput{Kind: signal.KindAssessment, Stance: fav}); !errors.Is(err, signal.ErrActorNotFound) {
		t.Fatalf("err = %v, want ErrActorNotFound", err)
	}
}

func TestAssessment_ReplacesPrevious(t *testing.T) {
	e := newEnv(t)
	steps := []struct {
		stance   signal.Stance
		replaced *signal.Stance
		want     trust.Counts
	}{
		{fav, nil, trust.Counts{Favorable: 1, FavorableActors: 1}},
		{unfav, ptr(fav), trust.Counts{Unfavorable: 1}},
		{unfav, ptr(unfav), trust.Counts{Unfavorable: 1}},
	}
	for i, s := range steps {
		r := e.mustCreate(t, signal.NewInput{Kind: signal.KindAssessment, Stance: s.stance, Body: "why"})
		if (r.ReplacedStance == nil) != (s.replaced == nil) || (r.ReplacedStance != nil && *r.ReplacedStance != *s.replaced) {
			t.Fatalf("step %d: replaced = %v, want %v", i, r.ReplacedStance, s.replaced)
		}
		if r.Signal.Stance != s.stance || r.Signal.SupersededAt != nil {
			t.Fatalf("step %d: signal = %+v", i, r.Signal)
		}
		if got := e.counts(t); got != s.want {
			t.Fatalf("step %d: counts = %+v, want %+v", i, got, s.want)
		}
	}
	all, err := e.repo.ListByContribution(context.Background(), e.claim.ID)
	if err != nil || len(all) != 3 {
		t.Fatalf("list = %d, %v", len(all), err)
	}
	if all[0].SupersededAt == nil || all[1].SupersededAt == nil || all[2].SupersededAt != nil {
		t.Fatalf("superseded flags wrong: %+v", all)
	}
}

func TestAssessment_PerActor(t *testing.T) {
	e := newEnv(t)
	other := e.newActor(t)
	e.mustCreate(t, signal.NewInput{Kind: signal.KindAssessment, Stance: fav})
	if r := e.mustCreate(t, signal.NewInput{ActorID: other, Kind: signal.KindAssessment, Stance: fav}); r.ReplacedStance != nil {
		t.Fatalf("other actor replaced %v", *r.ReplacedStance)
	}
	if got := e.counts(t); got != (trust.Counts{Favorable: 2, FavorableActors: 2}) {
		t.Fatalf("counts = %+v", got)
	}
}

func TestChallenge_NotLimited(t *testing.T) {
	e := newEnv(t)
	for range 2 {
		if r := e.mustCreate(t, signal.NewInput{Kind: signal.KindChallenge, Stance: unfav, Body: "no"}); r.ReplacedStance != nil {
			t.Fatal("challenge must not replace")
		}
	}
	// an assessment does not supersede challenges either
	e.mustCreate(t, signal.NewInput{Kind: signal.KindAssessment, Stance: unfav})
	if got := e.counts(t); got != (trust.Counts{Unfavorable: 3}) {
		t.Fatalf("counts = %+v", got)
	}
}

func ptr[T any](v T) *T { return &v }

func (e env) withdraw(actorID, signalID string) error {
	return signal.Withdraw(context.Background(), e.contribs, e.actors, e.repo, signal.WithdrawInput{ProjectID: "p", SignalID: signalID, ActorID: actorID})
}

func (e env) state(t *testing.T) trust.State {
	t.Helper()
	c, err := e.contribs.Get(context.Background(), "p", e.claim.ID)
	if err != nil {
		t.Fatal(err)
	}
	return *c.State()
}

func TestWithdraw(t *testing.T) {
	e := newEnv(t)
	other := e.newActor(t)
	e.mustCreate(t, signal.NewInput{Kind: signal.KindAssessment, Stance: fav})
	ch := e.mustCreate(t, signal.NewInput{ActorID: other, Kind: signal.KindChallenge, Stance: unfav, Body: "no"}).Signal
	if got := e.state(t); got != trust.StateConflict {
		t.Fatalf("state = %s, want Conflict", got)
	}
	if err := e.withdraw(e.actorID, ch.ID); !errors.Is(err, signal.ErrNotAuthor) {
		t.Fatalf("not the author: err = %v", err)
	}
	if got := e.counts(t); got != (trust.Counts{Favorable: 1, Unfavorable: 1, FavorableActors: 1}) {
		t.Fatalf("counts after refused withdraw = %+v", got)
	}
	if err := e.withdraw(other, ch.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.state(t); got != trust.StateFavorable {
		t.Fatalf("withdrawing the only challenge: state = %s, want Favorable", got)
	}
	before, _ := e.contribs.Get(context.Background(), "p", e.claim.ID)
	if err := e.withdraw(other, ch.ID); err != nil { // twice is a no-op
		t.Fatal(err)
	}
	after, _ := e.contribs.Get(context.Background(), "p", e.claim.ID)
	if after.Counts != before.Counts || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("second withdraw changed the claim: %+v -> %+v", before, after)
	}
	if err := e.withdraw(other, id.New()); !errors.Is(err, signal.ErrNotFound) {
		t.Fatalf("unknown signal: err = %v", err)
	}
	all, _ := e.repo.ListByContribution(context.Background(), e.claim.ID)
	if len(all) != 2 || all[1].WithdrawnAt == nil || all[0].WithdrawnAt != nil {
		t.Fatalf("history = %+v", all)
	}
	// a withdrawn assessment is not restored; the actor may assess again
	as := all[0]
	if err := e.withdraw(e.actorID, as.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.counts(t); got != (trust.Counts{}) {
		t.Fatalf("counts = %+v", got)
	}
	if r := e.mustCreate(t, signal.NewInput{Kind: signal.KindAssessment, Stance: unfav}); r.ReplacedStance != nil {
		t.Fatalf("replaced = %v, want nil: the withdrawn one is gone", *r.ReplacedStance)
	}
	if got := e.counts(t); got != (trust.Counts{Unfavorable: 1}) {
		t.Fatalf("counts = %+v", got)
	}
}

func TestSetHidden_RecountsAndDisabledActor(t *testing.T) {
	e := newEnv(t)
	other := e.newActor(t)
	a := e.mustCreate(t, signal.NewInput{Kind: signal.KindAssessment, Stance: fav}).Signal
	e.mustCreate(t, signal.NewInput{ActorID: other, Kind: signal.KindChallenge, Stance: unfav, Body: "no"})
	ctx := context.Background()
	if err := signal.SetHidden(ctx, e.contribs, e.repo, "p", e.claim.ID, a.ID, other, "", true); err != nil {
		t.Fatal(err)
	}
	if got := e.counts(t); got != (trust.Counts{Unfavorable: 1}) {
		t.Fatalf("hidden: counts = %+v", got)
	}
	if err := signal.SetHidden(ctx, e.contribs, e.repo, "p", e.question.ID, a.ID, other, "", false); !errors.Is(err, signal.ErrNotFound) {
		t.Fatalf("signal of another contribution: err = %v", err)
	}
	if err := signal.SetHidden(ctx, e.contribs, e.repo, "p", e.claim.ID, a.ID, other, "", false); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := e.actors.SetDisabled(ctx, other, &now); err != nil {
		t.Fatal(err)
	}
	if got := e.counts(t); got != (trust.Counts{Favorable: 1, FavorableActors: 1}) {
		t.Fatalf("disabled: counts = %+v", got)
	}
	all, _ := e.repo.ListByContribution(ctx, e.claim.ID)
	if !all[1].ActorDisabled || all[0].ActorDisabled {
		t.Fatalf("actor_disabled flags: %+v", all)
	}
	if err := e.actors.SetDisabled(ctx, other, nil); err != nil {
		t.Fatal(err)
	}
	if got := e.counts(t); got != (trust.Counts{Favorable: 1, Unfavorable: 1, FavorableActors: 1}) {
		t.Fatalf("enabled again: counts = %+v", got)
	}
}

// Spec 2026-11-21-ignore-self-support §3: the author's favorable signal is
// saved but reported as not counted; anyone else's, or the author's dispute, counts.
func TestCreate_AuthorSupportNotCounted(t *testing.T) {
	e := newEnv(t)
	author := e.claim.ActorID
	if r := e.mustCreate(t, signal.NewInput{ActorID: author, Kind: signal.KindAssessment, Stance: signal.StanceFavorable}); !r.NotCounted {
		t.Fatalf("author support: %+v", r)
	}
	if r := e.mustCreate(t, signal.NewInput{ActorID: author, Kind: signal.KindChallenge, Stance: signal.StanceUnfavorable, Body: "wrong"}); r.NotCounted {
		t.Fatalf("author dispute: %+v", r)
	}
	if r := e.mustCreate(t, signal.NewInput{Kind: signal.KindAssessment, Stance: signal.StanceFavorable}); r.NotCounted {
		t.Fatalf("other support: %+v", r)
	}
}
