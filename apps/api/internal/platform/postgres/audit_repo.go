package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"agent-verdicts/internal/audit"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditRepo implements audit.Repository on the audit_event table.
type AuditRepo struct{ pool *pgxpool.Pool }

func NewAuditRepo(pool *pgxpool.Pool) *AuditRepo { return &AuditRepo{pool: pool} }

var _ audit.Repository = (*AuditRepo)(nil)

func (r *AuditRepo) Append(ctx context.Context, e audit.Event) error {
	detail, err := json.Marshal(e.Detail)
	if err != nil {
		return fmt.Errorf("audit: append: %w", err)
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO audit_event (id, at, actor_id, action, target_type, target_id, project_id, detail)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6, NULLIF($7, ''), $8)`,
		e.ID, e.At.UTC(), e.ActorID, e.Action, e.TargetType, e.TargetID, e.ProjectID, detail)
	if err != nil {
		return fmt.Errorf("audit: append: %w", err)
	}
	return nil
}

// auditCursor is the opaque position: the last item's time and id.
type auditCursor struct {
	At time.Time `json:"t"`
	ID string    `json:"id"`
}

func (r *AuditRepo) List(ctx context.Context, f audit.Filter) (audit.Page, error) {
	switch {
	case f.Limit <= 0:
		f.Limit = audit.DefaultLimit
	case f.Limit > audit.MaxLimit:
		f.Limit = audit.MaxLimit
	}
	var args []any
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	where := []string{"true"}
	if f.ActorID != "" {
		where = append(where, "actor_id::text = "+arg(f.ActorID))
	}
	if f.ProjectID != "" {
		where = append(where, "project_id = "+arg(f.ProjectID))
	}
	if f.Action != "" {
		where = append(where, "action = "+arg(f.Action))
	}
	if !f.From.IsZero() {
		where = append(where, "at >= "+arg(f.From.UTC()))
	}
	if !f.To.IsZero() {
		where = append(where, "at < "+arg(f.To.UTC().AddDate(0, 0, 1)))
	}
	if f.Restrict {
		where = append(where, "(action LIKE 'assignment.%' OR action LIKE 'content.%')")
		if !f.AllProjects {
			where = append(where, "project_id = ANY("+arg(append([]string{}, f.Projects...))+")")
		}
	}
	if f.Cursor != "" {
		var c auditCursor
		b, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		if err == nil {
			err = json.Unmarshal(b, &c)
		}
		if err != nil || c.ID == "" {
			return audit.Page{}, audit.ErrBadCursor
		}
		where = append(where, "(at, id) < ("+arg(c.At)+", "+arg(c.ID)+"::uuid)")
	}
	rows, err := r.pool.Query(ctx, `SELECT id::text, at, coalesce(actor_id::text, ''), action, target_type, target_id,
		coalesce(project_id, ''), detail FROM audit_event WHERE `+strings.Join(where, " AND ")+
		` ORDER BY at DESC, id DESC LIMIT `+arg(f.Limit+1), args...)
	if err == nil {
		var items []audit.Event
		items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (audit.Event, error) {
			var e audit.Event
			var detail []byte
			err := row.Scan(&e.ID, &e.At, &e.ActorID, &e.Action, &e.TargetType, &e.TargetID, &e.ProjectID, &detail)
			e.At = e.At.UTC()
			if err == nil {
				err = json.Unmarshal(detail, &e.Detail)
			}
			return e, err
		})
		if err == nil {
			page := audit.Page{Items: items}
			if len(items) > f.Limit {
				page.Items = items[:f.Limit]
				last := page.Items[f.Limit-1]
				b, _ := json.Marshal(auditCursor{At: last.At, ID: last.ID})
				page.NextCursor = base64.RawURLEncoding.EncodeToString(b)
			}
			if page.Items == nil {
				page.Items = []audit.Event{}
			}
			return page, nil
		}
	}
	if isNoRow(err) { // 22P02: the cursor id is not a uuid
		return audit.Page{}, audit.ErrBadCursor
	}
	return audit.Page{}, fmt.Errorf("audit: list: %w", err)
}
