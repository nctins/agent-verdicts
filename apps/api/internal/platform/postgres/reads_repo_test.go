package postgres_test

import (
	"context"
	"testing"
	"time"

	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/reads"
	"agent-verdicts/internal/trust"
)

const (
	readerA = "00000000-0000-4000-8000-0000000000a1"
	readerB = "00000000-0000-4000-8000-0000000000b2"
)

func dayUTC(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

func readsToday() time.Time { return dayUTC(time.Now().UTC().Format(time.DateOnly)) }

func countRows(t *testing.T, f *cfix, q string, args ...any) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func item(t *testing.T, s reads.Stats, id string) reads.Item {
	t.Helper()
	for _, it := range s.Items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("no item %s in %+v", id, s.Items)
	return reads.Item{}
}

func TestReadsRepo_RecordCountsReadsAndDistinctReaders(t *testing.T) {
	f := newCFix(t)
	ctx := context.Background()
	repo := postgres.NewReadsRepo(f.pool)
	c := f.add("p", contribution.KindClaim, "", trust.Counts{})
	for _, r := range []string{readerA, readerA, readerB} {
		if err := repo.Record(ctx, "p", r, reads.Observe, []string{c.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Record(ctx, "p", readerA, reads.Reference, []string{c.ID}); err != nil {
		t.Fatal(err)
	}
	today := readsToday()
	s, err := repo.Stats(ctx, "p", today.AddDate(0, 0, -29), today)
	if err != nil {
		t.Fatal(err)
	}
	got := item(t, s, c.ID)
	if got.Observe != (reads.Count{Reads: 3, Readers: 2}) || got.Reference != (reads.Count{Reads: 1, Readers: 1}) {
		t.Fatalf("item = %+v", got)
	}
	if len(s.Daily) != 1 || s.Daily[0].Observe != 3 || s.Daily[0].Reference != 1 || !s.Daily[0].Start.Equal(today) {
		t.Fatalf("daily = %+v", s.Daily)
	}
	if s.DetailDays != reads.DefaultDetailDays || s.NeverRead != 0 {
		t.Fatalf("stats = %+v", s)
	}
}

func TestReadsRepo_RecordSkipsAuthorAndOtherProjects(t *testing.T) {
	f := newCFix(t)
	ctx := context.Background()
	repo := postgres.NewReadsRepo(f.pool)
	own := f.add("p", contribution.KindClaim, "", trust.Counts{})
	elsewhere := f.add("other", contribution.KindClaim, "", trust.Counts{})
	if err := repo.Record(ctx, "p", actorID, reads.Observe, []string{own.ID}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Record(ctx, "p", readerA, reads.Observe, []string{elsewhere.ID}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, f, `SELECT count(*) FROM contribution_read_day`); n != 0 {
		t.Fatalf("rows = %d, want 0 (author's own read and another project's id are skipped)", n)
	}
	today := readsToday()
	s, err := repo.Stats(ctx, "p", today, today)
	if err != nil {
		t.Fatal(err)
	}
	if s.NeverRead != 1 || len(s.Items) != 0 {
		t.Fatalf("stats = %+v", s)
	}
}

func TestReadsRepo_SearchHitsOneStatementNoActor(t *testing.T) {
	f := newCFix(t)
	ctx := context.Background()
	repo := postgres.NewReadsRepo(f.pool)
	a := f.add("p", contribution.KindClaim, "", trust.Counts{})
	b := f.add("p", contribution.KindQuestion, "", trust.Counts{})
	if err := repo.Record(ctx, "p", readerA, reads.SearchHit, []string{a.ID, b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Record(ctx, "p", readerB, reads.SearchHit, []string{a.ID}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, f, `SELECT count(*) FROM contribution_read_day WHERE actor_id <> '00000000-0000-0000-0000-000000000000'`); n != 0 {
		t.Fatalf("search hits kept %d actor ids", n)
	}
	today := readsToday()
	s, err := repo.Stats(ctx, "p", today, today)
	if err != nil {
		t.Fatal(err)
	}
	if got := item(t, s, a.ID); got.SearchHits != 3 || got.Observe.Reads != 0 {
		t.Fatalf("a = %+v", got)
	}
	if got := item(t, s, b.ID); got.SearchHits != 1 || got.Kind != "question" || got.State != nil {
		t.Fatalf("b = %+v", got)
	}
	if s.NeverRead != 1 { // a search hit is not a read of the claim
		t.Fatalf("never_read = %d", s.NeverRead)
	}
}

func TestReadsRepo_RollupPerProjectRetention(t *testing.T) {
	f := newCFix(t)
	ctx := context.Background()
	repo := postgres.NewReadsRepo(f.pool)
	p := f.add("p", contribution.KindClaim, "", trust.Counts{})
	o := f.add("other", contribution.KindClaim, "", trust.Counts{})
	ins := `INSERT INTO contribution_read_day (project_id, contribution_id, day, kind, actor_id, n) VALUES ($1, $2, $3, $4, $5, $6)`
	for _, r := range []struct {
		proj, id, day string
		kind          reads.Kind
		actor         string
		n             int
	}{
		{"p", p.ID, "2026-01-05", reads.Observe, readerA, 2},
		{"p", p.ID, "2026-01-20", reads.Observe, readerB, 1},
		{"p", p.ID, "2026-01-20", reads.Observe, readerA, 1},
		{"p", p.ID, "2026-01-20", reads.SearchHit, "00000000-0000-0000-0000-000000000000", 7},
		{"p", p.ID, "2026-06-01", reads.Observe, readerA, 5}, // inside 180 days of 2026-08-15
		{"other", o.ID, "2026-01-05", reads.Observe, readerA, 4},
	} {
		mustExec(t, f.pool, ins, r.proj, r.id, r.day, r.kind, r.actor, r.n)
	}
	mustExec(t, f.pool, `UPDATE project SET read_detail_days = 3650 WHERE id = 'other'`)
	today := dayUTC("2026-08-15") // 180 days back is 2026-02-16: January rolls up
	if err := repo.Rollup(ctx, today); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, f, `SELECT count(*) FROM contribution_read_day WHERE project_id = 'p'`); n != 1 {
		t.Fatalf("p daily rows = %d, want 1 (June kept)", n)
	}
	if n := countRows(t, f, `SELECT count(*) FROM contribution_read_day WHERE project_id = 'other'`); n != 1 {
		t.Fatalf("other daily rows = %d, want 1 (10-year retention)", n)
	}
	var reads1, readers1, hits int
	if err := f.pool.QueryRow(ctx, `SELECT reads, readers FROM contribution_read_month WHERE contribution_id = $1 AND month = '2026-01-01' AND kind = 1`, p.ID).Scan(&reads1, &readers1); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT reads FROM contribution_read_month WHERE contribution_id = $1 AND kind = 3`, p.ID).Scan(&hits); err != nil {
		t.Fatal(err)
	}
	if reads1 != 4 || readers1 != 2 || hits != 7 {
		t.Fatalf("January = %d reads, %d readers, %d hits", reads1, readers1, hits)
	}
	// Totals are unchanged across the rollup.
	s, err := repo.Stats(ctx, "p", dayUTC("2026-01-01"), today)
	if err != nil {
		t.Fatal(err)
	}
	if got := item(t, s, p.ID); got.Observe.Reads != 9 || got.SearchHits != 7 {
		t.Fatalf("item = %+v", got)
	}
	if len(s.Monthly) != 1 || s.Monthly[0].Observe != 4 || len(s.Daily) != 1 || s.Daily[0].Observe != 5 {
		t.Fatalf("monthly = %+v, daily = %+v", s.Monthly, s.Daily)
	}

	// Lowering retention rolls the excess up on the next run.
	if _, err := repo.SetDetailDays(ctx, "p", 30, today); err != nil {
		t.Fatal(err)
	}
	if err := repo.Rollup(ctx, today); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, f, `SELECT count(*) FROM contribution_read_day WHERE project_id = 'p'`); n != 0 {
		t.Fatalf("p daily rows = %d after lowering retention, want 0", n)
	}
}

func TestReadsRepo_SetDetailDaysEstimates(t *testing.T) {
	f := newCFix(t)
	ctx := context.Background()
	repo := postgres.NewReadsRepo(f.pool)
	c := f.add("p", contribution.KindClaim, "", trust.Counts{})
	today := readsToday()
	for i := range 60 {
		mustExec(t, f.pool, `INSERT INTO contribution_read_day (project_id, contribution_id, day, kind, actor_id, n) VALUES ('p', $1, $2, 1, $3, 1)`,
			c.ID, today.AddDate(0, 0, -i/2), []string{readerA, readerB}[i%2])
	}
	r, err := repo.SetDetailDays(ctx, "p", 365, today)
	if err != nil {
		t.Fatal(err)
	}
	// 60 rows over the last 30 days (days 0-29) = 2 rows a day.
	if r.DetailDays != 365 || r.RowsPerDay != 2 || r.EstimatedBytes != 2*365*reads.BytesPerRow {
		t.Fatalf("retention = %+v", r)
	}
	s, err := repo.Stats(ctx, "p", today, today)
	if err != nil {
		t.Fatal(err)
	}
	if s.DetailDays != 365 {
		t.Fatalf("detail days = %d", s.DetailDays)
	}
}
