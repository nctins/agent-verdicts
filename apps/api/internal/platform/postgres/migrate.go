package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies each embedded migrations/NNNN_*.sql not yet recorded in
// schema_migration, one transaction per file. Safe to call repeatedly and
// concurrently (serialized by an advisory lock).
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	// One dedicated connection holds a session-level advisory lock for the
	// whole run (table creation included), serializing api + cli migrators.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("postgres: migrate: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(727274)`); err != nil {
		return fmt.Errorf("postgres: migrate: %w", err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(727274)`) }()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migration (version int PRIMARY KEY, applied_at timestamptz NOT NULL)`); err != nil {
		return fmt.Errorf("postgres: migrate: %w", err)
	}
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("postgres: migrate: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		v, err := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("postgres: migrate: bad file name %q", e.Name())
		}
		sqlText, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return fmt.Errorf("postgres: migrate: %w", err)
		}
		if err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			var done bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migration WHERE version=$1)`, v).Scan(&done); err != nil {
				return err
			}
			if done {
				return nil
			}
			if _, err := tx.Exec(ctx, string(sqlText)); err != nil {
				return fmt.Errorf("%s: %w", e.Name(), err)
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migration VALUES ($1, now())`, v)
			return err
		}); err != nil {
			return fmt.Errorf("postgres: migrate: %w", err)
		}
	}
	return nil
}
