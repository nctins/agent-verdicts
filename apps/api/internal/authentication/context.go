package authentication

import "context"

// actorIDContextKey is unexported so no other package can construct a
// colliding key or set this value directly — the only way to place an
// Actor ID here is through WithActorID, which only Middleware (Task 7)
// calls after successful Verify.
type actorIDContextKey struct{}

// WithActorID returns a copy of ctx carrying actorID as the authenticated
// identity for this request.
func WithActorID(ctx context.Context, actorID string) context.Context {
	return context.WithValue(ctx, actorIDContextKey{}, actorID)
}

// ActorID returns the authenticated Actor ID carried by ctx, and whether
// one was present at all. Callers (MCP tool handlers) must treat ok=false
// as "no authenticated identity" — never substitute a default.
func ActorID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(actorIDContextKey{}).(string)
	return v, ok
}
