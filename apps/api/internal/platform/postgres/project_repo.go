package postgres

import (
	"context"
	"fmt"
	"time"

	"agent-verdicts/internal/project"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProjectRepo implements project.Repository on the project table.
type ProjectRepo struct{ pool *pgxpool.Pool }

func NewProjectRepo(pool *pgxpool.Pool) *ProjectRepo { return &ProjectRepo{pool: pool} }

var _ project.Repository = (*ProjectRepo)(nil)

func (r *ProjectRepo) Create(ctx context.Context, p project.Project) error {
	if _, err := r.pool.Exec(ctx, `INSERT INTO project (id, name, description, min_favorable, created_at) VALUES ($1, $2, $3, $4, $5)`, p.ID, p.Name, p.Description, max(p.MinFavorable, 1), p.CreatedAt.UTC()); err != nil {
		return fmt.Errorf("project: create: %w", err)
	}
	return nil
}

func scanProject(row pgx.Row) (project.Project, error) {
	var p project.Project
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.MinFavorable, &p.CreatedAt, &p.ArchivedAt)
	p.CreatedAt, p.ArchivedAt = p.CreatedAt.UTC(), utcPtr(p.ArchivedAt)
	return p, err
}

func (r *ProjectRepo) Get(ctx context.Context, id string) (project.Project, error) {
	p, err := scanProject(r.pool.QueryRow(ctx, `SELECT id, name, description, min_favorable, created_at, archived_at FROM project WHERE id = $1`, id))
	if isNoRow(err) {
		return project.Project{}, project.ErrNotFound
	}
	if err != nil {
		return project.Project{}, fmt.Errorf("project: get: %w", err)
	}
	return p, nil
}

func (r *ProjectRepo) Update(ctx context.Context, id, name, description string, minFavorable int) error {
	tag, err := r.pool.Exec(ctx, `UPDATE project SET name = $2, description = $3, min_favorable = $4 WHERE id = $1`, id, name, description, minFavorable)
	if err != nil {
		return fmt.Errorf("project: update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return project.ErrNotFound
	}
	return nil
}

func (r *ProjectRepo) SetArchived(ctx context.Context, id string, at *time.Time) error {
	tag, err := r.pool.Exec(ctx, `UPDATE project SET archived_at = $2 WHERE id = $1`, id, utcPtr(at))
	if err != nil {
		return fmt.Errorf("project: archive: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return project.ErrNotFound
	}
	return nil
}

func (r *ProjectRepo) List(ctx context.Context) ([]project.Project, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, description, min_favorable, created_at, archived_at FROM project ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("project: list: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (project.Project, error) { return scanProject(row) })
	if err != nil {
		return nil, fmt.Errorf("project: list: %w", err)
	}
	if out == nil {
		out = []project.Project{}
	}
	return out, nil
}
