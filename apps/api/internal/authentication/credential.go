// Package authentication implements per-Actor MCP credential
// authentication (Decision #9) — a construct architecturally separate
// from internal/actor. internal/actor does not import this package and
// has no knowledge that authentication exists; this package depends
// one-way on actor.Repository, the same coupling shape
// contribution/challenge/assessment already use to depend on
// actor.Repository (not a new architectural relationship).
package authentication

import (
	"context"
	"errors"
	"time"
)

// Credential is a per-Actor opaque authentication credential. SecretHash
// is a one-way hash — the raw secret is never persisted anywhere
// (Decision #9 §5).
type Credential struct {
	ID         string
	ActorID    string
	SecretHash string
	CreatedAt  time.Time
	RevokedAt  *time.Time
	// Label is a free note for humans (0-64 runes); ExpiresAt nil = never
	// expires; LastUsedAt is written at most once a minute (see Touch).
	Label      string
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
}

// ErrNotFound is returned when a credential_id does not name an existing
// credential row.
var ErrNotFound = errors.New("authentication: credential not found")

// Repository is the persistence port for Credential — mirrors
// actor.Repository's shape (Create/Get) plus Revoke.
type Repository interface {
	Create(ctx context.Context, c Credential) error
	Get(ctx context.Context, id string) (Credential, error)
	Revoke(ctx context.Context, id string, revokedAt time.Time) error
	// ListByActor returns every Credential of actorID, revoked ones
	// included, ordered by (created_at, id); an empty slice (no error) if
	// it has none. The admin console uses it to show and manage all of an
	// actor's tokens.
	ListByActor(ctx context.Context, actorID string) ([]Credential, error)
	// Touch sets last_used_at = now unless it is already within the last
	// minute: a single conditional UPDATE, so a busy token writes at most
	// once a minute.
	Touch(ctx context.Context, id string, now time.Time) error
	// Activity reports, per actor that has any, when it was last active (its
	// latest password login or token use) and when its next live token
	// expires (spec 2026-11-22-admin-ui-redesign A2).
	Activity(ctx context.Context, now time.Time) (map[string]Activity, error)
}

// Activity is one actor's latest sign of life and next token expiry; nil = none.
type Activity struct {
	LastActive *time.Time
	NextExpiry *time.Time
}
