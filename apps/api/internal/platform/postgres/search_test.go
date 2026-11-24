package postgres_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/platform/textfold"
	"agent-verdicts/internal/search"
	"agent-verdicts/internal/trust"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	_ search.LexicalIndex = (*postgres.LexicalIndex)(nil)
	_ search.VectorIndex  = (*postgres.VectorIndex)(nil)
	_ search.ChunkStore   = (*postgres.ChunkRepo)(nil)
)

type sfix struct {
	t    *testing.T
	pool *pgxpool.Pool
}

func newSFix(t *testing.T) *sfix {
	pool := pgtest.NewMigrated(t)
	mustExec(t, pool, `INSERT INTO actor (id,type,name,created_at) VALUES ($1,'Agent','a',now())`, actorID)
	return &sfix{t: t, pool: pool}
}

func (f *sfix) create(project string, kind contribution.Kind, title, body string) string {
	f.t.Helper()
	mustExec(f.t, f.pool, `INSERT INTO project (id,name,created_at) VALUES ($1,$1,now()) ON CONFLICT DO NOTHING`, project)
	c, err := contribution.Create(context.Background(), postgres.NewProjectRepo(f.pool), postgres.NewActorRepo(f.pool),
		postgres.NewContributionRepo(f.pool), contribution.NewInput{ProjectID: project, ActorID: actorID, Kind: kind, Title: title, Body: body})
	if err != nil {
		f.t.Fatal(err)
	}
	return c.ID
}

func (f *sfix) lex(project, q string, filt search.Filters) []string {
	f.t.Helper()
	hits, err := postgres.NewLexicalIndex(f.pool).Search(context.Background(), project, textfold.Fold(q), filt, 10)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, h := range hits {
		out = append(out, h.ContributionID)
	}
	return out
}

func TestLexical_RankingProjectKindState(t *testing.T) {
	f := newSFix(t)
	best := f.create("p", contribution.KindClaim, "Refund approver", "Every refund needs an approver; the approver signs the refund.")
	weak := f.create("p", contribution.KindQuestion, "Who signs off?", "Is an approver required here?")
	f.create("p", contribution.KindClaim, "Deploy schedule", "We deploy on Tuesdays.")
	other := f.create("other", contribution.KindClaim, "Refund approver", "refund approver")

	if got := f.lex("p", "refund approver", search.Filters{}); !slices.Equal(got, []string{best, weak}) {
		t.Fatalf("ranking: got %v want [%s %s]", got, best, weak)
	}
	if got := f.lex("other", "refund approver", search.Filters{}); !slices.Equal(got, []string{other}) {
		t.Fatalf("project scope: got %v", got)
	}
	if got := f.lex("p", "refund approver", search.Filters{Kind: contribution.KindQuestion}); !slices.Equal(got, []string{weak}) {
		t.Fatalf("kind=question: got %v", got)
	}
	// States applies to claims only: a fresh claim is InsufficientEvidence; the question drops out.
	if got := f.lex("p", "refund approver", search.Filters{States: []trust.State{trust.StateInsufficientEvidence}}); !slices.Equal(got, []string{best}) {
		t.Fatalf("state filter: got %v", got)
	}
	if got := f.lex("p", "refund approver", search.Filters{States: []trust.State{trust.StateFavorable}}); len(got) != 0 {
		t.Fatalf("state=Favorable: got %v", got)
	}
	mustExec(t, f.pool, `UPDATE contribution SET favorable_n = 1, favorable_actor_n = 1 WHERE id = $1`, best)
	if got := f.lex("p", "refund approver", search.Filters{States: []trust.State{trust.StateFavorable}}); !slices.Equal(got, []string{best}) {
		t.Fatalf("state=Favorable after update: got %v", got)
	}
	// Union: Favorable claims + open questions.
	got := f.lex("p", "refund approver", search.Filters{States: []trust.State{trust.StateFavorable}, QuestionStatus: []contribution.QuestionStatus{contribution.QuestionOpen}})
	if !slices.Equal(got, []string{best, weak}) {
		t.Fatalf("union: got %v", got)
	}
}

