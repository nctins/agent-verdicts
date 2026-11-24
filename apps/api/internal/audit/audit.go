// Package audit is the append-only log of administrative operations (spec
// 2026-11-15-audit-tokens-sessions §1). Events are written after the
// operation succeeded; a failed write is logged and never fails the
// operation. Detail holds only non-secret facts: never a password, token,
// secret or hash.
package audit

import (
	"context"
	"errors"
	"log"
	"time"

	"agent-verdicts/internal/platform/id"
)

// ErrBadCursor: the cursor is malformed.
var ErrBadCursor = errors.New("audit: invalid cursor")

// Event is one logged operation. ActorID "" is the operator CLI; ProjectID
// "" is an event not tied to a project.
type Event struct {
	ID         string
	At         time.Time
	ActorID    string
	Action     string
	TargetType string
	TargetID   string
	ProjectID  string
	Detail     map[string]any
}

const (
	DefaultLimit = 20
	MaxLimit     = 100
)

// Filter narrows List. Empty fields match everything.
type Filter struct {
	ActorID, ProjectID, Action string
	// From and To bound the event time by UTC day, both included; zero = open.
	From, To time.Time
	// Restrict limits the result to assignment.* and content.* events: of
	// any project when AllProjects (including those tied to none), else only
	// of Projects. It is what a member manager may see.
	Restrict    bool
	AllProjects bool
	Projects    []string
	Cursor      string
	Limit       int // 0 → DefaultLimit; > MaxLimit → MaxLimit
}

// Page is events newest first; NextCursor is "" on the last page.
type Page struct {
	Items      []Event
	NextCursor string
}

// Repository stores events. A malformed cursor is ErrBadCursor.
type Repository interface {
	Append(ctx context.Context, e Event) error
	List(ctx context.Context, f Filter) (Page, error)
}

// Record appends e and, if that fails, logs loudly and carries on (D2).
func Record(ctx context.Context, r Repository, e Event) {
	if e.ID == "" {
		e.ID = id.New()
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	if e.Detail == nil {
		e.Detail = map[string]any{}
	}
	if err := r.Append(context.WithoutCancel(ctx), e); err != nil {
		log.Printf("agentverdicts: AUDIT WRITE FAILED action=%s target=%s/%s: %v", e.Action, e.TargetType, e.TargetID, err)
	}
}
