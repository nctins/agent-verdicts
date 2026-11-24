package postgres

import (
	"context"
	"fmt"
	"math"
	"time"

	"agent-verdicts/internal/project"
	"agent-verdicts/internal/reads"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReadsRepo implements reads.Repository on contribution_read_day/_month.
type ReadsRepo struct{ pool *pgxpool.Pool }

func NewReadsRepo(pool *pgxpool.Pool) *ReadsRepo { return &ReadsRepo{pool: pool} }

var _ reads.Repository = (*ReadsRepo)(nil)

const nilActor = "00000000-0000-0000-0000-000000000000"

func (r *ReadsRepo) Record(ctx context.Context, projectID, actorID string, k reads.Kind, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	reader := actorID
	if k == reads.SearchHit {
		reader = nilActor
	}
	// ORDER BY id: concurrent batches lock the shared search_hit rows in the same order (no deadlock).
	_, err := r.pool.Exec(ctx, `
INSERT INTO contribution_read_day (project_id, contribution_id, day, kind, actor_id, n)
SELECT c.project_id, c.id, (now() AT TIME ZONE 'UTC')::date, $3, $4, count(*)
FROM unnest($2::uuid[]) AS u(id) JOIN contribution c ON c.id = u.id
WHERE c.project_id = $1 AND c.actor_id <> $5
GROUP BY c.project_id, c.id
ORDER BY c.id
ON CONFLICT (contribution_id, day, kind, actor_id) DO UPDATE SET n = contribution_read_day.n + EXCLUDED.n`,
		projectID, ids, k, reader, actorID)
	if err != nil {
		return fmt.Errorf("reads: record: %w", err)
	}
	return nil
}

func (r *ReadsRepo) Rollup(ctx context.Context, today time.Time) error {
	// One statement, so moving and deleting are one transaction.
	_, err := r.pool.Exec(ctx, `
WITH old AS (
  DELETE FROM contribution_read_day d USING project p
  WHERE p.id = d.project_id AND d.day < date_trunc('month', ($1::date - p.read_detail_days)::timestamp)::date
  RETURNING d.contribution_id, d.day, d.kind, d.actor_id, d.n
)
INSERT INTO contribution_read_month (contribution_id, month, kind, reads, readers)
SELECT contribution_id, date_trunc('month', day::timestamp)::date, kind, sum(n),
       CASE WHEN kind = 3 THEN 0 ELSE count(DISTINCT actor_id) END
FROM old GROUP BY 1, 2, 3
ON CONFLICT (contribution_id, month, kind) DO UPDATE
  SET reads = contribution_read_month.reads + EXCLUDED.reads, readers = contribution_read_month.readers + EXCLUDED.readers`,
		today.Format(time.DateOnly))
	if err != nil {
		return fmt.Errorf("reads: rollup: %w", err)
	}
	return nil
}

// readTotals is per (contribution, kind) reads and readers in [$2, $3] of project $1:
// distinct readers from the daily detail plus monthly readers (an upper bound).
const readTotals = `
WITH t AS (
  SELECT contribution_id, kind, sum(reads)::int AS reads, sum(readers)::int AS readers FROM (
    SELECT contribution_id, kind, sum(n) AS reads, count(DISTINCT actor_id) FILTER (WHERE kind <> 3) AS readers
    FROM contribution_read_day WHERE project_id = $1 AND day BETWEEN $2 AND $3 GROUP BY 1, 2
    UNION ALL
    SELECT m.contribution_id, m.kind, sum(m.reads), sum(m.readers)
    FROM contribution_read_month m JOIN contribution c ON c.id = m.contribution_id
    WHERE c.project_id = $1 AND m.month BETWEEN date_trunc('month', $2::timestamp)::date AND $3 GROUP BY 1, 2
  ) x GROUP BY 1, 2
)`

func (r *ReadsRepo) Stats(ctx context.Context, projectID string, from, to time.Time) (reads.Stats, error) {
	var s reads.Stats
	f, t := from.Format(time.DateOnly), to.Format(time.DateOnly)
	if err := r.pool.QueryRow(ctx, `SELECT read_detail_days FROM project WHERE id = $1`, projectID).Scan(&s.DetailDays); isNoRow(err) {
		return s, project.ErrNotFound
	} else if err != nil {
		return s, fmt.Errorf("reads: stats: %w", err)
	}
	periods := func(q string) ([]reads.Period, error) {
		rows, err := r.pool.Query(ctx, q, projectID, f, t)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (reads.Period, error) {
			var p reads.Period
			err := row.Scan(&p.Start, &p.Observe, &p.Reference, &p.SearchHit)
			return p, err
		})
	}
	var err error
	if s.Daily, err = periods(`
SELECT day, coalesce(sum(n) FILTER (WHERE kind = 1), 0), coalesce(sum(n) FILTER (WHERE kind = 2), 0), coalesce(sum(n) FILTER (WHERE kind = 3), 0)
FROM contribution_read_day WHERE project_id = $1 AND day BETWEEN $2 AND $3 GROUP BY day ORDER BY day`); err != nil {
		return s, fmt.Errorf("reads: daily: %w", err)
	}
	if s.Monthly, err = periods(`
SELECT m.month, coalesce(sum(m.reads) FILTER (WHERE m.kind = 1), 0), coalesce(sum(m.reads) FILTER (WHERE m.kind = 2), 0), coalesce(sum(m.reads) FILTER (WHERE m.kind = 3), 0)
FROM contribution_read_month m JOIN contribution c ON c.id = m.contribution_id
WHERE c.project_id = $1 AND m.month BETWEEN date_trunc('month', $2::timestamp)::date AND $3 GROUP BY m.month ORDER BY m.month`); err != nil {
		return s, fmt.Errorf("reads: monthly: %w", err)
	}
	rows, err := r.pool.Query(ctx, readTotals+`
SELECT c.id, c.title, c.kind, c.state, c.superseded_by IS NOT NULL,
  coalesce(sum(t.reads) FILTER (WHERE t.kind = 1), 0) AS obs, coalesce(sum(t.readers) FILTER (WHERE t.kind = 1), 0),
  coalesce(sum(t.reads) FILTER (WHERE t.kind = 2), 0) AS ref, coalesce(sum(t.readers) FILTER (WHERE t.kind = 2), 0),
  coalesce(sum(t.reads) FILTER (WHERE t.kind = 3), 0) AS hits
FROM t JOIN contribution_v c ON c.id = t.contribution_id
GROUP BY c.id, c.title, c.kind, c.state, c.superseded_by
ORDER BY obs DESC, ref DESC, hits DESC, c.id`, projectID, f, t)
	if err != nil {
		return s, fmt.Errorf("reads: items: %w", err)
	}
	s.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (reads.Item, error) {
		var it reads.Item
		err := row.Scan(&it.ID, &it.Title, &it.Kind, &it.State, &it.Superseded,
			&it.Observe.Reads, &it.Observe.Readers, &it.Reference.Reads, &it.Reference.Readers, &it.SearchHits)
		return it, err
	})
	if err != nil {
		return s, fmt.Errorf("reads: items: %w", err)
	}
	if err := r.pool.QueryRow(ctx, readTotals+`
SELECT count(*) FROM contribution c
WHERE c.project_id = $1 AND c.kind = 'claim' AND c.superseded_by IS NULL AND c.hidden_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM t WHERE t.contribution_id = c.id AND t.kind IN (1, 2))`, projectID, f, t).Scan(&s.NeverRead); err != nil {
		return s, fmt.Errorf("reads: never read: %w", err)
	}
	return s, nil
}

func (r *ReadsRepo) SetDetailDays(ctx context.Context, projectID string, days int, today time.Time) (reads.Retention, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE project SET read_detail_days = $2 WHERE id = $1`, projectID, days)
	if err != nil {
		return reads.Retention{}, fmt.Errorf("reads: set retention: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return reads.Retention{}, project.ErrNotFound
	}
	var rows int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM contribution_read_day WHERE project_id = $1 AND day > $2::date - 30`,
		projectID, today.Format(time.DateOnly)).Scan(&rows); err != nil {
		return reads.Retention{}, fmt.Errorf("reads: rows per day: %w", err)
	}
	perDay := float64(rows) / 30
	return reads.Retention{DetailDays: days, RowsPerDay: math.Round(perDay*10) / 10, EstimatedBytes: int64(math.Round(perDay * float64(days) * reads.BytesPerRow))}, nil
}
