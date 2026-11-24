package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/adapter/authflow"
	"agent-verdicts/internal/adapter/httpapi/session"
	"agent-verdicts/internal/adapter/view"
	"agent-verdicts/internal/audit"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/textfold"
	"agent-verdicts/internal/project"
	"agent-verdicts/internal/trust"
)

// Admin endpoints (spec §8 /admin/*). Every route here is wrapped in
// require or writeAs with its action in table().

// errBadRequest is a 400 for the rules of the /admin/actors routes that have
// no sentinel of their own.
func errBadRequest(msg string) error {
	return fmt.Errorf("%w: %s", actor.ErrInvalidArgument, msg)
}

const overviewRecent = 10

type recentItem struct {
	ProjectID string `json:"project_id"`
	view.ListItem
}

// GET /admin/overview: totals and the newest contributions, over the
// projects the caller may read; for a caller without actor:Manage (a project
// manager), only over the projects it holds member:Manage on.
func (h *handler) overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g := session.Grants(ctx)
	ps, err := h.readableProjects(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	scoped := !g.Can(access.ActorManage, "")
	if scoped {
		ps = slices.DeleteFunc(ps, func(p project.Project) bool { return !g.Can(access.MemberManage, p.ID) })
	}
	perProject, err := h.d.Contributions.CountByProject(ctx)
	if err != nil {
		fail(w, r, err)
		return
	}
	var ids []string // nil = every project
	if all, _ := g.Readable(); !all || scoped {
		ids = []string{}
		for _, p := range ps {
			ids = append(ids, p.ID)
		}
	}
	recent, err := h.d.Contributions.Recent(ctx, ids, overviewRecent)
	if err != nil {
		fail(w, r, err)
		return
	}
	shaped, err := view.ListItems(ctx, h.d.Contributions, h.d.Actors, recent)
	if err != nil {
		fail(w, r, err)
		return
	}
	var total, open int
	states := map[trust.State]int{trust.StateFavorable: 0, trust.StateUnfavorable: 0, trust.StateConflict: 0, trust.StateInsufficientEvidence: 0}
	for _, p := range ps {
		n := perProject[p.ID]
		total += n.Contributions
		open += n.OpenQuestions
		for st, c := range n.States {
			states[st] += c
		}
	}
	attention, err := h.d.Contributions.Attention(ctx, ids, time.Now())
	if err != nil {
		fail(w, r, err)
		return
	}
	items := make([]recentItem, len(shaped))
	for i, it := range shaped {
		items[i] = recentItem{ProjectID: recent[i].ProjectID, ListItem: it}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"project_count": len(ps), "contribution_count": total, "open_question_count": open,
		"states": states, "recent": items, "attention": attention,
	})
}

type adminActor struct {
	ID        string  `json:"id"`
	Type      string  `json:"type"`
	Name      string  `json:"name"`
	Disabled  bool    `json:"disabled"`
	CreatedAt string  `json:"created_at"`
	Username  *string `json:"username"`
	HasLogin  bool    `json:"has_login"`
	// Null for a caller without actor:Manage, like username (spec 2026-11-22-admin-ui-redesign A2).
	LastActiveAt    *string `json:"last_active_at"`
	NextTokenExpiry *string `json:"next_token_expiry"`
}

// activity is every actor's latest sign of life and next token expiry.
func (h *handler) activity(r *http.Request) (map[string]authentication.Activity, error) {
	return h.d.Credentials.Activity(r.Context(), time.Now())
}

// adminActorOf shapes a with its login (if any) and its entry of act.
func (h *handler) adminActorOf(r *http.Request, a actor.Actor, act map[string]authentication.Activity) (adminActor, error) {
	out := adminActor{ID: a.ID, Type: string(a.Type), Name: a.Name, Disabled: a.Disabled(), CreatedAt: view.TS(a.CreatedAt)}
	out.LastActiveAt, out.NextTokenExpiry = tsPtr(act[a.ID].LastActive), tsPtr(act[a.ID].NextExpiry)
	l, err := h.d.Logins.GetByActor(r.Context(), a.ID)
	switch {
	case err == nil:
		out.Username, out.HasLogin = &l.Username, true
	case !errors.Is(err, authentication.ErrLoginNotFound):
		return adminActor{}, err
	}
	return out, nil
}

// minSearchRunes is the shortest q accepted by GET /admin/actors.
const minSearchRunes = 2

// maxActorMatches caps what a project manager's name search returns.
const maxActorMatches = 20

