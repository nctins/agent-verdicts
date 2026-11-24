package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	mcpadapter "agent-verdicts/internal/adapter/mcp"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/search"
	"agent-verdicts/internal/setting"
	"agent-verdicts/internal/signal"

	"github.com/jackc/pgx/v5/pgxpool"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeReranker struct{ calls atomic.Int32 }

func (f *fakeReranker) Rerank(_ context.Context, _ string, docs []string) ([]float64, error) {
	f.calls.Add(1)
	return make([]float64, len(docs)), nil
}

// newSearch is a lexical search service; with rer it also has a reranker
// configured (no embedding).
func newSearch(t *testing.T, pool *pgxpool.Pool, rer search.Reranker) *search.Service {
	t.Helper()
	ctx := context.Background()
	store := postgres.NewSettingRepo(pool)
	if rer != nil {
		if err := store.Put(ctx, "search", []byte(`{"rerank":{"url":"http://rerank","model":"r1"}}`)); err != nil {
			t.Fatal(err)
		}
	}
	settings, err := setting.NewService(ctx, store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	contributions := postgres.NewContributionRepo(pool)
	return search.NewService(search.Deps{
		Lexical: postgres.NewLexicalIndex(pool), Vector: postgres.NewVectorIndex(pool), Chunks: postgres.NewChunkRepo(pool),
		Contributions: contributions, Settings: settings,
		NewReranker: func(setting.Provider) search.Reranker { return rer },
	})
}

func (e *env) contrib(t *testing.T, project, actorID string, kind contribution.Kind, title, answers string) string {
	t.Helper()
	e.project(t, project)
	c, err := contribution.Create(context.Background(), postgres.NewProjectRepo(e.pool), e.actors, postgres.NewContributionRepo(e.pool),
		contribution.NewInput{ProjectID: project, ActorID: actorID, Kind: kind, Title: title, Body: "body of " + title, AnswersID: answers})
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func (e *env) signal(t *testing.T, project, contributionID, actorID string, kind signal.Kind, stance signal.Stance) {
	t.Helper()
	_, err := signal.Create(context.Background(), postgres.NewContributionRepo(e.pool), e.actors, postgres.NewSignalRepo(e.pool),
		signal.NewInput{ProjectID: project, ContributionID: contributionID, ActorID: actorID, Kind: kind, Stance: stance, Body: "why"})
	if err != nil {
		t.Fatal(err)
	}
}

func items(t *testing.T, r resp) []map[string]any { t.Helper(); return array(t, r, "items") }

func array(t *testing.T, r resp, key string) []map[string]any {
	t.Helper()
	raw, ok := r.body[key].([]any)
	if !ok {
		t.Fatalf("no %s: %s", key, r.raw)
	}
	out := make([]map[string]any, len(raw))
	for i, it := range raw {
		out[i] = it.(map[string]any)
	}
	return out
}

func titles(t *testing.T, r resp) []string {
	t.Helper()
	var out []string
	for _, it := range items(t, r) {
		out = append(out, it["title"].(string))
	}
	return out
}

func TestProjects_ListSortedByID(t *testing.T) {
	e := newEnv(t, "")
	a := e.human(t, "ann", false, false)
	b := e.human(t, "bob", false, false)
	e.contrib(t, "zeta", a.ID, contribution.KindClaim, "z claim", "")
	fav := e.contrib(t, "alpha", a.ID, contribution.KindClaim, "a claim", "")
	e.contrib(t, "alpha", a.ID, contribution.KindQuestion, "a question", "")
	e.signal(t, "alpha", fav, b.ID, signal.KindAssessment, signal.StanceFavorable)
	r := e.reader(t).do("GET", "/projects", nil, nil)
	wantStatus(t, r, 200)
	got := items(t, r)
	if len(got) != 2 || got[0]["id"] != "alpha" || got[1]["id"] != "zeta" {
		t.Fatalf("projects = %s", r.raw)
	}
	if got[0]["contribution_count"] != 2.0 || got[0]["open_question_count"] != 1.0 || got[1]["contribution_count"] != 1.0 {
		t.Fatalf("counts = %s", r.raw)
	}
	if s, _ := got[0]["created_at"].(string); s == "" {
		t.Fatalf("created_at missing: %s", r.raw)
	}
	// Claims per trust state, every state present (questions are not counted).
	want := map[string]any{"Favorable": 1.0, "Unfavorable": 0.0, "Conflict": 0.0, "InsufficientEvidence": 0.0}
	if !reflect.DeepEqual(got[0]["states"], want) {
		t.Fatalf("alpha states = %v", got[0]["states"])
	}
	if got[1]["states"].(map[string]any)["InsufficientEvidence"] != 1.0 {
		t.Fatalf("zeta states = %v", got[1]["states"])
	}
}

func TestContributions_ListFiltersSortAndPaging(t *testing.T) {
	e := newEnv(t, "")
	a := e.human(t, "ann", false, false)
	b := e.human(t, "bob", false, false)
	fav := e.contrib(t, "p", a.ID, contribution.KindClaim, "fav", "")
	con := e.contrib(t, "p", a.ID, contribution.KindClaim, "con", "")
	e.contrib(t, "p", b.ID, contribution.KindClaim, "none", "")
	e.contrib(t, "p", b.ID, contribution.KindQuestion, "q open", "")
	e.signal(t, "p", fav, b.ID, signal.KindAssessment, signal.StanceFavorable)
	cat := e.human(t, "cat", false, false) // not the author: an author's own support does not count
	e.signal(t, "p", con, cat.ID, signal.KindAssessment, signal.StanceFavorable)
	e.signal(t, "p", con, b.ID, signal.KindChallenge, signal.StanceUnfavorable)
	c := e.reader(t)
	get := func(q string) resp { return c.do("GET", "/projects/p/contributions?"+q, nil, nil) }

	r := get("")
	wantStatus(t, r, 200)
	if got := titles(t, r); !reflect.DeepEqual(got, []string{"q open", "none", "con", "fav"}) {
		t.Fatalf("newest = %v", got)
	}
	it := items(t, r)[0]
	if it["actor_id"] != b.ID || it["actor_name"] != "Name bob" || it["state"] != nil || it["question_status"] != "open" {
		t.Fatalf("question item = %v", it)
	}
	if _, ok := it["body"]; ok {
		t.Fatal("list items must not carry body")
	}
	if _, ok := it["counts"]; ok {
		t.Fatal("questions have no counts")
	}
	// Claims carry their stance counts so a list can be scanned without opening each one.
	if got := items(t, r)[2]["counts"]; !reflect.DeepEqual(got, map[string]any{"favorable": 1.0, "unfavorable": 1.0, "neutral": 0.0, "favorable_actors": 1.0}) {
		t.Fatalf("con counts = %v", got)
	}
	cases := map[string][]string{
		"kind=question":                             {"q open"},
		"state=Favorable&state=Conflict":            {"con", "fav"},
		"question_status=open":                      {"q open"},
		"state=Favorable&question_status=open":      {"q open", "fav"},
		"actor=" + a.ID:                             {"con", "fav"},
		"actor=not-a-uuid":                          nil,
		"kind=claim&sort=debated":                   {"con", "fav", "none"},
		"kind=claim&sort=least":                     {"none", "fav", "con"},
		"unknown_param=1&kind=claim&state=Conflict": {"con"},
	}
	for q, want := range cases {
		r := get(q)
		wantStatus(t, r, 200)
		if got := titles(t, r); !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", q, got, want)
		}
	}

	// Paging: limit=3 then the cursor yields the rest; next_cursor null at the end.
	p1 := get("limit=3")
	cur, _ := p1.body["next_cursor"].(string)
	if len(items(t, p1)) != 3 || cur == "" {
		t.Fatalf("page 1 = %s", p1.raw)
	}
	p2 := get("limit=3&cursor=" + url.QueryEscape(cur))
	if got := titles(t, p2); !reflect.DeepEqual(got, []string{"fav"}) || p2.body["next_cursor"] != nil {
		t.Fatalf("page 2 = %s", p2.raw)
	}

	for _, q := range []string{"cursor=garbage", "limit=abc", "sort=random", "state=Bogus", "kind=nope"} {
		wantErr(t, get(q), 400, "invalid_argument")
	}
}

func TestContributionDetail_ClaimWithSignals(t *testing.T) {
	e := newEnv(t, "")
	a := e.human(t, "ann", false, false)
	b := e.human(t, "bob", false, false)
	q := e.contrib(t, "p", a.ID, contribution.KindQuestion, "which db?", "")
	id := e.contrib(t, "p", b.ID, contribution.KindClaim, "postgres", q)
	e.signal(t, "p", id, a.ID, signal.KindAssessment, signal.StanceUnfavorable)
	e.signal(t, "p", id, a.ID, signal.KindAssessment, signal.StanceFavorable) // replaces the first
	e.signal(t, "p", id, b.ID, signal.KindChallenge, signal.StanceNeutral)
	r := e.reader(t).do("GET", "/projects/p/contributions/"+id, nil, nil)
	wantStatus(t, r, 200)
	if r.body["state"] != "Favorable" || r.body["body"] != "body of postgres" || r.body["actor_name"] != "Name bob" {
		t.Fatalf("detail = %s", r.raw)
	}
	if at := r.body["answer_to"].(map[string]any); at["id"] != q {
		t.Fatalf("answer_to = %v", at)
	}
	counts := r.body["counts"].(map[string]any)
	if counts["favorable"] != 1.0 || counts["unfavorable"] != 0.0 || counts["neutral"] != 1.0 {
		t.Fatalf("counts = %v", counts)
	}
	sig := r.body["signals"].(map[string]any)
	n := func(k string) int { return len(sig[k].([]any)) }
	if n("favorable") != 1 || n("unfavorable") != 0 || n("neutral") != 1 || n("history") != 1 {
		t.Fatalf("signals = %v", sig)
	}
	if f := sig["favorable"].([]any)[0].(map[string]any); f["actor_id"] != a.ID {
		t.Fatalf("favorable[0].actor_id = %v, want %s", f["actor_id"], a.ID)
	}
	h := sig["history"].([]any)[0].(map[string]any)
	if h["superseded"] != true || h["stance"] != "unfavorable" {
		t.Fatalf("history = %v", h)
	}
}

func TestContributionDetail_Question(t *testing.T) {
	e := newEnv(t, "")
	a := e.human(t, "ann", false, false)
	q := e.contrib(t, "p", a.ID, contribution.KindQuestion, "which db?", "")
	ans := e.contrib(t, "p", a.ID, contribution.KindClaim, "postgres", q)
	e.signal(t, "p", ans, e.human(t, "abe", false, false).ID, signal.KindAssessment, signal.StanceFavorable) // not the author
	r := e.reader(t).do("GET", "/projects/p/contributions/"+q, nil, nil)
	wantStatus(t, r, 200)
	if v, ok := r.body["state"]; !ok || v != nil {
		t.Fatalf("state must be null for a question: %s", r.raw)
	}
	if r.body["question_status"] != "resolved" || r.body["answer_count"] != 1.0 || r.body["best_answer_state"] != "Favorable" {
		t.Fatalf("question fields = %s", r.raw)
	}
	if ans0 := r.body["answers"].([]any)[0].(map[string]any); ans0["id"] != ans {
		t.Fatalf("answers = %s", r.raw)
	}
	sig := r.body["signals"].(map[string]any)
	for _, k := range []string{"favorable", "unfavorable", "neutral", "history"} {
		if g, ok := sig[k].([]any); !ok || len(g) != 0 {
			t.Fatalf("signals.%s = %v, want []", k, sig[k])
		}
	}
}

func TestContributionDetail_NotFound(t *testing.T) {
	e := newEnv(t, "")
	a := e.human(t, "ann", false, false)
	id := e.contrib(t, "p", a.ID, contribution.KindClaim, "x", "")
	c := e.reader(t)
	for _, path := range []string{"/projects/p/contributions/not-a-uuid", "/projects/other/contributions/" + id,
		"/projects/p/contributions/00000000-0000-0000-0000-000000000000"} {
		wantErr(t, c.do("GET", path, nil, nil), 404, "not_found")
	}
}

func TestWrites_RequireLogin(t *testing.T) {
	e := newEnv(t, "")
	a := e.human(t, "ann", false, false)
	id := e.contrib(t, "p", a.ID, contribution.KindClaim, "x", "")
	c := e.client(t)
	wantErr(t, c.do("POST", "/projects/p/contributions", map[string]string{"title": "t"}, nil), 401, "unauthenticated")
	wantErr(t, c.do("POST", "/projects/p/contributions/"+id+"/signals", map[string]string{"kind": "assessment", "stance": "favorable"}, nil), 401, "unauthenticated")
}

func TestWrites_MustChangeForbidden(t *testing.T) {
	e := newEnv(t, "")
	e.human(t, "temp", false, true)
	c := e.client(t)
	wantStatus(t, c.login("temp", testPassword), 200)
	wantStatus(t, c.do("GET", "/session", nil, nil), 200) // CSRF token
	wantErr(t, c.do("POST", "/projects/p/contributions", map[string]string{"title": "t"}, nil), 403, "forbidden")
}

// reader is a client logged in as a fresh Human with viewer on every project.
func (e *env) reader(t *testing.T) *client {
	t.Helper()
	c, me := e.loggedIn(t, "reader"+id.New()[:6])
	e.grant(t, me, "viewer", "")
	return c
}

// loggedIn is a client logged in as a fresh Human who may write anywhere,
// creating projects on the way (contributor on every project + project:Create).
func loggedIn(t *testing.T, e *env, username string) (*client, actor.Actor) {
	t.Helper()
	a := e.human(t, username, false, false)
	e.grant(t, a.ID, "contributor", "")
	e.grant(t, a.ID, e.customRole(t, "creator-"+username, access.ProjectCreate), "")
	c := e.client(t)
	wantStatus(t, c.login(username, testPassword), 200)
	wantStatus(t, c.do("GET", "/session", nil, nil), 200)
	return c, a
}

func TestCreateContribution(t *testing.T) {
	e := newEnv(t, "")
	c, a := loggedIn(t, e, "ann")
	e.project(t, "fresh")
	r := c.do("POST", "/projects/fresh/contributions", map[string]string{"kind": "question", "title": "which db?", "body": "details"}, nil)
	wantStatus(t, r, 201)
	id, _ := r.body["id"].(string)
	if id == "" || r.body["actor_id"] != a.ID || r.body["actor_name"] != "Name ann" {
		t.Fatalf("created = %s", r.raw)
	}
	if got := e.worker.enqueued(); len(got) != 1 || got[0] != id {
		t.Fatalf("enqueued = %v", got)
	}
	r = c.do("POST", "/projects/fresh/contributions", map[string]string{"title": "postgres", "answers_id": id}, nil)
	wantStatus(t, r, 201)
	d := c.do("GET", "/projects/fresh/contributions/"+id, nil, nil)
	if d.body["kind"] != "question" || d.body["answer_count"] != 1.0 || d.body["actor_type"] != "Human" {
		t.Fatalf("detail = %s", d.raw)
	}
	l := c.do("GET", "/projects/fresh/contributions?kind=question", nil, nil)
	if items, _ := l.body["items"].([]any); len(items) != 1 || items[0].(map[string]any)["answer_count"] != 1.0 || items[0].(map[string]any)["actor_type"] != "Human" {
		t.Fatalf("list = %s", l.raw)
	}
	p := c.do("GET", "/projects", nil, nil)
	if !strings.Contains(p.raw, `"last_activity_at":"20`) {
		t.Fatalf("projects = %s", p.raw)
	}

	wantErr(t, c.do("POST", "/projects/fresh/contributions", map[string]string{"title": "  "}, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/projects/fresh/contributions", map[string]string{"title": "x", "extra": "y"}, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/projects/fresh/contributions", map[string]string{"title": "x", "answers_id": "not-a-uuid"}, nil), 404, "not_found")
	big := `{"title":"x","body":"` + strings.Repeat("a", 300<<10) + `"}`
	r = c.do("POST", "/projects/fresh/contributions", big, nil)
	wantErr(t, r, 400, "invalid_argument")
	if !strings.Contains(r.raw, "too large") {
		t.Fatalf("big body = %s", r.raw)
	}
	if got := e.worker.enqueued(); len(got) != 2 {
		t.Fatalf("failed creates must not enqueue: %v", got)
	}
}

func TestCreateSignal_ReplacedStance(t *testing.T) {
	e := newEnv(t, "")
	c, a := loggedIn(t, e, "ann")
	id := e.contrib(t, "p", a.ID, contribution.KindClaim, "claim", "")
	path := "/projects/p/contributions/" + id + "/signals"
	// One-click support without a body stays allowed on the web API; only MCP requires a body (spec 2026-11-22-assessment-reason-mcp).
	r := c.do("POST", path, map[string]string{"kind": "assessment", "stance": "favorable"}, nil)
	wantStatus(t, r, 201)
	if v, ok := r.body["replaced_stance"]; !ok || v != nil {
		t.Fatalf("first assessment = %s", r.raw)
	}
	if r.body["id"] == "" || r.body["actor_id"] != a.ID || r.body["actor_name"] != "Name ann" {
		t.Fatalf("created = %s", r.raw)
	}
	r = c.do("POST", path, map[string]string{"kind": "assessment", "stance": "unfavorable", "body": "changed my mind"}, nil)
	wantStatus(t, r, 201)
	if r.body["replaced_stance"] != "favorable" {
		t.Fatalf("second assessment = %s", r.raw)
	}
	wantStatus(t, c.do("POST", path, map[string]string{"kind": "challenge", "stance": "neutral", "body": "fyi"}, nil), 201)

	wantErr(t, c.do("POST", path, map[string]string{"kind": "challenge", "stance": "neutral"}, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", path, map[string]string{"kind": "assessment", "stance": "maybe"}, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/projects/p/contributions/not-a-uuid/signals", map[string]string{"kind": "assessment", "stance": "favorable"}, nil), 404, "not_found")
	q := e.contrib(t, "p", a.ID, contribution.KindQuestion, "q", "")
	wantErr(t, c.do("POST", "/projects/p/contributions/"+q+"/signals", map[string]string{"kind": "assessment", "stance": "favorable"}, nil), 400, "invalid_argument")
}

func TestSearch(t *testing.T) {
	e := newEnv(t, "")
	rer := &fakeReranker{}
	e.h.d.Search = newSearch(t, e.pool, rer)
	a := e.human(t, "ann", false, false)
	e.contrib(t, "p", a.ID, contribution.KindClaim, "postgres pooling claim", "")
	e.contrib(t, "p", a.ID, contribution.KindQuestion, "postgres pooling question", "")
	c := e.reader(t)

	r := c.do("GET", "/projects/p/search?q=pooling&rerank=false", nil, nil)
	wantStatus(t, r, 200)
	if len(array(t, r, "results")) != 2 || rer.calls.Load() != 0 {
		t.Fatalf("rerank=false: %s, rerank calls %d", r.raw, rer.calls.Load())
	}
	if _, ok := r.body["degraded"].(bool); !ok {
		t.Fatalf("degraded missing: %s", r.raw)
	}
	wantStatus(t, c.do("GET", "/projects/p/search?q=pooling", nil, nil), 200)
	if rer.calls.Load() != 1 {
		t.Fatalf("default rerank calls = %d, want 1", rer.calls.Load())
	}

	r = c.do("GET", "/projects/p/search?q=pooling&kind=question&limit=1&rerank=0", nil, nil)
	got := array(t, r, "results")
	if len(got) != 1 || got[0]["kind"] != "question" || got[0]["state"] != nil || got[0]["question_status"] != "open" {
		t.Fatalf("kind=question: %s", r.raw)
	}
	for _, q := range []string{"q=", "q=x&rerank=maybe", "q=x&limit=abc", "q=x&kind=nope"} {
		wantErr(t, c.do("GET", "/projects/p/search?"+q, nil, nil), 400, "invalid_argument")
	}
}

// TestParity_WithMCP reads one fixture through both adapters: the JSON API
// must carry every MCP field with the same value.
func TestParity_WithMCP(t *testing.T) {
	e := newEnv(t, "")
	ctx := context.Background()
	credentials := postgres.NewCredentialRepo(e.pool)
	ag, token, err := authentication.Bootstrap(ctx, e.actors, credentials, actor.TypeAgent, "agent")
	if err != nil {
		t.Fatal(err)
	}
	contributions := postgres.NewContributionRepo(e.pool)
	mmux := http.NewServeMux() // the project comes from the URL, as in cmd/agentverdicts
	mmux.Handle("/mcp/{project}", mcpadapter.NewHandler(mcpadapter.Deps{
		Projects: postgres.NewProjectRepo(e.pool), Actors: e.actors, Contributions: contributions,
		Signals: postgres.NewSignalRepo(e.pool), Credentials: credentials, Access: e.access, Search: e.h.d.Search, Worker: e.worker, Reads: postgres.NewReadsRepo(e.pool),
	}))
	mts := httptest.NewServer(mmux)
	e.grant(t, ag, "viewer", "")
	e.project(t, "p") // the endpoint 404s until its project exists
	t.Cleanup(mts.Close)
	ms, err := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "t", Version: "0"}, nil).Connect(ctx,
		&sdkmcp.StreamableClientTransport{Endpoint: mts.URL + "/mcp/p", HTTPClient: &http.Client{Transport: bearer(token)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ms.Close() })
	call := func(tool string, args map[string]any) map[string]any {
		t.Helper()
		res, err := ms.CallTool(ctx, &sdkmcp.CallToolParams{Name: tool, Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("%s: %v %+v", tool, err, res)
		}
		var out map[string]any
		b, _ := json.Marshal(res.StructuredContent)
		json.Unmarshal(b, &out)
		return out
	}

	h := e.human(t, "ann", false, false)
	q := e.contrib(t, "p", h.ID, contribution.KindQuestion, "which pooling?", "")
	claim := e.contrib(t, "p", ag, contribution.KindClaim, "pgbouncer pooling", q)
	e.signal(t, "p", claim, h.ID, signal.KindAssessment, signal.StanceUnfavorable)
	e.signal(t, "p", claim, h.ID, signal.KindAssessment, signal.StanceFavorable)
	e.signal(t, "p", claim, ag, signal.KindChallenge, signal.StanceNeutral)
	var root string
	if err := e.pool.QueryRow(ctx, `SELECT id::text FROM signal WHERE contribution_id = $1 AND kind = 'challenge'`, claim).Scan(&root); err != nil {
		t.Fatal(err)
	}
	if _, err := signal.Create(ctx, contributions, e.actors, postgres.NewSignalRepo(e.pool),
		signal.NewInput{ProjectID: "p", ContributionID: claim, ActorID: h.ID, Kind: signal.KindChallenge, Stance: signal.StanceNeutral, Body: "reply", ReplyTo: root}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE contribution SET sources = ARRAY['https://example.test/a', 'abc123'] WHERE id = $1`,
		`UPDATE signal SET sources = ARRAY['src/pool.go'] WHERE contribution_id = $1`} {
		if _, err := e.pool.Exec(ctx, q, claim); err != nil {
			t.Fatal(err)
		}
	}
	c := e.reader(t)

	for _, id := range []string{q, claim} {
		ref := call("reference_contribution", map[string]any{"contribution_id": id})
		api := c.do("GET", "/projects/p/contributions/"+id, nil, nil).body
		if !reflect.DeepEqual(ref, api) {
			t.Errorf("detail %s:\n mcp %v\n api %v", id, ref, api)
		}
		if id == claim {
			if s, _ := api["sources"].([]any); len(s) != 2 || s[1] != "abc123" {
				t.Errorf("claim sources = %v", api["sources"])
			}
			var replies int
			for _, s := range api["signals"].(map[string]any)["neutral"].([]any) {
				if s.(map[string]any)["reply_to"] == root {
					replies++
				}
			}
			if replies != 1 {
				t.Errorf("neutral signals = %v, want one reply_to %s", api["signals"].(map[string]any)["neutral"], root)
			}
			if sg := api["signals"].(map[string]any)["favorable"].([]any); len(sg) != 1 || sg[0].(map[string]any)["sources"].([]any)[0] != "src/pool.go" {
				t.Errorf("signal sources = %v", sg)
			}
			obs := call("observe_trust_signal", map[string]any{"contribution_id": id})
			delete(obs, "state")
			delete(obs, "counts")
			delete(obs, "min_favorable")
			if !reflect.DeepEqual(obs, api["signals"]) {
				t.Errorf("signals:\n mcp %v\n api %v", obs, api["signals"])
			}
		}
	}

	list := call("list_contributions", map[string]any{})
	apiList := c.do("GET", "/projects/p/contributions", nil, nil).body
	for _, it := range apiList["items"].([]any) {
		delete(it.(map[string]any), "actor_id")
	}
	if !reflect.DeepEqual(list, apiList) {
		t.Errorf("list:\n mcp %v\n api %v", list, apiList)
	}

	// Filtered and sorted: the same arguments give the same page on both sides.
	for _, tc := range []struct {
		args  map[string]any
		query string
	}{
		{map[string]any{"actor": ag, "sort": "active"}, "actor=" + ag + "&sort=active"},
		{map[string]any{"actor": "me", "sort": "debated", "since": "2000-01-01T00:00:00Z"}, "actor=" + ag + "&sort=debated&since=2000-01-01T00:00:00Z"},
	} {
		list := call("list_contributions", tc.args)
		apiList := c.do("GET", "/projects/p/contributions?"+tc.query, nil, nil).body
		items := apiList["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("%s: api items = %v, want only the agent's claim", tc.query, items)
		}
		delete(items[0].(map[string]any), "actor_id")
		if !reflect.DeepEqual(list, apiList) {
			t.Errorf("list %s:\n mcp %v\n api %v", tc.query, list, apiList)
		}
	}

	sr := call("search", map[string]any{"query": "pooling"})
	apiSearch := c.do("GET", "/projects/p/search?q=pooling", nil, nil).body
	if !reflect.DeepEqual(sr["results"], apiSearch["results"]) || sr["degraded"] != apiSearch["degraded"] {
		t.Errorf("search:\n mcp %v\n api %v", sr, apiSearch)
	}
	for _, tc := range []struct {
		args  map[string]any
		query string
		want  int
	}{
		{map[string]any{"query": "pooling", "state": []string{"Favorable"}}, "state=Favorable", 1},
		{map[string]any{"query": "pooling", "state": []string{"Conflict", "Unfavorable"}}, "state=Conflict&state=Unfavorable", 0},
		{map[string]any{"query": "pooling", "question_status": []string{"answered"}}, "question_status=answered", 0},
		{map[string]any{"query": "pooling", "state": []string{"Favorable"}, "question_status": []string{"resolved", "answered"}}, "state=Favorable&question_status=resolved&question_status=answered", 2},
	} {
		sr := call("search", tc.args)
		apiSearch := c.do("GET", "/projects/p/search?q=pooling&"+tc.query, nil, nil).body
		if !reflect.DeepEqual(sr, apiSearch) || len(apiSearch["results"].([]any)) != tc.want {
			t.Errorf("search %s (want %d results):\n mcp %v\n api %v", tc.query, tc.want, sr, apiSearch)
		}
	}
	if res, err := ms.CallTool(ctx, &sdkmcp.CallToolParams{Name: "search", Arguments: map[string]any{"query": "pooling", "state": []string{"Great"}}}); err == nil && !res.IsError {
		t.Error("search with an unknown state succeeded over MCP")
	}
}

type bearer string

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(r)
}

// TestNULAndInvalidUTF8 covers the inputs Postgres text cannot hold: free
// text is invalid_argument, a path id names nothing (not_found). Never 500.
func TestNULAndInvalidUTF8(t *testing.T) {
	e := newEnv(t, "")
	c, a := loggedIn(t, e, "ann")
	id := e.contrib(t, "p", a.ID, contribution.KindClaim, "claim", "")
	for _, q := range []string{"/projects/p/search?q=a%00b&rerank=false", "/projects/p/search?q=%ff&rerank=false",
		"/projects/p/contributions?actor=a%00b", "/projects/p/contributions?actor=%ff"} {
		wantErr(t, c.do("GET", q, nil, nil), 400, "invalid_argument")
	}
	for _, p := range []string{"/projects/p/contributions/a%00b", "/projects/p/contributions/%ff"} {
		wantErr(t, c.do("GET", p, nil, nil), 404, "not_found")
	}
	wantErr(t, c.do("POST", "/projects/p/contributions", map[string]string{"title": "a\x00b"}, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/projects/p/contributions", map[string]string{"title": "t", "body": "a\x00b"}, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/projects/p/contributions/"+id+"/signals",
		map[string]string{"kind": "challenge", "stance": "unfavorable", "body": "a\x00b"}, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/projects/p/contributions/a%00b/signals",
		map[string]string{"kind": "assessment", "stance": "neutral"}, nil), 404, "not_found")
}

// A signal aimed at a real contribution under the wrong project must look
// exactly like one aimed at a contribution that does not exist (no existence
// leak across projects).
func TestCreateSignal_WrongProjectIndistinguishableFromMissing(t *testing.T) {
	e := newEnv(t, "")
	c, a := loggedIn(t, e, "ann")
	id := e.contrib(t, "owner", a.ID, contribution.KindClaim, "claim", "")
	body := map[string]string{"kind": "challenge", "stance": "neutral", "body": "x"}
	wrong := c.do("POST", "/projects/other/contributions/"+id+"/signals", body, nil)
	missing := c.do("POST", "/projects/other/contributions/00000000-0000-0000-0000-000000000000/signals", body, nil)
	wantErr(t, wrong, 404, "not_found")
	wantErr(t, missing, 404, "not_found")
	if string(wrong.raw) != string(missing.raw) {
		t.Fatalf("existence leak:\nwrong:   %s\nmissing: %s", wrong.raw, missing.raw)
	}
}

// actor=me is the caller; since keeps only items active after it; sort=active
// puts the contribution with the newest signal first.
func TestListContributions_MeSinceActive(t *testing.T) {
	e := newEnv(t, "")
	c, ann := loggedIn(t, e, "ann")
	bob := e.human(t, "bob", false, false)
	mine := e.contrib(t, "p", ann.ID, contribution.KindClaim, "mine", "")
	theirs := e.contrib(t, "p", bob.ID, contribution.KindClaim, "theirs", "")
	ids := func(q string) []string {
		var out []string
		for _, it := range array(t, c.do("GET", "/projects/p/contributions?"+q, nil, nil), "items") {
			out = append(out, it["id"].(string))
		}
		return out
	}
	if got := ids("actor=me"); !reflect.DeepEqual(got, []string{mine}) {
		t.Fatalf("actor=me = %v, want [%s]", got, mine)
	}
	cut := time.Now().UTC().Format(time.RFC3339Nano)
	e.signal(t, "p", mine, bob.ID, signal.KindAssessment, signal.StanceFavorable)
	if got := ids("since=" + cut); !reflect.DeepEqual(got, []string{mine}) {
		t.Fatalf("since = %v, want [%s]", got, mine)
	}
	if got := ids("sort=active"); !reflect.DeepEqual(got, []string{mine, theirs}) {
		t.Fatalf("sort=active = %v, want [%s %s]", got, mine, theirs)
	}
	for _, it := range array(t, c.do("GET", "/projects/p/contributions", nil, nil), "items") {
		if it["updated_at"] == nil || it["updated_at"] == "" {
			t.Fatalf("item without updated_at: %v", it)
		}
	}
	wantErr(t, c.do("GET", "/projects/p/contributions?since=yesterday", nil, nil), 400, "invalid_argument")
}

type failingLexical struct{}

func (failingLexical) Search(context.Context, string, string, search.Filters, int) ([]search.Hit, error) {
	return nil, errors.New("index down")
}

// similar lists near-duplicate titles (checked before the insert, so never the
// new item itself); it is [] when nothing matches and when search fails.
func TestCreateContribution_Similar(t *testing.T) {
	e := newEnv(t, "")
	c, a := loggedIn(t, e, "ann")
	old := e.contrib(t, "p", a.ID, contribution.KindClaim, "pgbouncer pooling reduces connections", "")
	create := func(title string) resp {
		r := c.do("POST", "/projects/p/contributions", map[string]string{"kind": "claim", "title": title}, nil)
		wantStatus(t, r, 201)
		return r
	}
	r := create("does pgbouncer pooling help")
	got, _ := r.body["similar"].([]any)
	if len(got) != 1 || got[0].(map[string]any)["id"] != old || got[0].(map[string]any)["title"] == nil {
		t.Fatalf("similar = %s", r.raw)
	}
	if r := create("zebra stripes"); r.body["similar"] == nil || len(r.body["similar"].([]any)) != 0 {
		t.Fatalf("unrelated similar = %s", r.raw)
	}
	e.h.d.Search = search.NewService(search.Deps{Lexical: failingLexical{}, Contributions: postgres.NewContributionRepo(e.pool), Settings: e.settings})
	if r := create("pgbouncer pooling again"); r.body["similar"] == nil || len(r.body["similar"].([]any)) != 0 {
		t.Fatalf("failing search similar = %s", r.raw)
	}
}
