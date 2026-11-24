package actor

import "context"

// NameResolver looks up display names by ID for one response, caching each
// ID once. Errors and not-found both yield "": callers fall back to the ID,
// because the name is decoration, not data anything depends on.
//
// ponytail: one Get per distinct actor; switch to a GetMany when a page
// commonly shows more than a few dozen actors.
type NameResolver struct {
	repo  Repository
	cache map[string]Actor
}

func NewNameResolver(repo Repository) *NameResolver {
	return &NameResolver{repo: repo, cache: map[string]Actor{}}
}

// Name returns the name of actor id, or "" if there is none.
func (r *NameResolver) Name(ctx context.Context, id string) string { return r.get(ctx, id).Name }

// Type returns the type of actor id (Human/Agent), or "" if there is none.
func (r *NameResolver) Type(ctx context.Context, id string) string {
	return string(r.get(ctx, id).Type)
}

func (r *NameResolver) get(ctx context.Context, id string) Actor {
	if a, ok := r.cache[id]; ok {
		return a
	}
	var a Actor
	if r.repo != nil {
		if got, err := r.repo.Get(ctx, id); err == nil {
			a = got
		}
	}
	r.cache[id] = a
	return a
}
