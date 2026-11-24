package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/adapter/httpapi/session"
	"agent-verdicts/internal/adapter/view"
	"agent-verdicts/internal/audit"
	"agent-verdicts/internal/platform/id"
)

// Policies, roles and assignments (spec 2026-11-14-access-control §6, §7).

// requireAny is requireLogin plus ok(grants) (403 otherwise).
func (h *handler) requireAny(what string, ok func(access.Grants) bool, next http.Handler) http.Handler {
	return h.requireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ok(session.Grants(r.Context())) {
			writeError(w, "forbidden", http.StatusForbidden, what+" required")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func authorsOrManages(g access.Grants) bool { return g.Can(access.PolicyManage, "") || g.ManagesAny() }

func forbidden(format string, a ...any) error {
	return fmt.Errorf("%w: %s", access.ErrForbidden, fmt.Sprintf(format, a...))
}

type policyView struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Managed   bool            `json:"managed"`
	Actions   []access.Action `json:"actions"`
	CreatedAt string          `json:"created_at"`
}

func policyOf(p access.Policy) policyView {
	return policyView{ID: p.ID, Name: p.Name, Managed: p.Managed, Actions: p.Actions, CreatedAt: view.TS(p.CreatedAt)}
}

type roleView struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Managed   bool            `json:"managed"`
	PolicyIDs []string        `json:"policy_ids"`
	Actions   []access.Action `json:"actions"`
	CreatedAt string          `json:"created_at"`
}

func (h *handler) roleOf(ctx context.Context, x access.Role) (roleView, error) {
	acts, err := h.d.Access.RoleActions(ctx, x.ID)
	return roleView{ID: x.ID, Name: x.Name, Managed: x.Managed, PolicyIDs: x.PolicyIDs, Actions: acts, CreatedAt: view.TS(x.CreatedAt)}, err
}

