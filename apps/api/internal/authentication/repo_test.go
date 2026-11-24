package authentication_test

import (
	"context"

	"testing"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestRepos(t *testing.T) (*pgxpool.Pool, *postgres.CredentialRepo) {
	t.Helper()
	pool := pgtest.NewMigrated(t)
	return pool, postgres.NewCredentialRepo(pool)
}

func seedActor(t *testing.T, db *pgxpool.Pool) actor.Actor {
	t.Helper()
	a := actor.New(actor.TypeAgent)
	a.Name = "Test " + a.ID
	if err := postgres.NewActorRepo(db).Create(context.Background(), a); err != nil {
		t.Fatalf("seed actor: %v", err)
	}
	return a
}
