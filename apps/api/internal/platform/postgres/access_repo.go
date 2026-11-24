package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"agent-verdicts/internal/access"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AccessRepo struct{ pool *pgxpool.Pool }

func NewAccessRepo(pool *pgxpool.Pool) *AccessRepo { return &AccessRepo{pool: pool} }

var _ access.Repository = (*AccessRepo)(nil)

// accessErr maps a write's database error: duplicate → ErrConflict, a
// reference to a missing row → ErrNotFound (insert/update) or a row still
// referenced → ErrConflict (delete).
func accessErr(op string, err error, deleting bool) error {
	switch c := pgCode(err); {
	case err == nil:
		return nil
	case c == "23505":
		return fmt.Errorf("%w: %s: already exists", access.ErrConflict, op)
	case c == "23503" && deleting:
		return fmt.Errorf("%w: %s: still in use", access.ErrConflict, op)
	case c == "23503" || isNoRow(err):
		return fmt.Errorf("%w: %s: unknown actor, role, policy or project", access.ErrNotFound, op)
	}
	return fmt.Errorf("access: %s: %w", op, err)
}

func (r *AccessRepo) Grants(ctx context.Context, actorID string) (access.Grants, error) {
	var g access.Grants
	rows, err := r.pool.Query(ctx, `SELECT ra.project_id, p.actions FROM role_assignment ra
		JOIN actor a ON a.id = ra.actor_id AND a.disabled_at IS NULL
		JOIN role_policy rp ON rp.role_id = ra.role_id
		JOIN policy p ON p.id = rp.policy_id
		WHERE ra.actor_id = $1`, actorID)
	if err != nil {
		if isNoRow(err) {
			return g, nil
		}
		return g, fmt.Errorf("access: grants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var project *string
		var actions []string
		if err := rows.Scan(&project, &actions); err != nil {
			return access.Grants{}, fmt.Errorf("access: grants: %w", err)
		}
		p := ""
		if project != nil {
			p = *project
		}
		for _, a := range actions {
			g.Add(p, access.Action(a))
		}
	}
	if err := rows.Err(); err != nil {
		if isNoRow(err) {
			return access.Grants{}, nil
		}
		return access.Grants{}, fmt.Errorf("access: grants: %w", err)
	}
	return g, nil
}

func toActions(s []string) []access.Action {
	out := make([]access.Action, len(s))
	for i, a := range s {
		out[i] = access.Action(a)
	}
	return out
}

func fromActions(a []access.Action) []string {
	out := make([]string, len(a))
	for i, x := range a {
		out[i] = string(x)
	}
	return out
}

func scanPolicy(row pgx.Row) (access.Policy, error) {
	var p access.Policy
	var actions []string
	err := row.Scan(&p.ID, &p.Name, &p.Managed, &actions, &p.CreatedAt)
	p.Actions, p.CreatedAt = toActions(actions), p.CreatedAt.UTC()
	return p, err
}

const policyColumns = `id, name, managed, actions, created_at`

func (r *AccessRepo) ListPolicies(ctx context.Context) ([]access.Policy, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+policyColumns+` FROM policy ORDER BY managed DESC, name`)
	if err != nil {
		return nil, fmt.Errorf("access: list policies: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (access.Policy, error) { return scanPolicy(row) })
	if err != nil {
		return nil, fmt.Errorf("access: list policies: %w", err)
	}
	return nonNil(out), nil
}

func (r *AccessRepo) GetPolicy(ctx context.Context, id string) (access.Policy, error) {
	p, err := scanPolicy(r.pool.QueryRow(ctx, `SELECT `+policyColumns+` FROM policy WHERE id = $1`, id))
	if isNoRow(err) {
		return access.Policy{}, fmt.Errorf("%w: policy %q", access.ErrNotFound, id)
	}
	if err != nil {
		return access.Policy{}, fmt.Errorf("access: get policy: %w", err)
	}
	return p, nil
}

func (r *AccessRepo) CreatePolicy(ctx context.Context, p access.Policy) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO policy (id, name, managed, actions, created_at) VALUES ($1, $2, false, $3, $4)`,
		p.ID, p.Name, fromActions(p.Actions), p.CreatedAt.UTC())
	return accessErr("create policy", err, false)
}

// managedOr reports why a write to a custom-only row matched nothing:
// ErrForbidden if the row is managed, ErrNotFound if it does not exist.
func (r *AccessRepo) managedOr(ctx context.Context, table, id string) error {
	var managed bool
	err := r.pool.QueryRow(ctx, `SELECT managed FROM `+table+` WHERE id = $1`, id).Scan(&managed)
	switch {
	case isNoRow(err):
		return fmt.Errorf("%w: %s %q", access.ErrNotFound, table, id)
	case err != nil:
		return fmt.Errorf("access: %s: %w", table, err)
	case managed:
		return fmt.Errorf("%w: managed %s %q is read-only", access.ErrForbidden, table, id)
	}
	return fmt.Errorf("access: %s %q: write matched no row", table, id)
}

func (r *AccessRepo) UpdatePolicy(ctx context.Context, p access.Policy) error {
	tag, err := r.pool.Exec(ctx, `UPDATE policy SET name = $2, actions = $3 WHERE id = $1 AND NOT managed`,
		p.ID, p.Name, fromActions(p.Actions))
	if err != nil {
		return accessErr("update policy", err, false)
	}
	if tag.RowsAffected() == 0 {
		return r.managedOr(ctx, "policy", p.ID)
	}
	return nil
}

func (r *AccessRepo) DeletePolicy(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM policy WHERE id = $1 AND NOT managed`, id)
	if err != nil {
		return accessErr("delete policy", err, true)
	}
	if tag.RowsAffected() == 0 {
		return r.managedOr(ctx, "policy", id)
	}
	return nil
}

