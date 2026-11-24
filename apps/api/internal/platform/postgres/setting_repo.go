package postgres

import (
	"context"
	"fmt"

	"agent-verdicts/internal/setting"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SettingRepo implements setting.Store on the setting table.
type SettingRepo struct{ pool *pgxpool.Pool }

func NewSettingRepo(pool *pgxpool.Pool) *SettingRepo { return &SettingRepo{pool: pool} }

var _ setting.Store = (*SettingRepo)(nil)

func (r *SettingRepo) Get(ctx context.Context, key string) ([]byte, error) {
	var v []byte
	err := r.pool.QueryRow(ctx, `SELECT value FROM setting WHERE key = $1`, key).Scan(&v)
	if isNoRow(err) {
		return nil, setting.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("setting: get: %w", err)
	}
	return v, nil
}

func (r *SettingRepo) Put(ctx context.Context, key string, v []byte) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO setting (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, key, v)
	if err != nil {
		return fmt.Errorf("setting: put: %w", err)
	}
	return nil
}
