package contribution_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/platform/textfold"
	"agent-verdicts/internal/project"

	"github.com/jackc/pgx/v5/pgxpool"
)

type env struct {
	pool     *pgxpool.Pool
	projects project.Repository
	actors   actor.Repository
	repo     contribution.Repository
	actorID  string
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := pgtest.NewMigrated(t)
	e := env{pool: pool, projects: postgres.NewProjectRepo(pool), actors: postgres.NewActorRepo(pool), repo: postgres.NewContributionRepo(pool)}
	a := actor.New(actor.TypeAgent)
	a.Name = "a"
	if err := e.actors.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	e.actorID = a.ID
	if _, err := project.Create(context.Background(), e.projects, "p", "", ""); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e env) create(in contribution.NewInput) (contribution.Contribution, error) {
	if in.ProjectID == "" {
		in.ProjectID = "p"
	}
	if in.ActorID == "" {
		in.ActorID = e.actorID
	}
	return contribution.Create(context.Background(), e.projects, e.actors, e.repo, in)
}

func (e env) mustCreate(t *testing.T, in contribution.NewInput) contribution.Contribution {
	t.Helper()
	c, err := e.create(in)
	if err != nil {
		t.Fatalf("Create(%+v): %v", in, err)
	}
	return c
}

func TestCreate_ValidatesTitle(t *testing.T) {
	e := newEnv(t)
	for _, title := range []string{"", "   ", strings.Repeat("a", 201)} {
		if _, err := e.create(contribution.NewInput{Title: title}); !errors.Is(err, contribution.ErrInvalidArgument) {
			t.Errorf("title len %d: err = %v, want ErrInvalidArgument", len(title), err)
		}
	}
	long := strings.Repeat("ế", 200)
	c := e.mustCreate(t, contribution.NewInput{Title: "  " + long + "\n"})
	if c.Title != long {
		t.Fatalf("title not trimmed: %q", c.Title)
	}
	if c.Kind != contribution.KindClaim {
		t.Fatalf("default kind = %q, want claim", c.Kind)
	}
	got, err := e.repo.Get(context.Background(), "p", c.ID)
	if err != nil || got.Title != long || !got.CreatedAt.Equal(c.CreatedAt) {
		t.Fatalf("Get = %+v, %v", got, err)
	}
}

func TestCreate_ValidatesKind(t *testing.T) {
	e := newEnv(t)
	if _, err := e.create(contribution.NewInput{Title: "t", Kind: "essay"}); !errors.Is(err, contribution.ErrInvalidArgument) {
		t.Fatalf("err = %v, want ErrInvalidArgument", err)
	}
}

func TestCreate_BodyLimit(t *testing.T) {
	e := newEnv(t)
	e.mustCreate(t, contribution.NewInput{Title: "t", Body: strings.Repeat("x", 65536)})
	if _, err := e.create(contribution.NewInput{Title: "t", Body: strings.Repeat("x", 65537)}); !errors.Is(err, contribution.ErrInvalidArgument) {
		t.Fatalf("err = %v, want ErrInvalidArgument", err)
	}
}

func TestCreate_RejectsInvalidUTF8(t *testing.T) {
	e := newEnv(t)
	for _, in := range []contribution.NewInput{{Title: "bad \xff"}, {Title: "t", Body: "bad \xff"}, {Title: "a\x00b"}, {Title: "t", Body: "a\x00b"}} {
		if _, err := e.create(in); !errors.Is(err, contribution.ErrInvalidArgument) {
			t.Errorf("%+v: err = %v, want ErrInvalidArgument", in, err)
		}
	}
}

