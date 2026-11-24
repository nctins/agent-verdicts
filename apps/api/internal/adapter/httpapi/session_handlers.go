package httpapi

import (
	"errors"
	"log"
	"net/http"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/adapter/authflow"
	"agent-verdicts/internal/adapter/httpapi/session"
	"agent-verdicts/internal/authentication"
)

type actorView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// accessView is what the actor may do, for the UI to show or hide controls;
// the server still checks every request.
type accessView struct {
	All      []access.Action            `json:"all"`
	Projects map[string][]access.Action `json:"projects"`
}

type sessionView struct {
	Authenticated      bool        `json:"authenticated"`
	Actor              *actorView  `json:"actor"`
	CSRFToken          *string     `json:"csrf_token"`
	Access             *accessView `json:"access,omitempty"`
	MustChangePassword bool        `json:"must_change_password"`
	SetupRequired      bool        `json:"setup_required"`
}

func (h *handler) sessionView(r *http.Request, sessionID string, a actor.Actor, mustChange bool) (sessionView, error) {
	tok := h.d.Sessions.Token(sessionID)
	g, err := h.d.Access.Grants(r.Context(), a.ID)
	if err != nil {
		return sessionView{}, err
	}
	all, projects := g.List()
	return sessionView{
		Access:             &accessView{All: all, Projects: projects},
		Authenticated:      true,
		Actor:              &actorView{ID: a.ID, Name: a.Name, Type: string(a.Type)},
		CSRFToken:          &tok,
		MustChangePassword: mustChange,
		SetupRequired:      h.d.Auth.SetupActive(r.Context()),
	}, nil
}

// writeSession answers with the session of a (status 200 or 201).
func (h *handler) writeSession(w http.ResponseWriter, r *http.Request, status int, sessionID string, a actor.Actor, mustChange bool) {
	v, err := h.sessionView(r, sessionID, a, mustChange)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, status, v)
}

// getSession is public: it reports who (if anyone) the cookie belongs to.
func (h *handler) getSession(w http.ResponseWriter, r *http.Request) {
	c, ok, err := h.authenticate(w, r)
	if err != nil {
		fail(w, r, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, sessionView{SetupRequired: h.d.Auth.SetupActive(r.Context())})
		return
	}
	h.writeSession(w, r, http.StatusOK, c.sessionID, c.actor, c.login.MustChange)
}

// login checks a username and password (authflow.Flow.Login).
// Every credential failure is the same 401; a locked-out
// username gets 429 without its password being checked.
func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	a, l, err := h.d.Auth.Login(r.Context(), in.Username, in.Password, authflow.ClientAddr(r))
	if errors.Is(err, authentication.ErrUnauthenticated) {
		writeError(w, "unauthenticated", http.StatusUnauthorized, "invalid username or password")
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	sid, err := h.d.Sessions.Create(r.Context(), a.ID, l.ChangedAt)
	if err != nil {
		fail(w, r, err)
		return
	}
	authflow.SetSessionCookie(w, r, sid)
	h.writeSession(w, r, http.StatusOK, sid, a, l.MustChange)
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	if sid, ok := authflow.SessionID(r); ok {
		if err := h.d.Sessions.Delete(r.Context(), sid); err != nil {
			fail(w, r, err)
			return
		}
	}
	authflow.ClearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// changePassword replaces the signed-in actor's password. Other sessions of
// the actor end (their password stamp no longer matches); this one is moved
// to the new stamp. A wrong current password is a 400, not a login strike.
func (h *handler) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	actorID, _ := session.ActorID(r.Context()) // set by requireLogin
	err := authentication.ChangePassword(r.Context(), h.d.Logins, actorID, in.CurrentPassword, in.NewPassword)
	if errors.Is(err, authentication.ErrUnauthenticated) {
		writeError(w, "invalid_argument", http.StatusBadRequest, "current password is incorrect")
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	l, err := h.d.Logins.GetByActor(r.Context(), actorID)
	if err != nil {
		// The password did change; this session just goes stale with the rest.
		log.Printf("agentverdicts: api: change password: re-read login: %v", err)
	} else if sid, ok := authflow.SessionID(r); ok {
		if err := h.d.Sessions.SetPasswordChangedAt(r.Context(), sid, l.ChangedAt); err != nil {
			log.Printf("agentverdicts: api: change password: keep session: %v", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
