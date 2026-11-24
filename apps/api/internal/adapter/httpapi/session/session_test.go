package session_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/adapter/httpapi/session"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	stamp = time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	ctx   = context.Background()
)

// fixture is a migrated database, one actor and a store on it.
type fixture struct {
	pool  *pgxpool.Pool
	actor actor.Actor
	s     *session.Store
	now   time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := pgtest.NewMigrated(t)
	a := actor.New(actor.TypeHuman)
	a.Name = "Alice"
	if err := postgres.NewActorRepo(pool).Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	f := &fixture{pool: pool, actor: a, now: time.Now()}
	f.s = f.store(t)
	return f
}

// store builds a Store on the fixture's database, with the fixture's clock.
func (f *fixture) store(t *testing.T) *session.Store {
	t.Helper()
	s, err := session.NewStore(ctx, postgres.NewSessionRepo(f.pool))
	if err != nil {
		t.Fatal(err)
	}
	session.SetNowForTest(s, func() time.Time { return f.now })
	return s
}

func (f *fixture) create(t *testing.T) string {
	t.Helper()
	id, err := f.s.Create(ctx, f.actor.ID, stamp)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *fixture) count(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM session`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestStore_CreateAndGet_Roundtrip(t *testing.T) {
	f := newFixture(t)
	id := f.create(t)
	if id == "" {
		t.Fatal("Create() returned empty session ID")
	}
	got, ok, err := f.s.Get(ctx, id)
	if err != nil || !ok {
		t.Fatalf("Get() = %v, %v, want the session", ok, err)
	}
	if got.ActorID != f.actor.ID || !got.PasswordChangedAt.Equal(stamp) {
		t.Fatalf("Get() = %+v", got)
	}
}

func TestStore_Create_GeneratesUniqueSessionIDs(t *testing.T) {
	f := newFixture(t)
	if a, b := f.create(t), f.create(t); a == b {
		t.Fatalf("Create() returned the same session ID twice: %q", a)
	}
}

func TestStore_Get_UnknownSessionID(t *testing.T) {
	f := newFixture(t)
	if _, ok, err := f.s.Get(ctx, "does-not-exist"); ok || err != nil {
		t.Fatalf("Get() = %v, %v for an unknown session ID, want false, nil", ok, err)
	}
}

// Only the hash is stored: the raw id appears nowhere in the table.
func TestStore_StoresOnlyTheHash(t *testing.T) {
	f := newFixture(t)
	id := f.create(t)
	var hits int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM session WHERE session::text LIKE '%' || $1 || '%'`, id).Scan(&hits); err != nil {
		t.Fatal(err)
	}
	if hits != 0 {
		t.Fatal("the raw session id is stored")
	}
}

func TestStore_SurvivesANewStoreOnTheSameDatabase(t *testing.T) {
	f := newFixture(t)
	id := f.create(t)
	restarted := f.store(t)
	got, ok, err := restarted.Get(ctx, id)
	if err != nil || !ok || got.ActorID != f.actor.ID {
		t.Fatalf("after restart Get() = %+v, %v, %v", got, ok, err)
	}
}

// The CSRF token handed out before a restart is still accepted after it.
func TestStore_CSRFTokenSurvivesRestart(t *testing.T) {
	f := newFixture(t)
	id := f.create(t)
	token := f.s.Token(id)
	if !f.store(t).ValidToken(id, token) {
		t.Fatal("CSRF token rejected after a restart")
	}
}

func TestNewStore_ConcurrentStartsShareOneSecret(t *testing.T) {
	other := pgtest.NewMigrated(t)
	var wg sync.WaitGroup
	stores := make([]*session.Store, 8)
	for i := range stores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := session.NewStore(ctx, postgres.NewSessionRepo(other))
			if err != nil {
				t.Error(err)
				return
			}
			stores[i] = s
		}()
	}
	wg.Wait()
	for _, s := range stores[1:] {
		if s == nil || stores[0] == nil || s.Token("x") != stores[0].Token("x") {
			t.Fatal("concurrent starts ended with different CSRF secrets")
		}
	}
}

