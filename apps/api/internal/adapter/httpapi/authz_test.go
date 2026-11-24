package httpapi

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/project"
)

// grant gives actorID roleID on project ("" = every project).
func (e *env) grant(t *testing.T, actorID, roleID, project string) access.Assignment {
	t.Helper()
	var p *string
	if project != "" {
		p = &project
	}
	a := access.NewAssignment(actorID, roleID, p, actorID)
	if err := e.access.CreateAssignment(context.Background(), a); err != nil {
		t.Fatalf("grant %s on %q: %v", roleID, project, err)
	}
	return a
}

// customRole creates a role holding one custom policy with actions.
func (e *env) customRole(t *testing.T, name string, actions ...access.Action) string {
	t.Helper()
	ctx := context.Background()
	p := access.Policy{ID: id.New(), Name: name + "-policy", Actions: actions, CreatedAt: time.Now()}
	if err := e.access.CreatePolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	r := access.Role{ID: id.New(), Name: name, PolicyIDs: []string{p.ID}, CreatedAt: time.Now()}
	if err := e.access.CreateRole(ctx, r); err != nil {
		t.Fatal(err)
	}
	return r.ID
}

func (e *env) project(t *testing.T, ids ...string) {
	t.Helper()
	for _, p := range ids {
		if _, err := project.Create(context.Background(), e.h.d.Projects, p, "", ""); err != nil && !errors.Is(err, project.ErrAlreadyExists) {
			t.Fatal(err)
		}
	}
}

// loggedIn is a client logged in as a fresh Human with no role.
func (e *env) loggedIn(t *testing.T, username string) (*client, string) {
	t.Helper()
	a := e.human(t, username, false, false)
	c := e.client(t)
	wantStatus(t, c.login(username, testPassword), 200)
	return c, a.ID
}

// Only session, login, setup, logout and password change work without a
// role; every other route needs a session (401 without one).
func TestRoutes_EveryRouteNeedsLogin(t *testing.T) {
	e := newEnv(t, "")
	open := map[string]bool{"GET /session": true, "POST /session": true, "POST /setup": true}
	wild := regexp.MustCompile(`\{[^}]+\}`)
	anon := e.client(t)
	for _, rt := range e.h.table() {
		if open[rt.pattern] {
			continue
		}
		method, path, _ := strings.Cut(rt.pattern, " ")
		r := anon.do(method, wild.ReplaceAllString(path, "x"), "{}", nil)
		if r.status != 401 {
			t.Errorf("%s anonymous = %d %s, want 401", rt.pattern, r.status, r.raw)
		}
	}
	for _, p := range []string{"/projects", "/projects/x/contributions", "/projects/x/contributions/y", "/projects/x/search?q=a"} {
		wantErr(t, anon.do("GET", p, nil, nil), 401, "unauthenticated")
	}
}

func TestProjectRead_NoGrantIs404(t *testing.T) {
	e := newEnv(t, "")
	boss := e.human(t, "boss", true, false)
	cid := e.contrib(t, "p1", boss.ID, contribution.KindClaim, "hello world", "")
	c, _ := e.loggedIn(t, "nobody")
	if its := items(t, c.do("GET", "/projects", nil, nil)); len(its) != 0 {
		t.Fatalf("projects = %v, want none", its)
	}
	wantErr(t, c.do("GET", "/projects/p1/contributions", nil, nil), 404, "not_found")
	wantErr(t, c.do("GET", "/projects/p1/contributions/"+cid, nil, nil), 404, "not_found")
	wantErr(t, c.do("GET", "/projects/p1/search?q=hello", nil, nil), 404, "not_found")
	wantErr(t, c.do("POST", "/projects/p1/contributions", map[string]string{"title": "t", "body": "b"}, nil), 404, "not_found")
}

func TestProjectRead_ViewerCannotWrite(t *testing.T) {
	e := newEnv(t, "")
	boss := e.human(t, "boss", true, false)
	cid := e.contrib(t, "p1", boss.ID, contribution.KindClaim, "hello world", "")
	e.contrib(t, "p2", boss.ID, contribution.KindClaim, "other", "")
	c, me := e.loggedIn(t, "vera")
	e.grant(t, me, "viewer", "p1")

	its := items(t, c.do("GET", "/projects", nil, nil))
	if len(its) != 1 || its[0]["id"] != "p1" {
		t.Fatalf("projects = %v, want only p1", its)
	}
	wantStatus(t, c.do("GET", "/projects/p1/contributions", nil, nil), 200)
	wantStatus(t, c.do("GET", "/projects/p1/contributions/"+cid, nil, nil), 200)
	wantStatus(t, c.do("GET", "/projects/p1/search?q=hello", nil, nil), 200)
	wantErr(t, c.do("POST", "/projects/p1/contributions", map[string]string{"title": "t", "body": "b"}, nil), 403, "forbidden")
	wantErr(t, c.do("POST", "/projects/p1/contributions/"+cid+"/signals", map[string]string{"kind": "assessment", "stance": "favorable"}, nil), 403, "forbidden")
	wantErr(t, c.do("GET", "/projects/p2/contributions", nil, nil), 404, "not_found")
}