// Review Focus #4: unaccented query finds accented Vietnamese content.
func TestLexical_FoldedVietnamese(t *testing.T) {
	f := newSFix(t)
	want := f.create("p", contribution.KindClaim, "Quy trình", "Hoàn tiền lớn cần hai người duyệt")
	f.create("p", contribution.KindClaim, "Khác", "Triển khai vào thứ ba")
	if got := f.lex("p", "hoan tien lon", search.Filters{}); !slices.Equal(got, []string{want}) {
		t.Fatalf("got %v want [%s]", got, want)
	}
}

func TestLexical_SpecialCharactersAndEmpty(t *testing.T) {
	f := newSFix(t)
	want := f.create("p", contribution.KindClaim, "Charges endpoint", "POST /v1/charges creates a charge")
	if got := f.lex("p", `a:b "c" (d) -e /v1/charges? it's [x] {y} ~z ^2 AND OR NOT`, search.Filters{}); !slices.Contains(got, want) {
		t.Fatalf("special chars: got %v", got)
	}
	for _, q := range []string{"", "   ", "?!", `"`} {
		if got := f.lex("p", q, search.Filters{}); len(got) != 0 {
			t.Fatalf("query %q: got %v", q, got)
		}
	}
}

func vecSearch(t *testing.T, pool *pgxpool.Pool, project string, v []float32, model string, filt search.Filters, n int) []search.ChunkHit {
	t.Helper()
	hits, err := postgres.NewVectorIndex(pool).Search(context.Background(), project, v, model, filt, n)
	if err != nil {
		t.Fatal(err)
	}
	return hits
}

func TestVector_NearestFirstOneHitPerContribution(t *testing.T) {
	f := newSFix(t)
	ctx := context.Background()
	chunks := postgres.NewChunkRepo(f.pool)
	a := f.create("p", contribution.KindClaim, "A", "")
	b := f.create("p", contribution.KindClaim, "B", "")
	o := f.create("other", contribution.KindClaim, "O", "")
	must(t, chunks.Replace(ctx, a, "m", []string{"a0", "a1"}, [][]float32{{1, 0, 0}, {0.9, 0.1, 0}}))
	must(t, chunks.Replace(ctx, b, "m", []string{"b0", "b1"}, [][]float32{{0.2, 1, 0}, {0, 1, 0.05}}))
	must(t, chunks.Replace(ctx, o, "m", []string{"o0"}, [][]float32{{0, 1, 0}}))
	x := f.create("p", contribution.KindClaim, "X", "") // exact match, but another model
	must(t, chunks.Replace(ctx, x, "other-model", []string{"x"}, [][]float32{{0, 1, 0}}))

	got := vecSearch(t, f.pool, "p", []float32{0, 1, 0}, "m", search.Filters{}, 10)
	want := []search.ChunkHit{{ContributionID: b, Idx: 1, Text: "b1"}, {ContributionID: a, Idx: 1, Text: "a1"}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if got := vecSearch(t, f.pool, "p", []float32{0, 1, 0}, "m", search.Filters{}, 1); !slices.Equal(got, want[:1]) {
		t.Fatalf("n=1: got %+v", got)
	}
}

func TestVector_FiltersAndProjectScope(t *testing.T) {
	f := newSFix(t)
	ctx := context.Background()
	chunks := postgres.NewChunkRepo(f.pool)
	claim := f.create("p", contribution.KindClaim, "C", "")
	q := f.create("p", contribution.KindQuestion, "Q", "")
	o := f.create("other", contribution.KindClaim, "O", "")
	for _, id := range []string{claim, q, o} {
		must(t, chunks.Replace(ctx, id, "m", []string{id}, [][]float32{{1, 1, 0}}))
	}
	idsOf := func(hs []search.ChunkHit) []string {
		var out []string
		for _, h := range hs {
			out = append(out, h.ContributionID)
		}
		slices.Sort(out)
		return out
	}
	v := []float32{1, 1, 0}
	if got, want := idsOf(vecSearch(t, f.pool, "p", v, "m", search.Filters{}, 10)), sorted(claim, q); !slices.Equal(got, want) {
		t.Fatalf("project p: got %v want %v", got, want)
	}
	if got := idsOf(vecSearch(t, f.pool, "p", v, "m", search.Filters{Kind: contribution.KindQuestion}, 10)); !slices.Equal(got, []string{q}) {
		t.Fatalf("kind: got %v", got)
	}
	if got := idsOf(vecSearch(t, f.pool, "p", v, "m", search.Filters{States: []trust.State{trust.StateInsufficientEvidence}}, 10)); !slices.Equal(got, []string{claim}) {
		t.Fatalf("states: got %v", got)
	}
	if got := idsOf(vecSearch(t, f.pool, "p", v, "m", search.Filters{QuestionStatus: []contribution.QuestionStatus{contribution.QuestionOpen}}, 10)); !slices.Equal(got, []string{q}) {
		t.Fatalf("question status: got %v", got)
	}
	if got := vecSearch(t, f.pool, "p", v, "unknown", search.Filters{}, 10); len(got) != 0 {
		t.Fatalf("other model: got %v", got)
	}
}

func sorted(xs ...string) []string { slices.Sort(xs); return xs }

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func hnswIndexes(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT indexname FROM pg_indexes WHERE schemaname = current_schema() AND indexname LIKE 'chunk\_hnsw\_%' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		must(t, rows.Scan(&s))
		out = append(out, s)
	}
	must(t, rows.Err())
	return out
}

