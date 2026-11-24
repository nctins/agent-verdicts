package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/adapter/authflow"
	"agent-verdicts/internal/audit"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/project"
	"agent-verdicts/internal/reads"
	"agent-verdicts/internal/setting"
)

// Errors with no domain sentinel of their own; handlers return or wrap
// these so errorFrom can map them.
var (
	errForbidden = errors.New("forbidden")
	// errInvalidArgument: a request parameter that belongs to no domain is
	// malformed (the shared query-parameter parsing).
	errInvalidArgument = errors.New("invalid argument")
)

// errorFrom maps an error to spec §8's {code, HTTP status}. Anything not
// recognised is internal.
func errorFrom(err error) (code string, status int) {
	in := func(targets ...error) bool {
		for _, t := range targets {
			if errors.Is(err, t) {
				return true
			}
		}
		return false
	}
	switch {
	case in(errInvalidArgument, contribution.ErrInvalidArgument, setting.ErrInvalidArgument, actor.ErrInvalidArgument, actor.ErrNameRequired, actor.ErrNameTooLong,
		authentication.ErrInvalidUsername, authentication.ErrPasswordTooShort, authentication.ErrPasswordTooLong,
		authentication.ErrNotHuman, authentication.ErrInvalidLabel, audit.ErrBadCursor, project.ErrInvalidID, project.ErrInvalidField, access.ErrInvalidArgument, reads.ErrInvalidArgument):
		return "invalid_argument", http.StatusBadRequest
	case in(authentication.ErrUnauthenticated):
		return "unauthenticated", http.StatusUnauthorized
	case in(errForbidden, access.ErrForbidden):
		return "forbidden", http.StatusForbidden
	case in(contribution.ErrNotFound, project.ErrNotFound, actor.ErrNotFound, authentication.ErrNotFound,
		authentication.ErrActorNotFound, contribution.ErrActorNotFound, setting.ErrNotFound, access.ErrNotFound):
		return "not_found", http.StatusNotFound
	case in(project.ErrAlreadyExists, actor.ErrNameTaken, project.ErrArchived, contribution.ErrAlreadySuperseded, authentication.ErrUsernameTaken, access.ErrConflict, access.ErrLastAdmin):
		return "conflict", http.StatusConflict
	case in(authflow.ErrLocked):
		return "too_many_requests", http.StatusTooManyRequests
	case in(setting.ErrUpstream):
		return "upstream_failed", http.StatusBadGateway
	}
	return "internal", http.StatusInternalServerError
}

// fail writes err as a JSON error. Mapped errors carry their own message
// (domain validation text); internal ones are logged with the path and
// reach the client only as "internal error".
func fail(w http.ResponseWriter, r *http.Request, err error) {
	code, status := errorFrom(err)
	msg := err.Error()
	if status == http.StatusInternalServerError {
		log.Printf("agentverdicts: api: %s %s: %v", r.Method, r.URL.Path, err)
		msg = "internal error"
	}
	writeError(w, code, status, msg)
}

// writeError writes the {code, message} body every error response uses.
func writeError(w http.ResponseWriter, code string, status int, msg string) {
	writeJSON(w, status, map[string]string{"code": code, "message": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("agentverdicts: api: encode response: %v", err)
	}
}