func TestProjectWrite_ContributorOnOneProject(t *testing.T) {
	e := newEnv(t, "")
	boss := e.human(t, "boss", true, false)
	cid := e.contrib(t, "p1", boss.ID, contribution.KindClaim, "hello world", "")
	c, me := e.loggedIn(t, "cora")
	e.grant(t, me, "contributor", "p1")
	wantStatus(t, c.do("POST", "/projects/p1/contributions", map[string]string{"title": "mine", "body": "b"}, nil), 201)
	wantStatus(t, c.do("POST", "/projects/p1/contributions/"+cid+"/signals", map[string]string{"kind": "assessment", "stance": "favorable"}, nil), 201)
}

func TestCreateContribution_UnknownProjectIs404(t *testing.T) {
	e := newEnv(t, "")
	c, me := e.loggedIn(t, "cora")
	e.grant(t, me, "contributor", "")
	e.grant(t, me, e.customRole(t, "creator", access.ProjectCreate), "")
	wantErr(t, c.do("POST", "/projects/pnew/contributions", map[string]string{"title": "t", "body": "b"}, nil), 404, "not_found")
	if _, err := e.h.d.Projects.Get(context.Background(), "pnew"); err == nil {
		t.Fatal("project was auto-created")
	}
	e.project(t, "pnew")
	wantStatus(t, c.do("POST", "/projects/pnew/contributions", map[string]string{"title": "t", "body": "b"}, nil), 201)
}

func TestProjectRoutes_UnknownProjectIs404EvenForAdmin(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	wantErr(t, c.do("GET", "/projects/nope/contributions", nil, nil), 404, "not_found")
	wantErr(t, c.do("GET", "/projects/nope/search?q=x", nil, nil), 404, "not_found")
	wantErr(t, c.do("POST", "/projects/nope/contributions", map[string]string{"title": "t"}, nil), 404, "not_found")
}

func TestSystemRoutes_NeedTheirAction(t *testing.T) {
	e := newEnv(t, "")
	c, me := e.loggedIn(t, "sam")
	e.grant(t, me, e.customRole(t, "settings-only", access.SettingsManage), "")
	wantStatus(t, c.do("GET", "/admin/settings/search", nil, nil), 200)
	wantErr(t, c.do("GET", "/admin/actors", nil, nil), 403, "forbidden")
	wantErr(t, c.do("GET", "/admin/overview", nil, nil), 403, "forbidden")
	wantErr(t, c.do("POST", "/admin/projects", map[string]string{"id": "px"}, nil), 403, "forbidden")

	// A system action granted on one project grants nothing.
	c2, me2 := e.loggedIn(t, "pat")
	e.project(t, "p1")
	e.grant(t, me2, e.customRole(t, "projects-on-p1", access.ProjectCreate), "p1")
	wantErr(t, c2.do("POST", "/admin/projects", map[string]string{"id": "py"}, nil), 403, "forbidden")
}

func TestOverview_FilteredToReadable(t *testing.T) {
	e := newEnv(t, "")
	boss := e.human(t, "boss", true, false)
	e.contrib(t, "p1", boss.ID, contribution.KindClaim, "one", "")
	e.contrib(t, "p2", boss.ID, contribution.KindClaim, "two", "")
	c, me := e.loggedIn(t, "olga")
	e.grant(t, me, e.customRole(t, "actors", access.ActorManage), "")
	e.grant(t, me, "viewer", "p1")
	r := c.do("GET", "/admin/overview", nil, nil)
	wantStatus(t, r, 200)
	recent, _ := r.body["recent"].([]any)
	if r.body["project_count"] != float64(1) || r.body["contribution_count"] != float64(1) || len(recent) != 1 ||
		recent[0].(map[string]any)["project_id"] != "p1" {
		t.Fatalf("overview = %s", r.raw)
	}
	states, _ := r.body["states"].(map[string]any)
	total := 0.0
	for _, n := range states {
		total += n.(float64)
	}
	if total != 1 {
		t.Fatalf("states count other projects: %s", r.raw)
	}
}

func TestGrants_ReloadedEachRequest(t *testing.T) {
	e := newEnv(t, "")
	e.project(t, "p1")
	c, me := e.loggedIn(t, "rita")
	a := e.grant(t, me, "viewer", "p1")
	wantStatus(t, c.do("GET", "/projects/p1/contributions", nil, nil), 200)
	if err := e.access.DeleteAssignment(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	wantErr(t, c.do("GET", "/projects/p1/contributions", nil, nil), 404, "not_found")
}
