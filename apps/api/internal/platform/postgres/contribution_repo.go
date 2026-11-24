package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/trust"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ContributionRepo implements contribution.Repository on the contribution
// table; reads go through the contribution_v view (0002_views.sql).
type ContributionRepo struct{ pool *pgxpool.Pool }

func NewContributionRepo(pool *pgxpool.Pool) *ContributionRepo { return &ContributionRepo{pool: pool} }

var _ contribution.Repository = (*ContributionRepo)(nil)

const contributionColumns = `id, project_id, actor_id, kind, title, body, coalesce(answers_id::text, ''),
	favorable_n, unfavorable_n, neutral_n, favorable_actor_n, created_at, updated_at, coalesce(superseded_by::text, ''), superseded_at, hidden_at,
	sources, min_favorable, coalesce(hidden_reason, '')`

// stateByRank inverts the view's rank column (= trust.Rank).
var stateByRank = [...]trust.State{trust.StateFavorable, trust.StateConflict, trust.StateInsufficientEvidence, trust.StateUnfavorable}

func (r *ContributionRepo) Create(ctx context.Context, c contribution.Contribution, searchText, supersedes string) error {
	var answers *string
	if c.AnswersID != "" {
		answers = &c.AnswersID
	}
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if supersedes != "" { // lock the old claim so two replacements cannot both win
			var by *string
			if err := tx.QueryRow(ctx, `SELECT superseded_by::text FROM contribution WHERE id = $1 FOR UPDATE`, supersedes).Scan(&by); err != nil {
				return err
			}
			if by != nil {
				return contribution.ErrAlreadySuperseded
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO contribution
			(id, project_id, actor_id, kind, title, body, answers_id, search_text, created_at, updated_at, sources)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9, $10)`,
			c.ID, c.ProjectID, c.ActorID, string(c.Kind), c.Title, c.Body, answers, searchText, c.CreatedAt.UTC(), nonNil(c.Sources)); err != nil {
			return err
		}
		if supersedes != "" {
			_, err := tx.Exec(ctx, `UPDATE contribution SET superseded_by = $2, superseded_at = $3 WHERE id = $1`, supersedes, c.ID, c.CreatedAt.UTC())
			return err
		}
		return nil
	})
	if errors.Is(err, contribution.ErrAlreadySuperseded) {
		return err
	}
	if isNoRow(err) {
		return contribution.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("contribution: create: %w", err)
	}
	return nil
}

func (r *ContributionRepo) ClearSuperseded(ctx context.Context, id string) error {
	if _, err := r.pool.Exec(ctx, `UPDATE contribution SET superseded_by = NULL, superseded_at = NULL WHERE id = $1`, id); err != nil {
		return fmt.Errorf("contribution: clear superseded: %w", err)
	}
	return nil
}

// A re-hide without a reason keeps the reason already stored.
func (r *ContributionRepo) SetHidden(ctx context.Context, id, by, reason string, hidden bool) error {
	q, args := `UPDATE contribution SET hidden_at = NULL, hidden_by = NULL, hidden_reason = NULL WHERE id = $1`, []any{id}
	if hidden {
		q, args = `UPDATE contribution SET hidden_at = coalesce(hidden_at, now()), hidden_by = coalesce(hidden_by, $2),
			hidden_reason = coalesce(nullif($3, ''), hidden_reason) WHERE id = $1`, []any{id, by, reason}
	}
	tag, err := r.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("contribution: set hidden: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return contribution.ErrNotFound
	}
	return nil
}

func scanContribution(row pgx.Row) (contribution.Contribution, error) {
	var c contribution.Contribution
	var kind string
	err := row.Scan(&c.ID, &c.ProjectID, &c.ActorID, &kind, &c.Title, &c.Body, &c.AnswersID,
		&c.Counts.Favorable, &c.Counts.Unfavorable, &c.Counts.Neutral, &c.Counts.FavorableActors, &c.CreatedAt, &c.UpdatedAt, &c.SupersededByID, &c.SupersededAt, &c.HiddenAt,
		&c.Sources, &c.MinFavorable, &c.HiddenReason)
	c.Kind = contribution.Kind(kind)
	c.CreatedAt, c.UpdatedAt = c.CreatedAt.UTC(), c.UpdatedAt.UTC()
	c.SupersededAt, c.HiddenAt = utcPtr(c.SupersededAt), utcPtr(c.HiddenAt)
	return c, err
}

func (r *ContributionRepo) query(ctx context.Context, op, q string, args ...any) ([]contribution.Contribution, error) {
	rows, err := r.pool.Query(ctx, q, args...)
	if err == nil {
		var out []contribution.Contribution
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (contribution.Contribution, error) { return scanContribution(row) })
		if err == nil {
			return out, nil
		}
	}
	if isNoRow(err) {
		return nil, contribution.ErrNotFound
	}
	return nil, fmt.Errorf("contribution: %s: %w", op, err)
}

func (r *ContributionRepo) Get(ctx context.Context, projectID, id string) (contribution.Contribution, error) {
	return r.getWhere(ctx, `project_id = $1 AND id = $2`, projectID, id)
}

func (r *ContributionRepo) GetByID(ctx context.Context, id string) (contribution.Contribution, error) {
	return r.getWhere(ctx, `id = $1`, id)
}

func (r *ContributionRepo) getWhere(ctx context.Context, where string, args ...any) (contribution.Contribution, error) {
	c, err := scanContribution(r.pool.QueryRow(ctx, `SELECT `+contributionColumns+` FROM contribution_v WHERE `+where, args...))
	if isNoRow(err) {
		return contribution.Contribution{}, contribution.ErrNotFound
	}
	if err != nil {
		return contribution.Contribution{}, fmt.Errorf("contribution: get: %w", err)
	}
	return c, nil
}

// cursor is the opaque List position: the last item's sort key and id,
// tagged with the sort it belongs to.
type cursor struct {
	Sort contribution.Sort `json:"s"`
	Key  json.RawMessage   `json:"k"`
	ID   string            `json:"id"`
}

func encodeCursor(s contribution.Sort, c contribution.Contribution) string {
	var key any = c.CreatedAt
	switch s {
	case contribution.SortActive:
		key = c.UpdatedAt
	case contribution.SortDebated, contribution.SortLeast:
		key = c.Counts.Favorable + c.Counts.Unfavorable + c.Counts.Neutral
	}
	k, _ := json.Marshal(key)
	b, _ := json.Marshal(cursor{Sort: s, Key: k, ID: c.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeCursor returns the sort key (time.Time for newest and active, int otherwise) and id.
func decodeCursor(s contribution.Sort, raw string) (any, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	var c cursor
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	if err == nil && (c.Sort != s || c.ID == "") {
		err = fmt.Errorf("cursor for sort %q", c.Sort)
	}
	if err != nil {
		return nil, "", contribution.ErrBadCursor
	}
	if s == contribution.SortNewest || s == contribution.SortActive {
		var t time.Time
		err = json.Unmarshal(c.Key, &t)
		return t, c.ID, badCursorIf(err)
	}
	var n int
	err = json.Unmarshal(c.Key, &n)
	return n, c.ID, badCursorIf(err)
}

func badCursorIf(err error) error {
	if err != nil {
		return contribution.ErrBadCursor
	}
	return nil
}

func (r *ContributionRepo) List(ctx context.Context, projectID string, f contribution.ListFilter) (contribution.Page, error) {
	f, err := f.Normalize()
	if err != nil {
		return contribution.Page{}, err
	}
	args := []any{projectID}
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	where := []string{"project_id = $1"}
	if f.Kind != "" {
		where = append(where, "kind = "+arg(string(f.Kind)))
	}
	if f.ActorID != "" {
		where = append(where, "actor_id::text = "+arg(f.ActorID)) // text compare: a malformed id just matches nothing
	}
	if !f.IncludeSuperseded {
		where = append(where, "superseded_by IS NULL")
	}
	if !f.IncludeHidden {
		where = append(where, "hidden_at IS NULL")
	}
	if !f.Since.IsZero() {
		where = append(where, "updated_at > "+arg(f.Since.UTC()))
	}
	if s := stateFilter("", f.States, f.QuestionStatus, arg); s != "" {
		where = append(where, s)
	}
	keyCol, cmp, dir := "signal_n", "<", "DESC"
	switch f.Sort {
	case contribution.SortNewest:
		keyCol = "created_at"
	case contribution.SortActive:
		keyCol = "updated_at"
	case contribution.SortLeast:
		cmp, dir = ">", "ASC"
	}
	if f.Cursor != "" {
		key, id, err := decodeCursor(f.Sort, f.Cursor)
		if err != nil {
			return contribution.Page{}, err
		}
		where = append(where, fmt.Sprintf("(%s, id) %s (%s, %s::uuid)", keyCol, cmp, arg(key), arg(id)))
	}
	q := fmt.Sprintf(`SELECT %s FROM contribution_v WHERE %s ORDER BY %s %s, id %s LIMIT %d`,
		contributionColumns, strings.Join(where, " AND "), keyCol, dir, dir, f.Limit+1)

	items, err := r.query(ctx, "list", q, args...)
	if errors.Is(err, contribution.ErrNotFound) { // 22P02: the only uuid-cast input here is the cursor id
		return contribution.Page{}, contribution.ErrBadCursor
	}
	if err != nil {
		return contribution.Page{}, err
	}
	page := contribution.Page{Items: items}
	if len(items) > f.Limit {
		page.Items = items[:f.Limit]
		page.NextCursor = encodeCursor(f.Sort, page.Items[f.Limit-1])
	}
	if page.Items == nil {
		page.Items = []contribution.Contribution{}
	}
	return page, nil
}

// stateFilter is the States/QuestionStatus condition of contribution.ListFilter
// (shared with search.Filters) on contribution_v columns qualified by alias
// ("" or e.g. "v."): States picks claims, QuestionStatus picks questions,
// union when both. "" when neither is set.
func stateFilter(alias string, states []trust.State, qs []contribution.QuestionStatus, arg func(any) string) string {
	var either []string
	if len(states) > 0 {
		either = append(either, fmt.Sprintf("(%[1]skind = 'claim' AND %[1]sstate = ANY(%[2]s::text[]))", alias, arg(strs(states))))
	}
	if len(qs) > 0 {
		either = append(either, fmt.Sprintf("(%[1]skind = 'question' AND %[1]squestion_status = ANY(%[2]s::text[]))", alias, arg(strs(qs))))
	}
	if len(either) == 0 {
		return ""
	}
	return "(" + strings.Join(either, " OR ") + ")"
}

func strs[T ~string](xs []T) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = string(x)
	}
	return out
}

func (r *ContributionRepo) Answers(ctx context.Context, questionID string, includeHidden bool) ([]contribution.Contribution, error) {
	out, err := r.query(ctx, "answers", `SELECT `+contributionColumns+` FROM contribution_v
		WHERE answers_id = $1 AND (hidden_at IS NULL OR $2) ORDER BY rank, created_at, id`, questionID, includeHidden)
	if out == nil && err == nil {
		out = []contribution.Contribution{}
	}
	return out, err
}

func (r *ContributionRepo) QuestionInfo(ctx context.Context, questionIDs []string) (map[string]contribution.QuestionInfo, error) {
	out := make(map[string]contribution.QuestionInfo, len(questionIDs))
	for _, id := range questionIDs {
		out[id] = contribution.QuestionInfo{Status: contribution.QuestionOpen}
	}
	// Resolved only by a Favorable answer that is not superseded (hidden ones are ignored), as in contribution_v.
	rows, err := r.pool.Query(ctx, `SELECT answers_id::text, count(*), min(rank), min(rank) FILTER (WHERE superseded_by IS NULL)
		FROM contribution_v WHERE answers_id = ANY($1::uuid[]) AND hidden_at IS NULL GROUP BY answers_id`, questionIDs)
	if err == nil {
		var id string
		var n, best int
		var live *int
		_, err = pgx.ForEachRow(rows, []any{&id, &n, &best, &live}, func() error {
			s := stateByRank[best]
			info := contribution.QuestionInfo{Status: contribution.QuestionAnswered, AnswerCount: n, BestAnswerState: &s}
			if live != nil && stateByRank[*live] == trust.StateFavorable {
				info.Status = contribution.QuestionResolved
			}
			out[id] = info
			return nil
		})
	}
	if isNoRow(err) {
		return nil, contribution.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("contribution: question info: %w", err)
	}
	return out, nil
}

// CountByProject counts what the default lists show: neither hidden nor
// superseded. LastActivity also counts superseded ones (still not hidden).
func (r *ContributionRepo) CountByProject(ctx context.Context) (map[string]contribution.ProjectCounts, error) {
	rows, err := r.pool.Query(ctx, `SELECT project_id, count(*) FILTER (WHERE superseded_by IS NULL),
		count(*) FILTER (WHERE superseded_by IS NULL AND question_status = 'open'),
		count(*) FILTER (WHERE superseded_by IS NULL AND kind = 'claim' AND state = 'Favorable'),
		count(*) FILTER (WHERE superseded_by IS NULL AND kind = 'claim' AND state = 'Unfavorable'),
		count(*) FILTER (WHERE superseded_by IS NULL AND kind = 'claim' AND state = 'Conflict'),
		count(*) FILTER (WHERE superseded_by IS NULL AND kind = 'claim' AND state = 'InsufficientEvidence'),
		max(updated_at)
		FROM contribution_v WHERE hidden_at IS NULL GROUP BY project_id`)
	out := map[string]contribution.ProjectCounts{}
	if err == nil {
		var p string
		var pc contribution.ProjectCounts
		var fav, unf, con, ins int
		_, err = pgx.ForEachRow(rows, []any{&p, &pc.Contributions, &pc.OpenQuestions, &fav, &unf, &con, &ins, &pc.LastActivity}, func() error {
			pc.States = map[trust.State]int{trust.StateFavorable: fav, trust.StateUnfavorable: unf, trust.StateConflict: con, trust.StateInsufficientEvidence: ins}
			out[p] = pc
			return nil
		})
	}
	if err != nil {
		return nil, fmt.Errorf("contribution: count by project: %w", err)
	}
	return out, nil
}

func (r *ContributionRepo) Authors(ctx context.Context, projectID string, includeHidden bool) ([]contribution.Author, error) {
	rows, err := r.pool.Query(ctx, `SELECT a.id::text, a.name FROM actor a
		WHERE EXISTS (SELECT 1 FROM contribution c WHERE c.actor_id = a.id AND c.project_id = $1 AND (c.hidden_at IS NULL OR $2))
		ORDER BY a.name, a.id`, projectID, includeHidden)
	if err != nil {
		return nil, fmt.Errorf("contribution: authors: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[contribution.Author])
	if err != nil {
		return nil, fmt.Errorf("contribution: authors: %w", err)
	}
	if out == nil {
		out = []contribution.Author{}
	}
	return out, nil
}

func (r *ContributionRepo) Recent(ctx context.Context, projectIDs []string, limit int) ([]contribution.Contribution, error) {
	out, err := r.query(ctx, "recent", `SELECT `+contributionColumns+` FROM contribution_v
		WHERE hidden_at IS NULL AND superseded_by IS NULL AND ($2::text[] IS NULL OR project_id = ANY($2)) ORDER BY created_at DESC, id DESC LIMIT $1`, limit, projectIDs)
	if errors.Is(err, contribution.ErrNotFound) || (err == nil && out == nil) {
		return []contribution.Contribution{}, nil
	}
	return out, err
}

func (r *ContributionRepo) Attention(ctx context.Context, projectIDs []string, now time.Time) (contribution.Attention, error) {
	var a contribution.Attention
	err := r.pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE kind = 'claim' AND state = 'Conflict' AND updated_at > $2::timestamptz - interval '7 days'),
		count(*) FILTER (WHERE question_status = 'open' AND created_at < $2::timestamptz - interval '14 days')
		FROM contribution_v WHERE hidden_at IS NULL AND superseded_by IS NULL AND ($1::text[] IS NULL OR project_id = ANY($1))`,
		projectIDs, now.UTC()).Scan(&a.Conflicts7d, &a.StaleQuestions)
	if err != nil {
		return contribution.Attention{}, fmt.Errorf("contribution: attention: %w", err)
	}
	return a, nil
}
