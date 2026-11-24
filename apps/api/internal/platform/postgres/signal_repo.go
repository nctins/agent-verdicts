package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"agent-verdicts/internal/signal"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SignalRepo implements signal.Repository on the signal table, keeping the
// contribution counters in step within the same transaction.
type SignalRepo struct{ pool *pgxpool.Pool }

func NewSignalRepo(pool *pgxpool.Pool) *SignalRepo { return &SignalRepo{pool: pool} }

var _ signal.Repository = (*SignalRepo)(nil)

// Insert locks the contribution row, which serializes writers on the same
// contribution; the unique index signal_one_active_assessment is only a
// backstop, so a unique violation is retried once (spec §4.2).
func (r *SignalRepo) Insert(ctx context.Context, s signal.Signal) (*signal.Stance, error) {
	var replaced *signal.Stance
	try := func() error {
		replaced = nil
		return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
			if err := lockContribution(ctx, tx, s.ContributionID); err != nil {
				return err
			}
			if s.Kind == signal.KindAssessment {
				var old string
				err := tx.QueryRow(ctx, `UPDATE signal SET superseded_at = now()
					WHERE contribution_id = $1 AND actor_id = $2 AND kind = 'assessment' AND superseded_at IS NULL AND withdrawn_at IS NULL
					RETURNING stance`, s.ContributionID, s.ActorID).Scan(&old)
				switch {
				case errors.Is(err, pgx.ErrNoRows):
				case err != nil:
					return err
				default:
					st := signal.Stance(old)
					replaced = &st
				}
			}
			if _, err := tx.Exec(ctx, `INSERT INTO signal (id, contribution_id, actor_id, kind, stance, body, created_at, sources, reply_to)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, nullif($9, '')::uuid)`,
				s.ID, s.ContributionID, s.ActorID, string(s.Kind), string(s.Stance), s.Body, s.CreatedAt.UTC(), nonNil(s.Sources), s.ReplyTo); err != nil {
				return err
			}
			return recount(ctx, tx, s.ContributionID, s.CreatedAt)
		})
	}
	err := try()
	if isUniqueViolation(err) {
		err = try()
	}
	if err != nil {
		return nil, fmt.Errorf("signal: insert: %w", err)
	}
	return replaced, nil
}

func lockContribution(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `SELECT 1 FROM contribution WHERE id = $1 FOR UPDATE`, id)
	return err
}

// recount rebuilds the counters of a contribution from its active signals
// (spec 2026-11-15-retract-supersede-moderate D1, D2) and sets updated_at.
// The author's own favorable signals are not counted (spec
// 2026-11-21-ignore-self-support; migration 0012 holds the same query).
// Every writer of a signal, a hidden flag or an actor's disabled flag calls
// it inside its own transaction, after locking the contribution row.
func recount(ctx context.Context, tx pgx.Tx, contributionID string, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE contribution c SET favorable_n = n.f, unfavorable_n = n.u, neutral_n = n.z, favorable_actor_n = n.fa, updated_at = $2
		FROM (SELECT count(*) FILTER (WHERE s.stance = 'favorable' AND s.actor_id <> o.actor_id) AS f,
				count(DISTINCT s.actor_id) FILTER (WHERE s.stance = 'favorable' AND s.actor_id <> o.actor_id) AS fa,
				count(*) FILTER (WHERE s.stance = 'unfavorable') AS u,
				count(*) FILTER (WHERE s.stance = 'neutral') AS z
			FROM contribution o
			LEFT JOIN (signal s JOIN actor a ON a.id = s.actor_id AND a.disabled_at IS NULL)
				ON s.contribution_id = o.id AND s.superseded_at IS NULL AND s.withdrawn_at IS NULL AND s.hidden_at IS NULL
			WHERE o.id = $1) n
		WHERE c.id = $1`, contributionID, at.UTC())
	return err
}

// change applies q (args: signal id, then extra) to one signal and recounts
// its contribution, in one transaction.
func (r *SignalRepo) change(ctx context.Context, op, id, q string, args ...any) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var cid string
		if err := tx.QueryRow(ctx, `SELECT contribution_id FROM signal WHERE id = $1`, id).Scan(&cid); err != nil {
			return err
		}
		if err := lockContribution(ctx, tx, cid); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, q, append([]any{id}, args...)...); err != nil {
			return err
		}
		return recount(ctx, tx, cid, time.Now())
	})
	if isNoRow(err) {
		return signal.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("signal: %s: %w", op, err)
	}
	return nil
}

func (r *SignalRepo) Withdraw(ctx context.Context, id string) error {
	return r.change(ctx, "withdraw", id, `UPDATE signal SET withdrawn_at = now() WHERE id = $1 AND withdrawn_at IS NULL`)
}

// A re-hide without a reason keeps the reason already stored.
func (r *SignalRepo) SetHidden(ctx context.Context, id, by, reason string, hidden bool) error {
	if hidden {
		return r.change(ctx, "hide", id, `UPDATE signal SET hidden_at = coalesce(hidden_at, now()), hidden_by = coalesce(hidden_by, $2),
			hidden_reason = coalesce(nullif($3, ''), hidden_reason) WHERE id = $1`, by, reason)
	}
	return r.change(ctx, "unhide", id, `UPDATE signal SET hidden_at = NULL, hidden_by = NULL, hidden_reason = NULL WHERE id = $1`)
}

const signalSelect = `SELECT s.id, s.contribution_id, s.actor_id, s.kind, s.stance, s.body, s.created_at, s.sources,
	s.superseded_at, s.withdrawn_at, s.hidden_at, a.disabled_at IS NOT NULL, coalesce(s.reply_to::text, ''), coalesce(s.hidden_reason, '')
	FROM signal s JOIN actor a ON a.id = s.actor_id `

func scanSignal(row pgx.Row) (signal.Signal, error) {
	var s signal.Signal
	var kind, stance string
	err := row.Scan(&s.ID, &s.ContributionID, &s.ActorID, &kind, &stance, &s.Body, &s.CreatedAt, &s.Sources,
		&s.SupersededAt, &s.WithdrawnAt, &s.HiddenAt, &s.ActorDisabled, &s.ReplyTo, &s.HiddenReason)
	s.Kind, s.Stance = signal.Kind(kind), signal.Stance(stance)
	s.CreatedAt, s.SupersededAt, s.WithdrawnAt, s.HiddenAt = s.CreatedAt.UTC(), utcPtr(s.SupersededAt), utcPtr(s.WithdrawnAt), utcPtr(s.HiddenAt)
	return s, err
}

func (r *SignalRepo) Get(ctx context.Context, id string) (signal.Signal, error) {
	s, err := scanSignal(r.pool.QueryRow(ctx, signalSelect+`WHERE s.id = $1`, id))
	if isNoRow(err) {
		return signal.Signal{}, signal.ErrNotFound
	}
	if err != nil {
		return signal.Signal{}, fmt.Errorf("signal: get: %w", err)
	}
	return s, nil
}

func (r *SignalRepo) ListByContribution(ctx context.Context, contributionID string) ([]signal.Signal, error) {
	rows, err := r.pool.Query(ctx, signalSelect+`WHERE s.contribution_id = $1 ORDER BY s.created_at, s.id`, contributionID)
	var out []signal.Signal
	if err == nil {
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (signal.Signal, error) { return scanSignal(row) })
	}
	if isNoRow(err) { // malformed id names no contribution
		return []signal.Signal{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("signal: list: %w", err)
	}
	if out == nil {
		out = []signal.Signal{}
	}
	return out, nil
}
