package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newCredentialRepos(t *testing.T) (*pgxpool.Pool, *postgres.CredentialRepo) {
	t.Helper()
	pool := pgtest.NewMigrated(t)
	return pool, postgres.NewCredentialRepo(pool)
}

func seedActor(t *testing.T, db *pgxpool.Pool) actor.Actor {
	t.Helper()
	a := actor.New(actor.TypeAgent)
	a.Name = "Test " + a.ID // names are unique
	if err := postgres.NewActorRepo(db).Create(context.Background(), a); err != nil {
		t.Fatalf("seed actor: %v", err)
	}
	return a
}

func TestCredentialRepo_CreateAndGet_Roundtrip(t *testing.T) {
	db, repo := newCredentialRepos(t)
	a := seedActor(t, db)
	ctx := context.Background()

	want := authentication.Credential{
		ID:         id.New(),
		ActorID:    a.ID,
		SecretHash: "deadbeef",
		CreatedAt:  time.Now(),
	}
	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != want.ID || got.ActorID != want.ActorID || got.SecretHash != want.SecretHash {
		t.Fatalf("Get() = %+v, want %+v", got, want)
	}
	if got.RevokedAt != nil {
		t.Fatalf("Get().RevokedAt = %v, want nil for a freshly created credential", got.RevokedAt)
	}
}

func TestCredentialRepo_Get_NotFound(t *testing.T) {
	_, repo := newCredentialRepos(t)
	_, err := repo.Get(context.Background(), "does-not-exist")
	if !errors.Is(err, authentication.ErrNotFound) {
		t.Fatalf("Get() error = %v, want authentication.ErrNotFound", err)
	}
}

func TestCredentialRepo_Create_DuplicateID(t *testing.T) {
	db, repo := newCredentialRepos(t)
	a := seedActor(t, db)
	cred := authentication.Credential{ID: id.New(), ActorID: a.ID, SecretHash: "x", CreatedAt: time.Now()}
	if err := repo.Create(context.Background(), cred); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	if err := repo.Create(context.Background(), cred); err == nil {
		t.Fatal("second Create() with duplicate ID: want error, got nil")
	}
}

func TestCredentialRepo_Create_UnknownActor_ForeignKeyRejected(t *testing.T) {
	_, repo := newCredentialRepos(t)
	err := repo.Create(context.Background(), authentication.Credential{
		ID: id.New(), ActorID: id.New(), SecretHash: "x", CreatedAt: time.Now(),
	})
	if err == nil {
		t.Fatal("Create() with unknown actor_id: want error (foreign key violation), got nil")
	}
}

func TestCredentialRepo_Revoke_SetsRevokedAt(t *testing.T) {
	db, repo := newCredentialRepos(t)
	a := seedActor(t, db)
	ctx := context.Background()
	cred := authentication.Credential{ID: id.New(), ActorID: a.ID, SecretHash: "x", CreatedAt: time.Now()}
	if err := repo.Create(ctx, cred); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repo.Revoke(ctx, cred.ID, time.Now()); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	got, err := repo.Get(ctx, cred.ID)
	if err != nil {
		t.Fatalf("Get() after revoke error = %v", err)
	}
	if got.RevokedAt == nil {
		t.Fatal("Get() after Revoke(): RevokedAt = nil, want set")
	}
}

func TestCredentialRepo_Revoke_UnknownID(t *testing.T) {
	_, repo := newCredentialRepos(t)
	err := repo.Revoke(context.Background(), "does-not-exist", time.Now())
	if !errors.Is(err, authentication.ErrNotFound) {
		t.Fatalf("Revoke() error = %v, want authentication.ErrNotFound", err)
	}
}

// TestCredentialRepo_MultipleCredentialsPerActor proves Decision #9 §2
// (multiplicity) and that revoking one credential does not revoke another
// belonging to the same Actor.
func TestCredentialRepo_MultipleCredentialsPerActor(t *testing.T) {
	db, repo := newCredentialRepos(t)
	a := seedActor(t, db)
	ctx := context.Background()
	c1 := authentication.Credential{ID: id.New(), ActorID: a.ID, SecretHash: "x", CreatedAt: time.Now()}
	c2 := authentication.Credential{ID: id.New(), ActorID: a.ID, SecretHash: "y", CreatedAt: time.Now()}
	if err := repo.Create(ctx, c1); err != nil {
		t.Fatalf("Create() c1 error = %v", err)
	}
	if err := repo.Create(ctx, c2); err != nil {
		t.Fatalf("Create() c2 error = %v", err)
	}
	if err := repo.Revoke(ctx, c1.ID, time.Now()); err != nil {
		t.Fatalf("Revoke() c1 error = %v", err)
	}
	got2, err := repo.Get(ctx, c2.ID)
	if err != nil {
		t.Fatalf("Get() c2 error = %v", err)
	}
	if got2.RevokedAt != nil {
		t.Fatal("revoking c1 must not revoke c2 — multiple credentials per Actor must be independent (Decision #9 §2)")
	}
}