func TestCreate_AnswerRules(t *testing.T) {
	e := newEnv(t)
	if _, err := project.Create(context.Background(), e.projects, "other", "", ""); err != nil {
		t.Fatal(err)
	}
	claim := e.mustCreate(t, contribution.NewInput{Title: "c"})
	q := e.mustCreate(t, contribution.NewInput{Title: "q", Kind: contribution.KindQuestion})
	otherQ := e.mustCreate(t, contribution.NewInput{ProjectID: "other", Title: "q2", Kind: contribution.KindQuestion})

	cases := []struct {
		name string
		in   contribution.NewInput
		want error
	}{
		{"answers a claim", contribution.NewInput{Title: "a", AnswersID: claim.ID}, contribution.ErrInvalidArgument},
		{"question in other project", contribution.NewInput{Title: "a", AnswersID: otherQ.ID}, contribution.ErrNotFound},
		{"missing id", contribution.NewInput{Title: "a", AnswersID: id.New()}, contribution.ErrNotFound},
		{"malformed id", contribution.NewInput{Title: "a", AnswersID: "nope"}, contribution.ErrNotFound},
		{"question answering", contribution.NewInput{Title: "a", Kind: contribution.KindQuestion, AnswersID: q.ID}, contribution.ErrInvalidArgument},
	}
	for _, tc := range cases {
		if _, err := e.create(tc.in); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	a := e.mustCreate(t, contribution.NewInput{Title: "a", AnswersID: q.ID})
	if a.AnswersID != q.ID || a.State() == nil {
		t.Fatalf("answer = %+v", a)
	}
	if q.State() != nil {
		t.Fatal("question must have nil state")
	}
}

func TestCreate_UnknownProject(t *testing.T) {
	e := newEnv(t)
	if _, err := e.create(contribution.NewInput{ProjectID: "fresh", Title: "t"}); !errors.Is(err, project.ErrNotFound) {
		t.Fatalf("err = %v, want project.ErrNotFound", err)
	}
	if _, err := e.projects.Get(context.Background(), "fresh"); !errors.Is(err, project.ErrNotFound) {
		t.Fatalf("project must not be created: %v", err)
	}
}

func TestCreate_UnknownActor(t *testing.T) {
	e := newEnv(t)
	if _, err := e.create(contribution.NewInput{Title: "t", ActorID: id.New()}); !errors.Is(err, contribution.ErrActorNotFound) {
		t.Fatalf("err = %v, want ErrActorNotFound", err)
	}
}

func TestCreate_WritesFoldedSearchText(t *testing.T) {
	e := newEnv(t)
	c := e.mustCreate(t, contribution.NewInput{Title: "Tiếng Việt", Body: "Đường ĐI"})
	var got string
	if err := e.pool.QueryRow(context.Background(), `SELECT search_text FROM contribution WHERE id = $1`, c.ID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if want := textfold.Fold("Tiếng Việt\nĐường ĐI"); got != want {
		t.Fatalf("search_text = %q, want %q", got, want)
	}
}

func TestSupersedes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	other := actor.New(actor.TypeAgent)
	other.Name = "o"
	if err := e.actors.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	old := e.mustCreate(t, contribution.NewInput{Title: "old"})
	q := e.mustCreate(t, contribution.NewInput{Title: "q", Kind: contribution.KindQuestion})
	if _, err := project.Create(ctx, e.projects, "other", "", ""); err != nil {
		t.Fatal(err)
	}
	foreign, err := e.create(contribution.NewInput{ProjectID: "other", Title: "f"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		in   contribution.NewInput
		want error
	}{
		{"new one is a question", contribution.NewInput{Title: "n", Kind: contribution.KindQuestion, SupersedesID: old.ID}, contribution.ErrInvalidArgument},
		{"old one is a question", contribution.NewInput{Title: "n", SupersedesID: q.ID}, contribution.ErrInvalidArgument},
		{"other project", contribution.NewInput{Title: "n", SupersedesID: foreign.ID}, contribution.ErrNotFound},
		{"unknown id", contribution.NewInput{Title: "n", SupersedesID: id.New()}, contribution.ErrNotFound},
		{"not the author", contribution.NewInput{Title: "n", ActorID: other.ID, SupersedesID: old.ID}, access.ErrForbidden},
	}
	for _, tc := range cases {
		if _, err := e.create(tc.in); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if got, _ := e.repo.Get(ctx, "p", old.ID); got.SupersededByID != "" {
		t.Fatalf("a rejected create superseded the claim: %+v", got)
	}
	n := e.mustCreate(t, contribution.NewInput{Title: "new", SupersedesID: old.ID})
	got, _ := e.repo.Get(ctx, "p", old.ID)
	if got.SupersededByID != n.ID || got.SupersededAt == nil {
		t.Fatalf("old = %+v", got)
	}
	if _, err := e.create(contribution.NewInput{Title: "again", SupersedesID: old.ID}); !errors.Is(err, contribution.ErrAlreadySuperseded) {
		t.Fatalf("already superseded: err = %v", err)
	}
	// a moderator may replace someone else's claim
	theirs := e.mustCreate(t, contribution.NewInput{Title: "theirs"})
	if _, err := e.create(contribution.NewInput{Title: "mod", ActorID: other.ID, SupersedesID: theirs.ID, CanModerate: true}); err != nil {
		t.Fatal(err)
	}
	// undo: not the author is refused, the author and a moderator may
	if err := contribution.Unsupersede(ctx, e.repo, "p", old.ID, other.ID, false); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("undo by stranger: err = %v", err)
	}
	if err := contribution.Unsupersede(ctx, e.repo, "p", old.ID, e.actorID, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.repo.Get(ctx, "p", old.ID); got.SupersededByID != "" || got.SupersededAt != nil {
		t.Fatalf("not undone: %+v", got)
	}
	if err := contribution.Unsupersede(ctx, e.repo, "p", theirs.ID, other.ID, true); err != nil {
		t.Fatal(err)
	}
}

func TestHiddenContribution_NotVisibleOrReferencable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	q := e.mustCreate(t, contribution.NewInput{Title: "q", Kind: contribution.KindQuestion})
	if err := e.repo.SetHidden(ctx, q.ID, e.actorID, "", true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.create(contribution.NewInput{Title: "a", AnswersID: q.ID}); !errors.Is(err, contribution.ErrNotFound) {
		t.Fatalf("answer to a hidden question: err = %v", err)
	}
	if _, err := e.create(contribution.NewInput{Title: "a", AnswersID: q.ID, CanModerate: true}); err != nil {
		t.Fatal(err)
	}
	page, _ := e.repo.List(ctx, "p", contribution.ListFilter{})
	if len(page.Items) != 1 { // only the answer
		t.Fatalf("list = %d items", len(page.Items))
	}
	page, _ = e.repo.List(ctx, "p", contribution.ListFilter{IncludeHidden: true})
	if len(page.Items) != 2 {
		t.Fatalf("list with hidden = %d items", len(page.Items))
	}
}

func TestCreate_Sources(t *testing.T) {
	e := newEnv(t)
	c := e.mustCreate(t, contribution.NewInput{Title: "t", Sources: []string{" https://x.test/a ", "abc123"}})
	got, err := e.repo.Get(context.Background(), "p", c.ID)
	if err != nil || len(got.Sources) != 2 || got.Sources[0] != "https://x.test/a" || got.Sources[1] != "abc123" {
		t.Fatalf("stored = %#v, %v", got.Sources, err)
	}
	if none := e.mustCreate(t, contribution.NewInput{Title: "t"}); none.Sources == nil || len(none.Sources) != 0 {
		t.Fatalf("no sources = %#v", none.Sources)
	}
	many := make([]string, contribution.MaxSources+1)
	for i := range many {
		many[i] = "s"
	}
	for name, src := range map[string][]string{
		"too many": many, "empty": {""}, "too long": {strings.Repeat("x", 501)}, "tab": {"a\tb"}, "nul": {"a\x00b"}, "bad utf8": {"\xff"},
	} {
		if _, err := e.create(contribution.NewInput{Title: "t", Sources: src}); !errors.Is(err, contribution.ErrInvalidArgument) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	e.mustCreate(t, contribution.NewInput{Title: "t", Sources: many[:contribution.MaxSources]})
	e.mustCreate(t, contribution.NewInput{Title: "t", Sources: []string{strings.Repeat("é", 500)}})
}
