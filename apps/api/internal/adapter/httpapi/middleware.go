package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/adapter/authflow"
	"agent-verdicts/internal/adapter/httpapi/session"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/project"
)

// maxBodyBytes caps every JSON request body.
const maxBodyBytes = 256 << 10

// noStore marks every response uncacheable: bodies carry session state,
// CSRF tokens and one-time secrets.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// current is a request's live session, re-validated against the database.
type current struct {
	sessionID string
	actor     actor.Actor
	login     authentication.Login
}

// authenticate resolves the request's session. ok=false means no usable
// session: none, expired, password changed since it began, or the actor is
// gone or disabled — a dead session is deleted and its cookie cleared. A
// non-nil error is transient infrastructure trouble; the session is kept
// so a retry can work.
func (h *handler) authenticate(w http.ResponseWriter, r *http.Request) (c current, ok bool, err error) {
	sid, ok := authflow.SessionID(r)
	if !ok {
		return current{}, false, nil
	}
	sess, ok, err := h.d.Sessions.Get(r.Context(), sid)
	if err != nil {
		return current{}, false, fmt.Errorf("session lookup: %w", err)
	}
	if !ok {
		authflow.ClearSessionCookie(w, r)
		return current{}, false, nil
	}
	end := func() (current, bool, error) {
		if err := h.d.Sessions.Delete(r.Context(), sid); err != nil {
			log.Printf("agentverdicts: api: delete dead session: %v", err) // it stays dead: the next check ends it again
		}
		authflow.ClearSessionCookie(w, r)
		return current{}, false, nil
	}
	l, err := h.d.Logins.GetByActor(r.Context(), sess.ActorID)
	if errors.Is(err, authentication.ErrLoginNotFound) {
		return end()
	}
	if err != nil {
		return current{}, false, fmt.Errorf("session login lookup: %w", err)
	}
	if !l.ChangedAt.Equal(sess.PasswordChangedAt) {
		return end()
	}
	// The actor's current state wins over the login-time snapshot:
	// disabling or demoting takes effect on the next request.
	a, err := h.d.Actors.Get(r.Context(), sess.ActorID)
	if errors.Is(err, actor.ErrNotFound) {
		return end()
	}
	if err != nil {
		return current{}, false, fmt.Errorf("session actor lookup: %w", err)
	}
	if a.Disabled() {
		return end()
	}
	return current{sessionID: sid, actor: a, login: l}, true, nil
}

// mustChangeAllowed lists the login-required routes usable while a
// temporary password is in place (GET /session is public anyway).
func mustChangeAllowed(r *http.Request) bool {
	switch r.Method + " " + r.URL.Path {
	case "DELETE /session", "POST /account/password":
		return true
	}
	return false
}

// requireLogin runs next only for a live session (401 otherwise) whose
// password need not be changed first (403). It puts the actor ID and its
// current grants in the context (session.ActorID / session.Grants).
func (h *handler) requireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok, err := h.authenticate(w, r)
		if err != nil {
			fail(w, r, err)
			return
		}
		if !ok {
			writeError(w, "unauthenticated", http.StatusUnauthorized, "login required")
			return
		}
		if c.login.MustChange && !mustChangeAllowed(r) {
			writeError(w, "forbidden", http.StatusForbidden, "password change required")
			return
		}
		g, err := h.d.Access.Grants(r.Context(), c.actor.ID)
		if err != nil {
			fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(session.WithActor(r.Context(), c.actor.ID, g)))
	})
}

// require is requireLogin plus action a, instance-wide (403 without it).
// The grants are the ones requireLogin just re-read, never a login-time snapshot.
func (h *handler) require(a access.Action, next http.Handler) http.Handler {
	return h.requireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !session.Grants(r.Context()).Can(a, "") {
			writeError(w, "forbidden", http.StatusForbidden, string(a)+" required")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// requireProject is requireLogin plus action a on project {p}. A project
// the caller cannot read, or that does not exist, is 404 (spec §5, D8).
func (h *handler) requireProject(a access.Action, next http.Handler) http.Handler {
	return h.requireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, p := session.Grants(r.Context()), r.PathValue("p")
		_, err := h.d.Projects.Get(r.Context(), p)
		if err != nil && !errors.Is(err, project.ErrNotFound) {
			fail(w, r, err)
			return
		}
		if err != nil || !g.Can(access.ContributionRead, p) {
			writeError(w, "not_found", http.StatusNotFound, "project "+strconv.Quote(p)+" not found")
			return
		}
		if !g.Can(a, p) {
			writeError(w, "forbidden", http.StatusForbidden, string(a)+" required on project "+strconv.Quote(p))
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// write is requireLogin + csrf, for actions on the caller's own account.
func (h *handler) write(next http.HandlerFunc) http.Handler { return h.requireLogin(h.csrf(next)) }

// writeAs is require(a) + csrf.
func (h *handler) writeAs(a access.Action, next http.HandlerFunc) http.Handler {
	return h.require(a, h.csrf(next))
}

// projectWrite is requireProject(a) + csrf + the project must not be archived (409).
func (h *handler) projectWrite(a access.Action, next http.HandlerFunc) http.Handler {
	return h.requireProject(a, h.csrf(h.notArchived(next)))
}

func (h *handler) notArchived(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := h.d.Projects.Get(r.Context(), r.PathValue("p"))
		if err != nil {
			fail(w, r, err)
			return
		}
		if p.Archived() {
			fail(w, r, project.ErrArchived)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// canModerate: the caller holds content:Moderate on project {p}, so hidden content is visible to it.
func canModerate(r *http.Request) bool {
	return session.Grants(r.Context()).Can(access.ContentModerate, r.PathValue("p"))
}

func (h *handler) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sid, ok := authflow.SessionID(r)
		if r.Method != http.MethodGet && r.Method != http.MethodHead &&
			(!ok || !h.d.Sessions.ValidToken(sid, r.Header.Get("X-CSRF-Token"))) {
			writeError(w, "forbidden", http.StatusForbidden, "invalid or missing CSRF token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin guards the two session-less POSTs (login, setup) against
// login CSRF: the Origin header must be present and its host must equal
// Host, or the first X-Forwarded-Host value (the client-facing host when a
// proxy chain appended its own).
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authflow.OriginMatches(r) {
			writeError(w, "forbidden", http.StatusForbidden, "cross-origin request rejected")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// decode reads a JSON body (≤ maxBodyBytes, no unknown fields) into v. On
// failure it has already written 400 and returns false.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		msg := "invalid JSON body: " + err.Error()
		if errors.As(err, &tooBig) {
			msg = "request body too large"
		}
		writeError(w, "invalid_argument", http.StatusBadRequest, msg)
		return false
	}
	return true
}
