package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
)

func newLoginRepo(t *testing.T) (*postgres.LoginRepo, actor.Actor, actor.Actor) {
	t.Helper()
	pool := pgtest.NewMigrated(t)
	a, b := seedActor(t, pool), seedActor(t, pool)
	return postgres.NewLoginRepo(pool), a, b
}

func TestLoginRepo_UpsertAndGet_Roundtrip(t *testing.T) {
	repo, a, _ := newLoginRepo(t)
	ctx := context.Background()
	want := authentication.Login{ActorID: a.ID, Username: "alice", PasswordHash: "h1", ChangedAt: time.Now(), MustChange: true}
	if err := repo.Upsert(ctx, want); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	for name, get := range map[string]func() (authentication.Login, error){
		"GetByUsername": func() (authentication.Login, error) { return repo.GetByUsername(ctx, "alice") },
		"GetByActor":    func() (authentication.Login, error) { return repo.GetByActor(ctx, a.ID) },
	} {
		got, err := get()
		if err != nil {
			t.Fatalf("%s() error = %v", name, err)
		}
		if got.ActorID != a.ID || got.Username != "alice" || got.PasswordHash != "h1" || !got.MustChange ||
			!got.ChangedAt.Equal(want.ChangedAt.Truncate(time.Microsecond)) {
			t.Fatalf("%s() = %+v, want %+v", name, got, want)
		}
	}
}

func TestLoginRepo_Upsert_ReplacesSameActor(t *testing.T) {
	repo, a, _ := newLoginRepo(t)
	ctx := context.Background()
	repo.Upsert(ctx, authentication.Login{ActorID: a.ID, Username: "old", PasswordHash: "h1", ChangedAt: time.Now(), MustChange: true})
	if err := repo.Upsert(ctx, authentication.Login{ActorID: a.ID, Username: "new", PasswordHash: "h2", ChangedAt: time.Now()}); err != nil {
		t.Fatalf("second Upsert() error = %v", err)
	}
	got, err := repo.GetByActor(ctx, a.ID)
	if err != nil || got.Username != "new" || got.PasswordHash != "h2" || got.MustChange {
		t.Fatalf("GetByActor() = %+v, %v", got, err)
	}
	if _, err := repo.GetByUsername(ctx, "old"); !errors.Is(err, authentication.ErrLoginNotFound) {
		t.Fatalf("old username still resolves: %v", err)
	}
}

func TestLoginRepo_Upsert_UsernameTakenByAnotherActor(t *testing.T) {
	repo, a, b := newLoginRepo(t)
	ctx := context.Background()
	repo.Upsert(ctx, authentication.Login{ActorID: a.ID, Username: "taken", PasswordHash: "h", ChangedAt: time.Now()})
	err := repo.Upsert(ctx, authentication.Login{ActorID: b.ID, Username: "taken", PasswordHash: "h", ChangedAt: time.Now()})
	if !errors.Is(err, authentication.ErrUsernameTaken) {
		t.Fatalf("Upsert(taken) = %v, want ErrUsernameTaken", err)
	}
}

func TestLoginRepo_NotFound(t *testing.T) {
	repo, a, _ := newLoginRepo(t)
	ctx := context.Background()
	for name, err := range map[string]error{
		"GetByUsername(unknown)": func() error { _, err := repo.GetByUsername(ctx, "nobody"); return err }(),
		"GetByActor(no login)":   func() error { _, err := repo.GetByActor(ctx, a.ID); return err }(),
		"GetByActor(nope)":       func() error { _, err := repo.GetByActor(ctx, "nope"); return err }(),
	} {
		if !errors.Is(err, authentication.ErrLoginNotFound) {
			t.Errorf("%s = %v, want ErrLoginNotFound", name, err)
		}
	}
}
