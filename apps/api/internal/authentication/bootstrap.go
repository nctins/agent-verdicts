package authentication

import (
	"context"
	"errors"
	"fmt"

	"agent-verdicts/internal/actor"
)

// Bootstrap creates a brand-new Actor and issues its first credential in
// one step — the only place in this package that creates an Actor. It
// exists solely for the local/operator-only path (Task 13), never
// reachable over the authenticated MCP network boundary: a fresh database
// has no Actor yet, so an ordinary Issue() call has nothing to bind to.
//
// actorType must be actor.TypeHuman or actor.TypeAgent — same contract as
// actor.New, which panics on any other value by design (programmer error,
// not runtime input to handle softly). name is the display name; it is
// normalized here (actor.NormalizeName) so every Bootstrap caller gets the
// same "required, trimmed, <=64 runes" rule without having to remember it.
func Bootstrap(ctx context.Context, actors actor.Repository, credentials Repository, actorType actor.Type, name string) (actorID, token string, err error) {
	name, err = actor.NormalizeName(name)
	if err != nil {
		return "", "", fmt.Errorf("authentication: bootstrap: %w", err)
	}
	a := actor.New(actorType)
	a.Name = name
	if err := actors.Create(ctx, a); err != nil {
		if errors.Is(err, actor.ErrNameTaken) {
			return "", "", err // a caller-facing message, unwrapped
		}
		return "", "", fmt.Errorf("authentication: bootstrap: create actor: %w", err)
	}

	token, err = Issue(ctx, actors, credentials, a.ID)
	if err != nil {
		return "", "", fmt.Errorf("authentication: bootstrap: issue credential: %w", err)
	}

	return a.ID, token, nil
}
