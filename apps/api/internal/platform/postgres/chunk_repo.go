package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"agent-verdicts/internal/search"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ChunkRepo implements search.ChunkStore on contribution_chunk.
type ChunkRepo struct{ pool *pgxpool.Pool }

func NewChunkRepo(pool *pgxpool.Pool) *ChunkRepo { return &ChunkRepo{pool: pool} }

func (r *ChunkRepo) Replace(ctx context.Context, contributionID, model string, chunks []string, vecs [][]float32) error {
	if len(chunks) != len(vecs) {
		return fmt.Errorf("chunk: replace: %d chunks but %d vectors", len(chunks), len(vecs))
	}
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM contribution_chunk WHERE contribution_id = $1`, contributionID); err != nil {
			return err
		}
		for i, text := range chunks {
			if _, err := tx.Exec(ctx, `INSERT INTO contribution_chunk (contribution_id, idx, text, model, embedding)
				VALUES ($1, $2, $3, $4, $5::vector)`, contributionID, i, text, model, vecLiteral(vecs[i])); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("chunk: replace: %w", dimErr(err))
	}
	return nil
}

func (r *ChunkRepo) Pending(ctx context.Context, model string, limit int) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT c.id::text FROM contribution c
		WHERE NOT EXISTS (SELECT 1 FROM contribution_chunk ch WHERE ch.contribution_id = c.id AND ch.model = $1)
		ORDER BY c.created_at, c.id LIMIT $2`, model, limit)
	if err == nil {
		var ids []string
		if ids, err = pgx.CollectRows(rows, pgx.RowTo[string]); err == nil {
			return ids, nil
		}
	}
	return nil, fmt.Errorf("chunk: pending: %w", err)
}

func (r *ChunkRepo) DeleteAll(ctx context.Context) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM contribution_chunk`); err != nil {
		return fmt.Errorf("chunk: delete all: %w", err)
	}
	return nil
}

func (r *ChunkRepo) Count(ctx context.Context, model string) (embedded, total int, err error) {
	err = r.pool.QueryRow(ctx, `SELECT
		(SELECT count(DISTINCT contribution_id) FROM contribution_chunk WHERE model = $1),
		(SELECT count(*) FROM contribution)`, model).Scan(&embedded, &total)
	if err != nil {
		return 0, 0, fmt.Errorf("chunk: count: %w", err)
	}
	return embedded, total, nil
}

func (r *ChunkRepo) BestChunk(ctx context.Context, contributionID, model string, v []float32) (search.ChunkHit, error) {
	var h search.ChunkHit
	err := r.pool.QueryRow(ctx, `SELECT contribution_id::text, idx, text FROM contribution_chunk
		WHERE contribution_id = $1 AND model = $2 ORDER BY embedding <=> $3::vector, idx LIMIT 1`,
		contributionID, model, vecLiteral(v)).Scan(&h.ContributionID, &h.Idx, &h.Text)
	if isNoRow(err) {
		return search.ChunkHit{}, nil
	}
	if err != nil {
		return search.ChunkHit{}, fmt.Errorf("chunk: best chunk: %w", err)
	}
	return h, nil
}

// quoteLiteral renders s as an SQL string literal for DDL, where bind
// parameters are not allowed (standard_conforming_strings: only ' needs escaping).
func quoteLiteral(s string) (string, error) {
	if strings.IndexByte(s, 0) >= 0 {
		return "", errors.New("contains NUL byte")
	}
	if len(s) > 200 {
		return "", errors.New("longer than 200 bytes")
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'", nil
}

// EnsureIndex makes chunk_hnsw_<sha256(model+"\x00"+dim)[:12]> the only
// chunk_hnsw_* index in the current schema, so a new model or a new dim
// under the same model replaces the old index. CONCURRENTLY cannot run in a
// transaction, so it uses a dedicated connection. Invalid leftovers (a
// failed concurrent build) are dropped and rebuilt.
//
// On a model/dim change callers must DeleteAll first, then EnsureIndex, then
// re-embed: the old index's ::vector(oldDim) cast rejects new-dim rows, and
// old-dim rows would make the new index build fail.
func (r *ChunkRepo) EnsureIndex(ctx context.Context, model string, dim int) error {
	lit, err := quoteLiteral(model)
	if err == nil && dim <= 0 {
		err = fmt.Errorf("dimension %d", dim)
	}
	if err != nil {
		return fmt.Errorf("chunk: ensure index: model %q: %w", model, err)
	}
	sum := sha256.Sum256([]byte(model + "\x00" + strconv.Itoa(dim)))
	name := "chunk_hnsw_" + hex.EncodeToString(sum[:])[:12]

	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("chunk: ensure index: %w", err)
	}
	defer conn.Release()

	type index struct {
		Schema, Name string
		Valid        bool
	}
	rows, err := conn.Query(ctx, `SELECT n.nspname, c.relname, i.indisvalid FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relname LIKE 'chunk\_hnsw\_%'`)
	var existing []index
	if err == nil {
		existing, err = pgx.CollectRows(rows, pgx.RowToStructByPos[index])
	}
	for _, ix := range existing {
		if err != nil || (ix.Name == name && ix.Valid) {
			continue
		}
		_, err = conn.Exec(ctx, `DROP INDEX CONCURRENTLY IF EXISTS `+pgx.Identifier{ix.Schema, ix.Name}.Sanitize())
	}
	if err == nil {
		_, err = conn.Exec(ctx, fmt.Sprintf(`CREATE INDEX CONCURRENTLY IF NOT EXISTS %s ON contribution_chunk
			USING hnsw ((embedding::vector(%d)) vector_cosine_ops) WHERE model = %s`,
			pgx.Identifier{name}.Sanitize(), dim, lit))
	}
	if err != nil {
		return fmt.Errorf("chunk: ensure index: %w", dimErr(err))
	}
	return nil
}

// dimErr marks pgvector's "expected N dimensions, not M" (22000) as search.ErrDimMismatch.
func dimErr(err error) error {
	if pgCode(err) == "22000" && strings.Contains(err.Error(), "dimensions") {
		return fmt.Errorf("%w: %w", search.ErrDimMismatch, err)
	}
	return err
}
