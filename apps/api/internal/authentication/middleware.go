package authentication

import (
	"net/http"
	"strings"

	"agent-verdicts/internal/actor"
)

// bearerPrefix is the wire convention this package uses to carry a
// credential — a standard "Authorization: Bearer <token>" header. The
// exact header name/scheme is an implementation detail Decision #9 left
// to Technical Design (not the authentication architecture itself).
const bearerPrefix = "Bearer "

func extractToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, bearerPrefix) {
		return "", false
	}
	token := strings.TrimPrefix(h, bearerPrefix)
	if token == "" {
		return "", false
	}
	return token, true
}

// Middleware returns HTTP middleware enforcing Decision #9's
// authentication boundary: every request must carry a valid, unrevoked
// credential before it is allowed to reach next. On any failure it
// responds and returns without ever calling next.ServeHTTP — fail-closed,
// no exceptions. On success it injects the resolved Actor ID into the
// request's context (via WithActorID) before delegating to next.
func Middleware(actors actor.Repository, credentials Repository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := extractToken(r)
			if !ok {
				http.Error(w, "unauthenticated", http.StatusUnauthorized)
				return
			}

			actorID, err := Verify(r.Context(), actors, credentials, token)
			if err != nil {
				http.Error(w, "unauthenticated", http.StatusUnauthorized)
				return
			}

			r = r.WithContext(WithActorID(r.Context(), actorID))
			next.ServeHTTP(w, r)
		})
	}
}