func TestChunkRepo_EnsureIndex(t *testing.T) {
	f := newSFix(t)
	ctx := context.Background()
	chunks := postgres.NewChunkRepo(f.pool)
	id := f.create("p", contribution.KindClaim, "A", "")
	must(t, chunks.Replace(ctx, id, "m", []string{"a"}, [][]float32{{1, 0, 0}}))

	must(t, chunks.EnsureIndex(ctx, "m", 3))
	must(t, chunks.EnsureIndex(ctx, "m", 3)) // idempotent
	first := hnswIndexes(t, f.pool)
	if len(first) != 1 {
		t.Fatalf("indexes after m: %v", first)
	}

	// The vector query uses the index (seq scan disabled: tiny table).
	tx, err := f.pool.Begin(ctx)
	must(t, err)
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `SET LOCAL enable_seqscan = off`)
	must(t, err)
	var plan strings.Builder
	q, args := postgres.VectorQueryForTest("p", []float32{1, 0, 0}, "m", search.Filters{}, 10)
	rows, err := tx.Query(ctx, `EXPLAIN `+q, args...)
	must(t, err)
	for rows.Next() {
		var line string
		must(t, rows.Scan(&line))
		plan.WriteString(line + "\n")
	}
	must(t, rows.Err())
	if !strings.Contains(plan.String(), first[0]) {
		t.Fatalf("plan does not use %s:\n%s", first[0], plan.String())
	}
	must(t, tx.Rollback(ctx))

	must(t, chunks.EnsureIndex(ctx, "o'brien", 3))
	second := hnswIndexes(t, f.pool)
	if len(second) != 1 || second[0] == first[0] {
		t.Fatalf("after model change: %v (before %v)", second, first)
	}
	if err := chunks.EnsureIndex(ctx, "bad\x00model", 3); err == nil {
		t.Fatal("NUL in model: want error")
	}
	if err := chunks.EnsureIndex(ctx, strings.Repeat("x", 201), 3); err == nil {
		t.Fatal("long model: want error")
	}
}