// GET /admin/actors?q=. With actor:Manage: every actor (q filters by name).
// With only member:Manage: without q, the actors holding an assignment on a
// project the caller manages; with q (at least 2 characters), up to 20
// actors whose name contains it, so a manager can find someone to add.
//
// ponytail: one login lookup per actor; a join when actor lists grow past a small team.
func (h *handler) listActors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g := session.Grants(ctx)
	full := g.Can(access.ActorManage, "") // member managers see who, not how they log in
	q := ""
	if raw := r.URL.Query().Get("q"); raw != "" {
		if q = textfold.Fold(strings.TrimSpace(raw)); utf8.RuneCountInString(q) < minSearchRunes {
			fail(w, r, errBadRequest("q must have at least 2 characters"))
			return
		}
	}
	all, err := h.d.Actors.List(ctx)
	if err != nil {
		fail(w, r, err)
		return
	}
	if q != "" {
		// Fold drops case and Vietnamese accents (đ = d) on both sides.
		all = slices.DeleteFunc(all, func(a actor.Actor) bool { return !strings.Contains(textfold.Fold(a.Name), q) })
		if !full {
			all = all[:min(len(all), maxActorMatches)]
		}
	} else if !full {
		as, err := h.d.Access.ListAssignments(ctx, access.AssignmentFilter{})
		if err != nil {
			fail(w, r, err)
			return
		}
		members := map[string]bool{}
		for _, a := range as {
			if g.Can(access.MemberManage, scope(a)) {
				members[a.ActorID] = true
			}
		}
		all = slices.DeleteFunc(all, func(a actor.Actor) bool { return !members[a.ID] })
	}
	var act map[string]authentication.Activity
	if full {
		if act, err = h.activity(r); err != nil {
			fail(w, r, err)
			return
		}
	}
	out := []adminActor{}
	for _, a := range all {
		if !full {
			out = append(out, adminActor{ID: a.ID, Type: string(a.Type), Name: a.Name, Disabled: a.Disabled(), CreatedAt: view.TS(a.CreatedAt)})
			continue
		}
		v, err := h.adminActorOf(r, a, act)
		if err != nil {
			fail(w, r, err)
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

type credentialItem struct {
	ID         string  `json:"id"`
	CreatedAt  string  `json:"created_at"`
	RevokedAt  *string `json:"revoked_at"`
	Label      string  `json:"label"`
	ExpiresAt  *string `json:"expires_at"`   // null: never expires
	LastUsedAt *string `json:"last_used_at"` // null: never used; at most a minute stale
}

func tsPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := view.TS(*t)
	return &s
}

// actorDetail is GET /admin/actors/{id}. Credentials are listed for every
// actor: a person's tokens from before password logins still work over MCP
// and must stay visible and revocable.
func (h *handler) actorDetail(r *http.Request, id string) (any, error) {
	a, err := h.d.Actors.Get(r.Context(), id)
	if err != nil {
		return nil, err
	}
	act, err := h.activity(r)
	if err != nil {
		return nil, err
	}
	v, err := h.adminActorOf(r, a, act)
	if err != nil {
		return nil, err
	}
	creds, err := h.d.Credentials.ListByActor(r.Context(), a.ID)
	if err != nil {
		return nil, err
	}
	items := []credentialItem{}
	for _, c := range creds {
		items = append(items, credentialItem{ID: c.ID, CreatedAt: view.TS(c.CreatedAt), RevokedAt: tsPtr(c.RevokedAt),
			Label: c.Label, ExpiresAt: tsPtr(c.ExpiresAt), LastUsedAt: tsPtr(c.LastUsedAt)})
	}
	as, err := h.d.Access.ListAssignments(r.Context(), access.AssignmentFilter{ActorID: a.ID})
	if err != nil {
		return nil, err
	}
	assignments, err := h.assignmentViews(r.Context(), as)
	if err != nil {
		return nil, err
	}
	return struct {
		adminActor
		Credentials []credentialItem `json:"credentials"`
		Assignments []assignmentView `json:"assignments"`
	}{v, items, assignments}, nil
}

func (h *handler) getActor(w http.ResponseWriter, r *http.Request) {
	d, err := h.actorDetail(r, r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// POST /admin/actors. A Human gets a login (password returned once); an
// Agent gets a token (returned once).
func (h *handler) createActor(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Type       string `json:"type"`
		Name       string `json:"name"`
		Username   string `json:"username"`
		Password   string `json:"password"`
		MustChange bool   `json:"must_change"`
	}
	if !decode(w, r, &in) {
		return
	}
	ctx := r.Context()
	name, err := actor.NormalizeName(in.Name)
	if err != nil {
		fail(w, r, err)
		return
	}
	t := actor.Type(in.Type)
	switch t {
	case actor.TypeHuman:
		if in.Username, err = authentication.NormalizeUsername(in.Username); err != nil {
			fail(w, r, err)
			return
		}
		if in.Password != "" {
			if err := authentication.ValidatePassword(in.Password); err != nil {
				fail(w, r, err)
				return
			}
		}
		// Check before creating so a taken name leaves no actor behind.
		switch _, err := h.d.Logins.GetByUsername(ctx, in.Username); {
		case err == nil:
			fail(w, r, authentication.ErrUsernameTaken)
			return
		case !errors.Is(err, authentication.ErrLoginNotFound):
			fail(w, r, err)
			return
		}
	case actor.TypeAgent:
		if in.Username != "" || in.Password != "" || in.MustChange {
			fail(w, r, errBadRequest("agents have no username or password; they get a token"))
			return
		}
	default:
		fail(w, r, errBadRequest("type must be Human or Agent"))
		return
	}

	a := actor.New(t)
	a.Name = name
	var password, token string
	detail := map[string]any{"type": string(t)}
	if t == actor.TypeHuman {
		password, err = authentication.CreateHuman(ctx, h.d.Logins, h.d.Actors, a, in.Username, in.Password, in.MustChange)
		detail["username"] = in.Username
	} else if err = h.d.Actors.Create(ctx, a); err == nil {
		expires := defaultExpiry()
		if token, err = authentication.IssueWith(ctx, h.d.Actors, h.d.Credentials, a.ID, "", expires); err == nil {
			detail["credential_id"], detail["expires_at"] = tokenID(token), tsPtr(expires)
		}
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	h.record(r, audit.Event{Action: "actor.create", TargetType: "actor", TargetID: a.ID, Detail: detail})
	act, err := h.activity(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	v, err := h.adminActorOf(r, a, act)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		adminActor
		Password string `json:"password,omitempty"`
		Token    string `json:"token,omitempty"`
	}{v, password, token})
}

// PATCH /admin/actors/{id}: absent fields are unchanged. Nobody can disable
// themselves. Admin rights are a role assignment, not a field.
func (h *handler) patchActor(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     *string `json:"name"`
		Disabled *bool   `json:"disabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	ctx := r.Context()
	a, err := h.d.Actors.Get(ctx, r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	if err := h.canActOn(ctx, a.ID); err != nil {
		fail(w, r, err)
		return
	}
	self := a.ID == me(r)
	if self && in.Disabled != nil && *in.Disabled {
		fail(w, r, errBadRequest("you cannot disable yourself"))
		return
	}
	if in.Name != nil {
		name, err := actor.NormalizeName(*in.Name)
		if err != nil {
			fail(w, r, err)
			return
		}
		if err := h.d.Actors.UpdateProfile(ctx, a.ID, name); err != nil {
			fail(w, r, err)
			return
		}
		if name != a.Name {
			h.record(r, audit.Event{Action: "actor.rename", TargetType: "actor", TargetID: a.ID, Detail: map[string]any{"from": a.Name, "to": name}})
		}
	}
	if in.Disabled != nil && *in.Disabled != a.Disabled() {
		var at *time.Time
		if *in.Disabled {
			now := time.Now()
			at = &now
		}
		if err := h.d.Actors.SetDisabled(ctx, a.ID, at); err != nil { // ErrLastAdmin: the last admin stays enabled
			fail(w, r, err)
			return
		}
		action := "actor.enable"
		if at != nil {
			action = "actor.disable"
		}
		h.record(r, audit.Event{Action: action, TargetType: "actor", TargetID: a.ID})
	}
	h.getActor(w, r)
}

// POST /admin/actors/{id}/password: set (or generate) a Human's password,
// returned once. Your own reset never forces a change and keeps this session.
func (h *handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username   string `json:"username"` // "" keeps the current one
		Password   string `json:"password"`
		MustChange bool   `json:"must_change"`
	}
	if !decode(w, r, &in) {
		return
	}
	ctx := r.Context()
	a, err := h.d.Actors.Get(ctx, r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	id := a.ID // canonical form: the path may spell the uuid differently
	if err := h.canActOn(ctx, id); err != nil {
		fail(w, r, err)
		return
	}
	self := id == me(r)
	pw, err := authentication.SetLogin(ctx, h.d.Logins, h.d.Actors, id, in.Username, in.Password, in.MustChange && !self)
	if err != nil {
		fail(w, r, err)
		return
	}
	h.record(r, audit.Event{Action: "actor.password_reset", TargetType: "actor", TargetID: id, Detail: map[string]any{"must_change": in.MustChange && !self}})
	if self {
		if l, err := h.d.Logins.GetByActor(ctx, id); err != nil {
			log.Printf("agentverdicts: api: self reset: re-read login: %v", err)
		} else if sid, ok := authflow.SessionID(r); ok {
			if err := h.d.Sessions.SetPasswordChangedAt(ctx, sid, l.ChangedAt); err != nil {
				log.Printf("agentverdicts: api: self reset: keep session: %v", err)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"password": pw})
}

// defaultExpiry is when a token issued now expires unless the issuer says otherwise (D4).
func defaultExpiry() *time.Time {
	t := time.Now().AddDate(0, 0, authentication.DefaultExpiryDays)
	return &t
}

// expiryFrom reads expires_in_days: absent = the default, null = never,
// else 30, 90 or 365.
func expiryFrom(raw json.RawMessage) (*time.Time, error) {
	if raw == nil {
		return defaultExpiry(), nil
	}
	if bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var days int
	if err := json.Unmarshal(raw, &days); err != nil || (days != 30 && days != 90 && days != 365) {
		return nil, errBadRequest("expires_in_days must be 30, 90, 365 or null")
	}
	t := time.Now().AddDate(0, 0, days)
	return &t, nil
}

func tokenID(token string) string { id, _, _ := strings.Cut(token, "."); return id }

// POST /admin/actors/{id}/credentials {label?, expires_in_days?}: a new
// Agent token, returned once. expires_in_days is 30, 90, 365 or null (never
// expires); absent means 90. The body may be empty.
func (h *handler) issueCredential(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Label         string          `json:"label"`
		ExpiresInDays json.RawMessage `json:"expires_in_days"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	expires, err := expiryFrom(in.ExpiresInDays)
	if err != nil {
		fail(w, r, err)
		return
	}
	a, err := h.d.Actors.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	if a.Type != actor.TypeAgent {
		fail(w, r, errBadRequest("tokens are only issued to Agents; people log in with a password"))
		return
	}
	if err := h.canActOn(r.Context(), a.ID); err != nil {
		fail(w, r, err)
		return
	}
	tok, err := authentication.IssueWith(r.Context(), h.d.Actors, h.d.Credentials, a.ID, in.Label, expires)
	if err != nil {
		fail(w, r, err)
		return
	}
	label, _ := authentication.NormalizeLabel(in.Label)
	credID := tokenID(tok)
	h.record(r, audit.Event{Action: "credential.issue", TargetType: "credential", TargetID: credID,
		Detail: map[string]any{"actor_id": a.ID, "label": label, "expires_at": tsPtr(expires)}})
	writeJSON(w, http.StatusCreated, struct {
		ID        string  `json:"id"`
		Token     string  `json:"token"`
		Label     string  `json:"label"`
		ExpiresAt *string `json:"expires_at"`
	}{credID, tok, label, tsPtr(expires)})
}

// DELETE /admin/credentials/{id}: revoke; revoking twice is a no-op.
func (h *handler) revokeCredential(w http.ResponseWriter, r *http.Request) {
	c, err := h.d.Credentials.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	if err := h.canActOn(r.Context(), c.ActorID); err != nil {
		fail(w, r, err)
		return
	}
	if c.RevokedAt == nil {
		if err := authentication.Revoke(r.Context(), h.d.Credentials, c.ID); err != nil {
			fail(w, r, err)
			return
		}
		h.record(r, audit.Event{Action: "credential.revoke", TargetType: "credential", TargetID: c.ID, Detail: map[string]any{"actor_id": c.ActorID}})
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /admin/projects {id, name?, description?}
func (h *handler) createProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, err := project.Create(r.Context(), h.d.Projects, strings.TrimSpace(in.ID), in.Name, in.Description)
	if err != nil {
		fail(w, r, err)
		return
	}
	h.record(r, audit.Event{Action: "project.create", TargetType: "project", TargetID: p.ID, ProjectID: p.ID})
	writeJSON(w, http.StatusCreated, projectBody(p))
}

func projectBody(p project.Project) map[string]any {
	return map[string]any{"id": p.ID, "name": p.Name, "description": p.Description, "min_favorable": p.MinFavorable, "created_at": view.TS(p.CreatedAt), "archived": p.Archived()}
}

// PATCH /admin/projects/{id} {name?, description?, min_favorable?, archived?}: a missing field is kept.
func (h *handler) patchProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name         *string `json:"name"`
		Description  *string `json:"description"`
		MinFavorable *int    `json:"min_favorable"`
		Archived     *bool   `json:"archived"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, err := project.Update(r.Context(), h.d.Projects, r.PathValue("id"), in.Name, in.Description, in.MinFavorable, in.Archived)
	if err != nil {
		fail(w, r, err)
		return
	}
	var fields []string
	for name, set := range map[string]bool{"name": in.Name != nil, "description": in.Description != nil, "min_favorable": in.MinFavorable != nil, "archived": in.Archived != nil} {
		if set {
			fields = append(fields, name)
		}
	}
	slices.Sort(fields)
	h.record(r, audit.Event{Action: "project.update", TargetType: "project", TargetID: p.ID, ProjectID: p.ID, Detail: map[string]any{"fields": fields}})
	writeJSON(w, http.StatusOK, projectBody(p))
}
