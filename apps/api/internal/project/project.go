// Package project defines Project: a slug id, a display name and
// description, a trust threshold, and an archived flag. Projects scope
// content (contributions, and through them signals); actors are
// instance-wide and independent of projects.
package project

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"agent-verdicts/internal/platform/id"
)

// Project scopes contributions.
type Project struct {
	ID           string
	Name         string
	Description  string
	MinFavorable int // distinct supporters a claim needs to be Favorable, 1-10
	CreatedAt    time.Time
	ArchivedAt   *time.Time // archived = read-only, hidden from the default list
}

// Archived reports whether the project is read-only.
func (p Project) Archived() bool { return p.ArchivedAt != nil }

// New returns a new Project with a generated ID and CreatedAt set to now.
func New() Project {
	pid := id.New()
	return Project{
		ID:           pid,
		Name:         pid,
		MinFavorable: 1,
		CreatedAt:    time.Now().UTC().Truncate(time.Microsecond), // what timestamptz stores
	}
}

// ErrArchived: a write into an archived project.
var ErrArchived = errors.New("project is archived")

// ErrNotFound: no Project has that ID.
var ErrNotFound = errors.New("project: not found")

// Repository stores and queries Projects.
type Repository interface {
	Create(ctx context.Context, p Project) error
	Get(ctx context.Context, id string) (Project, error)
	// Update sets name, description and threshold; ErrNotFound if the project is gone.
	Update(ctx context.Context, id, name, description string, minFavorable int) error
	// List returns every Project, newest first (created_at descending, then
	// id ascending). Not paginated: the target is a small team.
	List(ctx context.Context) ([]Project, error)
	// SetArchived archives (at != nil) or restores a project; ErrNotFound if it is gone.
	SetArchived(ctx context.Context, id string, at *time.Time) error
}

// idPattern is the project slug rule.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Limits for name and description, in runes.
const (
	MaxNameLen        = 100
	MaxDescriptionLen = 500
)

// Bounds of Project.MinFavorable.
const (
	MinFavorableMin = 1
	MinFavorableMax = 10
)

// ErrInvalidID: the message says "invalid argument" so MCP callers read it
// as one (project cannot import contribution's sentinel).
var ErrInvalidID = errors.New("project: invalid argument: id")

// ErrInvalidField is returned for a bad name or description.
var ErrInvalidField = errors.New("project: invalid argument")

// ValidateID checks a project id taken from a request: a slug,
// ^[a-z0-9][a-z0-9_-]{0,63}$. Every adapter calls it before touching the
// database with the id.
func ValidateID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("%w must match ^[a-z0-9][a-z0-9_-]{0,63}$", ErrInvalidID)
	}
	return nil
}

// NormalizeName trims and validates a display name: 1-100 runes, no control characters.
func NormalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case !utf8.ValidString(name) || name == "" || utf8.RuneCountInString(name) > MaxNameLen:
		return "", fmt.Errorf("%w: name must be 1-%d valid characters", ErrInvalidField, MaxNameLen)
	case strings.ContainsFunc(name, unicode.IsControl):
		return "", fmt.Errorf("%w: name must not contain control characters", ErrInvalidField)
	}
	return name, nil
}

// NormalizeDescription trims and validates a description: 0-500 runes, no
// control characters except newline.
func NormalizeDescription(d string) (string, error) {
	d = strings.TrimSpace(d)
	switch {
	case !utf8.ValidString(d) || utf8.RuneCountInString(d) > MaxDescriptionLen:
		return "", fmt.Errorf("%w: description must be at most %d valid characters", ErrInvalidField, MaxDescriptionLen)
	case strings.ContainsFunc(d, func(r rune) bool { return unicode.IsControl(r) && r != '\n' }):
		return "", fmt.Errorf("%w: description must not contain control characters", ErrInvalidField)
	}
	return d, nil
}
