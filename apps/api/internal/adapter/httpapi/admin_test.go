package httpapi

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/signal"
)

// admin logs in a fresh admin "boss" and returns its client and ID.
func (e *env) admin(t *testing.T) (*client, string) {
	t.Helper()
	a := e.human(t, "boss", true, false)
	c := e.client(t)
	wantStatus(t, c.login("boss", testPassword), 200)
	return c, a.ID
}

// adminRoutes is every /admin route in the table, wildcards filled with "x".
func adminRoutes(t *testing.T, h *handler) [][2]string {
	wild := regexp.MustCompile(`\{[^}]+\}`)
	var out [][2]string
	for _, rt := range h.table() {
		method, path, _ := strings.Cut(rt.pattern, " ")
		if strings.HasPrefix(path, "/admin/") {
			out = append(out, [2]string{method, wild.ReplaceAllString(path, "x")})
		}
	}
	if len(out) < 13 {
		t.Fatalf("only %d admin routes in the table", len(out))
	}
	return out
}

func TestAdminRoutes_NonAdmin403Anonymous401(t *testing.T) {
	e := newEnv(t, "")
	e.human(t, "plain", false, false)
	c := e.client(t)
	wantStatus(t, c.login("plain", testPassword), 200)
	anon := e.client(t)
	for _, rt := range adminRoutes(t, e.h) {
		r := c.do(rt[0], rt[1], "{}", nil)
		if rt[1] == "/admin/projects" && rt[0] == "GET" { // the readable projects: none
			continue
		}
		if m, _ := r.body["message"].(string); r.status != 403 || !strings.HasSuffix(m, " required") {
			t.Errorf("no-role %s %s = %d %s, want 403 <action> required", rt[0], rt[1], r.status, r.raw)
		}
		wantErr(t, anon.do(rt[0], rt[1], "{}", nil), 401, "unauthenticated")
	}
}

func TestAdminActors_CreateHuman_PasswordOnceAndLogin(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	r := c.do("POST", "/admin/actors", map[string]any{"type": "Human", "name": " Carol ", "username": "Carol", "must_change": true}, nil)
	wantStatus(t, r, 201)
	pw, _ := r.body["password"].(string)
	id, _ := r.body["id"].(string)
	if pw == "" || id == "" || r.body["name"] != "Carol" || r.body["username"] != "carol" ||
		r.body["type"] != "Human" || r.body["has_login"] != true || r.body["token"] != nil {
		t.Fatalf("create = %s", r.raw)
	}
	nc := e.client(t)
	if lr := nc.login("carol", pw); lr.status != 200 || lr.body["must_change_password"] != true {
		t.Fatalf("login with generated password = %d %s", lr.status, lr.raw)
	}
	g := c.do("GET", "/admin/actors/"+id, nil, nil)
	wantStatus(t, g, 200)
	if strings.Contains(g.raw, pw) || g.body["password"] != nil || g.body["username"] != "carol" {
		t.Fatalf("GET actor leaks or misses fields: %s", g.raw)
	}
	l := c.do("GET", "/admin/actors", nil, nil)
	if strings.Contains(l.raw, pw) {
		t.Fatal("list leaks password")
	}

	// A chosen password is used as given.
	r = c.do("POST", "/admin/actors", map[string]any{"type": "Human", "name": "Dan", "username": "dan", "password": "dans-own-password"}, nil)
	wantStatus(t, r, 201)
	if r.body["password"] != "dans-own-password" {
		t.Fatalf("create = %s", r.raw)
	}
	wantStatus(t, e.client(t).login("dan", "dans-own-password"), 200)
}

func TestAdminActors_CreateValidation(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	before, _ := e.actors.List(context.Background())
	for _, body := range []map[string]any{
		{"type": "Human", "name": "No Username"},
		{"type": "Human", "name": "Bad", "username": "x"},
		{"type": "Human", "name": "Short", "username": "short", "password": "short"},
		{"type": "Human", "name": "  ", "username": "blank"},
		{"type": "Robot", "name": "R"},
		{"type": "Agent", "name": "A", "username": "agent"},
		{"type": "Agent", "name": "A", "password": "agent-password"},
		{"type": "Agent", "name": "A", "must_change": true},
		{"type": "Agent", "name": "A", "is_admin": true},
		{"type": "Human", "name": "H", "username": "hadmin", "is_admin": true}, // admin is a role now
	} {
		if r := c.do("POST", "/admin/actors", body, nil); r.status != 400 || r.body["code"] != "invalid_argument" {
			t.Errorf("%v = %d %s, want 400", body, r.status, r.raw)
		}
	}
	wantErr(t, c.do("POST", "/admin/actors", map[string]any{"type": "Human", "name": "Dup", "username": "boss"}, nil), 409, "conflict")
	after, _ := e.actors.List(context.Background())
	if len(after) != len(before) {
		t.Fatalf("failed creates left %d actors behind", len(after)-len(before))
	}
}

