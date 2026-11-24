package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/adapter/cli"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/project"
	"agent-verdicts/internal/signal"
)

type fx struct {
	deps     cli.Deps
	actor    actor.Actor
	t        *testing.T
	projects project.Repository
}

func newFx(t *testing.T) fx {
	pool := pgtest.NewMigrated(t)
	actors := postgres.NewActorRepo(pool)
	a := actor.New(actor.TypeAgent)
	a.Name = "Alice Agent"
	if err := actors.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	projects := postgres.NewProjectRepo(pool)
	if _, err := project.Create(context.Background(), projects, "p", "", ""); err != nil {
		t.Fatal(err)
	}
	return fx{t: t, actor: a, projects: projects, deps: cli.Deps{
		Contributions: postgres.NewContributionRepo(pool), Signals: postgres.NewSignalRepo(pool), Actors: actors,
	}}
}

func (f fx) contribution(kind contribution.Kind, title, answers string) contribution.Contribution {
	f.t.Helper()
	c, err := contribution.Create(context.Background(), f.projects, f.deps.Actors, f.deps.Contributions,
		contribution.NewInput{ProjectID: "p", ActorID: f.actor.ID, Kind: kind, Title: title, Body: "body of " + title, AnswersID: answers})
	if err != nil {
		f.t.Fatal(err)
	}
	return c
}

func (f fx) signal(c contribution.Contribution, k signal.Kind, s signal.Stance, body string) {
	f.t.Helper()
	_, err := signal.Create(context.Background(), f.deps.Contributions, f.deps.Actors, f.deps.Signals,
		signal.NewInput{ProjectID: "p", ContributionID: c.ID, ActorID: f.actor.ID, Kind: k, Stance: s, Body: body})
	if err != nil {
		f.t.Fatal(err)
	}
}

func run(f fx, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := cli.Run(context.Background(), args, &out, &errb, f.deps)
	return code, out.String(), errb.String()
}

func TestList_PrintsIDKindStateTitle(t *testing.T) {
	f := newFx(t)
	claim := f.contribution(contribution.KindClaim, "first claim", "")
	q := f.contribution(contribution.KindQuestion, "a question", "")
	// Another actor supports it: the author's own support would not count.
	r := actor.New(actor.TypeAgent)
	r.Name = "Reviewer"
	if err := f.deps.Actors.Create(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if _, err := signal.Create(context.Background(), f.deps.Contributions, f.deps.Actors, f.deps.Signals,
		signal.NewInput{ProjectID: "p", ContributionID: claim.ID, ActorID: r.ID, Kind: signal.KindAssessment, Stance: signal.StanceFavorable}); err != nil {
		t.Fatal(err)
	}

	code, out, _ := run(f, "list", "-project", "p")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %q", lines)
	}
	if lines[0] != q.ID+"  question  open  a question" { // newest first
		t.Errorf("line0 = %q", lines[0])
	}
	if lines[1] != claim.ID+"  claim  Favorable  first claim" {
		t.Errorf("line1 = %q", lines[1])
	}
}

func TestList_FiltersAndPaginates(t *testing.T) {
	f := newFx(t)
	for i := 0; i < 105; i++ {
		f.contribution(contribution.KindClaim, "c", "")
	}
	f.contribution(contribution.KindQuestion, "q", "")
	_, out, _ := run(f, "list", "-project", "p", "-kind", "claim")
	if n := strings.Count(out, "\n"); n != 105 {
		t.Fatalf("claim lines = %d, want 105 (all pages)", n)
	}
	_, out, _ = run(f, "list", "-project", "p", "-state", "Favorable")
	if out != "" {
		t.Fatalf("out = %q, want empty", out)
	}
}

func TestList_Errors(t *testing.T) {
	f := newFx(t)
	if code, _, e := run(f, "list", "-project", "empty"); code != 0 || e != "" {
		t.Errorf("empty project: code=%d stderr=%q", code, e)
	}
	if code, _, e := run(f, "list", "-project", ""); code != 2 || e == "" {
		t.Errorf("invalid project: code=%d stderr=%q", code, e)
	}
	if code, _, _ := run(f, "list", "-project", "p", "-kind", "nope"); code != 2 {
		t.Errorf("bad kind code=%d", code)
	}
	if code, _, _ := run(f, "list", "-project", "p", "-state", "nope"); code != 2 {
		t.Errorf("bad state code=%d", code)
	}
}
