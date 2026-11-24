package postgres

import (
	"context"
	"fmt"
	"time"

	"agent-verdicts/internal/authentication"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CredentialRepo implements authentication.Repository on the credential table.
type CredentialRepo struct{ pool *pgxpool.Pool }

func NewCredentialRepo(pool *pgxpool.Pool) *CredentialRepo { return &CredentialRepo{pool: pool} }

var _ authentication.Repository = (*CredentialRepo)(nil)

const credentialColumns = `id, actor_id, secret_hash, created_at, revoked_at, label, expires_at, last_used_at`

func (r *CredentialRepo) Create(ctx context.Context, c authentication.Credential) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO credential (id, actor_id, secret_hash, created_at, label, expires_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		c.ID, c.ActorID, c.SecretHash, c.CreatedAt.UTC(), c.Label, utcPtr(c.ExpiresAt))
	if err != nil {
		return fmt.Errorf("authentication: create credential: %w", err)
	}
	return nil
}

func scanCredential(row pgx.Row) (authentication.Credential, error) {
	var c authentication.Credential
	err := row.Scan(&c.ID, &c.ActorID, &c.SecretHash, &c.CreatedAt, &c.RevokedAt, &c.Label, &c.ExpiresAt, &c.LastUsedAt)
	c.CreatedAt = c.CreatedAt.UTC()
	c.RevokedAt = utcPtr(c.RevokedAt)
	c.ExpiresAt = utcPtr(c.ExpiresAt)
	c.LastUsedAt = utcPtr(c.LastUsedAt)
	return c, err
}

func (r *CredentialRepo) Get(ctx context.Context, id string) (authentication.Credential, error) {
	c, err := scanCredential(r.pool.QueryRow(ctx, `SELECT `+credentialColumns+` FROM credential WHERE id = $1`, id))
	if isNoRow(err) {
		return authentication.Credential{}, authentication.ErrNotFound
	}
	if err != nil {
		return authentication.Credential{}, fmt.Errorf("authentication: get credential: %w", err)
	}
	return c, nil
}

func (r *CredentialRepo) ListByActor(ctx context.Context, actorID string) ([]authentication.Credential, error) {
	out := []authentication.Credential{}
	rows, err := r.pool.Query(ctx,
		`SELECT `+credentialColumns+` FROM credential WHERE actor_id = $1 ORDER BY created_at, id`, actorID)
	if err == nil {
		var got []authentication.Credential
		if got, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (authentication.Credential, error) { return scanCredential(row) }); err == nil {
			out = append(out, got...)
		}
	}
	if isNoRow(err) {
		return out, nil // a non-uuid actor id owns nothing
	}
	if err != nil {
		return nil, fmt.Errorf("authentication: list credentials by actor: %w", err)
	}
	return out, nil
}

func (r *CredentialRepo) Revoke(ctx context.Context, id string, revokedAt time.Time) error {
	tag, err := r.pool.Exec(ctx, `UPDATE credential SET revoked_at = $1 WHERE id = $2`, revokedAt.UTC(), id)
	if isNoRow(err) || (err == nil && tag.RowsAffected() == 0) {
		return authentication.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("authentication: revoke credential: %w", err)
	}
	return nil
}

func (r *CredentialRepo) Touch(ctx context.Context, id string, now time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE credential SET last_used_at = $2
		WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < $2::timestamptz - interval '1 minute')`, id, now.UTC())
	if err != nil && !isNoRow(err) {
		return fmt.Errorf("authentication: touch credential: %w", err)
	}
	return nil
}

func (r *CredentialRepo) Activity(ctx context.Context, now time.Time) (map[string]authentication.Activity, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT actor_id::text, max(last) FILTER (WHERE last IS NOT NULL), min(expiry) FILTER (WHERE expiry IS NOT NULL)
		FROM (
			SELECT actor_id, last_used_at AS last,
				CASE WHEN revoked_at IS NULL AND expires_at > $1 THEN expires_at END AS expiry
			FROM credential
			UNION ALL
			SELECT actor_id, last_login_at, NULL FROM login
		) x GROUP BY actor_id`, now.UTC())
	if err != nil {
		return nil, fmt.Errorf("authentication: activity: %w", err)
	}
	out := map[string]authentication.Activity{}
	var actorID string
	var last, next *time.Time
	_, err = pgx.ForEachRow(rows, []any{&actorID, &last, &next}, func() error {
		a := authentication.Activity{}
		if last != nil {
			t := last.UTC()
			a.LastActive = &t
		}
		if next != nil {
			t := next.UTC()
			a.NextExpiry = &t
		}
		out[actorID] = a
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("authentication: activity: %w", err)
	}
	return out, nil
}
