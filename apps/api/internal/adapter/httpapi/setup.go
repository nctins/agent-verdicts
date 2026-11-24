package httpapi

import (
	"errors"
	"net/http"

	"agent-verdicts/internal/adapter/authflow"
	"agent-verdicts/internal/audit"
	"agent-verdicts/internal/authentication"
)

// setup creates the first admin from the browser (authflow.Flow.Setup)
// and logs them in.
func (h *handler) setup(w http.ResponseWriter, r *http.Request) {
	if !h.d.Auth.SetupActive(r.Context()) {
		writeError(w, "not_found", http.StatusNotFound, "setup already complete")
		return
	}
	var in struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	a, l, err := h.d.Auth.Setup(r.Context(), authflow.SetupInput{
		Code: in.Code, Name: in.Name, Username: in.Username, Password: in.Password, ClientAddr: authflow.ClientAddr(r),
	})
	switch {
	case errors.Is(err, authflow.ErrSetupClosed):
		writeError(w, "not_found", http.StatusNotFound, "setup already complete")
		return
	case errors.Is(err, authflow.ErrBadSetupCode):
		writeError(w, "unauthenticated", http.StatusUnauthorized, "invalid setup code")
		return
	case errors.Is(err, authentication.ErrUsernameTaken):
		writeError(w, "conflict", http.StatusConflict, "username already in use")
		return
	case err != nil:
		fail(w, r, err)
		return
	}
	h.record(r, audit.Event{ActorID: a.ID, Action: "setup.complete", TargetType: "actor", TargetID: a.ID, Detail: map[string]any{"username": l.Username}})
	sid, err := h.d.Sessions.Create(r.Context(), a.ID, l.ChangedAt)
	if err != nil {
		fail(w, r, err)
		return
	}
	authflow.SetSessionCookie(w, r, sid)
	h.writeSession(w, r, http.StatusCreated, sid, a, false)
}