func TestChunkRepo_PendingCountDeleteAllBestChunk(t *testing.T) {
	f := newSFix(t)
	ctx := context.Background()
	chunks := postgres.NewChunkRepo(f.pool)
	a := f.create("p", contribution.KindClaim, "A", "")
	b := f.create("p", contribution.KindClaim, "B", "")
	c := f.create("other", contribution.KindClaim, "C", "")

	pending := func(limit int) []string {
		t.Helper()
		ids, err := chunks.Pending(ctx, "m", limit)
		must(t, err)
		return ids
	}
	count := func() [2]int {
		t.Helper()
		e, n, err := chunks.Count(ctx, "m")
		must(t, err)
		return [2]int{e, n}
	}
	if got := pending(10); !slices.Equal(got, []string{a, b, c}) {
		t.Fatalf("pending oldest first: got %v", got)
	}
	if got := pending(2); !slices.Equal(got, []string{a, b}) {
		t.Fatalf("pending limit: got %v", got)
	}
	must(t, chunks.Replace(ctx, a, "m", []string{"a0", "a1"}, [][]float32{{1, 0, 0}, {0, 1, 0}}))
	must(t, chunks.Replace(ctx, b, "old", []string{"b0"}, [][]float32{{1, 0, 0}}))
	if got := pending(10); !slices.Equal(got, []string{b, c}) {
		t.Fatalf("pending after embed: got %v", got)
	}
	if got := count(); got != [2]int{1, 3} {
		t.Fatalf("count: %v", got)
	}

	// Replace swaps the chunk set; mismatched lengths are rejected.
	must(t, chunks.Replace(ctx, a, "m", []string{"only"}, [][]float32{{0, 0, 1}}))
	if err := chunks.Replace(ctx, a, "m", []string{"x", "y"}, [][]float32{{1, 0, 0}}); err == nil {
		t.Fatal("len mismatch: want error")
	}
	hit, err := chunks.BestChunk(ctx, a, "m", []float32{0, 0, 1})
	must(t, err)
	if hit != (search.ChunkHit{ContributionID: a, Idx: 0, Text: "only"}) {
		t.Fatalf("best chunk after replace: %+v", hit)
	}
	must(t, chunks.Replace(ctx, a, "m", []string{"a0", "a1"}, [][]float32{{1, 0, 0}, {0, 1, 0}}))
	hit, err = chunks.BestChunk(ctx, a, "m", []float32{0.1, 1, 0})
	must(t, err)
	if hit != (search.ChunkHit{ContributionID: a, Idx: 1, Text: "a1"}) {
		t.Fatalf("best chunk: %+v", hit)
	}
	for _, id := range []string{b, "not-a-uuid"} {
		hit, err = chunks.BestChunk(ctx, id, "m", []float32{1, 0, 0})
		must(t, err)
		if hit != (search.ChunkHit{}) {
			t.Fatalf("best chunk of %s: %+v", id, hit)
		}
	}

	must(t, chunks.DeleteAll(ctx))
	if got := count(); got != [2]int{0, 3} {
		t.Fatalf("count after delete all: %v", got)
	}
	if got := pending(10); !slices.Equal(got, []string{a, b, c}) {
		t.Fatalf("pending after delete all: %v", got)
	}
}

// A long document's many near chunks must not crowd other contributions out
// of the candidate window (n=5 → first window 200 < 250 chunks → retry).
func TestVector_ManyChunksDoNotStarveOthers(t *testing.T) {
	f := newSFix(t)
	ctx := context.Background()
	chunks := postgres.NewChunkRepo(f.pool)
	long := f.create("p", contribution.KindClaim, "Long", "")
	other := f.create("p", contribution.KindClaim, "Other", "")
	texts := make([]string, 250)
	vecs := make([][]float32, 250)
	for i := range texts {
		texts[i] = "c"
		vecs[i] = []float32{1, 0, float32(i) * 1e-4}
	}
	must(t, chunks.Replace(ctx, long, "m", texts, vecs))
	must(t, chunks.Replace(ctx, other, "m", []string{"o"}, [][]float32{{1, 0.5, 0}}))
	got := vecSearch(t, f.pool, "p", []float32{1, 0, 0}, "m", search.Filters{}, 5)
	if len(got) != 2 || got[0].ContributionID != long || got[1].ContributionID != other {
		t.Fatalf("got %+v", got)
	}
}

