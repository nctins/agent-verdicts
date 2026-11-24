package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	mcpadapter "agent-verdicts/internal/adapter/mcp"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/project"
	"agent-verdicts/internal/search"
	"agent-verdicts/internal/setting"
	"agent-verdicts/internal/signal"

	"github.com/jackc/pgx/v5/pgxpool"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeWorker struct {
	mu  sync.Mutex
	ids []string
}

func (f *fakeWorker) Enqueue(id string) { f.mu.Lock(); f.ids = append(f.ids, id); f.mu.Unlock() }

func (f *fakeWorker) enqueued() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ids)
}

type env struct {
	ts     *httptest.Server
	token  string
	worker *fakeWorker
	s      *sdkmcp.ClientSession
	pool   *pgxpool.Pool
	access *postgres.AccessRepo
	reads  *flakyReads
}

// newEnv serves the real handler over HTTP on a fresh Postgres schema, with
// search running lexical-only (no providers configured).
func newEnv(t *testing.T) *env { return newEnvRate(t, 0) }

// newEnvRate is newEnv with the per-actor rate limit set to ratePerMin (0 = off).
func newEnvRate(t *testing.T, ratePerMin int) *env {
	t.Helper()
	ctx := context.Background()
	pool := pgtest.NewMigrated(t)
	actors := postgres.NewActorRepo(pool)
	credentials := postgres.NewCredentialRepo(pool)
	contributions := postgres.NewContributionRepo(pool)
	settings, err := setting.NewService(ctx, postgres.NewSettingRepo(pool), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc := search.NewService(search.Deps{
		Lexical: postgres.NewLexicalIndex(pool), Vector: postgres.NewVectorIndex(pool), Chunks: postgres.NewChunkRepo(pool),
		Contributions: contributions, Settings: settings,
	})
	w := &fakeWorker{}
	acc := postgres.NewAccessRepo(pool)
	rd := &flakyReads{Repository: postgres.NewReadsRepo(pool)}
	mux := http.NewServeMux()
	mux.Handle("/mcp/{project}", mcpadapter.NewHandler(mcpadapter.Deps{
		Projects: postgres.NewProjectRepo(pool), Actors: actors, Contributions: contributions,
		Signals: postgres.NewSignalRepo(pool), Credentials: credentials, Access: acc, Search: svc, Worker: w, Reads: rd, RatePerMin: ratePerMin,
	}))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	e := &env{ts: ts, worker: w, pool: pool, access: acc, reads: rd}
	for _, p := range []string{"p", "other"} {
		if _, err := project.Create(ctx, postgres.NewProjectRepo(pool), p, "Project P", "About "+p); err != nil {
			t.Fatal(err)
		}
	}
	// The default agent may do everything a contributor does, anywhere.
	var id string
	id, e.token = e.agent(t, "test-agent")
	e.grant(t, id, "contributor", "")
	e.s = e.connect(t, e.token)
	return e
}

// agent bootstraps an Agent with no role; returns its id and token.
func (e *env) agent(t *testing.T, name string) (string, string) {
	t.Helper()
	id, token, err := authentication.Bootstrap(context.Background(), postgres.NewActorRepo(e.pool), postgres.NewCredentialRepo(e.pool), actor.TypeAgent, name)
	if err != nil {
		t.Fatal(err)
	}
	return id, token
}

func (e *env) grant(t *testing.T, actorID, roleID, project string) {
	t.Helper()
	var p *string
	if project != "" {
		p = &project
	}
	if err := e.access.CreateAssignment(context.Background(), access.NewAssignment(actorID, roleID, p, actorID)); err != nil {
		t.Fatal(err)
	}
}

func (e *env) customRole(t *testing.T, name string, actions ...access.Action) string {
	t.Helper()
	ctx := context.Background()
	p := access.Policy{ID: id.New(), Name: name, Actions: actions, CreatedAt: time.Now()}
	r := access.Role{ID: id.New(), Name: name, PolicyIDs: []string{p.ID}, CreatedAt: time.Now()}
	if err := e.access.CreatePolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := e.access.CreateRole(ctx, r); err != nil {
		t.Fatal(err)
	}
	return r.ID
}

// as is a copy of e whose calls go through token.
func (e *env) as(t *testing.T, token string) *env {
	t.Helper()
	c := *e
	c.token, c.s = token, e.connect(t, token)
	return &c
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func (e *env) connect(t *testing.T, token string) *sdkmcp.ClientSession {
	t.Helper()
	s, err := e.tryConnect(token)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func (e *env) tryConnect(token string) (*sdkmcp.ClientSession, error) {
	return e.tryConnectTo(token, "p")
}

func (e *env) tryConnectTo(token, project string) (*sdkmcp.ClientSession, error) {
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0"}, nil)
	return client.Connect(context.Background(), &sdkmcp.StreamableClientTransport{
		Endpoint: e.ts.URL + "/mcp/" + project, HTTPClient: &http.Client{Transport: bearer{token}},
	}, nil)
}

// call returns the structured result as a generic map; fails on tool error.
func (e *env) call(t *testing.T, tool string, args map[string]any) map[string]any {
	t.Helper()
	res := e.raw(t, tool, args)
	if res.IsError {
		t.Fatalf("%s: tool error: %s", tool, text(res))
	}
	var out map[string]any
	b, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// callErr asserts a tool error and returns its text.
func (e *env) callErr(t *testing.T, tool string, args map[string]any) string {
	t.Helper()
	res := e.raw(t, tool, args)
	if !res.IsError {
		t.Fatalf("%s: want tool error, got %+v", tool, res.StructuredContent)
	}
	return text(res)
}

func (e *env) raw(t *testing.T, tool string, args map[string]any) *sdkmcp.CallToolResult {
	t.Helper()
	res, err := e.s.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return res
}

func text(res *sdkmcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (e *env) create(t *testing.T, args map[string]any) string {
	t.Helper()
	return e.call(t, "create_contribution", args)["id"].(string)
}

func TestNoToken_FailClosed(t *testing.T) {
	e := newEnv(t)
	for _, tok := range []string{"", "not-a-real-token"} {
		req, _ := http.NewRequest(http.MethodPost, e.ts.URL+"/mcp/p", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		req.Header.Set("Content-Type", "application/json")
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("token %q: status = %d, want 401", tok, resp.StatusCode)
		}
		if s, err := e.tryConnect(tok); err == nil {
			s.Close()
			t.Errorf("token %q: client connected", tok)
		}
	}
}

func TestToolsList_ExactlyEight(t *testing.T) {
	e := newEnv(t)
	res, err := e.s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	want := []string{"create_assessment", "create_challenge", "create_contribution", "list_contributions", "observe_trust_signal", "reference_contribution", "search", "withdraw_signal"}
	if !slices.Equal(names, want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
}

func TestCreateContribution_MinimalEnqueues(t *testing.T) {
	e := newEnv(t)
	out := e.call(t, "create_contribution", map[string]any{"title": "Only a title"})
	id, _ := out["id"].(string)
	if id == "" || out["actor_id"] == "" || out["actor_name"] != "test-agent" {
		t.Fatalf("out = %v", out)
	}
	if got := e.worker.enqueued(); !slices.Equal(got, []string{id}) {
		t.Fatalf("enqueued = %v, want [%s]", got, id)
	}
	if msg := e.callErr(t, "create_contribution", map[string]any{"title": "  "}); !strings.Contains(msg, "invalid argument") {
		t.Fatalf("empty title error = %q", msg)
	}
}

func TestSearch_ShapeAndLimitClamp(t *testing.T) {
	e := newEnv(t)
	for i := range 25 {
		e.create(t, map[string]any{"title": fmt.Sprintf("Kafka partition note %d", i), "body": "kafka rebalancing"})
	}
	q := e.create(t, map[string]any{"title": "How does kafka rebalance?", "kind": "question"})
	e.create(t, map[string]any{"title": "Kafka answer", "answers_id": q})

	for _, tc := range []struct{ limit, want int }{{0, 5}, {50, 20}} {
		out := e.call(t, "search", map[string]any{"query": "kafka", "limit": tc.limit})
		if got := len(out["results"].([]any)); got != tc.want {
			t.Errorf("limit %d: %d results, want %d", tc.limit, got, tc.want)
		}
		if _, ok := out["degraded"].(bool); !ok {
			t.Errorf("degraded missing: %v", out)
		}
	}

	out := e.call(t, "search", map[string]any{"query": "kafka rebalance", "kind": "question"})
	res := out["results"].([]any)
	if len(res) != 1 {
		t.Fatalf("question results = %v", res)
	}
	r := res[0].(map[string]any)
	if r["id"] != q || r["kind"] != "question" || r["title"] == "" || r["state"] != nil ||
		r["question_status"] != "answered" || r["answer_count"] != 1.0 || r["best_answer_state"] != "InsufficientEvidence" {
		t.Fatalf("question result = %v", r)
	}
	if _, ok := r["state"]; !ok {
		t.Fatal("state key must be present (null) on questions")
	}
	if _, ok := r["snippet"]; !ok {
		t.Fatal("snippet missing")
	}

	claim := e.call(t, "search", map[string]any{"query": "kafka", "kind": "claim", "limit": 1})["results"].([]any)[0].(map[string]any)
	if claim["state"] != "InsufficientEvidence" {
		t.Fatalf("claim result = %v", claim)
	}
	for _, k := range []string{"question_status", "answer_count", "best_answer_state"} {
		if _, ok := claim[k]; ok {
			t.Errorf("claim has %s: %v", k, claim)
		}
	}

	if msg := e.callErr(t, "search", map[string]any{"query": "  "}); !strings.Contains(msg, "invalid argument") {
		t.Fatalf("empty query error = %q", msg)
	}
}

func TestReference_QuestionAnswersRankedAndAnswerTo(t *testing.T) {
	e := newEnv(t)
	q := e.create(t, map[string]any{"title": "Which cache?", "body": "full question body", "kind": "question"})
	weak := e.create(t, map[string]any{"title": "Use memcached", "answers_id": q})
	strong := e.create(t, map[string]any{"title": "Use redis", "answers_id": q})
	if out := e.call(t, "create_assessment", map[string]any{"contribution_id": strong, "stance": "favorable", "body": "checked it"}); out["not_counted"] != true {
		t.Fatalf("self-support = %v", out) // the author's own support is kept, not counted
	}
	if out := e.call(t, "reference_contribution", map[string]any{"contribution_id": q}); out["question_status"] != "answered" {
		t.Fatalf("self-supported answer resolved the question: %v", out)
	}
	otherID, otherToken := e.agent(t, "other")
	e.grant(t, otherID, "contributor", "")
	if out := e.as(t, otherToken).call(t, "create_assessment", map[string]any{"contribution_id": strong, "stance": "favorable", "body": "checked it"}); out["not_counted"] != nil {
		t.Fatalf("other's support = %v", out)
	}

	out := e.call(t, "reference_contribution", map[string]any{"contribution_id": q})
	if out["body"] != "full question body" || out["state"] != nil || out["question_status"] != "resolved" {
		t.Fatalf("question = %v", out)
	}
	answers := out["answers"].([]any)
	if len(answers) != 2 || answers[0].(map[string]any)["id"] != strong || answers[1].(map[string]any)["id"] != weak ||
		answers[0].(map[string]any)["state"] != "Favorable" {
		t.Fatalf("answers = %v", answers)
	}

	ans := e.call(t, "reference_contribution", map[string]any{"contribution_id": weak})
	to, _ := ans["answer_to"].(map[string]any)
	if to["id"] != q || to["title"] != "Which cache?" || ans["state"] != "InsufficientEvidence" {
		t.Fatalf("answer = %v", ans)
	}
	if _, ok := ans["answers"]; ok {
		t.Fatalf("answer has answers: %v", ans)
	}

	msg := e.callErr(t, "reference_contribution", map[string]any{"contribution_id": "nope"})
	if !strings.Contains(msg, "use search to find contributions in this project") {
		t.Fatalf("not-found error = %q", msg)
	}
}

func TestCreateAssessment_ReplacesPrevious(t *testing.T) {
	e := newEnv(t)
	c := e.create(t, map[string]any{"title": "claim"})
	args := map[string]any{"contribution_id": c, "stance": "favorable", "body": "looks right"}
	first := e.call(t, "create_assessment", args)
	if v, ok := first["replaced_stance"]; !ok || v != nil {
		t.Fatalf("first = %v, want replaced_stance null", first)
	}
	if first["actor_name"] != "test-agent" || first["id"] == "" {
		t.Fatalf("first = %v", first)
	}
	args["stance"] = "unfavorable"
	if second := e.call(t, "create_assessment", args); second["replaced_stance"] != "favorable" {
		t.Fatalf("second = %v", second)
	}
}

// #10 (spec 2026-11-22-assessment-reason-mcp): an agent's support or dispute must say what it checked.
func TestCreateAssessment_StanceNeedsBody(t *testing.T) {
	e := newEnv(t)
	c := e.create(t, map[string]any{"title": "claim"})
	for _, args := range []map[string]any{
		{"contribution_id": c, "stance": "favorable"},
		{"contribution_id": c, "stance": "unfavorable", "body": " \n\t "},
	} {
		if msg := e.callErr(t, "create_assessment", args); !strings.Contains(msg, "invalid argument") || !strings.Contains(msg, "what you checked") {
			t.Fatalf("%v: %q", args, msg)
		}
	}
	if n := e.call(t, "observe_trust_signal", map[string]any{"contribution_id": c})["counts"].(map[string]any); n["favorable"] != 0.0 || n["unfavorable"] != 0.0 {
		t.Fatalf("a rejected assessment was counted: %v", n)
	}
	e.call(t, "create_assessment", map[string]any{"contribution_id": c, "stance": "neutral"})
	e.call(t, "create_assessment", map[string]any{"contribution_id": c, "stance": "favorable", "body": "ran it"})
}

func TestCreateChallenge_Errors(t *testing.T) {
	e := newEnv(t)
	c := e.create(t, map[string]any{"title": "claim"})
	if msg := e.callErr(t, "create_challenge", map[string]any{"contribution_id": c, "stance": "unfavorable"}); !strings.Contains(msg, "invalid argument") {
		t.Fatalf("no body error = %q", msg)
	}
	q := e.create(t, map[string]any{"title": "question?", "kind": "question"})
	if msg := e.callErr(t, "create_challenge", map[string]any{"contribution_id": q, "stance": "unfavorable", "body": "wrong"}); !strings.Contains(msg, "answer it instead") {
		t.Fatalf("question error = %q", msg)
	}
	ok := e.call(t, "create_challenge", map[string]any{"contribution_id": c, "stance": "unfavorable", "body": "wrong"})
	if ok["id"] == "" || ok["actor_name"] != "test-agent" {
		t.Fatalf("challenge = %v", ok)
	}
	if _, has := ok["replaced_stance"]; has {
		t.Fatalf("challenge has replaced_stance: %v", ok)
	}
}

func TestListContributions_NoBodyAndCursor(t *testing.T) {
	e := newEnv(t)
	var ids []string
	for i := range 3 {
		ids = append(ids, e.create(t, map[string]any{"title": fmt.Sprintf("c%d", i), "body": "secret body"}))
	}
	p1 := e.call(t, "list_contributions", map[string]any{"limit": 2})
	items := p1["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["id"] != ids[2] {
		t.Fatalf("page 1 = %v", p1)
	}
	for _, it := range items {
		m := it.(map[string]any)
		if _, ok := m["body"]; ok {
			t.Fatalf("item has body: %v", m)
		}
		if m["actor_name"] != "test-agent" || m["created_at"] == "" || m["state"] != "InsufficientEvidence" {
			t.Fatalf("item = %v", m)
		}
	}
	cur, _ := p1["next_cursor"].(string)
	if cur == "" {
		t.Fatalf("next_cursor = %v", p1["next_cursor"])
	}
	p2 := e.call(t, "list_contributions", map[string]any{"limit": 2, "cursor": cur})
	if items := p2["items"].([]any); len(items) != 1 || items[0].(map[string]any)["id"] != ids[0] {
		t.Fatalf("page 2 = %v", p2)
	}
	if v, ok := p2["next_cursor"]; !ok || v != nil {
		t.Fatalf("last page next_cursor = %v, want null", p2["next_cursor"])
	}

	q := e.create(t, map[string]any{"title": "q?", "kind": "question"})
	qs := e.call(t, "list_contributions", map[string]any{"question_status": []string{"open"}})["items"].([]any)
	if len(qs) != 1 || qs[0].(map[string]any)["id"] != q || qs[0].(map[string]any)["question_status"] != "open" || qs[0].(map[string]any)["state"] != nil {
		t.Fatalf("questions = %v", qs)
	}

	if msg := e.callErr(t, "list_contributions", map[string]any{"cursor": "garbage!!"}); !strings.Contains(msg, "invalid") {
		t.Fatalf("bad cursor error = %q", msg)
	}
}

func TestObserveTrustSignal_GroupsActiveAndHistory(t *testing.T) {
	e := newEnv(t)
	c := e.create(t, map[string]any{"title": "claim"})
	sig := func(tool, stance, body string) string {
		return e.call(t, tool, map[string]any{"contribution_id": c, "stance": stance, "body": body})["id"].(string)
	}
	old := sig("create_assessment", "favorable", "first take")
	cur := sig("create_assessment", "unfavorable", "changed my mind")
	ch := sig("create_challenge", "neutral", "fyi: version 2 differs")

	out := e.call(t, "observe_trust_signal", map[string]any{"contribution_id": c})
	if out["state"] != "Unfavorable" {
		t.Fatalf("state = %v", out["state"])
	}
	counts := out["counts"].(map[string]any)
	if counts["favorable"] != 0.0 || counts["unfavorable"] != 1.0 || counts["neutral"] != 1.0 {
		t.Fatalf("counts = %v", counts)
	}
	group := func(k string) []map[string]any {
		var r []map[string]any
		for _, x := range out[k].([]any) {
			r = append(r, x.(map[string]any))
		}
		return r
	}
	if f := group("favorable"); len(f) != 0 {
		t.Fatalf("favorable = %v", f)
	}
	u := group("unfavorable")
	if len(u) != 1 || u[0]["id"] != cur || u[0]["kind"] != "assessment" || u[0]["stance"] != "unfavorable" ||
		u[0]["actor_name"] != "test-agent" || u[0]["body"] != "changed my mind" || u[0]["created_at"] == "" {
		t.Fatalf("unfavorable = %v", u)
	}
	if _, ok := u[0]["superseded"]; ok {
		t.Fatalf("active signal has superseded: %v", u[0])
	}
	if n := group("neutral"); len(n) != 1 || n[0]["id"] != ch || n[0]["kind"] != "challenge" {
		t.Fatalf("neutral = %v", n)
	}
	h := group("history")
	if len(h) != 1 || h[0]["id"] != old || h[0]["superseded"] != true || h[0]["stance"] != "favorable" {
		t.Fatalf("history = %v", h)
	}

	q := e.create(t, map[string]any{"title": "q?", "kind": "question"})
	if msg := e.callErr(t, "observe_trust_signal", map[string]any{"contribution_id": q}); !strings.Contains(msg, "invalid argument") {
		t.Fatalf("question error = %q", msg)
	}
	if msg := e.callErr(t, "observe_trust_signal", map[string]any{"contribution_id": "nope"}); !strings.Contains(msg, "use search") {
		t.Fatalf("not found error = %q", msg)
	}
}

func TestNULAndInvalidUTF8_InvalidArgumentOrNotFound(t *testing.T) {
	e := newEnv(t)
	c := e.create(t, map[string]any{"title": "claim"})
	for _, bad := range []string{"a\x00b", "\x00"} { // JSON cannot carry invalid UTF-8 (encoders write U+FFFD)
		for tool, args := range map[string]map[string]any{
			"search":              {"query": bad},
			"create_contribution": {"title": bad},
			"create_challenge":    {"contribution_id": c, "stance": "neutral", "body": bad},
		} {
			if msg := e.callErr(t, tool, args); !strings.Contains(msg, "invalid argument") {
				t.Errorf("%s %q: error = %q, want invalid argument", tool, bad, msg)
			}
		}
		for _, tool := range []string{"reference_contribution", "observe_trust_signal"} {
			if msg := e.callErr(t, tool, map[string]any{"contribution_id": bad}); !strings.Contains(msg, "use search") {
				t.Errorf("%s id %q: error = %q, want not found", tool, bad, msg)
			}
		}
	}
}

func TestInternalError_NoSQLTextToAgent(t *testing.T) {
	e := newEnv(t)
	c := e.create(t, map[string]any{"title": "claim"})
	// Break a column, not the table name: the dev database's public.signal
	// would be found through search_path instead.
	if _, err := e.pool.Exec(context.Background(), "ALTER TABLE signal RENAME COLUMN body TO body_gone"); err != nil {
		t.Fatal(err)
	}
	if msg := e.callErr(t, "observe_trust_signal", map[string]any{"contribution_id": c}); msg != "internal error" {
		t.Fatalf("error = %q, want %q", msg, "internal error")
	}
}

// TestSearch_TokenBudget pins spec §1: one search call with k=5 costs at
// most ~600 output tokens. Proxy: runes/4 of the JSON text the model reads
// (the TextContent block; structuredContent carries the same JSON for
// clients that read it instead, and a client feeds the model one of them).
// Fixture: long Vietnamese titles (120 runes) and long bodies, so every
// snippet is full length.
func TestSearch_TokenBudget(t *testing.T) {
	e := newEnv(t)
	title := []rune(strings.Repeat("Chính sách hoàn tiền cho giao dịch thẻ quốc tế ", 3))[:120]
	body := strings.Repeat("Hoàn tiền được xử lý trong vòng bảy ngày làm việc kể từ khi yêu cầu được duyệt. ", 40)
	for i := range 6 {
		e.create(t, map[string]any{"title": fmt.Sprintf("%d %s", i, string(title[2:])), "body": body})
	}
	res := e.raw(t, "search", map[string]any{"query": "hoàn tiền giao dịch"})
	if res.IsError {
		t.Fatal(text(res))
	}
	out := text(res)
	if n := strings.Count(out, `"id"`); n != 5 {
		t.Fatalf("%d results, want 5", n)
	}
	tokens := utf8.RuneCountInString(out) / 4
	t.Logf("search k=5: %d runes ≈ %d tokens", utf8.RuneCountInString(out), tokens)
	if tokens > 600 {
		t.Fatalf("search output ≈ %d tokens, budget 600: %s", tokens, out)
	}
}

func TestTools_NoRoleCannotConnect(t *testing.T) {
	e := newEnv(t)
	_, tok := e.agent(t, "stranger")
	if s, err := e.tryConnect(tok); err == nil {
		s.Close()
		t.Fatal("a role-less agent connected to p")
	}
}

func TestTools_ViewerCannotWrite(t *testing.T) {
	e := newEnv(t)
	cid := e.call(t, "create_contribution", map[string]any{"title": "postgres pooling"})["id"].(string)
	vid, tok := e.agent(t, "viewer")
	e.grant(t, vid, "viewer", "p")
	s := e.as(t, tok)
	s.call(t, "search", map[string]any{"query": "pooling"})
	s.call(t, "reference_contribution", map[string]any{"contribution_id": cid})
	for tool, args := range map[string]map[string]any{
		"create_contribution": {"title": "x"},
		"create_challenge":    {"contribution_id": cid, "stance": "unfavorable", "body": "no"},
		"create_assessment":   {"contribution_id": cid, "stance": "favorable", "body": "checked it"},
	} {
		if msg := s.callErr(t, tool, args); !strings.Contains(msg, "forbidden") || !strings.Contains(msg, ":Create") {
			t.Errorf("%s: %q, want forbidden naming the action", tool, msg)
		}
	}
}

func TestEndpoint_ProjectGate(t *testing.T) {
	e := newEnv(t)
	post := func(path, token string) (int, string) {
		req, _ := http.NewRequest(http.MethodPost, e.ts.URL+path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	// A reader of "other" only: "p" exists but is not readable to it.
	rid, rtok := e.agent(t, "reader")
	e.grant(t, rid, "viewer", "other")
	for path, token := range map[string]string{"/mcp/nope": e.token, "/mcp/Bad..Id": e.token, "/mcp/p": rtok} {
		want := fmt.Sprintf("project %q not found\n", strings.TrimPrefix(path, "/mcp/"))
		if code, body := post(path, token); code != http.StatusNotFound || body != want {
			t.Errorf("%s = %d %q, want 404 %q", path, code, body, want)
		}
	}
	if code, _ := post("/mcp/p", e.token); code != http.StatusOK {
		t.Errorf("/mcp/p = %d, want 200", code)
	}
	if code, _ := post("/mcp/nope", "bad-token"); code != http.StatusUnauthorized {
		t.Errorf("bad token on unknown project = %d, want 401", code)
	}
}

func TestTools_NoProjectIDInSchema(t *testing.T) {
	e := newEnv(t)
	res, err := e.s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		b, _ := json.Marshal(tool.InputSchema)
		if strings.Contains(string(b), "project_id") {
			t.Errorf("%s input schema has project_id: %s", tool.Name, b)
		}
	}
}

func TestInstructions_NameProject(t *testing.T) {
	e := newEnv(t)
	got := e.s.InitializeResult().Instructions
	for _, want := range []string{`"p"`, "Project P", "About p", "Start with search"} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions %q lack %q", got, want)
		}
	}
}

func TestTools_ScopedToURLProject(t *testing.T) {
	e := newEnv(t)
	e.create(t, map[string]any{"title": "kafka in p"})
	s, err := e.tryConnectTo(e.token, "other")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	res, err := s.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "list_contributions", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("list: %v %v", err, res)
	}
	if n := len(res.StructuredContent.(map[string]any)["items"].([]any)); n != 0 {
		t.Fatalf("project other lists %d items from p", n)
	}
}

func TestListContributions_ActorMeSinceSort(t *testing.T) {
	e := newEnv(t)
	mine := e.create(t, map[string]any{"title": "mine"})
	otherID, otherTok := e.agent(t, "other-agent")
	e.grant(t, otherID, "contributor", "")
	theirs := e.as(t, otherTok).create(t, map[string]any{"title": "theirs"})
	ids := func(args map[string]any) []string {
		var out []string
		for _, it := range e.call(t, "list_contributions", args)["items"].([]any) {
			out = append(out, it.(map[string]any)["id"].(string))
		}
		return out
	}
	if got := ids(map[string]any{"actor": "me"}); !slices.Equal(got, []string{mine}) {
		t.Fatalf("actor=me = %v, want [%s]", got, mine)
	}
	if got := ids(map[string]any{"actor": otherID}); !slices.Equal(got, []string{theirs}) {
		t.Fatalf("actor=id = %v, want [%s]", got, theirs)
	}
	cut := time.Now().UTC().Format(time.RFC3339Nano)
	e.call(t, "create_assessment", map[string]any{"contribution_id": mine, "stance": "favorable", "body": "checked it"})
	if got := ids(map[string]any{"since": cut, "sort": "active"}); !slices.Equal(got, []string{mine}) {
		t.Fatalf("since = %v, want [%s]", got, mine)
	}
	if got := ids(map[string]any{"sort": "active"}); !slices.Equal(got, []string{mine, theirs}) {
		t.Fatalf("sort=active = %v", got)
	}
	if msg := e.callErr(t, "list_contributions", map[string]any{"since": "yesterday"}); !strings.Contains(msg, "RFC 3339") {
		t.Fatalf("bad since error = %q", msg)
	}
	if msg := e.callErr(t, "list_contributions", map[string]any{"sort": "random"}); !strings.Contains(msg, "sort") {
		t.Fatalf("bad sort error = %q", msg)
	}
}

func TestCreateContribution_SimilarAndReferenceSignals(t *testing.T) {
	e := newEnv(t)
	first := e.call(t, "create_contribution", map[string]any{"title": "pgbouncer pooling reduces connections"})
	if s, ok := first["similar"].([]any); !ok || len(s) != 0 {
		t.Fatalf("first similar = %v, want []", first["similar"])
	}
	second := e.call(t, "create_contribution", map[string]any{"title": "does pgbouncer pooling help"})
	s, _ := second["similar"].([]any)
	if len(s) != 1 || s[0].(map[string]any)["id"] != first["id"] {
		t.Fatalf("second similar = %v", second["similar"])
	}
	id := first["id"].(string)
	e.call(t, "create_assessment", map[string]any{"contribution_id": id, "stance": "favorable", "body": "works"})
	sig := e.call(t, "reference_contribution", map[string]any{"contribution_id": id})["signals"].(map[string]any)
	if fav := sig["favorable"].([]any); len(fav) != 1 || fav[0].(map[string]any)["body"] != "works" {
		t.Fatalf("reference signals = %v", sig)
	}
}

func TestWithdrawSignal(t *testing.T) {
	e := newEnv(t)
	c := e.create(t, map[string]any{"title": "claim"})
	otherID, otherToken := e.agent(t, "other")
	e.grant(t, otherID, "contributor", "")
	other := e.as(t, otherToken)
	ch := other.call(t, "create_challenge", map[string]any{"contribution_id": c, "stance": "unfavorable", "body": "no"})["id"].(string)
	fanID, fanToken := e.agent(t, "fan")
	e.grant(t, fanID, "contributor", "")
	e.as(t, fanToken).call(t, "create_assessment", map[string]any{"contribution_id": c, "stance": "favorable", "body": "checked it"})
	if got := e.call(t, "observe_trust_signal", map[string]any{"contribution_id": c})["state"]; got != "Conflict" {
		t.Fatalf("state = %v", got)
	}
	if msg := e.callErr(t, "withdraw_signal", map[string]any{"signal_id": ch}); !strings.Contains(msg, "only the author") {
		t.Fatalf("not the author: %q", msg)
	}
	if msg := e.callErr(t, "withdraw_signal", map[string]any{"signal_id": id.New()}); !strings.Contains(msg, "observe_trust_signal") {
		t.Fatalf("unknown id: %q", msg)
	}
	if out := other.call(t, "withdraw_signal", map[string]any{"signal_id": ch}); out["withdrawn"] != true {
		t.Fatalf("out = %v", out)
	}
	other.call(t, "withdraw_signal", map[string]any{"signal_id": ch}) // twice is fine
	out := e.call(t, "observe_trust_signal", map[string]any{"contribution_id": c})
	if out["state"] != "Favorable" {
		t.Fatalf("state after withdrawing the only challenge = %v", out["state"])
	}
	h := out["history"].([]any)
	if len(h) != 1 || h[0].(map[string]any)["withdrawn"] != true {
		t.Fatalf("history = %v", h)
	}
	viewer, vtoken := e.agent(t, "viewer")
	e.grant(t, viewer, "viewer", "")
	if msg := e.as(t, vtoken).callErr(t, "withdraw_signal", map[string]any{"signal_id": ch}); !strings.Contains(msg, "signal:Create") {
		t.Fatalf("viewer: %q", msg)
	}
}

func TestSupersedes_HiddenFromSearchAndList(t *testing.T) {
	e := newEnv(t)
	old := e.create(t, map[string]any{"title": "Zebra caching rule", "body": "zebra"})
	n := e.create(t, map[string]any{"title": "Zebra caching rule v2", "body": "zebra", "supersedes_id": old})
	if msg := e.callErr(t, "create_contribution", map[string]any{"title": "again", "supersedes_id": old}); !strings.Contains(msg, "superseded") {
		t.Fatalf("second replacement: %q", msg)
	}
	ids := func(tool string, args map[string]any, key string) []string {
		var r []string
		for _, x := range e.call(t, tool, args)[key].([]any) {
			r = append(r, x.(map[string]any)["id"].(string))
		}
		return r
	}
	if got := ids("search", map[string]any{"query": "zebra"}, "results"); !slices.Equal(got, []string{n}) {
		t.Fatalf("search = %v", got)
	}
	if got := ids("search", map[string]any{"query": "zebra", "include_superseded": true}, "results"); len(got) != 2 {
		t.Fatalf("search with superseded = %v", got)
	}
	if got := ids("list_contributions", nil, "items"); !slices.Equal(got, []string{n}) {
		t.Fatalf("list = %v", got)
	}
	if got := ids("list_contributions", map[string]any{"include_superseded": true}, "items"); len(got) != 2 {
		t.Fatalf("list with superseded = %v", got)
	}
	ref := e.call(t, "reference_contribution", map[string]any{"contribution_id": old})
	if sb, _ := ref["superseded_by"].(map[string]any); sb["id"] != n || sb["title"] != "Zebra caching rule v2" {
		t.Fatalf("superseded_by = %v", ref["superseded_by"])
	}
}

func TestHiddenContent_VisibleToModeratorsOnly(t *testing.T) {
	e := newEnv(t)
	c := e.create(t, map[string]any{"title": "Quokka claim", "body": "quokka"})
	other := e.create(t, map[string]any{"title": "Quokka sibling", "body": "quokka"})
	sig := e.call(t, "create_challenge", map[string]any{"contribution_id": other, "stance": "unfavorable", "body": "x"})["id"].(string)
	mustExecT(t, e.pool, `UPDATE contribution SET hidden_at = now() WHERE id = $1`, c)
	if err := postgres.NewSignalRepo(e.pool).SetHidden(context.Background(), sig, otherActor(t, e), "", true); err != nil { // recounts
		t.Fatal(err)
	}
	// another contributor sees neither (the author does: TestHiddenContent_AuthorSeesOwn)
	peerID, peerToken := e.agent(t, "peer")
	e.grant(t, peerID, "contributor", "")
	peer := e.as(t, peerToken)
	if msg := peer.callErr(t, "reference_contribution", map[string]any{"contribution_id": c}); !strings.Contains(msg, "not found") {
		t.Fatalf("hidden read: %q", msg)
	}
	if msg := peer.callErr(t, "observe_trust_signal", map[string]any{"contribution_id": c}); !strings.Contains(msg, "not found") {
		t.Fatalf("hidden observe: %q", msg)
	}
	author := e.call(t, "reference_contribution", map[string]any{"contribution_id": c})["actor_id"]
	if n := len(peer.call(t, "list_contributions", map[string]any{"actor": author})["items"].([]any)); n != 1 {
		t.Fatalf("a peer listing the author sees %d items, want only the visible one", n)
	}
	if msg := e.callErr(t, "create_assessment", map[string]any{"contribution_id": c, "stance": "favorable", "body": "checked it"}); !strings.Contains(msg, "not found") {
		t.Fatalf("hidden assess: %q", msg)
	}
	if n := len(e.call(t, "search", map[string]any{"query": "quokka"})["results"].([]any)); n != 1 {
		t.Fatalf("search = %d results", n)
	}
	if n := len(e.call(t, "list_contributions", nil)["items"].([]any)); n != 1 {
		t.Fatalf("list = %d items", n)
	}
	obs := e.call(t, "observe_trust_signal", map[string]any{"contribution_id": other})
	if len(obs["history"].([]any)) != 0 || obs["state"] != "InsufficientEvidence" {
		t.Fatalf("hidden signal leaked or counted: %v", obs)
	}
	// a moderator sees both, flagged
	modID, modToken := e.agent(t, "mod")
	e.grant(t, modID, "manager", "")
	mod := e.as(t, modToken)
	if ref := mod.call(t, "reference_contribution", map[string]any{"contribution_id": c}); ref["hidden"] != true {
		t.Fatalf("moderator read: %v", ref)
	}
	if n := len(mod.call(t, "search", map[string]any{"query": "quokka"})["results"].([]any)); n != 2 {
		t.Fatalf("moderator search = %d results", n)
	}
	h := mod.call(t, "observe_trust_signal", map[string]any{"contribution_id": other})["history"].([]any)
	if len(h) != 1 || h[0].(map[string]any)["hidden"] != true {
		t.Fatalf("moderator history = %v", h)
	}
}

func TestArchivedProject_IsReadOnly(t *testing.T) {
	e := newEnv(t)
	c := e.create(t, map[string]any{"title": "claim"})
	sig := e.call(t, "create_challenge", map[string]any{"contribution_id": c, "stance": "unfavorable", "body": "x"})["id"].(string)
	mustExecT(t, e.pool, `UPDATE project SET archived_at = now() WHERE id = 'p'`)
	s := e.as(t, e.token)
	if got := s.s.InitializeResult().Instructions; !strings.Contains(got, "This project is archived and read-only.") {
		t.Fatalf("instructions = %q", got)
	}
	for tool, args := range map[string]map[string]any{
		"create_contribution": {"title": "late"},
		"create_challenge":    {"contribution_id": c, "stance": "unfavorable", "body": "x"},
		"create_assessment":   {"contribution_id": c, "stance": "favorable", "body": "checked it"},
		"withdraw_signal":     {"signal_id": sig},
	} {
		if msg := s.callErr(t, tool, args); !strings.Contains(msg, "project is archived") {
			t.Errorf("%s: %q", tool, msg)
		}
	}
	s.call(t, "reference_contribution", map[string]any{"contribution_id": c})
	s.call(t, "search", map[string]any{"query": "claim"})
}

func mustExecT(t *testing.T, pool *pgxpool.Pool, q string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), q, args...); err != nil {
		t.Fatal(err)
	}
}

func otherActor(t *testing.T, e *env) string {
	t.Helper()
	var id string
	if err := e.pool.QueryRow(context.Background(), `SELECT id::text FROM actor LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSources_RoundTripAndLimits(t *testing.T) {
	e := newEnv(t)
	c := e.create(t, map[string]any{"title": "claim", "sources": []string{"https://example.test/a", " commit abc "}})
	e.call(t, "create_challenge", map[string]any{"contribution_id": c, "stance": "unfavorable", "body": "no", "sources": []string{"src/x.go"}})
	e.call(t, "create_assessment", map[string]any{"contribution_id": c, "stance": "neutral"})
	ref := e.call(t, "reference_contribution", map[string]any{"contribution_id": c})
	if got := fmt.Sprint(ref["sources"]); got != "[https://example.test/a commit abc]" {
		t.Fatalf("sources = %v", got)
	}
	obs := e.call(t, "observe_trust_signal", map[string]any{"contribution_id": c})
	u := obs["unfavorable"].([]any)[0].(map[string]any)
	n := obs["neutral"].([]any)[0].(map[string]any)
	if fmt.Sprint(u["sources"]) != "[src/x.go]" || fmt.Sprint(n["sources"]) != "[]" {
		t.Fatalf("signal sources = %v / %v", u["sources"], n["sources"])
	}
	tooMany := make([]string, 11)
	for i := range tooMany {
		tooMany[i] = "s"
	}
	for name, src := range map[string][]string{"many": tooMany, "long": {strings.Repeat("x", 501)}, "control": {"a\nb"}} {
		if msg := e.callErr(t, "create_contribution", map[string]any{"title": "x", "sources": src}); !strings.Contains(msg, "invalid argument") {
			t.Errorf("create_contribution %s: %q", name, msg)
		}
		if msg := e.callErr(t, "create_assessment", map[string]any{"contribution_id": c, "stance": "favorable", "sources": src}); !strings.Contains(msg, "invalid argument") {
			t.Errorf("create_assessment %s: %q", name, msg)
		}
	}
}

func TestThreshold_InstructionsAndObserve(t *testing.T) {
	e := newEnv(t)
	if got := e.s.InitializeResult().Instructions; strings.Contains(got, "different supporters") {
		t.Fatalf("threshold 1 should not be mentioned: %q", got)
	}
	mustExecT(t, e.pool, `UPDATE project SET min_favorable = 2 WHERE id = 'p'`)
	s := e.as(t, e.token)
	if got := s.s.InitializeResult().Instructions; !strings.Contains(got, "A claim needs 2 different supporters to be Favorable.") {
		t.Fatalf("instructions = %q", got)
	}
	authorID, authorToken := e.agent(t, "author")
	e.grant(t, authorID, "contributor", "")
	c := e.as(t, authorToken).create(t, map[string]any{"title": "claim"})
	as := s.call(t, "create_assessment", map[string]any{"contribution_id": c, "stance": "favorable", "body": "yes"})
	// A favorable challenge from before the rule (new ones are refused): same actor, so still one supporter.
	legacy := signal.Signal{ID: id.New(), ContributionID: c, ActorID: as["actor_id"].(string), Kind: signal.KindChallenge, Stance: signal.StanceFavorable, Body: "yes again", CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if _, err := postgres.NewSignalRepo(e.pool).Insert(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	obs := s.call(t, "observe_trust_signal", map[string]any{"contribution_id": c})
	counts := obs["counts"].(map[string]any)
	if obs["state"] != "InsufficientEvidence" || obs["min_favorable"] != 2.0 || counts["favorable"] != 2.0 || counts["favorable_actors"] != 1.0 {
		t.Fatalf("observe = %v", obs)
	}
}