const roleSelect = `SELECT r.id, r.name, r.managed, r.created_at,
	coalesce(array_agg(rp.policy_id ORDER BY rp.policy_id COLLATE "C") FILTER (WHERE rp.policy_id IS NOT NULL), '{}')
	FROM role r LEFT JOIN role_policy rp ON rp.role_id = r.id`

func scanRole(row pgx.Row) (access.Role, error) {
	var x access.Role
	err := row.Scan(&x.ID, &x.Name, &x.Managed, &x.CreatedAt, &x.PolicyIDs)
	x.CreatedAt = x.CreatedAt.UTC()
	return x, err
}

func (r *AccessRepo) ListRoles(ctx context.Context) ([]access.Role, error) {
	rows, err := r.pool.Query(ctx, roleSelect+` GROUP BY r.id ORDER BY r.managed DESC, r.name`)
	if err != nil {
		return nil, fmt.Errorf("access: list roles: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (access.Role, error) { return scanRole(row) })
	if err != nil {
		return nil, fmt.Errorf("access: list roles: %w", err)
	}
	return nonNil(out), nil
}

func (r *AccessRepo) GetRole(ctx context.Context, id string) (access.Role, error) {
	x, err := scanRole(r.pool.QueryRow(ctx, roleSelect+` WHERE r.id = $1 GROUP BY r.id`, id))
	if isNoRow(err) {
		return access.Role{}, fmt.Errorf("%w: role %q", access.ErrNotFound, id)
	}
	if err != nil {
		return access.Role{}, fmt.Errorf("access: get role: %w", err)
	}
	return x, nil
}

func (r *AccessRepo) CreateRole(ctx context.Context, x access.Role) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO role (id, name, managed, created_at) VALUES ($1, $2, false, $3)`,
			x.ID, x.Name, x.CreatedAt.UTC()); err != nil {
			return accessErr("create role", err, false)
		}
		return setRolePolicies(ctx, tx, x)
	})
}

func setRolePolicies(ctx context.Context, tx pgx.Tx, x access.Role) error {
	if _, err := tx.Exec(ctx, `DELETE FROM role_policy WHERE role_id = $1`, x.ID); err != nil {
		return fmt.Errorf("access: role policies: %w", err)
	}
	_, err := tx.Exec(ctx, `INSERT INTO role_policy (role_id, policy_id) SELECT $1, unnest($2::text[]) ON CONFLICT DO NOTHING`,
		x.ID, x.PolicyIDs)
	return accessErr("role policies", err, false)
}

func (r *AccessRepo) UpdateRole(ctx context.Context, x access.Role) error {
	var gone bool
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE role SET name = $2 WHERE id = $1 AND NOT managed`, x.ID, x.Name)
		if err != nil {
			return accessErr("update role", err, false)
		}
		if tag.RowsAffected() == 0 {
			gone = true
			return nil
		}
		return setRolePolicies(ctx, tx, x)
	})
	if err == nil && gone {
		return r.managedOr(ctx, "role", x.ID)
	}
	return err
}

