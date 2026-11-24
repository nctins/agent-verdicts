package session

import (
	"context"

	"agent-verdicts/internal/access"
)

type actorContextKey struct{}

type actorContext struct {
	actorID string
	grants  access.Grants
}

// WithActor returns a copy of ctx carrying the authenticated actor and what
// it may do, loaded for this request (requireLogin).
func WithActor(ctx context.Context, actorID string, g access.Grants) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actorContext{actorID: actorID, grants: g})
}

// ActorID is the authenticated actor, if any.
func ActorID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(actorContextKey{}).(actorContext)
	if !ok {
		return "", false
	}
	return v.actorID, true
}

// Grants is what the authenticated actor may do; nothing without one.
func Grants(ctx context.Context) access.Grants {
	v, _ := ctx.Value(actorContextKey{}).(actorContext)
	return v.grants
}
