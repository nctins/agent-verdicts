package postgres

import (
	"context"
	"fmt"
	"time"

	"agent-verdicts/internal/authentication"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LoginRepo implements authentication.LoginRepository on the login table.
type LoginRepo struct{ pool *pgxpool.Pool }

func NewLoginRepo(pool *pgxpool.Pool) *LoginRepo { return &LoginRepo{pool: pool} }

var _ authentication.LoginRepository = (*LoginRepo)(nil)

func (r *LoginRepo) Upsert(ctx context.Context, l authentication.Login) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO login (actor_id, username, password_hash, changed_at, must_change) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (actor_id) DO UPDATE SET
			username = excluded.username, password_hash = excluded.password_hash,
			changed_at = excluded.changed_at, must_change = excluded.must_change`,
		l.ActorID, l.Username, l.PasswordHash, l.ChangedAt.UTC(), l.MustChange)
	if isUniqueViolation(err) {
		return authentication.ErrUsernameTaken
	}
	if err != nil {
		return fmt.Errorf("authentication: upsert login: %w", err)
	}
	return nil
}

func (r *LoginRepo) GetByUsername(ctx context.Context, username string) (authentication.Login, error) {
	return r.get(ctx, `username = $1`, username)
}

func (r *LoginRepo) GetByActor(ctx context.Context, actorID string) (authentication.Login, error) {
	return r.get(ctx, `actor_id = $1`, actorID)
}

func (r *LoginRepo) get(ctx context.Context, where, arg string) (authentication.Login, error) {
	var l authentication.Login
	err := r.pool.QueryRow(ctx,
		`SELECT actor_id, username, password_hash, changed_at, must_change FROM login WHERE `+where, arg,
	).Scan(&l.ActorID, &l.Username, &l.PasswordHash, &l.ChangedAt, &l.MustChange)
	if isNoRow(err) {
		return authentication.Login{}, authentication.ErrLoginNotFound
	}
	if err != nil {
		return authentication.Login{}, fmt.Errorf("authentication: get login: %w", err)
	}
	l.ChangedAt = l.ChangedAt.UTC()
	return l, nil
}

func (r *LoginRepo) TouchLogin(ctx context.Context, actorID string, at time.Time) error {
	if _, err := r.pool.Exec(ctx, `UPDATE login SET last_login_at = $2 WHERE actor_id = $1`, actorID, at.UTC()); err != nil {
		return fmt.Errorf("authentication: touch login: %w", err)
	}
	return nil
}