func (r *AccessRepo) DeleteRole(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM role WHERE id = $1 AND NOT managed`, id)
	if err != nil {
		return accessErr("delete role", err, true)
	}
	if tag.RowsAffected() == 0 {
		return r.managedOr(ctx, "role", id)
	}
	return nil
}

func (r *AccessRepo) RoleActions(ctx context.Context, roleID string) ([]access.Action, error) {
	if _, err := r.GetRole(ctx, roleID); err != nil {
		return nil, err
	}
	var actions []string
	if err := r.pool.QueryRow(ctx, `SELECT coalesce(array_agg(DISTINCT a), '{}') FROM role_policy rp
		JOIN policy p ON p.id = rp.policy_id, unnest(p.actions) a WHERE rp.role_id = $1`, roleID).Scan(&actions); err != nil {
		return nil, fmt.Errorf("access: role actions: %w", err)
	}
	if slices.Contains(actions, string(access.All)) {
		return []access.Action{access.All}, nil
	}
	out := []access.Action{}
	for _, a := range access.Actions {
		if slices.Contains(actions, string(a)) {
			out = append(out, a)
		}
	}
	return out, nil
}

const assignmentColumns = `id, actor_id, role_id, project_id, coalesce(created_by::text, ''), created_at`

func scanAssignment(row pgx.Row) (access.Assignment, error) {
	var a access.Assignment
	err := row.Scan(&a.ID, &a.ActorID, &a.RoleID, &a.ProjectID, &a.CreatedBy, &a.CreatedAt)
	a.CreatedAt = a.CreatedAt.UTC()
	return a, err
}

func (r *AccessRepo) ListAssignments(ctx context.Context, f access.AssignmentFilter) ([]access.Assignment, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+assignmentColumns+` FROM role_assignment
		WHERE ($1 = '' OR actor_id::text = $1) AND ($2 = '' OR project_id = $2)
		ORDER BY created_at, id`, f.ActorID, f.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("access: list assignments: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (access.Assignment, error) { return scanAssignment(row) })
	if err != nil {
		return nil, fmt.Errorf("access: list assignments: %w", err)
	}
	return nonNil(out), nil
}

func (r *AccessRepo) GetAssignment(ctx context.Context, id string) (access.Assignment, error) {
	a, err := scanAssignment(r.pool.QueryRow(ctx, `SELECT `+assignmentColumns+` FROM role_assignment WHERE id = $1`, id))
	if isNoRow(err) {
		return access.Assignment{}, fmt.Errorf("%w: assignment %q", access.ErrNotFound, id)
	}
	if err != nil {
		return access.Assignment{}, fmt.Errorf("access: get assignment: %w", err)
	}
	return a, nil
}

func (r *AccessRepo) CreateAssignment(ctx context.Context, a access.Assignment) error {
	var by *string
	if a.CreatedBy != "" {
		by = &a.CreatedBy
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO role_assignment (id, actor_id, role_id, project_id, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, a.ID, a.ActorID, a.RoleID, a.ProjectID, by, a.CreatedAt.UTC())
	return accessErr("create assignment", err, false)
}

// DeleteAssignment removes the assignment; removing the admin role of the
// last active Human admin is access.ErrLastAdmin. The check and the delete
// share one transaction (see guardLastAdmin), so two admins removing each
// other cannot both succeed.
func (r *AccessRepo) DeleteAssignment(ctx context.Context, id string) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var actorID, roleID string
		var projectID *string
		err := tx.QueryRow(ctx, `SELECT actor_id::text, role_id, project_id FROM role_assignment WHERE id = $1`, id).Scan(&actorID, &roleID, &projectID)
		if err != nil {
			return err
		}
		if roleID == access.AdminRole && projectID == nil {
			if err := guardLastAdmin(ctx, tx, actorID); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `DELETE FROM role_assignment WHERE id = $1`, id)
		if err == nil && tag.RowsAffected() == 0 {
			err = pgx.ErrNoRows
		}
		return err
	})
	if isNoRow(err) {
		return fmt.Errorf("%w: assignment %q", access.ErrNotFound, id)
	}
	if errors.Is(err, access.ErrLastAdmin) {
		return err
	}
	return accessErr("delete assignment", err, true)
}

// guardLastAdmin returns access.ErrLastAdmin if actorID is the only active
// Human admin. It first locks the actor row of every admin (in id order, so
// two callers never deadlock), then counts with a fresh statement: a caller
// that waited on the lock sees what the one before it committed. Every
// operation that can remove an admin calls it inside its transaction.
func guardLastAdmin(ctx context.Context, tx pgx.Tx, actorID string) error {
	const admins = `SELECT a.id::text FROM actor a WHERE a.type = 'Human' AND a.disabled_at IS NULL
		AND EXISTS (SELECT 1 FROM role_assignment ra WHERE ra.actor_id = a.id AND ra.role_id = $1 AND ra.project_id IS NULL)
		ORDER BY a.id`
	rows, err := tx.Query(ctx, admins+` FOR UPDATE OF a`, access.AdminRole)
	if err != nil {
		return err
	}
	if _, err := pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return err
	}
	rows, err = tx.Query(ctx, admins, access.AdminRole)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	if len(ids) == 1 && strings.EqualFold(ids[0], actorID) {
		return access.ErrLastAdmin
	}
	return nil
}

func (r *AccessRepo) AdminHumans(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT a.id::text FROM role_assignment ra
		JOIN actor a ON a.id = ra.actor_id AND a.type = 'Human' AND a.disabled_at IS NULL
		WHERE ra.role_id = $1 AND ra.project_id IS NULL ORDER BY 1`, access.AdminRole)
	if err != nil {
		return nil, fmt.Errorf("access: admin humans: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("access: admin humans: %w", err)
	}
	return nonNil(out), nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