func (h *handler) listPolicies(w http.ResponseWriter, r *http.Request) {
	ps, err := h.d.Access.ListPolicies(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	out := make([]policyView, len(ps))
	for i, p := range ps {
		out[i] = policyOf(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

type policyInput struct {
	Name    string          `json:"name"`
	Actions []access.Action `json:"actions"`
}

// authorPolicy validates in and checks the caller may author it (and, when
// replacing old, everything old held).
func authorPolicy(r *http.Request, in policyInput, old []access.Action) (string, []access.Action, error) {
	name, err := access.NormalizeName(in.Name)
	if err != nil {
		return "", nil, err
	}
	actions, err := access.ValidateCustom(in.Actions)
	if err != nil {
		return "", nil, err
	}
	if !session.Grants(r.Context()).CanAuthor(append(slices.Clone(actions), old...)) {
		return "", nil, forbidden("you can only author policies with actions you hold on every project")
	}
	return name, actions, nil
}

func (h *handler) createPolicy(w http.ResponseWriter, r *http.Request) {
	var in policyInput
	if !decode(w, r, &in) {
		return
	}
	name, actions, err := authorPolicy(r, in, nil)
	if err != nil {
		fail(w, r, err)
		return
	}
	p := access.Policy{ID: id.New(), Name: name, Actions: actions, CreatedAt: time.Now()}
	if err := h.d.Access.CreatePolicy(r.Context(), p); err != nil {
		fail(w, r, err)
		return
	}
	h.record(r, audit.Event{Action: "policy.create", TargetType: "policy", TargetID: p.ID, Detail: map[string]any{"name": p.Name, "actions": p.Actions}})
	writeJSON(w, http.StatusCreated, policyOf(p))
}

func (h *handler) updatePolicy(w http.ResponseWriter, r *http.Request) {
	var in policyInput
	if !decode(w, r, &in) {
		return
	}
	ctx := r.Context()
	old, err := h.d.Access.GetPolicy(ctx, r.PathValue("id"))
	if err == nil && old.Managed {
		err = forbidden("managed policy %q is read-only", old.ID)
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	name, actions, err := authorPolicy(r, in, old.Actions)
	if err != nil {
		fail(w, r, err)
		return
	}
	old.Name, old.Actions = name, actions
	if err := h.d.Access.UpdatePolicy(ctx, old); err != nil {
		fail(w, r, err)
		return
	}
	h.record(r, audit.Event{Action: "policy.update", TargetType: "policy", TargetID: old.ID, Detail: map[string]any{"name": old.Name, "actions": old.Actions}})
	writeJSON(w, http.StatusOK, policyOf(old))
}

func (h *handler) deletePolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	old, err := h.d.Access.GetPolicy(ctx, r.PathValue("id"))
	if err == nil && !old.Managed && !session.Grants(ctx).CanAuthor(old.Actions) {
		err = forbidden("you can only delete policies with actions you hold on every project")
	}
	if err == nil {
		err = h.d.Access.DeletePolicy(ctx, old.ID) // managed → ErrForbidden
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	h.record(r, audit.Event{Action: "policy.delete", TargetType: "policy", TargetID: old.ID, Detail: map[string]any{"name": old.Name}})
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) listRoles(w http.ResponseWriter, r *http.Request) {
	rs, err := h.d.Access.ListRoles(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	out := make([]roleView, len(rs))
	for i, x := range rs {
		if out[i], err = h.roleOf(r.Context(), x); err != nil {
			fail(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

type roleInput struct {
	Name      string   `json:"name"`
	PolicyIDs []string `json:"policy_ids"`
}

// authorRole validates in: at least one existing policy, never the
// administrator policy (admin is the managed admin role), every action
// within the caller's reach (old: the role's current actions, if replacing).
func (h *handler) authorRole(r *http.Request, in roleInput, old []access.Action) (string, []string, error) {
	name, err := access.NormalizeName(in.Name)
	if err != nil {
		return "", nil, err
	}
	if len(in.PolicyIDs) == 0 {
		return "", nil, fmt.Errorf("%w: a role needs at least one policy", access.ErrInvalidArgument)
	}
	actions := slices.Clone(old)
	ids := slices.Compact(slices.Sorted(slices.Values(in.PolicyIDs)))
	for _, pid := range ids {
		p, err := h.d.Access.GetPolicy(r.Context(), pid)
		if err != nil {
			return "", nil, err
		}
		if slices.Contains(p.Actions, access.All) {
			return "", nil, fmt.Errorf("%w: only the managed admin role holds %s", access.ErrInvalidArgument, p.Name)
		}
		actions = append(actions, p.Actions...)
	}
	if !session.Grants(r.Context()).CanAuthor(actions) {
		return "", nil, forbidden("you can only author roles with actions you hold on every project")
	}
	return name, ids, nil
}

func (h *handler) writeRole(w http.ResponseWriter, r *http.Request, status int, x access.Role) {
	v, err := h.roleOf(r.Context(), x)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, status, v)
}

func (h *handler) createRole(w http.ResponseWriter, r *http.Request) {
	var in roleInput
	if !decode(w, r, &in) {
		return
	}
	name, ids, err := h.authorRole(r, in, nil)
	if err != nil {
		fail(w, r, err)
		return
	}
	x := access.Role{ID: id.New(), Name: name, PolicyIDs: ids, CreatedAt: time.Now()}
	if err := h.d.Access.CreateRole(r.Context(), x); err != nil {
		fail(w, r, err)
		return
	}
	h.record(r, audit.Event{Action: "role.create", TargetType: "role", TargetID: x.ID, Detail: map[string]any{"name": x.Name, "policy_ids": x.PolicyIDs}})
	h.writeRole(w, r, http.StatusCreated, x)
}

func (h *handler) updateRole(w http.ResponseWriter, r *http.Request) {
	var in roleInput
	if !decode(w, r, &in) {
		return
	}
	ctx := r.Context()
	old, err := h.d.Access.GetRole(ctx, r.PathValue("id"))
	if err == nil && old.Managed {
		err = forbidden("managed role %q is read-only", old.ID)
	}
	var oldActions []access.Action
	if err == nil {
		oldActions, err = h.d.Access.RoleActions(ctx, old.ID)
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	name, ids, err := h.authorRole(r, in, oldActions)
	if err != nil {
		fail(w, r, err)
		return
	}
	old.Name, old.PolicyIDs = name, ids
	if err := h.d.Access.UpdateRole(ctx, old); err != nil {
		fail(w, r, err)
		return
	}
	h.record(r, audit.Event{Action: "role.update", TargetType: "role", TargetID: old.ID, Detail: map[string]any{"name": old.Name, "policy_ids": old.PolicyIDs}})
	h.writeRole(w, r, http.StatusOK, old)
}

func (h *handler) deleteRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	old, err := h.d.Access.GetRole(ctx, r.PathValue("id"))
	if err == nil && !old.Managed {
		var acts []access.Action
		if acts, err = h.d.Access.RoleActions(ctx, old.ID); err == nil && !session.Grants(ctx).CanAuthor(acts) {
			err = forbidden("you can only delete roles with actions you hold on every project")
		}
	}
	if err == nil {
		err = h.d.Access.DeleteRole(ctx, old.ID) // managed → ErrForbidden, assigned → ErrConflict
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	h.record(r, audit.Event{Action: "role.delete", TargetType: "role", TargetID: old.ID, Detail: map[string]any{"name": old.Name}})
	w.WriteHeader(http.StatusNoContent)
}

type assignmentView struct {
	ID        string  `json:"id"`
	ActorID   string  `json:"actor_id"`
	ActorName string  `json:"actor_name"`
	RoleID    string  `json:"role_id"`
	RoleName  string  `json:"role_name"`
	ProjectID *string `json:"project_id"`
	CreatedAt string  `json:"created_at"`
}

// assignmentViews shapes as with actor and role names.
func (h *handler) assignmentViews(ctx context.Context, as []access.Assignment) ([]assignmentView, error) {
	roles, err := h.d.Access.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	roleName := map[string]string{}
	for _, x := range roles {
		roleName[x.ID] = x.Name
	}
	names := actor.NewNameResolver(h.d.Actors)
	out := make([]assignmentView, len(as))
	for i, a := range as {
		out[i] = assignmentView{ID: a.ID, ActorID: a.ActorID, ActorName: names.Name(ctx, a.ActorID), RoleID: a.RoleID,
			RoleName: roleName[a.RoleID], ProjectID: a.ProjectID, CreatedAt: view.TS(a.CreatedAt)}
	}
	return out, nil
}

func scope(a access.Assignment) string {
	if a.ProjectID == nil {
		return ""
	}
	return *a.ProjectID
}

// GET /admin/assignments?actor=&project=: the assignments the caller may manage.
func (h *handler) listAssignments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	as, err := h.d.Access.ListAssignments(ctx, access.AssignmentFilter{ActorID: q.Get("actor"), ProjectID: q.Get("project")})
	if err != nil {
		fail(w, r, err)
		return
	}
	g := session.Grants(ctx)
	as = slices.DeleteFunc(as, func(a access.Assignment) bool { return !g.Can(access.MemberManage, scope(a)) })
	out, err := h.assignmentViews(ctx, as)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// checkAssign is spec §6 for giving or taking away a.
func (h *handler) checkAssign(ctx context.Context, a access.Assignment) error {
	acts, err := h.d.Access.RoleActions(ctx, a.RoleID)
	if err != nil {
		return err
	}
	if !session.Grants(ctx).CanAssign(acts, scope(a)) {
		return forbidden("you can only assign roles whose actions you hold, with member:Manage, on that scope")
	}
	return nil
}

func (h *handler) recordAssignment(r *http.Request, action string, a access.Assignment) {
	e := audit.Event{Action: action, TargetType: "assignment", TargetID: a.ID, Detail: map[string]any{"actor_id": a.ActorID, "role_id": a.RoleID}}
	if a.ProjectID != nil {
		e.ProjectID = *a.ProjectID
	}
	h.record(r, e)
}

func (h *handler) createAssignment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ActorID   string  `json:"actor_id"`
		RoleID    string  `json:"role_id"`
		ProjectID *string `json:"project_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	ctx := r.Context()
	a := access.NewAssignment(in.ActorID, in.RoleID, in.ProjectID, me(r))
	if err := h.checkAssign(ctx, a); err != nil {
		fail(w, r, err)
		return
	}
	if err := h.d.Access.CreateAssignment(ctx, a); err != nil {
		fail(w, r, err)
		return
	}
	h.recordAssignment(r, "assignment.create", a)
	out, err := h.assignmentViews(ctx, []access.Assignment{a})
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out[0])
}

func (h *handler) deleteAssignment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, err := h.d.Access.GetAssignment(ctx, r.PathValue("id"))
	if err == nil {
		err = h.checkAssign(ctx, a)
	}
	if err == nil {
		err = h.d.Access.DeleteAssignment(ctx, a.ID) // ErrLastAdmin when it would leave no admin
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	h.recordAssignment(r, "assignment.delete", a)
	w.WriteHeader(http.StatusNoContent)
}

// canActOn is the account takeover guard: acting on another actor's account
// needs every right that actor holds.
func (h *handler) canActOn(ctx context.Context, actorID string) error {
	target, err := h.d.Access.Grants(ctx, actorID)
	if err != nil {
		return err
	}
	if !session.Grants(ctx).Covers(target) {
		return forbidden("that actor holds rights you do not have")
	}
	return nil
}
