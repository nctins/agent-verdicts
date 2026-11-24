// Package pgtest gives each test its own Postgres schema.
package pgtest

import (
	"context"
	"os"
	"strings"
	"testing"

	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/platform/postgres"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// New returns a pool whose search_path is a fresh schema (then public, for
// extensions). The schema is dropped at test cleanup. Skips when
// AGENTVERDICTS_TEST_DATABASE_URL is unset; fails if set but unreachable.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("AGENTVERDICTS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AGENTVERDICTS_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	schema := "t_" + strings.ReplaceAll(id.New(), "-", "")[:16]
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("pgtest: connect: %v", err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("pgtest: create schema: %v", err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		c, err := pgx.Connect(context.Background(), url)
		if err == nil {
			_, _ = c.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
			c.Close(context.Background())
		}
	})
	return pool
}

// NewMigrated is New plus the production migrations.
func NewMigrated(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := New(t)
	if err := postgres.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("pgtest: migrate: %v", err)
	}
	return pool
}
