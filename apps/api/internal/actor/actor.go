// Package actor defines Actor, the single persistent identity of a
// participant. Actors are instance-wide (not per project) and shared by
// contributions and signals. Actor knows nothing about authentication or
// authorization.
package actor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"agent-verdicts/internal/platform/id"
)

// Type is the kind of an Actor.
type Type string

const (
	TypeHuman Type = "Human"
	TypeAgent Type = "Agent"
)

// MaxNameLen is the maximum length of Actor.Name, in runes.
const MaxNameLen = 64

var (
	// ErrInvalidArgument: a request about actors breaks a rule that has no
	// sentinel of its own (the message says which).
	ErrInvalidArgument = errors.New("actor: invalid argument")
	// ErrNameRequired: the name is empty after trimming.
	ErrNameRequired = errors.New("actor: name is required")
	// ErrNameTooLong: the name is longer than MaxNameLen runes.
	ErrNameTooLong = errors.New("actor: name too long")
	// ErrNameTaken: another actor already has this name (compared ignoring case).
	ErrNameTaken = errors.New("actor: name already in use")
)

// NormalizeName trims surrounding whitespace and checks emptiness and
// length. Every boundary that takes a name from a user (CLI, API) calls it
// before setting Actor.Name; the repository does not validate again,
// because actors created before the name column existed have an empty name.
func NormalizeName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ErrNameRequired
	}
	if utf8.RuneCountInString(s) > MaxNameLen {
		return "", ErrNameTooLong
	}
	return s, nil
}

// Actor is the identity of one participant (Human or Agent).
type Actor struct {
	ID   string
	Type Type

	// Name is the display name, unique ignoring case. It is empty for actors
	// created before the column existed; the UI and MCP fall back to the ID.
	// New() does not take it: callers set it after NormalizeName.
	Name string

	CreatedAt time.Time

	// DisabledAt is set when an admin disables the actor: every way in
	// (MCP token, password, open sessions) is
	// refused while it is non-nil. Nil = active.
	DisabledAt *time.Time
}

// Disabled reports whether the actor is currently disabled.
func (a Actor) Disabled() bool { return a.DisabledAt != nil }

// New returns a new Actor with a generated ID and CreatedAt set to now. It
// panics if t is not TypeHuman or TypeAgent: valid callers always pass one
// of the exported constants, so anything else is a programming error, not
// runtime input to handle gracefully.
func New(t Type) Actor {
	if !isValidType(t) {
		panic(fmt.Sprintf("actor: invalid type %q", t))
	}
	return Actor{
		ID:        id.New(),
		Type:      t,
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond), // what timestamptz stores
	}
}

// ErrNotFound: no Actor has that ID.
var ErrNotFound = errors.New("actor: not found")

// Repository stores and queries Actors; the domain does not depend on its
// implementation.
type Repository interface {
	// Create, Update and UpdateProfile return ErrNameTaken when the name is
	// already used by another actor (case-insensitively).
	Create(ctx context.Context, a Actor) error
	Get(ctx context.Context, id string) (Actor, error)
	// List returns every Actor ordered by (created_at, id). Not paginated:
	// the target is a small team.
	List(ctx context.Context) ([]Actor, error)
	// Update overwrites the mutable fields (Name, DisabledAt) of an
	// existing actor; ErrNotFound if a.ID does not exist. ID, Type and
	// CreatedAt never change.
	Update(ctx context.Context, a Actor) error
	// UpdateProfile and SetDisabled write only their own columns, so two
	// admins editing different aspects of one actor never undo each other.
	// SetDisabled refuses with access.ErrLastAdmin to disable the last
	// active Human admin (the postgres implementation checks atomically).
	UpdateProfile(ctx context.Context, id, name string) error
	SetDisabled(ctx context.Context, id string, at *time.Time) error
}

func isValidType(t Type) bool {
	switch t {
	case TypeHuman, TypeAgent:
		return true
	default:
		return false
	}
}
