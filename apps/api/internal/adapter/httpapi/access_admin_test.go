package httpapi

import (
	"context"
	"slices"
	"testing"

	"agent-verdicts/internal/access"
)

func strs(v any) []string {
	var out []string
	l, _ := v.([]any)
	for _, x := range l {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

func TestPolicyCRUD(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	r := c.do("POST", "/admin/policies", map[string]any{"name": " Reviewer ", "actions": []string{"signal:Create", "contribution:Read", "signal:Create"}}, nil)
	wantStatus(t, r, 201)
	id, _ := r.body["id"].(string)
	if r.body["name"] != "Reviewer" || r.body["managed"] != false || !slices.Equal(strs(r.body["actions"]), []string{"contribution:Read", "signal:Create"}) {
		t.Fatalf("create = %s", r.raw)
	}
	wantErr(t, c.do("POST", "/admin/policies", map[string]any{"name": "Reviewer", "actions": []string{"contribution:Read"}}, nil), 409, "conflict")
	wantErr(t, c.do("POST", "/admin/policies", map[string]any{"name": "Star", "actions": []string{"*"}}, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/admin/policies", map[string]any{"name": "Empty", "actions": []string{}}, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/admin/policies", map[string]any{"name": "", "actions": []string{"contribution:Read"}}, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/admin/policies", map[string]any{"name": "Bad", "actions": []string{"contribution:Delete"}}, nil), 400, "invalid_argument")

	r = c.do("PUT", "/admin/policies/"+id, map[string]any{"name": "Reader", "actions": []string{"contribution:Read"}}, nil)
	wantStatus(t, r, 200)
	if r.body["name"] != "Reader" || !slices.Equal(strs(r.body["actions"]), []string{"contribution:Read"}) {
		t.Fatalf("update = %s", r.raw)
	}
	wantErr(t, c.do("PUT", "/admin/policies/ReadOnlyAccess", map[string]any{"name": "x", "actions": []string{"contribution:Read"}}, nil), 403, "forbidden")
	wantErr(t, c.do("DELETE", "/admin/policies/ReadOnlyAccess", nil, nil), 403, "forbidden")
	wantErr(t, c.do("PUT", "/admin/policies/nope", map[string]any{"name": "x", "actions": []string{"contribution:Read"}}, nil), 404, "not_found")

	list := items(t, c.do("GET", "/admin/policies", nil, nil))
	if len(list) != 5 || list[0]["managed"] != true {
		t.Fatalf("policies = %v", list)
	}

	r = c.do("POST", "/admin/roles", map[string]any{"name": "reader", "policy_ids": []string{id}}, nil)
	wantStatus(t, r, 201)
	wantErr(t, c.do("DELETE", "/admin/policies/"+id, nil, nil), 409, "conflict") // in a role
	wantStatus(t, c.do("DELETE", "/admin/roles/"+r.body["id"].(string), nil, nil), 204)
	wantStatus(t, c.do("DELETE", "/admin/policies/"+id, nil, nil), 204)
	wantErr(t, c.do("DELETE", "/admin/policies/"+id, nil, nil), 404, "not_found")
}

func TestRoleCRUD(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	r := c.do("POST", "/admin/roles", map[string]any{"name": "helper", "policy_ids": []string{"ReadOnlyAccess", "ContributorAccess"}}, nil)
	wantStatus(t, r, 201)
	id, _ := r.body["id"].(string)
	if r.body["managed"] != false || !slices.Equal(strs(r.body["actions"]), []string{"contribution:Read", "contribution:Create", "signal:Create"}) {
		t.Fatalf("create = %s", r.raw)
	}
	wantErr(t, c.do("POST", "/admin/roles", map[string]any{"name": "none", "policy_ids": []string{}}, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/admin/roles", map[string]any{"name": "ghost", "policy_ids": []string{"nope"}}, nil), 404, "not_found")
	wantErr(t, c.do("POST", "/admin/roles", map[string]any{"name": "sneaky-admin", "policy_ids": []string{"AdministratorAccess"}}, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/admin/roles", map[string]any{"name": "helper", "policy_ids": []string{"ReadOnlyAccess"}}, nil), 409, "conflict")

	r = c.do("PUT", "/admin/roles/"+id, map[string]any{"name": "helper2", "policy_ids": []string{"ReadOnlyAccess"}}, nil)
	wantStatus(t, r, 200)
	if r.body["name"] != "helper2" || !slices.Equal(strs(r.body["policy_ids"]), []string{"ReadOnlyAccess"}) {
		t.Fatalf("update = %s", r.raw)
	}
	wantErr(t, c.do("PUT", "/admin/roles/viewer", map[string]any{"name": "viewer", "policy_ids": []string{"ContributorAccess"}}, nil), 403, "forbidden")
	wantErr(t, c.do("DELETE", "/admin/roles/admin", nil, nil), 403, "forbidden")

	roles := items(t, c.do("GET", "/admin/roles", nil, nil))
	if len(roles) != 5 {
		t.Fatalf("roles = %v", roles)
	}
	for _, x := range roles {
		if x["id"] == "admin" && !slices.Equal(strs(x["actions"]), []string{"*"}) {
			t.Fatalf("admin role = %v", x)
		}
	}
}

func TestAuthorEscalation(t *testing.T) {
	e := newEnv(t, "")
	c, me := e.loggedIn(t, "pam")
	e.grant(t, me, e.customRole(t, "policy-author", access.PolicyManage, access.ContributionRead), "")
	wantStatus(t, c.do("POST", "/admin/policies", map[string]any{"name": "ok", "actions": []string{"contribution:Read"}}, nil), 201)
	wantErr(t, c.do("POST", "/admin/policies", map[string]any{"name": "more", "actions": []string{"signal:Create"}}, nil), 403, "forbidden")
	wantErr(t, c.do("POST", "/admin/roles", map[string]any{"name": "more", "policy_ids": []string{"ContributorAccess"}}, nil), 403, "forbidden")
	wantStatus(t, c.do("POST", "/admin/roles", map[string]any{"name": "readers", "policy_ids": []string{"ReadOnlyAccess"}}, nil), 201)

	// Without policy:Manage nothing can be authored.
	c2, me2 := e.loggedIn(t, "max")
	e.grant(t, me2, "viewer", "")
	wantErr(t, c2.do("POST", "/admin/policies", map[string]any{"name": "x", "actions": []string{"contribution:Read"}}, nil), 403, "forbidden")
	wantErr(t, c2.do("GET", "/admin/policies", nil, nil), 403, "forbidden")
}

func TestAssign_ManagerScoped(t *testing.T) {
	e := newEnv(t, "")
	_, bossID := e.admin(t)
	e.project(t, "p1", "p2")
	c, me := e.loggedIn(t, "mona")
	e.grant(t, me, "manager", "p1")
	target := e.human(t, "tom", false, false)
	bossOnP1 := e.grant(t, bossID, "viewer", "p1")

	wantStatus(t, c.do("GET", "/admin/roles", nil, nil), 200)  // to pick a role
	wantStatus(t, c.do("GET", "/admin/actors", nil, nil), 200) // to pick an actor
	r := c.do("POST", "/admin/assignments", map[string]any{"actor_id": target.ID, "role_id": "viewer", "project_id": "p1"}, nil)
	wantStatus(t, r, 201)
	if r.body["actor_name"] != "Name tom" || r.body["role_name"] != "viewer" || r.body["project_id"] != "p1" {
		t.Fatalf("assignment = %s", r.raw)
	}
	for name, body := range map[string]map[string]any{
		"other project": {"actor_id": target.ID, "role_id": "viewer", "project_id": "p2"},
		"every project": {"actor_id": target.ID, "role_id": "viewer", "project_id": nil},
		"admin":         {"actor_id": target.ID, "role_id": "admin", "project_id": "p1"},
	} {
		if r := c.do("POST", "/admin/assignments", body, nil); r.status != 403 {
			t.Errorf("%s: %d %s, want 403", name, r.status, r.raw)
		}
	}
	creator := e.customRole(t, "creator", access.ContributionRead, access.ProjectCreate)
	wantErr(t, c.do("POST", "/admin/assignments", map[string]any{"actor_id": target.ID, "role_id": creator, "project_id": "p1"}, nil), 403, "forbidden")
	// Removing is checked like granting; viewer on p1 is within mona's reach.
	wantStatus(t, c.do("DELETE", "/admin/assignments/"+bossOnP1.ID, nil, nil), 204)
	adminRow, _ := e.access.ListAssignments(context.Background(), access.AssignmentFilter{ActorID: bossID})
	wantErr(t, c.do("DELETE", "/admin/assignments/"+adminRow[0].ID, nil, nil), 403, "forbidden")

	// A caller who manages nobody cannot list or assign.
	c2, _ := e.loggedIn(t, "nina")
	wantErr(t, c2.do("GET", "/admin/assignments", nil, nil), 403, "forbidden")
	wantErr(t, c2.do("GET", "/admin/roles", nil, nil), 403, "forbidden")
	wantErr(t, c2.do("POST", "/admin/assignments", map[string]any{"actor_id": target.ID, "role_id": "viewer", "project_id": "p1"}, nil), 403, "forbidden")
}

func TestAssign_Errors(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	e.project(t, "p1")
	target := e.human(t, "tom", false, false)
	body := map[string]any{"actor_id": target.ID, "role_id": "viewer", "project_id": "p1"}
	wantStatus(t, c.do("POST", "/admin/assignments", body, nil), 201)
	wantErr(t, c.do("POST", "/admin/assignments", body, nil), 409, "conflict")
	wantErr(t, c.do("POST", "/admin/assignments", map[string]any{"actor_id": target.ID, "role_id": "viewer", "project_id": "nope"}, nil), 404, "not_found")
	wantErr(t, c.do("POST", "/admin/assignments", map[string]any{"actor_id": target.ID, "role_id": "nope", "project_id": nil}, nil), 404, "not_found")
	wantErr(t, c.do("POST", "/admin/assignments", map[string]any{"actor_id": "00000000-0000-4000-8000-000000000000", "role_id": "viewer", "project_id": nil}, nil), 404, "not_found")
	wantErr(t, c.do("DELETE", "/admin/assignments/00000000-0000-4000-8000-000000000000", nil, nil), 404, "not_found")
}

func TestAssignments_ListScoped(t *testing.T) {
	e := newEnv(t, "")
	ac, bossID := e.admin(t)
	e.project(t, "p1", "p2")
	c, me := e.loggedIn(t, "mona")
	e.grant(t, me, "manager", "p1")
	tom := e.human(t, "tom", false, false)
	e.grant(t, tom.ID, "viewer", "p1")
	e.grant(t, tom.ID, "viewer", "p2")
	for _, it := range items(t, c.do("GET", "/admin/assignments", nil, nil)) {
		if it["project_id"] != "p1" {
			t.Fatalf("manager of p1 sees %v", it)
		}
	}
	if n := len(items(t, c.do("GET", "/admin/assignments?project=p1", nil, nil))); n != 2 { // mona + tom
		t.Fatalf("p1 assignments = %d", n)
	}
	all := items(t, ac.do("GET", "/admin/assignments?actor="+tom.ID, nil, nil))
	if len(all) != 2 {
		t.Fatalf("admin sees %v", all)
	}
	if items(t, ac.do("GET", "/admin/assignments?actor="+bossID, nil, nil))[0]["project_id"] != nil {
		t.Fatal("admin assignment must have project_id null")
	}
}

func TestLastAdmin(t *testing.T) {
	e := newEnv(t, "")
	c, bossID := e.admin(t)
	rows, _ := e.access.ListAssignments(context.Background(), access.AssignmentFilter{ActorID: bossID})
	wantErr(t, c.do("DELETE", "/admin/assignments/"+rows[0].ID, nil, nil), 409, "conflict")

	// A second admin can disable the first; then the second is the last one.
	b := e.human(t, "bea", true, false)
	cb := e.client(t)
	wantStatus(t, cb.login("bea", testPassword), 200)
	wantStatus(t, cb.do("PATCH", "/admin/actors/"+bossID, map[string]any{"disabled": true}, nil), 200)
	bRows, _ := e.access.ListAssignments(context.Background(), access.AssignmentFilter{ActorID: b.ID})
	wantErr(t, cb.do("DELETE", "/admin/assignments/"+bRows[0].ID, nil, nil), 409, "conflict")

	// actor:Manage alone cannot disable the last admin.
	ops, me := e.loggedIn(t, "ops")
	e.grant(t, me, e.customRole(t, "ops", access.ActorManage), "")
	wantErr(t, ops.do("PATCH", "/admin/actors/"+b.ID, map[string]any{"disabled": true}, nil), 403, "forbidden")
}

// Acting on an account with more rights than your own is refused.
func TestActorManage_CannotTakeOverStrongerAccounts(t *testing.T) {
	e := newEnv(t, "")
	_, bossID := e.admin(t)
	ops, me := e.loggedIn(t, "ops")
	e.grant(t, me, e.customRole(t, "ops", access.ActorManage), "")
	wantErr(t, ops.do("POST", "/admin/actors/"+bossID+"/password", map[string]any{}, nil), 403, "forbidden")
	wantErr(t, ops.do("PATCH", "/admin/actors/"+bossID, map[string]any{"name": "pwned"}, nil), 403, "forbidden")
	weak := e.human(t, "weak", false, false)
	wantStatus(t, ops.do("POST", "/admin/actors/"+weak.ID+"/password", map[string]any{}, nil), 200)

	r := ops.do("POST", "/admin/actors", map[string]any{"type": "Agent", "name": "Bot"}, nil)
	wantStatus(t, r, 201)
	botID := r.body["id"].(string)
	e.grant(t, botID, "contributor", "")
	wantErr(t, ops.do("POST", "/admin/actors/"+botID+"/credentials", nil, nil), 403, "forbidden")
}

func TestSession_Access(t *testing.T) {
	e := newEnv(t, "")
	ac, _ := e.admin(t)
	r := ac.do("GET", "/session", nil, nil)
	acc, _ := r.body["access"].(map[string]any)
	if !slices.Equal(strs(acc["all"]), []string{"*"}) {
		t.Fatalf("admin session = %s", r.raw)
	}
	if act, _ := r.body["actor"].(map[string]any); act["is_admin"] != nil {
		t.Fatalf("is_admin still in session: %s", r.raw)
	}
	e.project(t, "p1")
	c, me := e.loggedIn(t, "vera")
	e.grant(t, me, "viewer", "p1")
	r = c.do("GET", "/session", nil, nil)
	acc, _ = r.body["access"].(map[string]any)
	projects, _ := acc["projects"].(map[string]any)
	if len(strs(acc["all"])) != 0 || acc["all"] == nil || !slices.Equal(strs(projects["p1"]), []string{"contribution:Read"}) {
		t.Fatalf("viewer session = %s", r.raw)
	}
	if r := e.client(t).do("GET", "/session", nil, nil); r.body["access"] != nil {
		t.Fatalf("anonymous session has access: %s", r.raw)
	}
}

func TestListActors_MemberManagerSeesNoLogins(t *testing.T) {
	e := newEnv(t, "")
	e.admin(t)
	e.project(t, "p1")
	c, me := e.loggedIn(t, "mona")
	e.grant(t, me, "manager", "p1")
	for _, it := range items(t, c.do("GET", "/admin/actors", nil, nil)) {
		if it["username"] != nil || it["has_login"] != false {
			t.Fatalf("member manager sees login details: %v", it)
		}
	}
	wantErr(t, c.do("GET", "/admin/actors/"+me, nil, nil), 403, "forbidden")
}

func TestActorDetail_ListsAssignments(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	e.project(t, "p1")
	tom := e.human(t, "tom", false, false)
	e.grant(t, tom.ID, "viewer", "p1")
	r := c.do("GET", "/admin/actors/"+tom.ID, nil, nil)
	as, _ := r.body["assignments"].([]any)
	if len(as) != 1 || as[0].(map[string]any)["role_id"] != "viewer" {
		t.Fatalf("detail = %s", r.raw)
	}
}
