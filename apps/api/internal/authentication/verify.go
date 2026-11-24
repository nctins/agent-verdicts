package authentication

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"agent-verdicts/internal/actor"
)

// ErrUnauthenticated is the single sentinel Verify returns for every
// failure class — missing/malformed token, unknown credential_id, wrong
// secret, revoked or expired credential, and an actor_id the credential names but
// that no longer exists. Collapsing all of these avoids handing a network
// caller a signal about *which* check failed (Technical Design §11).
var ErrUnauthenticated = errors.New("authentication: unauthenticated")

// Verify parses and checks a bearer token ("<credential_id>.<secret>"),
// returning the Actor ID it authoritatively resolves to. Fail-closed:
// every error path returns ErrUnauthenticated (or, for a genuine
// infrastructure error unrelated to the credential itself, a wrapped
// error) — never a partial or default identity.
func Verify(ctx context.Context, actors actor.Repository, credentials Repository, token string) (actorID string, err error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", ErrUnauthenticated
	}
	credentialID, secret := parts[0], parts[1]

	c, err := credentials.Get(ctx, credentialID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", ErrUnauthenticated
		}
		return "", fmt.Errorf("authentication: verify: %w", err)
	}

	now := time.Now()
	if c.RevokedAt != nil || (c.ExpiresAt != nil && !now.Before(*c.ExpiresAt)) {
		return "", ErrUnauthenticated
	}

	if subtle.ConstantTimeCompare([]byte(hashSecret(secret)), []byte(c.SecretHash)) != 1 {
		return "", ErrUnauthenticated
	}

	a, err := actors.Get(ctx, c.ActorID)
	if err != nil {
		if errors.Is(err, actor.ErrNotFound) {
			return "", ErrUnauthenticated
		}
		return "", fmt.Errorf("authentication: verify: resolve actor: %w", err)
	}
	if a.Disabled() {
		return "", ErrUnauthenticated
	}

	if err := credentials.Touch(ctx, c.ID, now); err != nil {
		log.Printf("agentverdicts: credential %s: last_used_at: %v", c.ID, err) // bookkeeping must not fail the request
	}
	return c.ActorID, nil
}

// Revoke marks credentialID as no longer valid for authentication. It
// does not touch the Actor it was bound to, and does not affect any other
// credential belonging to the same Actor (Decision #9 §5-6).
func Revoke(ctx context.Context, credentials Repository, credentialID string) error {
	return credentials.Revoke(ctx, credentialID, time.Now())
}