// TestCredentialRepo_ListByActor_ReturnsAllInCreatedOrder proves
// ListByActor (Decision #10, UC11) returns every credential belonging to an
// Actor — including a revoked one — ordered by created_at ascending, and
// excludes credentials belonging to a different Actor.
func TestCredentialRepo_ListByActor_ReturnsAllInCreatedOrder(t *testing.T) {
	db, repo := newCredentialRepos(t)
	a := seedActor(t, db)
	other := seedActor(t, db)
	ctx := context.Background()

	c1 := authentication.Credential{ID: id.New(), ActorID: a.ID, SecretHash: "x", CreatedAt: time.Now()}
	c2 := authentication.Credential{ID: id.New(), ActorID: a.ID, SecretHash: "y", CreatedAt: time.Now().Add(time.Second)}
	c3 := authentication.Credential{ID: id.New(), ActorID: other.ID, SecretHash: "z", CreatedAt: time.Now()}
	for _, c := range []authentication.Credential{c1, c2, c3} {
		if err := repo.Create(ctx, c); err != nil {
			t.Fatalf("Create(%s) error = %v", c.ID, err)
		}
	}
	if err := repo.Revoke(ctx, c1.ID, time.Now()); err != nil {
		t.Fatalf("Revoke(c1) error = %v", err)
	}

	got, err := repo.ListByActor(ctx, a.ID)
	if err != nil {
		t.Fatalf("ListByActor() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListByActor() returned %d credentials, want 2", len(got))
	}
	if got[0].ID != c1.ID || got[1].ID != c2.ID {
		t.Fatalf("ListByActor() order = [%s, %s], want [%s, %s]", got[0].ID, got[1].ID, c1.ID, c2.ID)
	}
	if got[0].RevokedAt == nil {
		t.Fatal("ListByActor()[0].RevokedAt = nil, want set (revoked credential must still be listed)")
	}
	if got[1].RevokedAt != nil {
		t.Fatal("ListByActor()[1].RevokedAt = non-nil, want nil (not revoked)")
	}
}

// TestCredentialRepo_ListByActor_NoCredentials_ReturnsEmptyNotError proves
// "no credentials yet" is a valid state, not an error.
func TestCredentialRepo_ListByActor_NoCredentials_ReturnsEmptyNotError(t *testing.T) {
	db, repo := newCredentialRepos(t)
	a := seedActor(t, db)

	got, err := repo.ListByActor(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("ListByActor() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListByActor() = %d credentials, want 0", len(got))
	}
}

// TestCredentialRepo_RevokeDoesNotTouchActor proves revocation is
// credential-scoped: the Actor the credential was bound to remains
// completely unaffected and retrievable.
func TestCredentialRepo_RevokeDoesNotTouchActor(t *testing.T) {
	db, repo := newCredentialRepos(t)
	a := seedActor(t, db)
	ctx := context.Background()
	cred := authentication.Credential{ID: id.New(), ActorID: a.ID, SecretHash: "x", CreatedAt: time.Now()}
	if err := repo.Create(ctx, cred); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repo.Revoke(ctx, cred.ID, time.Now()); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	got, err := postgres.NewActorRepo(db).Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("actor.Get() after credential revoke error = %v, want the Actor to still exist", err)
	}
	if got.ID != a.ID {
		t.Fatalf("actor.Get() = %+v, want unchanged Actor %+v", got, a)
	}
}

// A non-UUID id (e.g. the prefix of a garbled token) names no credential.
func TestCredentialRepo_NonUUIDID_IsNotFound(t *testing.T) {
	_, repo := newCredentialRepos(t)
	ctx := context.Background()
	if _, err := repo.Get(ctx, "nope"); !errors.Is(err, authentication.ErrNotFound) {
		t.Fatalf("Get(nope) = %v, want ErrNotFound", err)
	}
	if err := repo.Revoke(ctx, "nope", time.Now()); !errors.Is(err, authentication.ErrNotFound) {
		t.Fatalf("Revoke(nope) = %v, want ErrNotFound", err)
	}
	got, err := repo.ListByActor(ctx, "nope")
	if err != nil || len(got) != 0 {
		t.Fatalf("ListByActor(nope) = %v, %v; want empty, nil", got, err)
	}
}
