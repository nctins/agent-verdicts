package postgres_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/signal"
)

func newSignalFix(t *testing.T) (*postgres.SignalRepo, func(string, ...any), string) {
	pool := pgtest.NewMigrated(t)
	mustExec(t, pool, `INSERT INTO actor (id,type,name,created_at) VALUES ($1,'Agent','a',now()), ($2,'Agent','b',now())`, actorID, otherActorID)
	mustExec(t, pool, `INSERT INTO project (id,name,created_at) VALUES ('p','p', now())`)
	cid := id.New()
	mustExec(t, pool, `INSERT INTO contribution (id, project_id, actor_id, kind, title, search_text, created_at, updated_at)
		VALUES ($1, 'p', $2, 'claim', 't', 't', now(), now())`, cid, actorID)
	scan := func(q string, dest ...any) {
		t.Helper()
		if err := pool.QueryRow(context.Background(), q, cid).Scan(dest...); err != nil {
			t.Fatal(err)
		}
	}
	return postgres.NewSignalRepo(pool), scan, cid
}

// otherActorID is a second actor that did not write the fixture's claim.
const otherActorID = "00000000-0000-4000-8000-000000000002"

func newSignal(cid string, k signal.Kind, st signal.Stance, at time.Time) signal.Signal {
	return signal.Signal{ID: id.New(), ContributionID: cid, ActorID: actorID, Kind: k, Stance: st, Body: "b", CreatedAt: at}
}

func TestSignalRepo_ListByContribution(t *testing.T) {
	repo, _, cid := newSignalFix(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var want []signal.Signal
	for i, st := range []signal.Stance{signal.StanceFavorable, signal.StanceNeutral} {
		s := newSignal(cid, signal.KindAssessment, st, base.Add(time.Duration(i)*time.Second))
		if _, err := repo.Insert(ctx, s); err != nil {
			t.Fatal(err)
		}
		want = append(want, s)
	}
	got, err := repo.ListByContribution(ctx, cid)
	if err != nil || len(got) != 2 {
		t.Fatalf("list = %+v, %v", got, err)
	}
	for i := range got {
		if got[i].ID != want[i].ID || got[i].Stance != want[i].Stance || got[i].Kind != want[i].Kind ||
			got[i].Body != "b" || got[i].ActorID != actorID || !got[i].CreatedAt.Equal(want[i].CreatedAt) {
			t.Fatalf("item %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got[0].SupersededAt == nil || got[0].SupersededAt.Location() != time.UTC || got[1].SupersededAt != nil {
		t.Fatalf("superseded: %v %v", got[0].SupersededAt, got[1].SupersededAt)
	}
	if empty, err := repo.ListByContribution(ctx, "nope"); err != nil || len(empty) != 0 {
		t.Fatalf("malformed id: %v, %v", empty, err)
	}
}

// Review Focus #1: concurrent re-assessments by one actor never error and
// leave exactly one active assessment that the counters agree with.
func TestAssessment_ConcurrentSameActor(t *testing.T) {
	repo, scan, cid := newSignalFix(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := range 20 {
		st := signal.StanceFavorable
		if i%2 == 1 {
			st = signal.StanceUnfavorable
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := newSignal(cid, signal.KindAssessment, st, time.Now().UTC().Truncate(time.Microsecond))
			s.ActorID = otherActorID // not the author, whose support would not count
			_, err := repo.Insert(ctx, s)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}
	var active int
	var stance string
	scan(`SELECT count(*), min(stance) FROM signal WHERE contribution_id=$1 AND superseded_at IS NULL`, &active, &stance)
	var total, f, u, n int
	scan(`SELECT (SELECT count(*) FROM signal WHERE contribution_id=$1), favorable_n, unfavorable_n, neutral_n FROM contribution WHERE id=$1`, &total, &f, &u, &n)
	if active != 1 || total != 20 || f+u+n != 1 {
		t.Fatalf("active=%d total=%d counts=%d/%d/%d", active, total, f, u, n)
	}
	if (stance == "favorable") != (f == 1) || (stance == "unfavorable") != (u == 1) {
		t.Fatalf("active stance %q disagrees with counts %d/%d/%d", stance, f, u, n)
	}
}

// Spec 2026-11-21-ignore-self-support §2: the author's favorable signals are
// kept but not counted; their unfavorable ones are.
func TestRecount_IgnoresAuthorSupport(t *testing.T) {
	repo, scan, cid := newSignalFix(t)
	ctx := context.Background()
	other := otherActorID
	state := func() (f, fa, u int, st string) {
		t.Helper()
		scan(`SELECT favorable_n, favorable_actor_n, unfavorable_n, state FROM contribution_v WHERE id = $1`, &f, &fa, &u, &st)
		return
	}
	now := time.Now()
	if _, err := repo.Insert(ctx, newSignal(cid, signal.KindAssessment, signal.StanceFavorable, now)); err != nil {
		t.Fatal(err)
	}
	if f, fa, _, st := state(); f != 0 || fa != 0 || st != "InsufficientEvidence" {
		t.Fatalf("after self-support: f=%d fa=%d %s", f, fa, st)
	}
	dispute := newSignal(cid, signal.KindChallenge, signal.StanceUnfavorable, now.Add(time.Second))
	dispute.ActorID = other
	if _, err := repo.Insert(ctx, dispute); err != nil {
		t.Fatal(err)
	}
	if _, _, u, st := state(); u != 1 || st != "Unfavorable" { // not Conflict
		t.Fatalf("self-support + dispute: u=%d %s", u, st)
	}
	if err := repo.Withdraw(ctx, dispute.ID); err != nil {
		t.Fatal(err)
	}
	support := newSignal(cid, signal.KindAssessment, signal.StanceFavorable, now.Add(2*time.Second))
	support.ActorID = other
	if _, err := repo.Insert(ctx, support); err != nil {
		t.Fatal(err)
	}
	if f, fa, _, st := state(); f != 1 || fa != 1 || st != "Favorable" {
		t.Fatalf("other's support: f=%d fa=%d %s", f, fa, st)
	}
	// The author disputing their own claim still counts.
	if _, err := repo.Insert(ctx, newSignal(cid, signal.KindChallenge, signal.StanceUnfavorable, now.Add(3*time.Second))); err != nil {
		t.Fatal(err)
	}
	if _, _, u, st := state(); u != 1 || st != "Conflict" {
		t.Fatalf("author dispute: u=%d %s", u, st)
	}
}
