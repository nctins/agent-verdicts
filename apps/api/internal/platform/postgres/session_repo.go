package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionRepo is the session table (implements session.Repository).
type SessionRepo struct{ pool *pgxpool.Pool }

func NewSessionRepo(pool *pgxpool.Pool) *SessionRepo { return &SessionRepo{pool: pool} }

const csrfSecretKey = "csrf_secret"

func (r *SessionRepo) Insert(ctx context.Context, idHash, actorID string, passwordChangedAt, now, idleCutoff, absCutoff time.Time) error {
	_, err := r.pool.Exec(ctx, `WITH gc AS (DELETE FROM session WHERE last_seen_at < $5 OR created_at < $6)
		INSERT INTO session (id_hash, actor_id, password_changed_at, created_at, last_seen_at) VALUES ($1, $2, $3, $4, $4)`,
		idHash, actorID, passwordChangedAt.UTC(), now.UTC(), idleCutoff.UTC(), absCutoff.UTC())
	if err != nil {
		return fmt.Errorf("session: insert: %w", err)
	}
	return nil
}

func (r *SessionRepo) Get(ctx context.Context, idHash string, now, idleCutoff, absCutoff time.Time) (string, time.Time, bool, error) {
	var actorID string
	var changed, seen time.Time
	err := r.pool.QueryRow(ctx, `SELECT actor_id::text, password_changed_at, last_seen_at FROM session
		WHERE id_hash = $1 AND last_seen_at >= $2 AND created_at >= $3`, idHash, idleCutoff.UTC(), absCutoff.UTC()).Scan(&actorID, &changed, &seen)
	if isNoRow(err) {
		return "", time.Time{}, false, nil
	}
	if err != nil {
		return "", time.Time{}, false, fmt.Errorf("session: get: %w", err)
	}
	if seen.Before(now.Add(-time.Minute)) { // at most one write a minute
		if _, err := r.pool.Exec(ctx, `UPDATE session SET last_seen_at = $2 WHERE id_hash = $1 AND last_seen_at < $3`,
			idHash, now.UTC(), now.Add(-time.Minute).UTC()); err != nil {
			return "", time.Time{}, false, fmt.Errorf("session: touch: %w", err)
		}
	}
	return actorID, changed.UTC(), true, nil
}

func (r *SessionRepo) Delete(ctx context.Context, idHash string) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM session WHERE id_hash = $1`, idHash); err != nil {
		return fmt.Errorf("session: delete: %w", err)
	}
	return nil
}

func (r *SessionRepo) SetPasswordChangedAt(ctx context.Context, idHash string, t time.Time) error {
	if _, err := r.pool.Exec(ctx, `UPDATE session SET password_changed_at = $2 WHERE id_hash = $1`, idHash, t.UTC()); err != nil {
		return fmt.Errorf("session: set password changed: %w", err)
	}
	return nil
}

func (r *SessionRepo) CSRFSecret(ctx context.Context, candidate string) (string, error) {
	if _, err := r.pool.Exec(ctx, `INSERT INTO setting (key, value, updated_at) VALUES ($1, to_jsonb($2::text), now())
		ON CONFLICT (key) DO NOTHING`, csrfSecretKey, candidate); err != nil {
		return "", err
	}
	var secret string
	err := r.pool.QueryRow(ctx, `SELECT value #>> '{}' FROM setting WHERE key = $1`, csrfSecretKey).Scan(&secret)
	return secret, err
}