func TestStore_IdleExpiry(t *testing.T) {
	f := newFixture(t)
	id := f.create(t)
	f.now = f.now.Add(session.IdleTimeout - time.Hour)
	if _, ok, _ := f.s.Get(ctx, id); !ok { // activity slides the window
		t.Fatal("session died before the idle timeout")
	}
	f.now = f.now.Add(session.IdleTimeout - time.Hour)
	if _, ok, _ := f.s.Get(ctx, id); !ok {
		t.Fatal("activity did not extend the session")
	}
	f.now = f.now.Add(session.IdleTimeout + time.Hour)
	if _, ok, err := f.s.Get(ctx, id); ok || err != nil {
		t.Fatalf("session alive after idling past the timeout: %v, %v", ok, err)
	}
}

func TestStore_AbsoluteExpiry(t *testing.T) {
	f := newFixture(t)
	id := f.create(t)
	// Active every few days, so never idle, yet gone 30 days after creation.
	for day := 5; day < 30; day += 5 {
		f.now = f.now.Add(5 * 24 * time.Hour)
		if _, ok, _ := f.s.Get(ctx, id); !ok {
			t.Fatalf("session died on day %d", day)
		}
	}
	f.now = f.now.Add(5*24*time.Hour + time.Hour)
	if _, ok, _ := f.s.Get(ctx, id); ok {
		t.Fatal("session alive past its absolute lifetime")
	}
}

func TestStore_LastSeenWrittenAtMostOncePerMinute(t *testing.T) {
	f := newFixture(t)
	id := f.create(t)
	seen := func() time.Time {
		var at time.Time
		if err := f.pool.QueryRow(ctx, `SELECT last_seen_at FROM session`).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	created := seen()
	f.now = f.now.Add(30 * time.Second)
	f.s.Get(ctx, id)
	if !seen().Equal(created) {
		t.Fatal("last_seen_at written again within a minute")
	}
	f.now = f.now.Add(61 * time.Second)
	f.s.Get(ctx, id)
	if !seen().After(created) {
		t.Fatal("last_seen_at not moved after a minute")
	}
}

func TestStore_Create_PrunesExpiredSessions(t *testing.T) {
	f := newFixture(t)
	f.create(t)
	f.now = f.now.Add(session.IdleTimeout + time.Hour)
	live := f.create(t)
	if got := f.count(t); got != 1 {
		t.Fatalf("session count after pruning Create = %d, want 1", got)
	}
	if _, ok, _ := f.s.Get(ctx, live); !ok {
		t.Fatal("the live session created after the prune is missing")
	}
}

func TestStore_Delete_RemovesSession(t *testing.T) {
	f := newFixture(t)
	id := f.create(t)
	if err := f.s.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := f.s.Get(ctx, id); ok {
		t.Fatal("Get() ok = true after Delete(), want false")
	}
	if err := f.s.Delete(ctx, "does-not-exist"); err != nil { // no-op
		t.Fatal(err)
	}
}

func TestStore_SetPasswordChangedAt(t *testing.T) {
	f := newFixture(t)
	id := f.create(t)
	later := stamp.Add(time.Hour)
	if err := f.s.SetPasswordChangedAt(ctx, id, later); err != nil {
		t.Fatal(err)
	}
	got, _, _ := f.s.Get(ctx, id)
	if !got.PasswordChangedAt.Equal(later) {
		t.Fatalf("PasswordChangedAt = %v, want %v", got.PasswordChangedAt, later)
	}
}

func TestStore_Token(t *testing.T) {
	f := newFixture(t)
	a, b := f.create(t), f.create(t)
	token := f.s.Token(a)
	if token == "" || !f.s.ValidToken(a, token) {
		t.Fatal("ValidToken() = false for a token generated by Token() for the same session ID")
	}
	if f.s.ValidToken(a, "not-the-right-token") {
		t.Fatal("ValidToken() = true for a bogus token")
	}
	if f.s.Token(a) == f.s.Token(b) || f.s.ValidToken(b, token) {
		t.Fatal("a token is valid for another session")
	}
	if strings.Contains(token, a) {
		t.Fatal("token leaks the session id")
	}
}
