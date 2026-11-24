package postgres_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/search"
	"agent-verdicts/internal/signal"
	"agent-verdicts/internal/trust"

	"github.com/jackc/pgx/v5/pgxpool"
)

type cfix struct {
	t    *testing.T
	pool *pgxpool.Pool
	repo *postgres.ContributionRepo
	base time.Time
	n    int
}

func newCFix(t *testing.T) *cfix {
	pool := pgtest.NewMigrated(t)
	mustExec(t, pool, `INSERT INTO actor (id,type,name,created_at) VALUES ($1,'Agent','a',now())`, actorID)
	mustExec(t, pool, `INSERT INTO project (id,name,created_at) VALUES ('p','p', now()), ('other','other', now())`)
	return &cfix{t: t, pool: pool, repo: postgres.NewContributionRepo(pool), base: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

// add inserts a contribution with strictly increasing created_at and the given counts.
func (f *cfix) add(project string, kind contribution.Kind, answers string, c trust.Counts) contribution.Contribution {
	f.t.Helper()
	f.n++
	x := contribution.Contribution{ID: id.New(), ProjectID: project, ActorID: actorID, Kind: kind, Title: "t", AnswersID: answers,
		CreatedAt: f.base.Add(time.Duration(f.n) * time.Second)}
	if err := f.repo.Create(context.Background(), x, "t", ""); err != nil {
		f.t.Fatal(err)
	}
	if c.FavorableActors == 0 { // by default every favorable signal comes from its own actor
		c.FavorableActors = c.Favorable
	}
	mustExec(f.t, f.pool, `UPDATE contribution SET favorable_n=$1, unfavorable_n=$2, neutral_n=$3, favorable_actor_n=$4 WHERE id=$5`, c.Favorable, c.Unfavorable, c.Neutral, c.FavorableActors, x.ID)
	x.Counts = c
	return x
}

func (f *cfix) all(filter contribution.ListFilter) [][]string {
	f.t.Helper()
	var pages [][]string
	for {
		p, err := f.repo.List(context.Background(), "p", filter)
		if err != nil {
			f.t.Fatal(err)
		}
		var ids []string
		for _, c := range p.Items {
			ids = append(ids, c.ID)
		}
		pages = append(pages, ids)
		if p.NextCursor == "" {
			return pages
		}
		if len(pages) > 50 {
			f.t.Fatal("pagination does not terminate")
		}
		filter.Cursor = p.NextCursor
	}
}

func ids(cs []contribution.Contribution) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func TestContributionRepo_GetRoundtripAndNotFound(t *testing.T) {
	f := newCFix(t)
	ctx := context.Background()
	q := f.add("p", contribution.KindQuestion, "", trust.Counts{})
	a := f.add("p", contribution.KindClaim, q.ID, trust.Counts{Favorable: 2, Neutral: 1})
	got, err := f.repo.Get(ctx, "p", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AnswersID != q.ID || got.Counts != a.Counts || !got.CreatedAt.Equal(a.CreatedAt) || got.Kind != contribution.KindClaim || got.ActorID != actorID {
		t.Fatalf("Get = %+v, want %+v", got, a)
	}
	if s := got.State(); s == nil || *s != trust.StateFavorable {
		t.Fatalf("State = %v", s)
	}
	for _, tc := range [][2]string{{"other", a.ID}, {"nope", a.ID}, {"p", id.New()}, {"p", "not-a-uuid"}, {"p", "a\x00b"}, {"p", "\xff"}} {
		if _, err := f.repo.Get(ctx, tc[0], tc[1]); !errors.Is(err, contribution.ErrNotFound) {
			t.Errorf("Get(%q,%q) err = %v, want ErrNotFound", tc[0], tc[1], err)
		}
	}
}

func TestContributionRepo_GetByIDAnyProject(t *testing.T) {
	f := newCFix(t)
	ctx := context.Background()
	a := f.add("other", contribution.KindClaim, "", trust.Counts{Favorable: 1})
	got, err := f.repo.GetByID(ctx, a.ID)
	if err != nil || got.ID != a.ID || got.ProjectID != "other" || got.Counts != a.Counts {
		t.Fatalf("GetByID = %+v, %v", got, err)
	}
	for _, bad := range []string{id.New(), "not-a-uuid"} {
		if _, err := f.repo.GetByID(ctx, bad); !errors.Is(err, contribution.ErrNotFound) {
			t.Errorf("GetByID(%q) err = %v, want ErrNotFound", bad, err)
		}
	}
}

func TestList_CursorPagination(t *testing.T) {
	f := newCFix(t)
	var want []string
	for i := 0; i < 25; i++ {
		// counts 0,1,2 repeating -> lots of ties for debated/least
		want = append(want, f.add("p", contribution.KindClaim, "", trust.Counts{Neutral: i % 3}).ID)
	}
	f.add("other", contribution.KindClaim, "", trust.Counts{})

	pages := f.all(contribution.ListFilter{Limit: 10})
	if len(pages) != 3 || len(pages[0]) != 10 || len(pages[2]) != 5 {
		t.Fatalf("page sizes = %v", pages)
	}
	newest := slices.Concat(pages...)
	slices.Reverse(want)
	if !slices.Equal(newest, want) {
		t.Fatalf("newest order = %v, want %v", newest, want)
	}

	for _, s := range []contribution.Sort{contribution.SortDebated, contribution.SortLeast} {
		got := slices.Concat(f.all(contribution.ListFilter{Sort: s, Limit: 7})...)
		full, err := f.repo.List(context.Background(), "p", contribution.ListFilter{Sort: s, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, ids(full.Items)) || len(got) != 25 {
			t.Fatalf("%s paged %v != unpaged %v", s, got, ids(full.Items))
		}
		for i := 1; i < len(full.Items); i++ {
			a, b := full.Items[i-1].Counts.Neutral, full.Items[i].Counts.Neutral
			if (s == contribution.SortDebated && a < b) || (s == contribution.SortLeast && a > b) {
				t.Fatalf("%s not ordered at %d: %d then %d", s, i, a, b)
			}
		}
	}
}

func TestList_LimitDefaultAndClamp(t *testing.T) {
	f := newCFix(t)
	for i := 0; i < 105; i++ {
		f.add("p", contribution.KindClaim, "", trust.Counts{})
	}
	for _, tc := range []struct{ limit, want int }{{0, 20}, {500, 100}} {
		p, err := f.repo.List(context.Background(), "p", contribution.ListFilter{Limit: tc.limit})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Items) != tc.want || p.NextCursor == "" {
			t.Errorf("limit %d: got %d items, cursor %q", tc.limit, len(p.Items), p.NextCursor)
		}
	}
}

func TestList_BadCursor(t *testing.T) {
	f := newCFix(t)
	f.add("p", contribution.KindClaim, "", trust.Counts{})
	f.add("p", contribution.KindClaim, "", trust.Counts{})
	ctx := context.Background()
	p, err := f.repo.List(ctx, "p", contribution.ListFilter{Limit: 1})
	if err != nil || p.NextCursor == "" {
		t.Fatalf("List: %v %+v", err, p)
	}
	bad := []contribution.ListFilter{
		{Cursor: "zzz"},
		{Cursor: base64.RawURLEncoding.EncodeToString([]byte(`{"s":"newest","k":"x","id":"y"}`))},
		{Cursor: p.NextCursor, Sort: contribution.SortDebated},
		{Cursor: base64.RawURLEncoding.EncodeToString([]byte(`{"s":"newest","k":"2026-01-01T00:00:00Z","id":"a\u0000b"}`))},
	}
	for _, fl := range bad {
		if _, err := f.repo.List(ctx, "p", fl); !errors.Is(err, contribution.ErrBadCursor) || !errors.Is(err, contribution.ErrInvalidArgument) {
			t.Errorf("cursor %q sort %q: err = %v, want ErrBadCursor", fl.Cursor, fl.Sort, err)
		}
	}
	for _, actor := range []string{"a\x00b", "\xff"} {
		if _, err := f.repo.List(ctx, "p", contribution.ListFilter{ActorID: actor}); !errors.Is(err, contribution.ErrInvalidArgument) {
			t.Errorf("actor %q: err = %v, want ErrInvalidArgument", actor, err)
		}
	}
	if _, err := f.repo.List(ctx, "p", contribution.ListFilter{Sort: "random"}); !errors.Is(err, contribution.ErrInvalidArgument) {
		t.Errorf("bad sort: err = %v", err)
	}
}

func TestList_Filters(t *testing.T) {
	f := newCFix(t)
	ctx := context.Background()
	fav := f.add("p", contribution.KindClaim, "", trust.Counts{Favorable: 1})
	unf := f.add("p", contribution.KindClaim, "", trust.Counts{Unfavorable: 1})
	openQ := f.add("p", contribution.KindQuestion, "", trust.Counts{})
	resolvedQ := f.add("p", contribution.KindQuestion, "", trust.Counts{})
	ans := f.add("p", contribution.KindClaim, resolvedQ.ID, trust.Counts{Favorable: 1, Unfavorable: 1, Neutral: 1}) // Conflict
	f.add("p", contribution.KindClaim, resolvedQ.ID, trust.Counts{Favorable: 3})                                    // makes resolvedQ resolved

	list := func(fl contribution.ListFilter) []string {
		t.Helper()
		fl.Limit = 100
		p, err := f.repo.List(ctx, "p", fl)
		if err != nil {
			t.Fatal(err)
		}
		return ids(p.Items)
	}
	has := func(got []string, want ...string) bool {
		for _, w := range want {
			if !slices.Contains(got, w) {
				return false
			}
		}
		return true
	}

	if got := list(contribution.ListFilter{Kind: contribution.KindQuestion}); len(got) != 2 || !has(got, openQ.ID, resolvedQ.ID) {
		t.Errorf("kind=question: %v", got)
	}
	// States only: questions excluded.
	if got := list(contribution.ListFilter{States: []trust.State{trust.StateFavorable, trust.StateConflict}}); len(got) != 3 || !has(got, fav.ID, ans.ID) {
		t.Errorf("states fav/conflict: %v", got)
	}
	// QuestionStatus only: claims excluded.
	if got := list(contribution.ListFilter{QuestionStatus: []contribution.QuestionStatus{contribution.QuestionOpen}}); !slices.Equal(got, []string{openQ.ID}) {
		t.Errorf("status open: %v", got)
	}
	// Both: union of matching claims and matching questions.
	got := list(contribution.ListFilter{States: []trust.State{trust.StateUnfavorable}, QuestionStatus: []contribution.QuestionStatus{contribution.QuestionResolved}})
	if len(got) != 2 || !has(got, unf.ID, resolvedQ.ID) {
		t.Errorf("unfavorable+resolved: %v", got)
	}
	// Kind narrows further.
	if got := list(contribution.ListFilter{Kind: contribution.KindClaim, QuestionStatus: []contribution.QuestionStatus{contribution.QuestionOpen}}); len(got) != 0 {
		t.Errorf("claim+status open: %v", got)
	}
	if got := list(contribution.ListFilter{ActorID: id.New()}); len(got) != 0 {
		t.Errorf("unknown actor: %v", got)
	}
	if got := list(contribution.ListFilter{ActorID: "garbage"}); len(got) != 0 {
		t.Errorf("garbage actor: %v", got)
	}
	if got := list(contribution.ListFilter{ActorID: actorID}); len(got) != 6 {
		t.Errorf("actor: %v", got)
	}
}

func TestQuestionInfo(t *testing.T) {
	f := newCFix(t)
	ctx := context.Background()
	q := f.add("p", contribution.KindQuestion, "", trust.Counts{})
	info := func() contribution.QuestionInfo {
		t.Helper()
		m, err := f.repo.QuestionInfo(ctx, []string{q.ID})
		if err != nil {
			t.Fatal(err)
		}
		return m[q.ID]
	}
	if got := info(); got.Status != contribution.QuestionOpen || got.AnswerCount != 0 || got.BestAnswerState != nil {
		t.Fatalf("no answers: %+v", got)
	}
	f.add("p", contribution.KindClaim, q.ID, trust.Counts{Unfavorable: 1})
	if got := info(); got.Status != contribution.QuestionAnswered || got.AnswerCount != 1 || got.BestAnswerState == nil || *got.BestAnswerState != trust.StateUnfavorable {
		t.Fatalf("one unfavorable: %+v", got)
	}
	f.add("p", contribution.KindClaim, q.ID, trust.Counts{Favorable: 1})
	if got := info(); got.Status != contribution.QuestionResolved || got.AnswerCount != 2 || got.BestAnswerState == nil || *got.BestAnswerState != trust.StateFavorable {
		t.Fatalf("plus favorable: %+v", got)
	}
}

func TestContributionRepo_AnswersOrder(t *testing.T) {
	f := newCFix(t)
	q := f.add("p", contribution.KindQuestion, "", trust.Counts{})
	unf := f.add("p", contribution.KindClaim, q.ID, trust.Counts{Unfavorable: 1})
	ins := f.add("p", contribution.KindClaim, q.ID, trust.Counts{})
	fav2 := f.add("p", contribution.KindClaim, q.ID, trust.Counts{Favorable: 1})
	con := f.add("p", contribution.KindClaim, q.ID, trust.Counts{Favorable: 1, Unfavorable: 1})
	fav1 := f.add("p", contribution.KindClaim, q.ID, trust.Counts{Favorable: 5})
	f.add("p", contribution.KindClaim, "", trust.Counts{})
	mustExec(t, f.pool, `UPDATE contribution SET created_at = created_at - interval '1 hour' WHERE id = $1`, fav1.ID)

	got, err := f.repo.Answers(context.Background(), q.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{fav1.ID, fav2.ID, con.ID, ins.ID, unf.ID}; !slices.Equal(ids(got), want) {
		t.Fatalf("Answers = %v, want %v", ids(got), want)
	}
}

func TestContributionRepo_CountByProject(t *testing.T) {
	f := newCFix(t)
	q := f.add("p", contribution.KindQuestion, "", trust.Counts{})
	f.add("p", contribution.KindQuestion, "", trust.Counts{})
	f.add("p", contribution.KindClaim, q.ID, trust.Counts{})
	f.add("other", contribution.KindClaim, "", trust.Counts{})
	newer := f.add("other", contribution.KindClaim, "", trust.Counts{Favorable: 1, Unfavorable: 1})
	// Superseded: hidden from the default lists, so not counted either.
	old := f.add("other", contribution.KindClaim, "", trust.Counts{Favorable: 1, Unfavorable: 1})
	mustExec(t, f.pool, `UPDATE contribution SET superseded_by = $2, superseded_at = now() WHERE id = $1`, old.ID, newer.ID)
	// Last activity: the superseded claim counts, a hidden one does not.
	mustExec(t, f.pool, `UPDATE contribution SET updated_at = '2026-01-01T00:00:00Z' WHERE project_id IN ('p', 'other')`)
	mustExec(t, f.pool, `UPDATE contribution SET updated_at = '2026-02-01T00:00:00Z' WHERE id = $1`, old.ID)
	hidden := f.add("other", contribution.KindClaim, "", trust.Counts{})
	mustExec(t, f.pool, `UPDATE contribution SET hidden_at = now(), updated_at = '2026-03-01T00:00:00Z' WHERE id = $1`, hidden.ID)
	got, err := f.repo.CountByProject(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	last := map[string]string{}
	for p, c := range got {
		if c.LastActivity == nil {
			t.Fatalf("%s: LastActivity nil", p)
		}
		last[p] = c.LastActivity.UTC().Format("2006-01-02")
		c.LastActivity = nil
		got[p] = c
	}
	if want := map[string]string{"p": "2026-01-01", "other": "2026-02-01"}; !reflect.DeepEqual(last, want) {
		t.Fatalf("LastActivity = %v, want %v", last, want)
	}
	states := func(con, ins int) map[trust.State]int {
		return map[trust.State]int{trust.StateFavorable: 0, trust.StateUnfavorable: 0, trust.StateConflict: con, trust.StateInsufficientEvidence: ins}
	}
	want := map[string]contribution.ProjectCounts{
		"p":     {Contributions: 3, OpenQuestions: 1, States: states(0, 1)},
		"other": {Contributions: 2, States: states(1, 1)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CountByProject = %+v", got)
	}
}

// The view re-implements trust.StateOf/Rank in SQL; both run the same cases
// (internal/trust/testdata/state_cases.json), so changing one side alone fails.
func TestContributionView_MatchesSharedStateCases(t *testing.T) {
	b, err := os.ReadFile("../../trust/testdata/state_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases struct {
		State []struct {
			MinFavorable int `json:"min_favorable"`
			Counts       struct {
				Favorable, Unfavorable, Neutral int
				FavorableActors                 int `json:"favorable_actors"`
			} `json:"counts"`
			Want string `json:"want"`
		} `json:"state"`
	}
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	f := newCFix(t)
	ctx := context.Background()
	for _, tc := range cases.State {
		mustExec(t, f.pool, `UPDATE project SET min_favorable = $1 WHERE id = 'p'`, tc.MinFavorable)
		x := f.add("p", contribution.KindClaim, "", trust.Counts(tc.Counts))
		var state string
		var rank int
		if err := f.pool.QueryRow(ctx, `SELECT state, rank FROM contribution_v WHERE id = $1`, x.ID).Scan(&state, &rank); err != nil {
			t.Fatal(err)
		}
		if state != tc.Want || rank != trust.Rank(trust.State(tc.Want)) {
			t.Errorf("%+v min %d: view state=%s rank=%d, want %s", tc.Counts, tc.MinFavorable, state, rank, tc.Want)
		}
	}
}

// question_status in the view (List filter) must agree with QuestionInfo.
func TestQuestionStatus_ViewAgreesWithQuestionInfo(t *testing.T) {
	f := newCFix(t)
	ctx := context.Background()
	q := func(answer *trust.Counts) string {
		x := f.add("p", contribution.KindQuestion, "", trust.Counts{})
		if answer != nil {
			f.add("p", contribution.KindClaim, x.ID, *answer)
		}
		return x.ID
	}
	open := q(nil)
	unf := q(&trust.Counts{Unfavorable: 1})
	con := q(&trust.Counts{Favorable: 1, Unfavorable: 1})
	neu := q(&trust.Counts{Neutral: 2})
	fav := q(&trust.Counts{Favorable: 1, Neutral: 1})
	want := map[string]struct {
		status contribution.QuestionStatus
		best   trust.State
	}{
		open: {contribution.QuestionOpen, ""},
		unf:  {contribution.QuestionAnswered, trust.StateUnfavorable},
		con:  {contribution.QuestionAnswered, trust.StateConflict},
		neu:  {contribution.QuestionAnswered, trust.StateInsufficientEvidence},
		fav:  {contribution.QuestionResolved, trust.StateFavorable},
	}
	info, err := f.repo.QuestionInfo(ctx, []string{open, unf, con, neu, fav})
	if err != nil {
		t.Fatal(err)
	}
	for id, w := range want {
		got := info[id]
		if got.Status != w.status || (w.best == "") != (got.BestAnswerState == nil) || (got.BestAnswerState != nil && *got.BestAnswerState != w.best) {
			t.Errorf("QuestionInfo[%s] = %+v, want %+v", id, got, w)
		}
	}
	for _, s := range []contribution.QuestionStatus{contribution.QuestionOpen, contribution.QuestionAnswered, contribution.QuestionResolved} {
		p, err := f.repo.List(ctx, "p", contribution.ListFilter{QuestionStatus: []contribution.QuestionStatus{s}, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		var expect []string
		for id, w := range want {
			if w.status == s {
				expect = append(expect, id)
			}
		}
		got := ids(p.Items)
		slices.Sort(got)
		slices.Sort(expect)
		if !slices.Equal(got, expect) {
			t.Errorf("List(status=%s) = %v, want %v", s, got, expect)
		}
	}
}

func TestContributionRepo_Recent(t *testing.T) {
	f := newCFix(t)
	f.add("p", contribution.KindQuestion, "", trust.Counts{})
	f.add("p", contribution.KindClaim, "", trust.Counts{Favorable: 1})
	f.add("other", contribution.KindClaim, "", trust.Counts{Favorable: 1, Unfavorable: 1})
	newest := f.add("other", contribution.KindClaim, "", trust.Counts{})
	ctx := context.Background()
	recent, err := f.repo.Recent(ctx, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].ID != newest.ID || recent[0].ProjectID != "other" || recent[1].ProjectID != "other" {
		t.Fatalf("Recent = %+v", recent)
	}
	only, err := f.repo.Recent(ctx, []string{"p"}, 5)
	if err != nil || len(only) != 2 || only[0].ProjectID != "p" || only[1].ProjectID != "p" {
		t.Fatalf("Recent(p) = %+v, %v", only, err)
	}
	if none, err := f.repo.Recent(ctx, []string{}, 2); err != nil || len(none) != 0 {
		t.Fatalf("Recent(no projects) = %+v, %v", none, err)
	}
	// A superseded contribution is left out, like in the default lists and the counts.
	mustExec(t, f.pool, `UPDATE contribution SET superseded_by = $2, superseded_at = now() WHERE id = $1`, newest.ID, only[0].ID)
	if got, err := f.repo.Recent(ctx, []string{"other"}, 5); err != nil || len(got) != 1 || got[0].ID == newest.ID {
		t.Fatalf("Recent(other) after supersede = %+v, %v", got, err)
	}
}

// A signal moves updated_at to its own timestamp: sort=active and since follow it.
func TestList_ActiveSortAndSince(t *testing.T) {
	f := newCFix(t)
	ctx := context.Background()
	a := f.add("p", contribution.KindClaim, "", trust.Counts{})
	b := f.add("p", contribution.KindClaim, "", trust.Counts{})
	c := f.add("p", contribution.KindClaim, "", trust.Counts{})
	if got, err := f.repo.Get(ctx, "p", a.ID); err != nil || !got.UpdatedAt.Equal(a.CreatedAt) {
		t.Fatalf("new contribution updated_at = %v (%v), want created_at %v", got.UpdatedAt, err, a.CreatedAt)
	}
	at := f.base.Add(time.Hour)
	if _, err := postgres.NewSignalRepo(f.pool).Insert(ctx, newSignal(a.ID, signal.KindAssessment, signal.StanceFavorable, at)); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.repo.Get(ctx, "p", a.ID); !got.UpdatedAt.Equal(at) || !got.CreatedAt.Equal(a.CreatedAt) {
		t.Fatalf("after signal: updated_at = %v created_at = %v, want %v", got.UpdatedAt, got.CreatedAt, at)
	}

	want := []string{a.ID, c.ID, b.ID}
	if got := slices.Concat(f.all(contribution.ListFilter{Sort: contribution.SortActive})...); !slices.Equal(got, want) {
		t.Fatalf("active = %v, want %v", got, want)
	}
	if got := slices.Concat(f.all(contribution.ListFilter{Sort: contribution.SortActive, Limit: 1})...); !slices.Equal(got, want) {
		t.Fatalf("active paged = %v, want %v", got, want)
	}
	// Strictly after: c's own updated_at excludes c, keeping only a.
	if got := slices.Concat(f.all(contribution.ListFilter{Since: c.CreatedAt})...); !slices.Equal(got, []string{a.ID}) {
		t.Fatalf("since = %v, want [%s]", got, a.ID)
	}
	if got := slices.Concat(f.all(contribution.ListFilter{Since: b.CreatedAt, Sort: contribution.SortActive})...); !slices.Equal(got, []string{a.ID, c.ID}) {
		t.Fatalf("since+active = %v", got)
	}
	p, err := f.repo.List(ctx, "p", contribution.ListFilter{Sort: contribution.SortActive, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []contribution.Sort{contribution.SortNewest, contribution.SortDebated} {
		if _, err := f.repo.List(ctx, "p", contribution.ListFilter{Sort: s, Cursor: p.NextCursor}); !errors.Is(err, contribution.ErrBadCursor) {
			t.Errorf("active cursor with sort %s: err = %v, want ErrBadCursor", s, err)
		}
	}
}

// A question is resolved only by a Favorable answer that is neither
// superseded nor hidden; the view and QuestionInfo agree.
func TestQuestionStatus_IgnoresSupersededAndHiddenAnswers(t *testing.T) {
	f := newCFix(t)
	ctx := context.Background()
	q := f.add("p", contribution.KindQuestion, "", trust.Counts{})
	a := f.add("p", contribution.KindClaim, q.ID, trust.Counts{Favorable: 1})
	view := func() string {
		var st string
		if err := f.pool.QueryRow(ctx, `SELECT question_status FROM contribution_v WHERE id = $1`, q.ID).Scan(&st); err != nil {
			t.Fatal(err)
		}
		return st
	}
	info := func() contribution.QuestionInfo {
		m, err := f.repo.QuestionInfo(ctx, []string{q.ID})
		if err != nil {
			t.Fatal(err)
		}
		return m[q.ID]
	}
	check := func(step string, want contribution.QuestionStatus) {
		t.Helper()
		if view() != string(want) || info().Status != want {
			t.Fatalf("%s: view = %s, info = %s, want %s", step, view(), info().Status, want)
		}
	}
	check("favorable answer", contribution.QuestionResolved)
	newer := f.add("p", contribution.KindClaim, "", trust.Counts{})
	if err := f.repo.Create(ctx, contribution.Contribution{ID: id.New(), ProjectID: "p", ActorID: actorID, Kind: contribution.KindClaim, Title: "n", CreatedAt: f.base.Add(time.Hour)}, "n", a.ID); err != nil {
		t.Fatal(err)
	}
	check("superseded answer", contribution.QuestionAnswered)
	if err := f.repo.ClearSuperseded(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	check("supersession undone", contribution.QuestionResolved)
	if err := f.repo.SetHidden(ctx, a.ID, actorID, "", true); err != nil {
		t.Fatal(err)
	}
	check("hidden answer", contribution.QuestionOpen)
	if got, _ := f.repo.Answers(ctx, q.ID, true); len(got) != 1 {
		t.Fatalf("Answers with hidden = %d", len(got))
	}
	if err := f.repo.Create(ctx, contribution.Contribution{ID: id.New(), ProjectID: "p", ActorID: actorID, Kind: contribution.KindClaim, Title: "n2", CreatedAt: f.base.Add(time.Hour)}, "n2", newer.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Create(ctx, contribution.Contribution{ID: id.New(), ProjectID: "p", ActorID: actorID, Kind: contribution.KindClaim, Title: "n3", CreatedAt: f.base.Add(time.Hour)}, "n3", newer.ID); !errors.Is(err, contribution.ErrAlreadySuperseded) {
		t.Fatalf("second replacement: err = %v", err)
	}
}

func TestLexicalSearch_SkipsSupersededAndHidden(t *testing.T) {
	f := newSFix(t)
	ctx := context.Background()
	repo := postgres.NewContributionRepo(f.pool)
	old := f.create("p", contribution.KindClaim, "Kafka rebalancing old", "")
	hid := f.create("p", contribution.KindClaim, "Kafka rebalancing hidden", "")
	live := f.create("p", contribution.KindClaim, "Kafka rebalancing live", "")
	mustExec(t, f.pool, `UPDATE contribution SET superseded_by = $2 WHERE id = $1`, old, live)
	if err := repo.SetHidden(ctx, hid, actorID, "", true); err != nil {
		t.Fatal(err)
	}
	if got := f.lex("p", "kafka", search.Filters{}); !slices.Equal(got, []string{live}) {
		t.Fatalf("default = %v, want only %s", got, live)
	}
	got := f.lex("p", "kafka", search.Filters{IncludeSuperseded: true, IncludeHidden: true})
	if len(got) != 3 {
		t.Fatalf("with both = %v", got)
	}
	if got := f.lex("p", "kafka", search.Filters{States: []trust.State{trust.StateInsufficientEvidence}}); !slices.Equal(got, []string{live}) {
		t.Fatalf("state filter = %v", got)
	}
}

// A question is resolved only by an answer that is Favorable under the project's threshold.
func TestQuestionResolved_FollowsThreshold(t *testing.T) {
	f := newCFix(t)
	ctx := context.Background()
	mustExec(t, f.pool, `UPDATE project SET min_favorable = 2 WHERE id = 'p'`)
	q := f.add("p", contribution.KindQuestion, "", trust.Counts{})
	a := f.add("p", contribution.KindClaim, q.ID, trust.Counts{Favorable: 1, FavorableActors: 1})
	status := func() (string, contribution.QuestionStatus) {
		var v string
		if err := f.pool.QueryRow(ctx, `SELECT question_status FROM contribution_v WHERE id = $1`, q.ID).Scan(&v); err != nil {
			t.Fatal(err)
		}
		info, err := f.repo.QuestionInfo(ctx, []string{q.ID})
		if err != nil {
			t.Fatal(err)
		}
		return v, info[q.ID].Status
	}
	if v, s := status(); v != "answered" || s != contribution.QuestionAnswered {
		t.Fatalf("one supporter: view %s, info %s", v, s)
	}
	mustExec(t, f.pool, `UPDATE contribution SET favorable_n = 2, favorable_actor_n = 2 WHERE id = $1`, a.ID)
	if v, s := status(); v != "resolved" || s != contribution.QuestionResolved {
		t.Fatalf("two supporters: view %s, info %s", v, s)
	}
}

// Spec 2026-11-15-role-play-fixes §A.5: the reason is stored with the hidden
// flag and cleared on unhide, for contributions and signals.
func TestSetHidden_ReasonStoredAndCleared(t *testing.T) {
	f := newSFix(t)
	ctx := context.Background()
	repo, signals := postgres.NewContributionRepo(f.pool), postgres.NewSignalRepo(f.pool)
	id := f.create("p", contribution.KindClaim, "claim", "")
	res, err := signal.Create(ctx, repo, postgres.NewActorRepo(f.pool), signals,
		signal.NewInput{ProjectID: "p", ContributionID: id, ActorID: actorID, Kind: signal.KindChallenge, Stance: signal.StanceUnfavorable, Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	reasons := func() (string, string) {
		c, err := repo.Get(ctx, "p", id)
		if err != nil {
			t.Fatal(err)
		}
		s, err := signals.Get(ctx, res.Signal.ID)
		if err != nil {
			t.Fatal(err)
		}
		return c.HiddenReason, s.HiddenReason
	}
	if err := repo.SetHidden(ctx, id, actorID, "off topic", true); err != nil {
		t.Fatal(err)
	}
	if err := signals.SetHidden(ctx, res.Signal.ID, actorID, "spam", true); err != nil {
		t.Fatal(err)
	}
	if c, s := reasons(); c != "off topic" || s != "spam" {
		t.Fatalf("reasons = %q, %q", c, s)
	}
	if err := repo.SetHidden(ctx, id, actorID, "", false); err != nil {
		t.Fatal(err)
	}
	if err := signals.SetHidden(ctx, res.Signal.ID, actorID, "", false); err != nil {
		t.Fatal(err)
	}
	if c, s := reasons(); c != "" || s != "" {
		t.Fatalf("reasons after unhide = %q, %q", c, s)
	}
}

// Spec 2026-11-22-admin-ui-redesign A4.
func TestContributionRepo_Attention(t *testing.T) {
	f := newCFix(t)
	ctx := context.Background()
	now := time.Now()
	conflict := trust.Counts{Favorable: 1, Unfavorable: 1}
	fresh := f.add("p", contribution.KindClaim, "", conflict)
	old := f.add("p", contribution.KindClaim, "", conflict)
	mustExec(t, f.pool, `UPDATE contribution SET updated_at = now() - interval '8 days' WHERE id = $1`, old.ID)
	otherFresh := f.add("other", contribution.KindClaim, "", conflict)
	staleQ := f.add("p", contribution.KindQuestion, "", trust.Counts{})
	mustExec(t, f.pool, `UPDATE contribution SET created_at = now() - interval '15 days' WHERE id = $1`, staleQ.ID)
	newQ := f.add("p", contribution.KindQuestion, "", trust.Counts{}) // open, but recent
	hidden := f.add("p", contribution.KindClaim, "", conflict)
	// The fixture dates everything in the past: make the "recent" rows recent.
	mustExec(t, f.pool, `UPDATE contribution SET created_at = now(), updated_at = now() WHERE id = ANY($1)`, []string{fresh.ID, otherFresh.ID, newQ.ID, hidden.ID})
	mustExec(t, f.pool, `UPDATE contribution SET hidden_at = now() WHERE id = $1`, hidden.ID)

	if got, err := f.repo.Attention(ctx, []string{"p"}, now); err != nil || got != (contribution.Attention{Conflicts7d: 1, StaleQuestions: 1}) {
		t.Fatalf("Attention(p) = %+v, %v", got, err)
	}
	if got, err := f.repo.Attention(ctx, nil, now); err != nil || got.Conflicts7d != 2 {
		t.Fatalf("Attention(all) = %+v, %v", got, err)
	}
	if got, err := f.repo.Attention(ctx, []string{}, now); err != nil || got != (contribution.Attention{}) {
		t.Fatalf("Attention(none) = %+v, %v", got, err)
	}
}
