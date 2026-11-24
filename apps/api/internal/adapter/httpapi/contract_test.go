package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"agent-verdicts/internal/adapter/authflow"
	"agent-verdicts/internal/reads"
	"agent-verdicts/internal/setting"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// api/openapi.yaml is the JSON API contract. These tests hold the handlers
// to it: every route is documented (and the reverse), and every request and
// response of a scenario touching every route validates against it.

const specPath = "../../../openapi.yaml"

func loadSpec(t *testing.T) (*openapi3.T, routers.Router) {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile(specPath)
	if err != nil {
		t.Fatalf("load %s: %v", specPath, err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("spec is not valid OpenAPI: %v", err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	return doc, router
}

func TestContract_RoutesMatchSpec(t *testing.T) {
	doc, _ := loadSpec(t)
	var inSpec, inTable []string
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			inSpec = append(inSpec, method+" "+path)
		}
	}
	for _, rt := range (&handler{}).table() {
		inTable = append(inTable, rt.pattern)
	}
	sort.Strings(inSpec)
	sort.Strings(inTable)
	for _, s := range inTable {
		if !slices.Contains(inSpec, s) {
			t.Errorf("route %q is not in openapi.yaml", s)
		}
	}
	for _, s := range inSpec {
		if !slices.Contains(inTable, s) {
			t.Errorf("openapi.yaml documents %q, which has no route", s)
		}
	}
}

// contractRecorder validates every exchange of the handler it wraps.
type contractRecorder struct {
	t        *testing.T
	router   routers.Router
	next     http.Handler
	mu       sync.Mutex
	covered  map[string]bool // "METHOD /spec/path" that answered < 300
	statuses map[string]bool // "METHOD /spec/path STATUS"
}

func (c *contractRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	rec := httptest.NewRecorder()
	c.next.ServeHTTP(rec, r)
	c.check(r, body, rec)
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	w.WriteHeader(rec.Code)
	w.Write(rec.Body.Bytes())
}

func (c *contractRecorder) check(r *http.Request, body []byte, rec *httptest.ResponseRecorder) {
	t := c.t
	label := fmt.Sprintf("%s %s -> %d %s", r.Method, r.URL.RequestURI(), rec.Code, rec.Body.String())
	req := r.Clone(r.Context())
	req.URL.Path = "/api/v1" + r.URL.Path
	route, params, err := c.router.FindRoute(req)
	if err != nil {
		t.Errorf("%s: no such operation in openapi.yaml: %v", label, err)
		return
	}
	opts := &openapi3filter.Options{
		AuthenticationFunc: func(context.Context, *openapi3filter.AuthenticationInput) error { return nil }, // auth is tested elsewhere
	}
	in := func() *openapi3filter.RequestValidationInput {
		rq := req.Clone(r.Context())
		rq.Body = io.NopCloser(bytes.NewReader(body))
		return &openapi3filter.RequestValidationInput{Request: rq, PathParams: params, Route: route, Options: opts}
	}
	// A request the spec rejects must be one the server rejects too.
	if err := openapi3filter.ValidateRequest(r.Context(), in()); err != nil && rec.Code != http.StatusBadRequest {
		t.Errorf("%s: request violates openapi.yaml but was not a 400: %v", label, err)
	}
	out := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: in(), Status: rec.Code, Header: rec.Header(), Options: opts,
	}
	out.SetBodyBytes(rec.Body.Bytes())
	if err := openapi3filter.ValidateResponse(r.Context(), out); err != nil {
		t.Errorf("%s: response violates openapi.yaml: %v", label, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := route.Method + " " + route.Path
	c.statuses[fmt.Sprintf("%s %d", key, rec.Code)] = true
	if rec.Code < 300 {
		c.covered[key] = true
	}
}

func TestContract_Scenario(t *testing.T) {
	_, router := loadSpec(t)
	e := newEnv(t, "SETUP-CODE")
	rec := &contractRecorder{t: t, router: router, next: noStore(e.h.routes()), covered: map[string]bool{}, statuses: map[string]bool{}}
	e.ts = httptest.NewServer(rec)
	t.Cleanup(e.ts.Close)

	anon := e.client(t)
	boss := e.client(t)

	// Session and first-run setup.
	wantStatus(t, anon.do("GET", "/session", nil, nil), 200)
	setupBody := map[string]string{"code": "SETUP-CODE", "name": "Boss", "username": "boss", "password": testPassword}
	wantErr(t, boss.do("POST", "/setup", map[string]string{"code": "nope", "name": "Boss", "username": "boss", "password": testPassword}, nil), 401, "unauthenticated")
	wantErr(t, boss.do("POST", "/setup", "{", nil), 400, "invalid_argument")
	wantStatus(t, boss.do("POST", "/setup", setupBody, nil), 201)
	wantErr(t, boss.do("POST", "/setup", setupBody, nil), 404, "not_found")
	wantStatus(t, boss.do("GET", "/session", nil, nil), 200)

	// Admin: projects, actors, credentials.
	wantStatus(t, boss.do("POST", "/admin/projects", map[string]string{"id": "demo"}, nil), 201)
	wantErr(t, boss.do("POST", "/admin/projects", map[string]string{"id": "demo"}, nil), 409, "conflict")
	wantErr(t, boss.do("POST", "/admin/projects", map[string]string{"id": ""}, nil), 400, "invalid_argument")
	wantStatus(t, boss.do("GET", "/admin/projects", nil, nil), 200)
	wantStatus(t, boss.do("PATCH", "/admin/projects/demo", map[string]any{"name": "Demo", "description": "A demo", "min_favorable": 2}, nil), 200)
	wantErr(t, boss.do("PATCH", "/admin/projects/demo", map[string]any{"min_favorable": 0}, nil), 400, "invalid_argument")
	wantErr(t, boss.do("PATCH", "/admin/projects/demo", map[string]any{"min_favorable": 11}, nil), 400, "invalid_argument")
	wantErr(t, boss.do("PATCH", "/admin/projects/nope", map[string]string{"name": "x"}, nil), 404, "not_found")
	wantErr(t, boss.do("PATCH", "/admin/projects/demo", map[string]string{"name": " "}, nil), 400, "invalid_argument")

	r := boss.do("POST", "/admin/actors", map[string]any{"type": "Human", "name": "Alice", "username": "alice", "must_change": true}, nil)
	wantStatus(t, r, 201)
	aliceID, _ := r.body["id"].(string)
	wantErr(t, boss.do("POST", "/admin/actors", map[string]any{"type": "Human", "name": "Alice 2", "username": "alice"}, nil), 409, "conflict")
	r = boss.do("POST", "/admin/actors", map[string]any{"type": "Human", "name": "Bob", "username": "bob", "password": testPassword}, nil)
	wantStatus(t, r, 201)
	bobID, _ := r.body["id"].(string)
	r = boss.do("POST", "/admin/actors", map[string]any{"type": "Agent", "name": "Robot"}, nil)
	wantStatus(t, r, 201)
	agentID, _ := r.body["id"].(string)
	// Access: policies, roles, assignments.
	wantStatus(t, boss.do("GET", "/admin/policies", nil, nil), 200)
	r = boss.do("POST", "/admin/policies", map[string]any{"name": "Reviewer", "actions": []string{"contribution:Read", "signal:Create"}}, nil)
	wantStatus(t, r, 201)
	policyID, _ := r.body["id"].(string)
	wantErr(t, boss.do("POST", "/admin/policies", map[string]any{"name": "Star", "actions": []string{"*"}}, nil), 400, "invalid_argument")
	wantStatus(t, boss.do("PUT", "/admin/policies/"+policyID, map[string]any{"name": "Reviewer", "actions": []string{"contribution:Read"}}, nil), 200)
	wantErr(t, boss.do("PUT", "/admin/policies/ReadOnlyAccess", map[string]any{"name": "x", "actions": []string{"contribution:Read"}}, nil), 403, "forbidden")
	wantStatus(t, boss.do("GET", "/admin/roles", nil, nil), 200)
	r = boss.do("POST", "/admin/roles", map[string]any{"name": "reviewer", "policy_ids": []string{policyID}}, nil)
	wantStatus(t, r, 201)
	roleID, _ := r.body["id"].(string)
	wantErr(t, boss.do("DELETE", "/admin/policies/"+policyID, nil, nil), 409, "conflict")
	wantStatus(t, boss.do("PUT", "/admin/roles/"+roleID, map[string]any{"name": "reviewer", "policy_ids": []string{"ReadOnlyAccess", policyID}}, nil), 200)
	wantErr(t, boss.do("PUT", "/admin/roles/nope", map[string]any{"name": "x", "policy_ids": []string{policyID}}, nil), 404, "not_found")
	for _, who := range []string{aliceID, bobID} {
		wantStatus(t, boss.do("POST", "/admin/assignments", map[string]any{"actor_id": who, "role_id": "contributor", "project_id": "demo"}, nil), 201)
	}
	r = boss.do("POST", "/admin/assignments", map[string]any{"actor_id": agentID, "role_id": roleID, "project_id": nil}, nil)
	wantStatus(t, r, 201)
	assignmentID, _ := r.body["id"].(string)
	wantErr(t, boss.do("POST", "/admin/assignments", map[string]any{"actor_id": agentID, "role_id": roleID, "project_id": nil}, nil), 409, "conflict")
	wantStatus(t, boss.do("GET", "/admin/assignments?actor="+agentID, nil, nil), 200)
	wantStatus(t, boss.do("DELETE", "/admin/assignments/"+assignmentID, nil, nil), 204)
	wantErr(t, boss.do("DELETE", "/admin/assignments/"+assignmentID, nil, nil), 404, "not_found")
	wantStatus(t, boss.do("DELETE", "/admin/roles/"+roleID, nil, nil), 204)
	wantStatus(t, boss.do("DELETE", "/admin/policies/"+policyID, nil, nil), 204)
	wantStatus(t, boss.do("GET", "/admin/actors", nil, nil), 200)
	wantStatus(t, boss.do("GET", "/admin/actors/"+agentID, nil, nil), 200)
	wantErr(t, boss.do("GET", "/admin/actors/00000000-0000-4000-8000-000000000000", nil, nil), 404, "not_found")
	wantStatus(t, boss.do("PATCH", "/admin/actors/"+agentID, map[string]any{"name": "Robot 2", "disabled": false}, nil), 200)
	wantStatus(t, boss.do("POST", "/admin/actors/"+aliceID+"/password", map[string]any{"must_change": true}, nil), 200)
	wantStatus(t, boss.do("POST", "/admin/actors/"+agentID+"/credentials", map[string]any{"label": "ci", "expires_in_days": nil}, nil), 201)
	wantErr(t, boss.do("POST", "/admin/actors/"+agentID+"/credentials", map[string]any{"expires_in_days": 7}, nil), 400, "invalid_argument")
	wantErr(t, boss.do("POST", "/admin/actors/"+agentID+"/credentials", map[string]any{"label": strings.Repeat("x", 65)}, nil), 400, "invalid_argument")
	r = boss.do("POST", "/admin/actors/"+agentID+"/credentials", nil, nil)
	wantStatus(t, r, 201)
	credID, _ := r.body["id"].(string)
	wantErr(t, boss.do("POST", "/admin/actors/"+aliceID+"/credentials", nil, nil), 400, "invalid_argument")
	wantStatus(t, boss.do("DELETE", "/admin/credentials/"+credID, nil, nil), 204)
	wantStatus(t, boss.do("GET", "/admin/audit", nil, nil), 200)
	wantStatus(t, boss.do("GET", "/admin/audit?limit=2&action=credential.issue&project=demo&actor="+aliceID, nil, nil), 200)
	wantErr(t, boss.do("GET", "/admin/audit?cursor=nope", nil, nil), 400, "invalid_argument")
	wantErr(t, boss.do("GET", "/admin/audit?limit=x", nil, nil), 400, "invalid_argument")
	wantErr(t, anon.do("GET", "/admin/audit", nil, nil), 401, "unauthenticated")
	wantErr(t, boss.do("DELETE", "/admin/credentials/00000000-0000-4000-8000-000000000000", nil, nil), 404, "not_found")

	// A temporary password blocks everything but changing it.
	alice := e.client(t)
	r = boss.do("POST", "/admin/actors/"+aliceID+"/password", map[string]any{"password": "alice-secret-1", "must_change": true}, nil)
	wantStatus(t, r, 200)
	wantStatus(t, alice.login("alice", "alice-secret-1"), 200)
	wantErr(t, alice.do("POST", "/projects/demo/contributions", map[string]string{"kind": "claim", "title": "x"}, nil), 403, "forbidden")
	wantErr(t, alice.do("POST", "/account/password", map[string]string{"current_password": "wrong-password", "new_password": "alice-secret-2"}, nil), 400, "invalid_argument")
	wantStatus(t, alice.do("POST", "/account/password", map[string]string{"current_password": "alice-secret-1", "new_password": "alice-secret-2"}, nil), 204)
	wantStatus(t, alice.do("DELETE", "/session", nil, nil), 204)
	wantErr(t, alice.do("DELETE", "/session", nil, nil), 401, "unauthenticated")
	wantStatus(t, alice.login("alice", "alice-secret-2"), 200)
	wantErr(t, alice.do("GET", "/admin/overview", nil, nil), 403, "forbidden")
	wantErr(t, alice.do("POST", "/projects/demo/contributions", map[string]string{"kind": "claim", "title": "x"}, map[string]string{"X-CSRF-Token": "bad"}), 403, "forbidden")

	// Contributions and signals.
	wantErr(t, anon.do("POST", "/projects/demo/contributions", map[string]string{"kind": "claim", "title": "x"}, nil), 401, "unauthenticated")
	r = alice.do("POST", "/projects/demo/contributions", map[string]string{"kind": "question", "title": "Which database?", "body": "details"}, nil)
	wantStatus(t, r, 201)
	qID, _ := r.body["id"].(string)
	r = alice.do("POST", "/projects/demo/contributions", map[string]any{"kind": "claim", "title": "Postgres is enough", "body": "database claim", "answers_id": qID, "sources": []string{"https://example.test/bench", "commit 1a2b3c"}}, nil)
	wantStatus(t, r, 201)
	claimID, _ := r.body["id"].(string)
	r = alice.do("POST", "/projects/demo/contributions", map[string]string{"kind": "claim", "title": "Plain database claim"}, nil)
	wantStatus(t, r, 201)
	plainID, _ := r.body["id"].(string)
	wantErr(t, alice.do("POST", "/projects/demo/contributions", map[string]string{"kind": "essay", "title": "x"}, nil), 400, "invalid_argument")

	bob := e.client(t)
	wantStatus(t, bob.login("bob", testPassword), 200)
	r = bob.do("POST", "/projects/demo/contributions/"+claimID+"/signals", map[string]string{"kind": "challenge", "stance": "unfavorable", "body": "scale?"}, nil)
	wantStatus(t, r, 201)
	challengeID, _ := r.body["id"].(string)
	wantStatus(t, bob.do("POST", "/projects/demo/contributions/"+claimID+"/signals", map[string]any{"kind": "assessment", "stance": "favorable", "body": "ok", "sources": []string{"notes.md"}}, nil), 201)
	wantErr(t, bob.do("POST", "/projects/demo/contributions/"+claimID+"/signals", map[string]any{"kind": "assessment", "stance": "favorable", "sources": []string{"bad\nline"}}, nil), 400, "invalid_argument")
	r = bob.do("POST", "/projects/demo/contributions/"+claimID+"/signals", map[string]string{"kind": "assessment", "stance": "neutral", "body": "hmm"}, nil)
	wantStatus(t, r, 201)
	wantErr(t, bob.do("POST", "/projects/demo/contributions/"+claimID+"/signals", map[string]string{"kind": "challenge", "stance": "neutral", "body": ""}, nil), 400, "invalid_argument")
	wantErr(t, bob.do("POST", "/projects/demo/contributions/"+claimID+"/signals", map[string]string{"kind": "challenge", "stance": "favorable", "body": "yes"}, nil), 400, "invalid_argument")
	wantStatus(t, alice.do("POST", "/projects/demo/contributions/"+claimID+"/signals", map[string]string{"kind": "challenge", "stance": "neutral", "body": "it does", "reply_to": challengeID}, nil), 201)
	wantErr(t, alice.do("POST", "/projects/demo/contributions/"+claimID+"/signals", map[string]string{"kind": "challenge", "stance": "unfavorable", "body": "no", "reply_to": challengeID}, nil), 400, "invalid_argument")
	wantErr(t, bob.do("POST", "/projects/demo/contributions/00000000-0000-4000-8000-000000000000/signals", map[string]string{"kind": "assessment", "stance": "neutral"}, nil), 404, "not_found")
	if r := alice.do("POST", "/projects/demo/contributions/"+claimID+"/signals", map[string]string{"kind": "assessment", "stance": "favorable"}, nil); r.status != 201 || r.body["not_counted"] != true {
		t.Fatalf("author's own support = %d %s", r.status, r.raw)
	}

	// Public reads.
	wantErr(t, anon.do("GET", "/projects", nil, nil), 401, "unauthenticated")
	wantStatus(t, alice.do("GET", "/projects", nil, nil), 200)
	wantStatus(t, alice.do("GET", "/projects/demo/contributions", nil, nil), 200)
	wantStatus(t, alice.do("GET", "/projects/demo/contributions?kind=claim&state=Conflict&state=InsufficientEvidence&sort=debated&limit=1", nil, nil), 200)
	r = alice.do("GET", "/projects/demo/contributions?limit=1", nil, nil)
	wantStatus(t, r, 200)
	cursor, _ := r.body["next_cursor"].(string)
	if cursor == "" {
		t.Fatalf("expected a next_cursor: %s", r.raw)
	}
	wantStatus(t, alice.do("GET", "/projects/demo/contributions?limit=1&cursor="+cursor, nil, nil), 200)
	wantStatus(t, alice.do("GET", "/projects/demo/contributions?kind=question&question_status=open", nil, nil), 200)
	wantStatus(t, alice.do("GET", "/projects/demo/contributions?actor=me&sort=active&since=2020-01-01T00:00:00Z", nil, nil), 200)
	wantErr(t, alice.do("GET", "/projects/demo/contributions?since=yesterday", nil, nil), 400, "invalid_argument")
	wantErr(t, alice.do("GET", "/projects/demo/contributions?limit=abc", nil, nil), 400, "invalid_argument")
	wantStatus(t, alice.do("GET", "/projects/demo/contributions/"+claimID, nil, nil), 200)
	wantStatus(t, alice.do("GET", "/projects/demo/contributions/"+qID, nil, nil), 200)
	wantErr(t, alice.do("GET", "/projects/demo/contributions/00000000-0000-4000-8000-000000000000", nil, nil), 404, "not_found")
	r = alice.do("GET", "/projects/demo/search?q=database&limit=5&rerank=false", nil, nil)
	wantStatus(t, r, 200)
	wantStatus(t, alice.do("GET", "/projects/demo/search?q=database&kind=question", nil, nil), 200)
	wantErr(t, alice.do("GET", "/projects/demo/search?q=database&kind=essay", nil, nil), 400, "invalid_argument")
	wantStatus(t, alice.do("GET", "/projects/demo/search?q=database&state=Conflict&state=InsufficientEvidence&question_status=open", nil, nil), 200)
	wantErr(t, alice.do("GET", "/projects/demo/search?q=database&state=Great", nil, nil), 400, "invalid_argument")
	wantErr(t, alice.do("GET", "/projects/demo/search?q=database&question_status=maybe", nil, nil), 400, "invalid_argument")
	wantStatus(t, alice.do("GET", "/projects/demo/authors", nil, nil), 200)

	// Retract, supersede, moderate, archive.
	cp := "/projects/demo/contributions/"
	r = bob.do("POST", cp+claimID+"/signals", map[string]string{"kind": "challenge", "stance": "unfavorable", "body": "again"}, nil)
	wantStatus(t, r, 201)
	sigID, _ := r.body["id"].(string)
	wantErr(t, alice.do("DELETE", cp+claimID+"/signals/"+sigID, nil, nil), 403, "forbidden")
	wantStatus(t, bob.do("DELETE", cp+claimID+"/signals/"+sigID, nil, nil), 204)
	wantStatus(t, bob.do("DELETE", cp+claimID+"/signals/"+sigID, nil, nil), 204)
	wantErr(t, bob.do("DELETE", cp+claimID+"/signals/00000000-0000-4000-8000-000000000000", nil, nil), 404, "not_found")
	r = bob.do("POST", cp+claimID+"/signals", map[string]string{"kind": "challenge", "stance": "unfavorable", "body": "spam"}, nil)
	spamID, _ := r.body["id"].(string)
	wantStatus(t, boss.do("PUT", cp+claimID+"/signals/"+spamID+"/hidden", map[string]any{"hidden": true, "reason": "spam"}, nil), 204)
	wantErr(t, alice.do("PUT", cp+claimID+"/signals/"+spamID+"/hidden", map[string]bool{"hidden": false}, nil), 403, "forbidden")
	wantErr(t, boss.do("PUT", cp+claimID+"/signals/"+spamID+"/hidden", map[string]string{"hidden": "yes"}, nil), 400, "invalid_argument")
	if r = boss.do("GET", cp+claimID, nil, nil); !strings.Contains(r.raw, `"hidden_reason":"spam"`) {
		t.Errorf("moderator does not see the signal's hidden_reason: %s", r.raw)
	}
	wantStatus(t, boss.do("PUT", cp+claimID+"/signals/"+spamID+"/hidden", map[string]bool{"hidden": false}, nil), 204)

	r = alice.do("POST", "/projects/demo/contributions", map[string]string{"kind": "claim", "title": "Postgres is enough, v2", "supersedes_id": claimID}, nil)
	wantStatus(t, r, 201)
	wantErr(t, alice.do("POST", "/projects/demo/contributions", map[string]string{"kind": "claim", "title": "again", "supersedes_id": claimID}, nil), 409, "conflict")
	wantErr(t, bob.do("POST", "/projects/demo/contributions", map[string]string{"kind": "claim", "title": "mine now", "supersedes_id": plainID}, nil), 403, "forbidden")
	wantErr(t, alice.do("POST", "/projects/demo/contributions", map[string]string{"kind": "claim", "title": "x", "supersedes_id": qID}, nil), 400, "invalid_argument")
	r = alice.do("GET", "/projects/demo/contributions?limit=100", nil, nil)
	if strings.Contains(r.raw, claimID) {
		t.Errorf("superseded claim listed by default: %s", r.raw)
	}
	r = alice.do("GET", "/projects/demo/contributions?limit=100&include_superseded=true", nil, nil)
	if !strings.Contains(r.raw, claimID) {
		t.Errorf("include_superseded did not list the claim: %s", r.raw)
	}
	wantStatus(t, alice.do("GET", "/projects/demo/search?q=postgres&include_superseded=true", nil, nil), 200)
	wantErr(t, alice.do("GET", "/projects/demo/search?q=postgres&include_superseded=maybe", nil, nil), 400, "invalid_argument")
	r = alice.do("GET", cp+claimID, nil, nil)
	if _, ok := r.body["superseded_by"]; !ok {
		t.Errorf("superseded claim without superseded_by: %s", r.raw)
	}
	r = bob.do("POST", cp+claimID+"/signals", map[string]string{"kind": "challenge", "stance": "neutral", "body": "still relevant?"}, nil)
	wantStatus(t, r, 201)
	if _, ok := r.body["superseded_by"]; !ok {
		t.Errorf("signal on a superseded claim without superseded_by: %s", r.raw)
	}
	wantErr(t, bob.do("DELETE", cp+claimID+"/supersession", nil, nil), 403, "forbidden")
	wantStatus(t, alice.do("DELETE", cp+claimID+"/supersession", nil, nil), 204)
	wantStatus(t, alice.do("DELETE", cp+claimID+"/supersession", nil, nil), 204)

	wantErr(t, boss.do("PUT", cp+plainID+"/hidden", map[string]any{"hidden": true, "reason": strings.Repeat("x", 501)}, nil), 400, "invalid_argument")
	wantStatus(t, boss.do("PUT", cp+plainID+"/hidden", map[string]any{"hidden": true, "reason": "off topic"}, nil), 204)
	wantErr(t, alice.do("PUT", cp+plainID+"/hidden", map[string]bool{"hidden": false}, nil), 403, "forbidden")
	wantErr(t, bob.do("GET", cp+plainID, nil, nil), 404, "not_found")
	for _, c := range []*client{boss, alice} { // a moderator, and the author
		if r = c.do("GET", cp+plainID, nil, nil); r.body["hidden"] != true || r.body["hidden_reason"] != "off topic" {
			t.Errorf("moderator or author does not see hidden: %s", r.raw)
		}
	}
	if r = alice.do("GET", "/projects/demo/contributions?actor=me&limit=100", nil, nil); !strings.Contains(r.raw, `"hidden_reason":"off topic"`) {
		t.Errorf("the author's own list lacks the hidden claim: %s", r.raw)
	}
	wantStatus(t, boss.do("PUT", cp+plainID+"/hidden", map[string]bool{"hidden": false}, nil), 204)
	wantStatus(t, alice.do("GET", cp+plainID, nil, nil), 200)

	wantStatus(t, boss.do("PATCH", "/admin/projects/demo", map[string]bool{"archived": true}, nil), 200)
	if r = alice.do("GET", "/projects", nil, nil); !strings.Contains(r.raw, `"archived":true`) {
		t.Errorf("project list does not flag the archived project: %s", r.raw)
	}
	if r = boss.do("GET", "/admin/projects", nil, nil); !strings.Contains(r.raw, `"archived":true`) {
		t.Errorf("admin list lacks the archived project: %s", r.raw)
	}
	wantStatus(t, alice.do("GET", cp+claimID, nil, nil), 200)
	wantErr(t, alice.do("POST", "/projects/demo/contributions", map[string]string{"kind": "claim", "title": "late"}, nil), 409, "conflict")
	wantErr(t, bob.do("POST", cp+claimID+"/signals", map[string]string{"kind": "assessment", "stance": "favorable"}, nil), 409, "conflict")
	wantErr(t, bob.do("DELETE", cp+claimID+"/signals/"+spamID, nil, nil), 409, "conflict")
	wantErr(t, alice.do("DELETE", cp+claimID+"/supersession", nil, nil), 409, "conflict")
	wantErr(t, boss.do("PUT", cp+claimID+"/hidden", map[string]bool{"hidden": true}, nil), 409, "conflict")
	wantErr(t, boss.do("PUT", cp+claimID+"/signals/"+spamID+"/hidden", map[string]bool{"hidden": true}, nil), 409, "conflict")
	wantStatus(t, boss.do("PATCH", "/admin/projects/demo", map[string]bool{"archived": false}, nil), 200)

	// Admin overview and search settings.
	wantStatus(t, boss.do("GET", "/admin/overview", nil, nil), 200)
	wantStatus(t, boss.do("GET", "/admin/actors?q=bo", nil, nil), 200)
	wantErr(t, boss.do("GET", "/admin/actors?q=b", nil, nil), 400, "invalid_argument")
	wantStatus(t, boss.do("GET", "/admin/settings/search", nil, nil), 200)
	prov := func(url string, key any) map[string]any {
		return map[string]any{"embedding": map[string]any{"url": url, "model": "e1", "api_key": key}, "rerank": map[string]any{"url": "", "model": ""}}
	}
	wantStatus(t, boss.do("PUT", "/admin/settings/search", prov("http://emb.example/v1", "sk-secret-1234"), nil), 200)
	wantStatus(t, boss.do("PUT", "/admin/settings/search", prov("http://emb.example/v1", nil), nil), 200)
	wantStatus(t, boss.do("POST", "/admin/settings/search/test", prov("http://emb.example/v1", ""), nil), 204)
	e.tester.fail(fmt.Errorf("%w: 401 invalid api key", setting.ErrUpstream))
	wantErr(t, boss.do("PUT", "/admin/settings/search", prov("http://emb.example/v1", "sk-other"), nil), 502, "upstream_failed")
	e.tester.fail(nil)
	wantErr(t, boss.do("PUT", "/admin/settings/search", `{"embedding":{"api_key":5}}`, nil), 400, "invalid_argument")
	wantStatus(t, boss.do("POST", "/admin/settings/search/reindex", nil, nil), 202)

	// Read counts (managers): one recorded observe, then the retention.
	if err := e.h.d.Reads.Record(context.Background(), "demo", "00000000-0000-4000-8000-0000000000b0", reads.Observe, []string{claimID}); err != nil {
		t.Fatal(err)
	}
	if r := boss.do("GET", "/projects/demo/reads", nil, nil); r.status != 200 || len(r.body["items"].([]any)) != 1 || len(r.body["daily"].([]any)) != 1 {
		t.Fatalf("reads = %d %s", r.status, r.raw)
	}
	wantStatus(t, boss.do("GET", "/projects/demo/reads?from=2020-01-01&to=2020-01-31", nil, nil), 200)
	wantErr(t, boss.do("GET", "/projects/demo/reads?from=2020-02-01&to=2020-01-31", nil, nil), 400, "invalid_argument")
	wantErr(t, alice.do("GET", "/projects/demo/reads", nil, nil), 403, "forbidden")
	wantStatus(t, boss.do("PUT", "/projects/demo/reads/retention", map[string]any{"detail_days": 365}, nil), 200)
	wantErr(t, boss.do("PUT", "/projects/demo/reads/retention", map[string]any{"detail_days": 10}, nil), 400, "invalid_argument")

	// Login failures end in 429.
	wantErr(t, anon.login("bob", "wrong-password"), 401, "unauthenticated")
	for i := 1; i < authflow.MaxFailures; i++ {
		anon.login("bob", "wrong-password")
	}
	wantErr(t, anon.login("bob", testPassword), 429, "too_many_requests")
	wantErr(t, anon.do("POST", "/session", map[string]string{"username": "bob", "password": "x"}, map[string]string{"Origin": "http://evil.example"}), 403, "forbidden")

	// Every route answered at least once with a success status.
	for _, rt := range e.h.table() {
		if !rec.covered[rt.pattern] {
			t.Errorf("scenario never got a success response from %q", rt.pattern)
		}
	}
	if t.Failed() {
		return
	}
	var seen []string
	for k := range rec.statuses {
		seen = append(seen, k)
	}
	sort.Strings(seen)
	t.Logf("validated %d method/path/status combinations:\n%s", len(seen), strings.Join(seen, "\n"))
}
