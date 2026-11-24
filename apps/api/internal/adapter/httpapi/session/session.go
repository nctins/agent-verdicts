// Package session holds login sessions for the browser surfaces (the JSON
// API). A session is an HTTP transport concern, never shared with MCP or
// the CLI.
//
// Sessions live in Postgres (spec 2026-11-15-audit-tokens-sessions §3) so
// they survive a restart. Only the SHA-256 of the session id is stored; the
// raw id exists in the cookie alone.
package session

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"time"
)

// Sliding expiry (D8): a session dies after IdleTimeout without a request,
// or MaxLifetime after it began, whichever comes first.
const (
	IdleTimeout = 7 * 24 * time.Hour
	MaxLifetime = 30 * 24 * time.Hour
)

// sessionIDByteLength / csrfSecretByteLength: 256-bit / 32-byte
// crypto/rand entropy, same sizing convention as
// internal/authentication/issue.go's secretByteLength.
const (
	sessionIDByteLength  = 32
	csrfSecretByteLength = 32
)

// Session is the server-side state bound to one session ID.
type Session struct {
	ActorID string
	// PasswordChangedAt is the login's changed_at when the session started;
	// a later password change makes the session stale.
	PasswordChangedAt time.Time
}

// Repository is the storage Store needs (postgres.SessionRepo). Sessions
// are keyed by idHash; the caller passes the clock so a repository never
// reads one.
type Repository interface {
	// Insert deletes every session past idleCutoff (last_seen_at) or
	// absCutoff (created_at), then adds the new one.
	Insert(ctx context.Context, idHash, actorID string, passwordChangedAt, now, idleCutoff, absCutoff time.Time) error
	// Get returns a live session and moves its last_seen_at to now, at most
	// once a minute. ok is false when it is unknown or expired.
	Get(ctx context.Context, idHash string, now, idleCutoff, absCutoff time.Time) (actorID string, passwordChangedAt time.Time, ok bool, err error)
	Delete(ctx context.Context, idHash string) error
	SetPasswordChangedAt(ctx context.Context, idHash string, t time.Time) error
	// CSRFSecret returns the persisted CSRF secret, storing candidate first
	// if none exists yet (safe under concurrent starts).
	CSRFSecret(ctx context.Context, candidate string) (string, error)
}

// Store is the session store plus the CSRF secret, which is persisted so a
// CSRF token stays valid across a restart.
type Store struct {
	repo       Repository
	csrfSecret []byte
	now        func() time.Time
}

// NewStore loads (or creates) the CSRF secret.
func NewStore(ctx context.Context, repo Repository) (*Store, error) {
	candidate := hex.EncodeToString(randomBytes(csrfSecretByteLength))
	secret, err := repo.CSRFSecret(ctx, candidate)
	if err != nil {
		return nil, fmt.Errorf("session: csrf secret: %w", err)
	}
	return &Store{repo: repo, csrfSecret: []byte(secret), now: time.Now}, nil
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("session: failed to read random bytes: " + err.Error())
	}
	return b
}

func hashID(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:])
}

func (s *Store) cutoffs() (now, idle, abs time.Time) {
	now = s.now()
	return now, now.Add(-IdleTimeout), now.Add(-MaxLifetime)
}

// Create starts a session for actorID and returns its session ID.
// passwordChangedAt is the login's changed_at at that moment; a later
// password change makes the session stale (checked by requireLogin).
func (s *Store) Create(ctx context.Context, actorID string, passwordChangedAt time.Time) (sessionID string, err error) {
	sessionID = hex.EncodeToString(randomBytes(sessionIDByteLength))
	now, idle, abs := s.cutoffs()
	if err := s.repo.Insert(ctx, hashID(sessionID), actorID, passwordChangedAt, now, idle, abs); err != nil {
		return "", err
	}
	return sessionID, nil
}

// SetPasswordChangedAt keeps the current session valid after its own
// password change (every other session of that actor goes stale).
func (s *Store) SetPasswordChangedAt(ctx context.Context, sessionID string, t time.Time) error {
	return s.repo.SetPasswordChangedAt(ctx, hashID(sessionID), t)
}

// Get returns the Session for sessionID and whether it is present AND not
// expired. A non-nil error is infrastructure trouble, not "no session".
func (s *Store) Get(ctx context.Context, sessionID string) (Session, bool, error) {
	now, idle, abs := s.cutoffs()
	actorID, changed, ok, err := s.repo.Get(ctx, hashID(sessionID), now, idle, abs)
	if err != nil || !ok {
		return Session{}, false, err
	}
	return Session{ActorID: actorID, PasswordChangedAt: changed}, true, nil
}

// Delete removes sessionID, if present. Deleting an unknown session ID is a
// no-op, not an error.
func (s *Store) Delete(ctx context.Context, sessionID string) error {
	return s.repo.Delete(ctx, hashID(sessionID))
}

// Token returns the stateless CSRF token for sessionID:
// hex(HMAC-SHA256(csrfSecret, sessionID)). No separate token storage — the
// token is fully determined by the session ID and the persisted secret.
func (s *Store) Token(sessionID string) string {
	mac := hmac.New(sha256.New, s.csrfSecret)
	mac.Write([]byte(sessionID))
	return hex.EncodeToString(mac.Sum(nil))
}

// ValidToken reports whether token is the correct CSRF token for
// sessionID, using a constant-time comparison to avoid leaking timing
// information about the correct value.
func (s *Store) ValidToken(sessionID, token string) bool {
	want := s.Token(sessionID)
	return subtle.ConstantTimeCompare([]byte(want), []byte(token)) == 1
}