func TestAdminActors_CreateAgent_TokenOnce(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	r := c.do("POST", "/admin/actors", map[string]any{"type": "Agent", "name": "Bot"}, nil)
	wantStatus(t, r, 201)
	tok, _ := r.body["token"].(string)
	id, _ := r.body["id"].(string)
	if tok == "" || r.body["password"] != nil || r.body["username"] != nil || r.body["has_login"] != false {
		t.Fatalf("create agent = %s", r.raw)
	}
	creds := postgres.NewCredentialRepo(e.pool)
	if got, err := authentication.Verify(context.Background(), e.actors, creds, tok); err != nil || got != id {
		t.Fatalf("Verify = %q, %v", got, err)
	}
	credID, secret, _ := strings.Cut(tok, ".")
	g := c.do("GET", "/admin/actors/"+id, nil, nil)
	wantStatus(t, g, 200)
	if strings.Contains(g.raw, secret) {
		t.Fatalf("GET actor leaks the token: %s", g.raw)
	}
	list, _ := g.body["credentials"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["id"] != credID || list[0].(map[string]any)["revoked_at"] != nil {
		t.Fatalf("credentials = %s", g.raw)
	}
}

func TestAdminActors_ListShape(t *testing.T) {
	e := newEnv(t, "")
	c, bossID := e.admin(t)
	c.do("POST", "/admin/actors", map[string]any{"type": "Agent", "name": "Bot"}, nil)
	r := c.do("GET", "/admin/actors", nil, nil)
	wantStatus(t, r, 200)
	its := items(t, r)
	if len(its) != 2 {
		t.Fatalf("items = %s", r.raw)
	}
	byType := map[string]map[string]any{}
	for _, it := range its {
		byType[it["type"].(string)] = it
		for _, k := range []string{"id", "type", "name", "disabled", "created_at", "username", "has_login"} {
			if _, ok := it[k]; !ok {
				t.Fatalf("item lacks %s: %v", k, it)
			}
		}
	}
	h, a := byType["Human"], byType["Agent"]
	if h["id"] != bossID || h["username"] != "boss" || h["has_login"] != true || h["disabled"] != false {
		t.Fatalf("human = %v", h)
	}
	if a["username"] != nil || a["has_login"] != false {
		t.Fatalf("agent = %v", a)
	}
	wantErr(t, c.do("GET", "/admin/actors/does-not-exist", nil, nil), 404, "not_found")
}

func TestAdminActors_Patch(t *testing.T) {
	e := newEnv(t, "")
	c, bossID := e.admin(t)
	target := e.human(t, "target", false, false)
	tc := e.client(t)
	wantStatus(t, tc.login("target", testPassword), 200)

	r := c.do("PATCH", "/admin/actors/"+target.ID, map[string]any{"name": "Renamed"}, nil)
	wantStatus(t, r, 200)
	if r.body["name"] != "Renamed" || r.body["disabled"] != false {
		t.Fatalf("patch = %s", r.raw)
	}
	wantErr(t, c.do("PATCH", "/admin/actors/"+target.ID, map[string]any{"is_admin": true}, nil), 400, "invalid_argument") // a role now
	wantErr(t, c.do("PATCH", "/admin/actors/"+target.ID, map[string]any{"name": " "}, nil), 400, "invalid_argument")

	r = c.do("PATCH", "/admin/actors/"+target.ID, map[string]any{"disabled": true}, nil)
	if r.body["disabled"] != true {
		t.Fatalf("disable = %s", r.raw)
	}
	wantErr(t, tc.do("GET", "/test/login", nil, nil), 401, "unauthenticated")
	wantErr(t, e.client(t).login("target", testPassword), 401, "unauthenticated")
	r = c.do("PATCH", "/admin/actors/"+target.ID, map[string]any{"disabled": false}, nil)
	if r.body["disabled"] != false {
		t.Fatalf("enable = %s", r.raw)
	}
	wantStatus(t, e.client(t).login("target", testPassword), 200)

	// No locking yourself out.
	wantErr(t, c.do("PATCH", "/admin/actors/"+bossID, map[string]any{"disabled": true}, nil), 400, "invalid_argument")
	got, _ := e.actors.Get(context.Background(), bossID)
	if got.Disabled() {
		t.Fatalf("admin locked themselves out: %+v", got)
	}
	wantErr(t, c.do("PATCH", "/admin/actors/nope", map[string]any{"name": "x"}, nil), 404, "not_found")
}

func TestAdminActors_ResetPassword(t *testing.T) {
	e := newEnv(t, "")
	c, bossID := e.admin(t)
	target := e.human(t, "target", false, false)
	r := c.do("POST", "/admin/actors/"+target.ID+"/password", map[string]any{"must_change": true}, nil)
	wantStatus(t, r, 200)
	pw, _ := r.body["password"].(string)
	if pw == "" {
		t.Fatalf("reset = %s", r.raw)
	}
	if lr := e.client(t).login("target", pw); lr.status != 200 || lr.body["must_change_password"] != true {
		t.Fatalf("login after reset = %s", lr.raw)
	}
	r = c.do("POST", "/admin/actors/"+target.ID+"/password", map[string]any{"password": "chosen-password-1"}, nil)
	if r.body["password"] != "chosen-password-1" {
		t.Fatalf("reset = %s", r.raw)
	}
	wantErr(t, c.do("POST", "/admin/actors/"+target.ID+"/password", map[string]any{"password": "short"}, nil), 400, "invalid_argument")

	// Resetting your own password never forces a change and keeps your session.
	r = c.do("POST", "/admin/actors/"+bossID+"/password", map[string]any{"must_change": true}, nil)
	wantStatus(t, r, 200)
	if l, _ := e.logins.GetByActor(context.Background(), bossID); l.MustChange {
		t.Fatal("self reset stored must-change")
	}
	wantStatus(t, c.do("GET", "/test/admin", nil, nil), 200)
	// Same through another textual form of the own uuid.
	r = c.do("POST", "/admin/actors/"+strings.ToUpper(bossID)+"/password", map[string]any{"must_change": true}, nil)
	wantStatus(t, r, 200)
	if l, _ := e.logins.GetByActor(context.Background(), bossID); l.MustChange {
		t.Fatal("self reset via uppercase id stored must-change")
	}
	wantStatus(t, c.do("GET", "/test/admin", nil, nil), 200)

	agent := c.do("POST", "/admin/actors", map[string]any{"type": "Agent", "name": "Bot"}, nil)
	wantErr(t, c.do("POST", "/admin/actors/"+agent.body["id"].(string)+"/password", map[string]any{}, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/admin/actors/nope/password", map[string]any{}, nil), 404, "not_found")
}

func TestAdminCredentials_IssueAndRevoke(t *testing.T) {
	e := newEnv(t, "")
	c, bossID := e.admin(t)
	agent := c.do("POST", "/admin/actors", map[string]any{"type": "Agent", "name": "Bot"}, nil)
	agentID := agent.body["id"].(string)

	r := c.do("POST", "/admin/actors/"+agentID+"/credentials", nil, nil)
	wantStatus(t, r, 201)
	tok, _ := r.body["token"].(string)
	credID, _ := r.body["id"].(string)
	if !strings.HasPrefix(tok, credID+".") {
		t.Fatalf("issue = %s", r.raw)
	}
	creds := postgres.NewCredentialRepo(e.pool)
	if _, err := authentication.Verify(context.Background(), e.actors, creds, tok); err != nil {
		t.Fatal(err)
	}
	_, secret, _ := strings.Cut(tok, ".")
	if g := c.do("GET", "/admin/actors/"+agentID, nil, nil); strings.Contains(g.raw, secret) || len(g.body["credentials"].([]any)) != 2 {
		t.Fatalf("GET actor = %s", g.raw)
	}

	wantStatus(t, c.do("DELETE", "/admin/credentials/"+credID, nil, nil), 204)
	if _, err := authentication.Verify(context.Background(), e.actors, creds, tok); err == nil {
		t.Fatal("revoked token still verifies")
	}
	wantStatus(t, c.do("DELETE", "/admin/credentials/"+credID, nil, nil), 204) // idempotent
	wantErr(t, c.do("DELETE", "/admin/credentials/nope", nil, nil), 404, "not_found")

	wantErr(t, c.do("POST", "/admin/actors/"+bossID+"/credentials", nil, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/admin/actors/nope/credentials", nil, nil), 404, "not_found")
}

func TestAdminProjects(t *testing.T) {
	e := newEnv(t, "")
	c, bossID := e.admin(t)
	r := c.do("POST", "/admin/projects", map[string]string{"id": " fresh "}, nil)
	wantStatus(t, r, 201)
	if r.body["id"] != "fresh" {
		t.Fatalf("create = %s", r.raw)
	}
	wantErr(t, c.do("POST", "/admin/projects", map[string]string{"id": "fresh"}, nil), 409, "conflict")
	wantErr(t, c.do("POST", "/admin/projects", map[string]string{"id": "  "}, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/admin/projects", map[string]string{"id": "a\x00b"}, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/admin/projects", map[string]string{"id": "Has Space"}, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/admin/projects", map[string]string{"id": strings.Repeat("p", 65)}, nil), 400, "invalid_argument")
	if r.body["name"] != "fresh" || r.body["description"] != "" {
		t.Fatalf("create defaults = %s", r.raw)
	}
	r2 := c.do("PATCH", "/admin/projects/fresh", map[string]string{"description": "about fresh"}, nil)
	wantStatus(t, r2, 200)
	if r2.body["name"] != "fresh" || r2.body["description"] != "about fresh" {
		t.Fatalf("patch = %s", r2.raw)
	}

	e.contrib(t, "fresh", bossID, contribution.KindQuestion, "open q", "")
	r = c.do("GET", "/admin/projects", nil, nil)
	wantStatus(t, r, 200)
	its := items(t, r)
	if len(its) != 1 || its[0]["id"] != "fresh" || its[0]["contribution_count"] != 1.0 || its[0]["open_question_count"] != 1.0 || its[0]["created_at"] == "" {
		t.Fatalf("list = %s", r.raw)
	}
}

func TestAdminOverview(t *testing.T) {
	e := newEnv(t, "")
	c, bossID := e.admin(t)
	empty := c.do("GET", "/admin/overview", nil, nil)
	wantStatus(t, empty, 200)
	if empty.body["project_count"] != 0.0 || empty.body["contribution_count"] != 0.0 {
		t.Fatalf("empty overview = %s", empty.raw)
	}
	if rec, ok := empty.body["recent"].([]any); !ok || len(rec) != 0 {
		t.Fatalf("empty recent must be []: %s", empty.raw)
	}

	fav := e.contrib(t, "a", bossID, contribution.KindClaim, "fav", "")
	e.signal(t, "a", fav, e.human(t, "fan", false, false).ID, signal.KindAssessment, signal.StanceFavorable) // not the author
	e.contrib(t, "a", bossID, contribution.KindQuestion, "q", "")
	var last string
	for i := 0; i < 11; i++ {
		last = e.contrib(t, "b", bossID, contribution.KindClaim, fmt.Sprintf("claim %d", i), "")
	}
	r := c.do("GET", "/admin/overview", nil, nil)
	wantStatus(t, r, 200)
	st, _ := r.body["states"].(map[string]any)
	if r.body["project_count"] != 2.0 || r.body["contribution_count"] != 13.0 || r.body["open_question_count"] != 1.0 ||
		st["Favorable"] != 1.0 || st["InsufficientEvidence"] != 11.0 || st["Unfavorable"] != 0.0 || st["Conflict"] != 0.0 {
		t.Fatalf("overview = %s", r.raw)
	}
	rec, _ := r.body["recent"].([]any)
	if len(rec) != 10 {
		t.Fatalf("recent has %d items, want 10", len(rec))
	}
	first := rec[0].(map[string]any)
	if first["id"] != last || first["project_id"] != "b" || first["actor_name"] != "Name boss" {
		t.Fatalf("recent[0] = %v", first)
	}
}

func TestProjectID_ControlCharacters400(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	wantErr(t, c.do("GET", "/projects/%00/contributions", nil, nil), 400, "invalid_argument")
	wantErr(t, c.do("GET", "/projects/a%01b/search?q=x", nil, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/projects/%00/contributions", map[string]string{"title": "t"}, nil), 400, "invalid_argument")
}

// When the login cannot be written after the actor is (a username taken by a
// concurrent create, a DB error), no enabled login-less person stays behind.
func TestAdminActors_CreateHuman_LoginFailureDisablesActor(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	e.logins.upsertBroken.Store(true)
	wantErr(t, c.do("POST", "/admin/actors", map[string]any{"type": "Human", "name": "Eve", "username": "eve"}, nil), 500, "internal")
	all, _ := e.actors.List(context.Background())
	for _, a := range all {
		if a.Name == "Eve" && !a.Disabled() {
			t.Fatalf("orphan actor %s is enabled", a.ID)
		}
	}
}
