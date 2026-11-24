package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"agent-verdicts/internal/search"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// VectorIndex implements search.VectorIndex with pgvector over
// contribution_chunk (HNSW index per model, see ChunkRepo.EnsureIndex).
type VectorIndex struct{ pool *pgxpool.Pool }

func NewVectorIndex(pool *pgxpool.Pool) *VectorIndex { return &VectorIndex{pool: pool} }

// vecLiteral formats v as pgvector text input: "[x,y,...]".
func vecLiteral(v []float32) string {
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = strconv.FormatFloat(float64(x), 'g', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// Inner candidate window (nearest chunks before picking one per
// contribution): n*vectorWindowPerHit, doubled once when it filled up yet
// gave fewer than n contributions, at most vectorWindowMax (= pgvector's
// default hnsw.max_scan_tuples).
const (
	vectorWindowPerHit = 40 // a 64KB body is ~47 chunks
	vectorWindowMax    = 20000
)

// vectorQuery: nearest chunks (cosine) of model in projectID passing f, best
// chunk per contribution, nearest first; each row also carries the number of
// candidates the window held. The ::vector(dim) cast matches the HNSW index
// expression; dim is len(v), never user text.
func vectorQuery(projectID string, v []float32, model string, f search.Filters, n, window int) (string, []any) {
	args := []any{projectID, vecLiteral(v), model}
	arg := func(x any) string { args = append(args, x); return fmt.Sprintf("$%d", len(args)) }
	where := "ch.model = $3 AND v.project_id = $1"
	if f.Kind != "" {
		where += " AND v.kind = " + arg(string(f.Kind))
	}
	if !f.IncludeSuperseded {
		where += " AND v.superseded_by IS NULL"
	}
	if !f.IncludeHidden {
		where += " AND v.hidden_at IS NULL"
	}
	if s := stateFilter("v.", f.States, f.QuestionStatus, arg); s != "" {
		where += " AND " + s
	}
	dim := len(v)
	return fmt.Sprintf(`SELECT contribution_id::text, idx, text, window_n FROM (
		SELECT DISTINCT ON (k.contribution_id) k.*, count(*) OVER () AS window_n FROM (
			SELECT ch.contribution_id, ch.idx, ch.text, (ch.embedding::vector(%d)) <=> $2::vector(%d) AS d
			FROM contribution_chunk ch JOIN contribution_v v ON v.id = ch.contribution_id
			WHERE %s ORDER BY d LIMIT %d) k
		ORDER BY k.contribution_id, k.d) best
	ORDER BY d, contribution_id LIMIT %d`, dim, dim, where, window, n), args
}

func (x *VectorIndex) Search(ctx context.Context, projectID string, v []float32, model string, f search.Filters, n int) ([]search.ChunkHit, error) {
	if n <= 0 {
		return nil, nil
	}
	if len(v) == 0 {
		return nil, errors.New("search: vector: empty query vector")
	}
	var hits []search.ChunkHit
	err := pgx.BeginFunc(ctx, x.pool, func(tx pgx.Tx) error {
		// Keep scanning the HNSW index until LIMIT rows survive the filters.
		if _, err := tx.Exec(ctx, `SET LOCAL hnsw.iterative_scan = relaxed_order`); err != nil {
			return err
		}
		window := min(n*vectorWindowPerHit, vectorWindowMax)
		for try := 0; ; try++ {
			q, args := vectorQuery(projectID, v, model, f, n, window)
			rows, err := tx.Query(ctx, q, args...)
			if err != nil {
				return err
			}
			var held int
			hits, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (search.ChunkHit, error) {
				var h search.ChunkHit
				return h, row.Scan(&h.ContributionID, &h.Idx, &h.Text, &held)
			})
			if err != nil || try > 0 || len(hits) >= n || held < window || window >= vectorWindowMax {
				return err
			}
			window = min(window*2, vectorWindowMax) // the window filled with chunks of too few contributions
		}
	})
	if err != nil {
		return nil, fmt.Errorf("search: vector: %w", err)
	}
	return hits, nil
}
