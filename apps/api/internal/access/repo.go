package access

import (
	"context"
	"time"

	"agent-verdicts/internal/platform/id"
)

// Repository stores policies, roles and assignments. Not found is
// ErrNotFound (also for an unknown actor, role, policy or project
// referenced by a write), a taken name / duplicate assignment / record still
// in use is ErrConflict, and changing a managed record is ErrForbidden.
type Repository interface {
	// Grants is what actorID may do; empty for an unknown or disabled actor.
	Grants(ctx context.Context, actorID string) (Grants, error)

	ListPolicies(ctx context.Context) ([]Policy, error)
	GetPolicy(ctx context.Context, id string) (Policy, error)
	CreatePolicy(ctx context.Context, p Policy) error
	UpdatePolicy(ctx context.Context, p Policy) error // Name and Actions
	DeletePolicy(ctx context.Context, id string) error

	ListRoles(ctx context.Context) ([]Role, error)
	GetRole(ctx context.Context, id string) (Role, error)
	CreateRole(ctx context.Context, r Role) error
	UpdateRole(ctx context.Context, r Role) error // Name and PolicyIDs
	DeleteRole(ctx context.Context, id string) error
	// RoleActions is the union of the role's policies' actions, in Actions order ([*] for admin).
	RoleActions(ctx context.Context, roleID string) ([]Action, error)

	ListAssignments(ctx context.Context, f AssignmentFilter) ([]Assignment, error)
	GetAssignment(ctx context.Context, id string) (Assignment, error)
	CreateAssignment(ctx context.Context, a Assignment) error
	DeleteAssignment(ctx context.Context, id string) error
	// AdminHumans is the active Humans holding the admin role on every project.
	AdminHumans(ctx context.Context) ([]string, error)
}

// NewAssignment is a fresh assignment of roleID to actorID on projectID (nil = every project).
func NewAssignment(actorID, roleID string, projectID *string, by string) Assignment {
	return Assignment{ID: id.New(), ActorID: actorID, RoleID: roleID, ProjectID: projectID, CreatedBy: by, CreatedAt: time.Now()}
}

// GrantAdmin gives actorID the admin role on every project (setup, CLI, seed).
func GrantAdmin(ctx context.Context, r Repository, actorID string) error {
	return r.CreateAssignment(ctx, NewAssignment(actorID, AdminRole, nil, actorID))
}
