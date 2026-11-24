package postgres

import (
	"context"
	"fmt"
	"strings"

	"agent-verdicts/internal/search"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LexicalIndex implements search.LexicalIndex with ParadeDB BM25
// (index contribution_bm25 on contribution.search_text).
type LexicalIndex struct{ pool *pgxpool.Pool }

func NewLexicalIndex(pool *pgxpool.Pool) *LexicalIndex { return &LexicalIndex{pool: pool} }

// lexicalCandidates is the top-N pg_search keeps (its fast top-K path) when
// no state filter is set.
const lexicalCandidates = 200

// Search matches foldedQuery with `|||` (match disjunction: tokenized, no
// query-parser syntax, so any characters are safe). project_id and kind
// filter inside pg_search; state/question_status come from contribution_v
// and are applied outside, because pg_search 0.26.6 fails ("unrecognized
// node type") when the view's question_status subqueries are pushed into it.
// So that a filter never drops a match ranked below the top-N (spec §6.1),
// a filtered search scores every match in the project instead.
// ponytail: filtered searches scan all BM25 matches of the project; push the
// counter predicates into pg_search if that gets slow on large projects.
func (l *LexicalIndex) Search(ctx context.Context, projectID, foldedQuery string, f search.Filters, n int) ([]search.Hit, error) {
	if strings.TrimSpace(foldedQuery) == "" || n <= 0 {
		return nil, nil
	}
	args := []any{projectID, foldedQuery}
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	inner := "project_id = $1 AND search_text ||| $2"
	if f.Kind != "" {
		inner += " AND kind = " + arg(string(f.Kind))
	}
	if !f.IncludeSuperseded {
		inner += " AND superseded_by IS NULL"
	}
	if !f.IncludeHidden {
		inner += " AND hidden_at IS NULL"
	}
	outer, window := "true", fmt.Sprintf("LIMIT %d", lexicalCandidates)
	if s := stateFilter("v.", f.States, f.QuestionStatus, arg); s != "" {
		outer, window = s, ""
	}
	q := fmt.Sprintf(`SELECT b.id::text FROM (
		SELECT id, pdb.score(id) AS s FROM contribution WHERE %s ORDER BY s DESC %s) b
		JOIN contribution_v v ON v.id = b.id
		WHERE %s ORDER BY b.s DESC, b.id LIMIT %d`, inner, window, outer, n)
	rows, err := l.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("search: lexical: %w", err)
	}
	hits, err := pgx.CollectRows(rows, pgx.RowToStructByPos[search.Hit])
	if err != nil {
		return nil, fmt.Errorf("search: lexical: %w", err)
	}
	return hits, nil
}
