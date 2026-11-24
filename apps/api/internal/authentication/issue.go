package authentication

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/platform/id"
)

// secretByteLength: 32 raw bytes (256 bits) of crypto/rand entropy, hex
// encoded to 64 characters. Sized for brute-force infeasibility, not for
// human memorability (Decision #9 §1: opaque random, not a chosen
// password) — see Technical Design §3.4 for why this justifies a fast
// hash (SHA-256) instead of a slow KDF (bcrypt/argon2): the secret's own
// entropy is the defense, not hash cost.
const secretByteLength = 32

func generateSecret() (string, error) {
	b := make([]byte, secretByteLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("authentication: generate secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// ErrActorNotFound is returned by Issue when actorID does not name an
// existing Actor.
var ErrActorNotFound = errors.New("authentication: actor not found")

// Issue mints a new credential bound to an existing actorID and returns
// the full bearer token, "<credential_id>.<secret>". The token — and in
// particular the secret half — is returned exactly once, here; only its
// hash is ever persisted (Decision #9 §5).
func Issue(ctx context.Context, actors actor.Repository, credentials Repository, actorID string) (token string, err error) {
	return IssueWith(ctx, actors, credentials, actorID, "", nil)
}

// DefaultExpiryDays is how long a token issued through the API or the CLI
// lives unless the issuer says otherwise (spec 2026-11-15 D4).
const DefaultExpiryDays = 90

// ErrInvalidLabel: a token label must be 0-64 characters without control characters.
var ErrInvalidLabel = errors.New("authentication: label must be at most 64 characters without control characters")

// NormalizeLabel trims a token label and checks it.
func NormalizeLabel(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > 64 || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return "", ErrInvalidLabel
	}
	return s, nil
}

// IssueWith is Issue with a label and an expiry (nil = never expires).
func IssueWith(ctx context.Context, actors actor.Repository, credentials Repository, actorID, label string, expiresAt *time.Time) (token string, err error) {
	label, err = NormalizeLabel(label)
	if err != nil {
		return "", err
	}
	if _, err := actors.Get(ctx, actorID); err != nil {
		if errors.Is(err, actor.ErrNotFound) {
			return "", ErrActorNotFound
		}
		return "", fmt.Errorf("authentication: issue: check actor: %w", err)
	}

	secret, err := generateSecret()
	if err != nil {
		return "", err
	}

	c := Credential{
		ID:         id.New(),
		ActorID:    actorID,
		SecretHash: hashSecret(secret),
		CreatedAt:  time.Now(),
		Label:      label,
		ExpiresAt:  expiresAt,
	}
	if err := credentials.Create(ctx, c); err != nil {
		return "", fmt.Errorf("authentication: issue: %w", err)
	}

	return c.ID + "." + secret, nil
}
