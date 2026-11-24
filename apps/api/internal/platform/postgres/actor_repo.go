package postgres

import (
	"context"
	"fmt"
	"time"

	"agent-verdicts/internal/actor"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ActorRepo implements actor.Repository on the actor table.
type ActorRepo struct{ pool *pgxpool.Pool }

func NewActorRepo(pool *pgxpool.Pool) *ActorRepo { return &ActorRepo{pool: pool} }

var _ actor.Repository = (*ActorRepo)(nil)

const actorColumns = `id, type, name, created_at, disabled_at`

func (r *ActorRepo) Create(ctx context.Context, a actor.Actor) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO actor (id, type, name, created_at, disabled_at) VALUES ($1, $2, $3, $4, $5)`,
		a.ID, string(a.Type), a.Name, a.CreatedAt.UTC(), utcPtr(a.DisabledAt))
	if isUniqueViolation(err) {
		return actor.ErrNameTaken
	}
	if err != nil {
		return fmt.Errorf("actor: create: %w", err)
	}
	return nil
}

func scanActor(row pgx.Row) (actor.Actor, error) {
	var a actor.Actor
	var typ string
	if err := row.Scan(&a.ID, &typ, &a.Name, &a.CreatedAt, &a.DisabledAt); err != nil {
		return actor.Actor{}, err
	}
	a.Type = actor.Type(typ)
	a.CreatedAt = a.CreatedAt.UTC()
	a.DisabledAt = utcPtr(a.DisabledAt)
	return a, nil
}

func (r *ActorRepo) Get(ctx context.Context, id string) (actor.Actor, error) {
	a, err := scanActor(r.pool.QueryRow(ctx, `SELECT `+actorColumns+` FROM actor WHERE id = $1`, id))
	if isNoRow(err) {
		return actor.Actor{}, actor.ErrNotFound
	}
	if err != nil {
		return actor.Actor{}, fmt.Errorf("actor: get: %w", err)
	}
	return a, nil
}

func (r *ActorRepo) List(ctx context.Context) ([]actor.Actor, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+actorColumns+` FROM actor ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("actor: list: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (actor.Actor, error) { return scanActor(row) })
	if err != nil {
		return nil, fmt.Errorf("actor: list: %w", err)
	}
	if out == nil {
		out = []actor.Actor{}
	}
	return out, nil
}

func (r *ActorRepo) Update(ctx context.Context, a actor.Actor) error {
	return r.exec1(ctx, `UPDATE actor SET name = $1, disabled_at = $2 WHERE id = $3`,
		a.Name, utcPtr(a.DisabledAt), a.ID)
}

func (r *ActorRepo) UpdateProfile(ctx context.Context, id, name string) error {
	return r.exec1(ctx, `UPDATE actor SET name = $1 WHERE id = $2`, name, id)
}

// SetDisabled flips the flag and, in the same transaction, recounts every
// contribution the actor has signals on: a disabled actor's signals stop
// counting, an enabled one's count again (spec 2026-11-15-retract-supersede-moderate §6).
// Disabling the last active Human admin is access.ErrLastAdmin, checked in
// that same transaction (guardLastAdmin).
func (r *ActorRepo) SetDisabled(ctx context.Context, id string, at *time.Time) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if at != nil {
			if err := guardLastAdmin(ctx, tx, id); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE actor SET disabled_at = $1 WHERE id = $2`, utcPtr(at), id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		rows, err := tx.Query(ctx, `SELECT DISTINCT contribution_id::text FROM signal WHERE actor_id = $1 ORDER BY 1`, id)
		if err != nil {
			return err
		}
		cids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		now := time.Now() // ids are locked in sorted order, so two of these never deadlock
		for _, cid := range cids {
			if err := lockContribution(ctx, tx, cid); err != nil {
				return err
			}
			if err := recount(ctx, tx, cid, now); err != nil {
				return err
			}
		}
		return nil
	})
	if isNoRow(err) {
		return actor.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("actor: set disabled: %w", err)
	}
	return nil
}

// exec1 runs a single-row UPDATE; actor.ErrNotFound when no row matched.
func (r *ActorRepo) exec1(ctx context.Context, q string, args ...any) error {
	tag, err := r.pool.Exec(ctx, q, args...)
	if isUniqueViolation(err) {
		return actor.ErrNameTaken
	}
	if isNoRow(err) || (err == nil && tag.RowsAffected() == 0) {
		return actor.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("actor: update: %w", err)
	}
	return nil
}
