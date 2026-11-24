// Package access is authorization: fixed actions, policies (sets of
// actions), roles (sets of policies) and assignments (actor + role + scope:
// every project or one). See "Access control" in docs/architecture.md.
package access

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Action is one permission a policy can Allow.
type Action string

const (
	ContributionRead   Action = "contribution:Read"
	ContributionCreate Action = "contribution:Create"
	SignalCreate       Action = "signal:Create"
	ContentModerate    Action = "content:Moderate"
	MemberManage       Action = "member:Manage"
	ProjectCreate      Action = "project:Create"
	ActorManage        Action = "actor:Manage"
	SettingsManage     Action = "settings:Manage"
	PolicyManage       Action = "policy:Manage"
	// All is every action; only the managed AdministratorAccess policy holds it.
	All Action = "*"
)

// Actions is every concrete action, in display order.
var Actions = []Action{ContributionRead, ContributionCreate, SignalCreate, ContentModerate, MemberManage,
	ProjectCreate, ActorManage, SettingsManage, PolicyManage}

// System reports whether a is instance-wide: it only counts from an
// assignment on every project ("*").
func (a Action) System() bool {
	switch a {
	case ProjectCreate, ActorManage, SettingsManage, PolicyManage:
		return true
	}
	return false
}

var (
	ErrInvalidArgument = errors.New("access: invalid argument")
	ErrNotFound        = errors.New("access: not found")
	ErrConflict        = errors.New("access: conflict")
	ErrForbidden       = errors.New("access: forbidden")
	ErrLastAdmin       = errors.New("access: the last admin cannot lose admin rights")
)

// AdminRole is the managed role holding AdministratorAccess ("*").
const AdminRole = "admin"

type Policy struct {
	ID, Name  string
	Managed   bool
	Actions   []Action
	CreatedAt time.Time
}

type Role struct {
	ID, Name  string
	Managed   bool
	PolicyIDs []string
	CreatedAt time.Time
}

// Assignment gives ActorID the role on ProjectID, or on every project when nil.
type Assignment struct {
	ID, ActorID, RoleID string
	ProjectID           *string
	CreatedBy           string
	CreatedAt           time.Time
}

type AssignmentFilter struct{ ActorID, ProjectID string }

type set map[Action]bool

// Grants is what one actor may do: actions on every project (All) and per
// project. The zero value grants nothing.
type Grants struct {
	All      set
	Projects map[string]set
}

// Add grants actions on projectID ("" = every project).
func (g *Grants) Add(projectID string, actions ...Action) {
	if projectID == "" {
		if g.All == nil {
			g.All = set{}
		}
		for _, a := range actions {
			g.All[a] = true
		}
		return
	}
	if g.Projects == nil {
		g.Projects = map[string]set{}
	}
	if g.Projects[projectID] == nil {
		g.Projects[projectID] = set{}
	}
	for _, a := range actions {
		g.Projects[projectID][a] = true
	}
}

func (g Grants) IsAdmin() bool { return g.All[All] }

// Can reports whether a is allowed on projectID ("" = instance-wide).
func (g Grants) Can(a Action, projectID string) bool {
	if g.IsAdmin() || g.All[a] {
		return true
	}
	if a.System() || projectID == "" {
		return false
	}
	return g.Projects[projectID][a]
}

// Readable is the projects whose contributions may be read: all of them, or ids (sorted).
func (g Grants) Readable() (all bool, ids []string) {
	if g.Can(ContributionRead, "") {
		return true, nil
	}
	for p, s := range g.Projects {
		if s[ContributionRead] {
			ids = append(ids, p)
		}
	}
	slices.Sort(ids)
	return false, ids
}

// ManagesAny reports whether member:Manage is held anywhere.
func (g Grants) ManagesAny() bool {
	if g.Can(MemberManage, "") {
		return true
	}
	for _, s := range g.Projects {
		if s[MemberManage] {
			return true
		}
	}
	return false
}

// CanAssign reports whether a role with roleActions may be given (or taken
// away) on projectID ("" = every project): the caller needs member:Manage
// there and every one of those actions there (spec §6).
func (g Grants) CanAssign(roleActions []Action, projectID string) bool {
	if slices.Contains(roleActions, All) {
		return g.IsAdmin()
	}
	if !g.Can(MemberManage, projectID) {
		return false
	}
	for _, a := range roleActions {
		if !g.Can(a, projectID) {
			return false
		}
	}
	return true
}

// CanAuthor reports whether a custom policy or role holding actions may be
// created or changed: policy:Manage plus every action, instance-wide.
func (g Grants) CanAuthor(actions []Action) bool {
	if !g.Can(PolicyManage, "") {
		return false
	}
	for _, a := range actions {
		if !g.Can(a, "") {
			return false
		}
	}
	return true
}

// Covers reports whether g holds everything o holds, scope by scope. Acting
// on another actor's account (password, tokens, disabling) requires it, so
// nobody takes over an account with more rights than their own.
func (g Grants) Covers(o Grants) bool {
	for a := range o.All {
		if !g.Can(a, "") {
			return false
		}
	}
	for p, s := range o.Projects {
		for a := range s {
			if !g.Can(a, p) {
				return false
			}
		}
	}
	return true
}

// List is g for display: actions on every project and per project, in
// Actions order (an admin is just [*]). Never nil.
func (g Grants) List() (all []Action, projects map[string][]Action) {
	sorted := func(s set) []Action {
		out := []Action{}
		if s[All] {
			return []Action{All}
		}
		for _, a := range Actions {
			if s[a] {
				out = append(out, a)
			}
		}
		return out
	}
	all, projects = sorted(g.All), map[string][]Action{}
	for p, s := range g.Projects {
		if l := sorted(s); len(l) > 0 && l[0] != All {
			projects[p] = l
		}
	}
	return all, projects
}

// ValidateCustom checks a custom policy's actions: at least one, all known,
// never "*". Returns them deduplicated in Actions order.
func ValidateCustom(actions []Action) ([]Action, error) {
	if len(actions) == 0 {
		return nil, fmt.Errorf("%w: a policy needs at least one action", ErrInvalidArgument)
	}
	var s set = map[Action]bool{}
	for _, a := range actions {
		if !slices.Contains(Actions, a) {
			return nil, fmt.Errorf("%w: unknown action %q", ErrInvalidArgument, a)
		}
		s[a] = true
	}
	out := []Action{}
	for _, a := range Actions {
		if s[a] {
			out = append(out, a)
		}
	}
	return out, nil
}

const maxNameRunes = 64

// NormalizeName trims a policy or role name: 1..64 characters, no control characters.
func NormalizeName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > maxNameRunes || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("%w: name must be 1-%d characters without control characters", ErrInvalidArgument, maxNameRunes)
	}
	return s, nil
}

// Managed is the projects whose members the holder may manage: all of them
// (member:Manage on every project), or ids (sorted).
func (g Grants) Managed() (all bool, ids []string) {
	if g.Can(MemberManage, "") {
		return true, nil
	}
	for p, s := range g.Projects {
		if s[MemberManage] {
			ids = append(ids, p)
		}
	}
	slices.Sort(ids)
	return false, ids
}