// Changing dim under the same model replaces the index, so new-dim inserts work.
func TestChunkRepo_EnsureIndexDimChange(t *testing.T) {
	f := newSFix(t)
	ctx := context.Background()
	chunks := postgres.NewChunkRepo(f.pool)
	id := f.create("p", contribution.KindClaim, "A", "")
	must(t, chunks.EnsureIndex(ctx, "m", 3))
	must(t, chunks.Replace(ctx, id, "m", []string{"a"}, [][]float32{{1, 0, 0}}))
	before := hnswIndexes(t, f.pool)
	must(t, chunks.DeleteAll(ctx))
	must(t, chunks.EnsureIndex(ctx, "m", 4))
	must(t, chunks.Replace(ctx, id, "m", []string{"a"}, [][]float32{{1, 0, 0, 0}}))
	after := hnswIndexes(t, f.pool)
	if len(before) != 1 || len(after) != 1 || after[0] == before[0] {
		t.Fatalf("indexes before %v after %v", before, after)
	}
	if got := vecSearch(t, f.pool, "p", []float32{1, 0, 0, 0}, "m", search.Filters{}, 5); len(got) != 1 {
		t.Fatalf("search after dim change: %+v", got)
	}
}

func TestChunkRepo_DimMismatchMapsToSentinel(t *testing.T) {
	f := newSFix(t)
	ctx := context.Background()
	chunks := postgres.NewChunkRepo(f.pool)
	id := f.create("p", contribution.KindClaim, "A", "")

	// Insert into a model whose index has another dim.
	must(t, chunks.EnsureIndex(ctx, "m", 3))
	err := chunks.Replace(ctx, id, "m", []string{"a"}, [][]float32{{1, 0, 0, 0}})
	if !errors.Is(err, search.ErrDimMismatch) {
		t.Fatalf("Replace err = %v, want ErrDimMismatch", err)
	}
	// Build an index over rows of another dim (stale chunks after a restart).
	must(t, chunks.DeleteAll(ctx))
	must(t, chunks.EnsureIndex(ctx, "other", 2)) // drops m's index
	must(t, chunks.Replace(ctx, id, "m", []string{"a"}, [][]float32{{1, 0, 0, 0}}))
	if err := chunks.EnsureIndex(ctx, "m", 3); !errors.Is(err, search.ErrDimMismatch) {
		t.Fatalf("EnsureIndex err = %v, want ErrDimMismatch", err)
	}
	// Other errors are not mapped.
	if err := chunks.EnsureIndex(ctx, "m", 0); err == nil || errors.Is(err, search.ErrDimMismatch) {
		t.Fatalf("dim 0 err = %v", err)
	}
}

// Spec §6.1: state / question_status filters must not lose a match that
// ranks below the candidate window.
func TestLexical_FilterBeyondCandidateWindow(t *testing.T) {
	f := newSFix(t)
	q := f.create("p", contribution.KindQuestion, "refund?", strings.Repeat("unrelated filler words ", 50))
	mustExec(t, f.pool, `INSERT INTO contribution (id, project_id, actor_id, kind, title, body, search_text, created_at, updated_at)
		SELECT gen_random_uuid(), 'p', $1, 'claim', 'refund', 'refund refund', 'refund refund refund', now(), now()
		FROM generate_series(1, 250)`, actorID)
	if got := f.lex("p", "refund", search.Filters{QuestionStatus: []contribution.QuestionStatus{contribution.QuestionOpen}}); !slices.Equal(got, []string{q}) {
		t.Fatalf("open questions = %v, want [%s]", got, q)
	}
}
