package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/adapter/authflow"
	"agent-verdicts/internal/adapter/httpapi/session"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/search"
	"agent-verdicts/internal/setting"

	"github.com/jackc/pgx/v5/pgxpool"
)

const testPassword = "correct-horse-1"

// flakyLogins fails GetByActor while broken is set — a transient DB error.
type flakyLogins struct {
	authentication.LoginRepository
	broken       atomic.Bool
	upsertBroken atomic.Bool
}

func (f *flakyLogins) Upsert(ctx context.Context, l authentication.Login) error {
	if f.upsertBroken.Load() {
		return errors.New("db: connection reset")
	}
	return f.LoginRepository.Upsert(ctx, l)
}

func (f *flakyLogins) GetByActor(ctx context.Context, id string) (authentication.Login, error) {
	if f.broken.Load() {
		return authentication.Login{}, errors.New("db: connection reset")
	}
	return f.LoginRepository.GetByActor(ctx, id)
}

type fakeWorker struct {
	mu         sync.Mutex
	ids        []string
	reindexing atomic.Bool
}

func (f *fakeWorker) Enqueue(id string) { f.mu.Lock(); f.ids = append(f.ids, id); f.mu.Unlock() }

// enqueued is a copy of the IDs enqueued so far, safe against a concurrent Enqueue.
func (f *fakeWorker) enqueued() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ids)
}

func (f *fakeWorker) Reindex(context.Context) error { f.reindexing.Store(true); return nil }
func (f *fakeWorker) Reindexing() bool              { return f.reindexing.Load() }

type env struct {
	h        *handler
	ts       *httptest.Server
	pool     *pgxpool.Pool
	actors   *postgres.ActorRepo
	access   *postgres.AccessRepo
	logins   *flakyLogins
	sessions *session.Store
	worker   *fakeWorker
	tester   *fakeTester
	settings *setting.Service
}

// fakeTester stands in for the provider connection test: err fails it,
// otherwise it reports dim 3 for an enabled embedding provider.
type fakeTester struct {
	mu    sync.Mutex
	err   error
	calls int
}

func (f *fakeTester) test(_ context.Context, s setting.Search) (setting.Search, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return setting.Search{}, f.err
	}
	if s.Embedding.URL != "" {
		s.Embedding.Dim = 3
	}
	return s, nil
}

func (f *fakeTester) fail(err error) { f.mu.Lock(); f.err = err; f.mu.Unlock() }
func (f *fakeTester) count() int     { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }

// newEnv serves the real routes plus three test-only routes guarded by the
// shared middleware: /test/login (requireLogin), /test/write (requireLogin
// + csrf) and /test/admin (require *: admins only).
func newEnv(t *testing.T, setupCode string) *env {
	t.Helper()
	pool := pgtest.NewMigrated(t)
	e := &env{
		pool:   pool,
		actors: postgres.NewActorRepo(pool),
		access: postgres.NewAccessRepo(pool),
		logins: &flakyLogins{LoginRepository: postgres.NewLoginRepo(pool)},
		worker: &fakeWorker{},
		tester: &fakeTester{},
	}
	ctx := context.Background()
	var err error
	if e.sessions, err = session.NewStore(ctx, postgres.NewSessionRepo(pool)); err != nil {
		t.Fatal(err)
	}
	cipher, err := setting.LoadKey(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if e.settings, err = setting.NewService(ctx, postgres.NewSettingRepo(pool), cipher, e.tester.test); err != nil {
		t.Fatal(err)
	}
	contributions := postgres.NewContributionRepo(pool)
	searchSvc := search.NewService(search.Deps{
		Lexical: postgres.NewLexicalIndex(pool), Vector: postgres.NewVectorIndex(pool), Chunks: postgres.NewChunkRepo(pool),
		Contributions: contributions, Settings: e.settings, Reindexing: e.worker.Reindexing,
	})
	h := newHandler(Deps{
		Actors: e.actors, Logins: e.logins, Sessions: e.sessions, Access: e.access, Audit: postgres.NewAuditRepo(pool), Auth: authflow.New(e.actors, e.logins, e.access, setupCode),
		Projects: postgres.NewProjectRepo(pool), Contributions: contributions, Signals: postgres.NewSignalRepo(pool),
		Credentials: postgres.NewCredentialRepo(pool), Search: searchSvc, Worker: e.worker, Settings: e.settings, Reads: postgres.NewReadsRepo(pool),
	})
	e.h = h
	mux := h.routes()
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("GET /test/login", h.requireLogin(ok))
	mux.Handle("POST /test/write", h.requireLogin(h.csrf(ok)))
	mux.Handle("GET /test/admin", h.require(access.All, ok))
	e.ts = httptest.NewServer(noStore(mux))
	t.Cleanup(e.ts.Close)
	return e
}

func (e *env) human(t *testing.T, username string, admin, mustChange bool) actor.Actor {
	t.Helper()
	ctx := context.Background()
	a := actor.New(actor.TypeHuman)
	a.Name = "Name " + username
	if err := e.actors.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	if admin {
		if err := access.GrantAdmin(ctx, e.access, a.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := authentication.SetLogin(ctx, e.logins, e.actors, a.ID, username, testPassword, mustChange); err != nil {
		t.Fatal(err)
	}
	return a
}

// client is a browser: a cookie jar, Origin on every request, and the CSRF
// token from the last session response.
type client struct {
	t    *testing.T
	e    *env
	http *http.Client
	csrf string
}

func (e *env) client(t *testing.T) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, e: e, http: &http.Client{Jar: jar}}
}

type resp struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

func (c *client) do(method, path string, body any, hdr map[string]string) resp {
	c.t.Helper()
	var rd *bytes.Reader
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case string:
		rd = bytes.NewReader([]byte(b))
	default:
		j, _ := json.Marshal(b)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, c.e.ts.URL+path, rd)
	req.Header.Set("Origin", c.e.ts.URL)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(res.Body)
	r := resp{status: res.StatusCode, header: res.Header, raw: buf.String()}
	if buf.Len() > 0 {
		json.Unmarshal(buf.Bytes(), &r.body)
	}
	if tok, ok := r.body["csrf_token"].(string); ok {
		c.csrf = tok
	}
	return r
}

func (c *client) login(username, password string) resp {
	c.t.Helper()
	return c.do("POST", "/session", map[string]string{"username": username, "password": password}, nil)
}

// isAdminSession reports whether a session response grants "*".
func isAdminSession(r resp) bool {
	acc, _ := r.body["access"].(map[string]any)
	all, _ := acc["all"].([]any)
	return len(all) == 1 && all[0] == "*"
}

func wantErr(t *testing.T, r resp, status int, code string) {
	t.Helper()
	if r.status != status || r.body["code"] != code {
		t.Fatalf("got %d %s, want %d %s", r.status, r.raw, status, code)
	}
	if m, _ := r.body["message"].(string); m == "" {
		t.Fatalf("error without message: %s", r.raw)
	}
}

func wantStatus(t *testing.T, r resp, status int) {
	t.Helper()
	if r.status != status {
		t.Fatalf("status = %d (%s), want %d", r.status, r.raw, status)
	}
}

func TestNoStore_OnEveryResponse(t *testing.T) {
	e := newEnv(t, "")
	c := e.client(t)
	for _, r := range []resp{
		c.do("GET", "/session", nil, nil),
		c.login("nobody", "wrong-password"),
		c.do("GET", "/no/such/route", nil, nil),
		c.do("GET", "/test/login", nil, nil),
	} {
		if got := r.header.Get("Cache-Control"); got != "no-store" {
			t.Fatalf("Cache-Control = %q on %d %s", got, r.status, r.raw)
		}
	}
}

func TestUnknownRoute_JSON404(t *testing.T) {
	e := newEnv(t, "")
	wantErr(t, e.client(t).do("GET", "/nope", nil, nil), 404, "not_found")
}

func TestGetSession_Anonymous(t *testing.T) {
	e := newEnv(t, "")
	r := e.client(t).do("GET", "/session", nil, nil)
	wantStatus(t, r, 200)
	if r.body["authenticated"] != false || r.body["actor"] != nil || r.body["csrf_token"] != nil ||
		r.body["must_change_password"] != false || r.body["setup_required"] != false {
		t.Fatalf("body = %s", r.raw)
	}
}

func TestLogin_SuccessSetsCookieAndReturnsSession(t *testing.T) {
	e := newEnv(t, "")
	a := e.human(t, "alice", true, false)
	c := e.client(t)
	r := c.login("Alice", testPassword)
	wantStatus(t, r, 200)
	act, _ := r.body["actor"].(map[string]any)
	if r.body["authenticated"] != true || act["id"] != a.ID || act["name"] != a.Name ||
		act["type"] != "Human" || !isAdminSession(r) || c.csrf == "" || r.body["must_change_password"] != false {
		t.Fatalf("body = %s", r.raw)
	}
	cookie := r.header.Get("Set-Cookie")
	for _, want := range []string{"av_session=", "Path=/;", "HttpOnly", "SameSite=Lax"} {
		if !strings.Contains(cookie+";", want) {
			t.Fatalf("Set-Cookie %q missing %q", cookie, want)
		}
	}
	if strings.Contains(cookie, "Secure") {
		t.Fatalf("Secure on plain http: %q", cookie)
	}
	r = c.do("GET", "/session", nil, nil)
	if r.body["authenticated"] != true || r.body["csrf_token"] != c.csrf {
		t.Fatalf("GET /session after login = %s", r.raw)
	}
	wantStatus(t, c.do("GET", "/test/login", nil, nil), 200)
}

func TestLogin_WrongCredentials_OneGenericMessage(t *testing.T) {
	e := newEnv(t, "")
	e.human(t, "alice", false, false)
	c := e.client(t)
	wrong := c.login("alice", "wrong-password")
	unknown := c.login("nobody", testPassword)
	wantErr(t, wrong, 401, "unauthenticated")
	wantErr(t, unknown, 401, "unauthenticated")
	if wrong.body["message"] != unknown.body["message"] {
		t.Fatalf("messages differ: %q vs %q", wrong.body["message"], unknown.body["message"])
	}
	if wrong.header.Get("Set-Cookie") != "" {
		t.Fatal("cookie set on failed login")
	}
}

func TestLogin_FiveFailuresThen429(t *testing.T) {
	e := newEnv(t, "")
	e.human(t, "alice", false, false)
	c := e.client(t)
	for i := 0; i < authflow.MaxFailures; i++ {
		wantErr(t, c.login("alice", "wrong-password"), 401, "unauthenticated")
	}
	wantErr(t, c.login("alice", testPassword), 429, "too_many_requests")
}

func TestLogin_OriginCheck(t *testing.T) {
	e := newEnv(t, "")
	e.human(t, "alice", false, false)
	c := e.client(t)
	body := map[string]string{"username": "alice", "password": testPassword}
	wantErr(t, c.do("POST", "/session", body, map[string]string{"Origin": "https://evil.example"}), 403, "forbidden")
	wantErr(t, c.do("POST", "/session", body, map[string]string{"Origin": ""}), 403, "forbidden")
	// Behind a proxy Host is the upstream address; X-Forwarded-Host carries
	// the public one the browser used.
	r := c.do("POST", "/session", body, map[string]string{"Origin": "https://av.example", "X-Forwarded-Host": "av.example"})
	wantStatus(t, r, 200)
	// A proxy chain appends hosts; the first is the client-facing one.
	r = c.do("POST", "/session", body, map[string]string{"Origin": "https://av.example", "X-Forwarded-Host": " av.example , proxy.internal"})
	wantStatus(t, r, 200)
	r = c.do("POST", "/session", body, map[string]string{"Origin": "https://proxy.internal", "X-Forwarded-Host": "av.example, proxy.internal"})
	wantErr(t, r, 403, "forbidden")
}

func TestLogin_BadBodies400(t *testing.T) {
	e := newEnv(t, "")
	c := e.client(t)
	wantErr(t, c.do("POST", "/session", `{"username":"a","password":"b","extra":1}`, nil), 400, "invalid_argument")
	wantErr(t, c.do("POST", "/session", `not json`, nil), 400, "invalid_argument")
	big := `{"username":"` + strings.Repeat("a", 300<<10) + `"}`
	wantErr(t, c.do("POST", "/session", big, nil), 400, "invalid_argument")
}

func TestLogout_RequiresCSRF(t *testing.T) {
	e := newEnv(t, "")
	e.human(t, "alice", false, false)
	c := e.client(t)
	c.login("alice", testPassword)
	wantErr(t, c.do("DELETE", "/session", nil, map[string]string{"X-CSRF-Token": ""}), 403, "forbidden")
	wantErr(t, c.do("DELETE", "/session", nil, map[string]string{"X-CSRF-Token": "bogus"}), 403, "forbidden")
	wantStatus(t, c.do("DELETE", "/session", nil, nil), 204)
	r := c.do("GET", "/session", nil, nil)
	if r.body["authenticated"] != false {
		t.Fatalf("still authenticated after logout: %s", r.raw)
	}
	wantErr(t, c.do("DELETE", "/session", nil, nil), 401, "unauthenticated")
}

func TestWrite_WithoutCSRF403(t *testing.T) {
	e := newEnv(t, "")
	e.human(t, "alice", false, false)
	c := e.client(t)
	c.login("alice", testPassword)
	wantErr(t, c.do("POST", "/test/write", nil, map[string]string{"X-CSRF-Token": ""}), 403, "forbidden")
	wantStatus(t, c.do("POST", "/test/write", nil, nil), 200)
}

func TestRequireLogin_Rejections(t *testing.T) {
	e := newEnv(t, "")
	e.human(t, "alice", false, false)
	c := e.client(t)
	wantErr(t, c.do("GET", "/test/login", nil, nil), 401, "unauthenticated") // no cookie

	c.login("alice", testPassword)
	// Transient DB trouble fails the request but keeps the session.
	e.logins.broken.Store(true)
	wantErr(t, c.do("GET", "/test/login", nil, nil), 500, "internal")
	e.logins.broken.Store(false)
	wantStatus(t, c.do("GET", "/test/login", nil, nil), 200)

	// A session idle past its timeout is gone.
	session.SetNowForTest(e.sessions, func() time.Time { return time.Now().Add(session.IdleTimeout + time.Hour) })
	wantErr(t, c.do("GET", "/test/login", nil, nil), 401, "unauthenticated")
}

func TestDisabledActor_SessionRejected(t *testing.T) {
	e := newEnv(t, "")
	a := e.human(t, "alice", false, false)
	c := e.client(t)
	c.login("alice", testPassword)
	now := time.Now()
	if err := e.actors.SetDisabled(context.Background(), a.ID, &now); err != nil {
		t.Fatal(err)
	}
	wantErr(t, c.do("GET", "/test/login", nil, nil), 401, "unauthenticated")
	if r := c.do("GET", "/session", nil, nil); r.body["authenticated"] != false {
		t.Fatalf("disabled actor still authenticated: %s", r.raw)
	}
}

func TestRequireAdmin_ReReadEachRequest(t *testing.T) {
	e := newEnv(t, "")
	a := e.human(t, "alice", true, false)
	b := e.human(t, "bob", false, false)
	e.human(t, "root", true, false) // keeps an admin when alice is demoted
	ca, cb := e.client(t), e.client(t)
	ca.login("alice", testPassword)
	cb.login("bob", testPassword)
	wantStatus(t, ca.do("GET", "/test/admin", nil, nil), 200)
	wantErr(t, cb.do("GET", "/test/admin", nil, nil), 403, "forbidden")

	ctx := context.Background()
	as, _ := e.access.ListAssignments(ctx, access.AssignmentFilter{ActorID: a.ID})
	if err := e.access.DeleteAssignment(ctx, as[0].ID); err != nil {
		t.Fatal(err)
	}
	wantErr(t, ca.do("GET", "/test/admin", nil, nil), 403, "forbidden")
	r := ca.do("GET", "/session", nil, nil)
	if isAdminSession(r) {
		t.Fatalf("demoted admin still admin: %s", r.raw)
	}
	if err := access.GrantAdmin(ctx, e.access, b.ID); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, cb.do("GET", "/test/admin", nil, nil), 200)
}

func TestMustChange_BlocksUntilPasswordChanged(t *testing.T) {
	e := newEnv(t, "")
	e.human(t, "alice", false, true)
	c := e.client(t)
	r := c.login("alice", testPassword)
	if r.body["must_change_password"] != true {
		t.Fatalf("login = %s", r.raw)
	}
	for _, r := range []resp{c.do("POST", "/test/write", nil, nil), c.do("GET", "/test/login", nil, nil)} {
		wantErr(t, r, 403, "forbidden")
		if r.body["message"] != "password change required" {
			t.Fatalf("message = %q", r.body["message"])
		}
	}
	if r := c.do("GET", "/session", nil, nil); r.body["must_change_password"] != true || r.body["authenticated"] != true {
		t.Fatalf("GET /session = %s", r.raw)
	}
	wantStatus(t, c.do("POST", "/account/password", map[string]string{"current_password": testPassword, "new_password": "brand-new-pass-2"}, nil), 204)
	wantStatus(t, c.do("POST", "/test/write", nil, nil), 200)
	if r := c.do("GET", "/session", nil, nil); r.body["must_change_password"] != false {
		t.Fatalf("GET /session = %s", r.raw)
	}
	// Logout stays allowed while a change is pending.
	e.human(t, "bob", false, true)
	cb := e.client(t)
	cb.login("bob", testPassword)
	wantStatus(t, cb.do("DELETE", "/session", nil, nil), 204)
}

func TestChangePassword_EndsOtherSessionsKeepsThisOne(t *testing.T) {
	e := newEnv(t, "")
	e.human(t, "alice", false, false)
	a, b := e.client(t), e.client(t)
	a.login("alice", testPassword)
	b.login("alice", testPassword)
	wantErr(t, a.do("POST", "/account/password", map[string]string{"current_password": testPassword, "new_password": "brand-new-pass-2"},
		map[string]string{"X-CSRF-Token": ""}), 403, "forbidden")
	wantStatus(t, a.do("POST", "/account/password", map[string]string{"current_password": testPassword, "new_password": "brand-new-pass-2"}, nil), 204)
	wantStatus(t, a.do("GET", "/test/login", nil, nil), 200)
	wantErr(t, b.do("GET", "/test/login", nil, nil), 401, "unauthenticated")
	wantStatus(t, e.client(t).login("alice", "brand-new-pass-2"), 200)
}

func TestChangePassword_WrongCurrentIs400AndNotThrottled(t *testing.T) {
	e := newEnv(t, "")
	e.human(t, "alice", false, false)
	c := e.client(t)
	c.login("alice", testPassword)
	for i := 0; i < authflow.MaxFailures+1; i++ {
		wantErr(t, c.do("POST", "/account/password", map[string]string{"current_password": "wrong-password", "new_password": "brand-new-pass-2"}, nil), 400, "invalid_argument")
	}
	wantErr(t, c.do("POST", "/account/password", map[string]string{"current_password": testPassword, "new_password": "short"}, nil), 400, "invalid_argument")
	wantStatus(t, e.client(t).login("alice", testPassword), 200)
}

func setupBody(code string) map[string]string {
	return map[string]string{"code": code, "name": "Root", "username": "root", "password": testPassword}
}

func TestSetup_CreatesAdminAndLogsIn(t *testing.T) {
	e := newEnv(t, "ABC123")
	c := e.client(t)
	if r := c.do("GET", "/session", nil, nil); r.body["setup_required"] != true {
		t.Fatalf("GET /session = %s", r.raw)
	}
	r := c.do("POST", "/setup", setupBody(" abc123 "), nil)
	wantStatus(t, r, 201)
	act, _ := r.body["actor"].(map[string]any)
	if r.body["authenticated"] != true || !isAdminSession(r) || act["name"] != "Root" || r.body["setup_required"] != false || c.csrf == "" {
		t.Fatalf("setup = %s", r.raw)
	}
	wantStatus(t, c.do("GET", "/test/admin", nil, nil), 200)
	if r := c.do("GET", "/session", nil, nil); r.body["setup_required"] != false {
		t.Fatalf("GET /session = %s", r.raw)
	}
	wantErr(t, c.do("POST", "/setup", setupBody("ABC123"), nil), 404, "not_found")
}

func TestSetup_WrongCodeLocksAfterFive(t *testing.T) {
	e := newEnv(t, "ABC123")
	c := e.client(t)
	for i := 0; i < authflow.MaxFailures; i++ {
		wantErr(t, c.do("POST", "/setup", setupBody("ZZZZZZ"), nil), 401, "unauthenticated")
	}
	wantErr(t, c.do("POST", "/setup", setupBody("ABC123"), nil), 429, "too_many_requests")
}

func TestSetup_ThrottlePerXRealIP(t *testing.T) {
	e := newEnv(t, "ABC123")
	c := e.client(t)
	for i := 0; i < authflow.MaxFailures; i++ {
		wantErr(t, c.do("POST", "/setup", setupBody("ZZZZZZ"), map[string]string{"X-Real-IP": "198.51.100.1"}), 401, "unauthenticated")
	}
	wantErr(t, c.do("POST", "/setup", setupBody("ZZZZZZ"), map[string]string{"X-Real-IP": "198.51.100.2"}), 401, "unauthenticated")
	wantErr(t, c.do("POST", "/setup", setupBody("ZZZZZZ"), map[string]string{"X-Real-IP": "198.51.100.1"}), 429, "too_many_requests")
}

func TestSetup_ClosedWithoutCodeOrOnceAdminExists(t *testing.T) {
	wantErr(t, newEnv(t, "").client(t).do("POST", "/setup", setupBody(""), nil), 404, "not_found")

	e := newEnv(t, "ABC123")
	e.human(t, "admin", true, false) // created another way (CLI)
	c := e.client(t)
	wantErr(t, c.do("POST", "/setup", setupBody("ABC123"), nil), 404, "not_found")
	if r := c.do("GET", "/session", nil, nil); r.body["setup_required"] != false {
		t.Fatalf("GET /session = %s", r.raw)
	}
}

func TestSetup_OriginAndValidation(t *testing.T) {
	e := newEnv(t, "ABC123")
	e.human(t, "taken", false, false)
	c := e.client(t)
	wantErr(t, c.do("POST", "/setup", setupBody("ABC123"), map[string]string{"Origin": "https://evil.example"}), 403, "forbidden")
	bad := setupBody("ABC123")
	bad["password"] = "short"
	wantErr(t, c.do("POST", "/setup", bad, nil), 400, "invalid_argument")
	bad = setupBody("ABC123")
	bad["name"] = "  "
	wantErr(t, c.do("POST", "/setup", bad, nil), 400, "invalid_argument")

	before, _ := e.actors.List(context.Background())
	taken := setupBody("ABC123")
	taken["username"] = "taken"
	wantErr(t, c.do("POST", "/setup", taken, nil), 409, "conflict")
	after, _ := e.actors.List(context.Background())
	if len(after) != len(before) {
		t.Fatal("taken username left an orphan admin behind")
	}
}

// Every non-GET route in the table must demand a CSRF token from a
// logged-in session; only the session-less login and setup are exempt, and
// those must reject a cross-origin request instead. Routes added later are
// covered automatically.
func TestRoutes_NonGETRequireCSRF(t *testing.T) {
	e := newEnv(t, "ABC123")
	e.human(t, "boss", true, false)
	c := e.client(t)
	wantStatus(t, c.login("boss", testPassword), 200)
	e.project(t, "x") // routes under /projects/{p} answer 404 for an unknown project, before the CSRF check
	wild := regexp.MustCompile(`\{[^}]+\}`)
	exempt := map[string]bool{"POST /session": true, "POST /setup": true}
	checked := 0
	for _, rt := range e.h.table() {
		method, path, _ := strings.Cut(rt.pattern, " ")
		if method == "GET" || method == "HEAD" {
			continue
		}
		path = wild.ReplaceAllString(path, "x")
		if exempt[rt.pattern] {
			r := c.do(method, path, "{}", map[string]string{"Origin": "https://evil.example"})
			if r.status != 403 || r.body["message"] != "cross-origin request rejected" {
				t.Errorf("%s cross-origin = %d %s, want 403 origin rejection", rt.pattern, r.status, r.raw)
			}
			continue
		}
		r := c.do(method, path, "{}", map[string]string{"X-CSRF-Token": ""})
		if r.status != 403 || r.body["message"] != "invalid or missing CSRF token" {
			t.Errorf("%s without CSRF = %d %s, want 403 CSRF rejection", rt.pattern, r.status, r.raw)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no CSRF-guarded routes found")
	}
}

func TestSetup_ConcurrentCorrectCodes_OneAdmin(t *testing.T) {
	e := newEnv(t, "ABC123")
	const n = 10
	statuses := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b := setupBody("ABC123")
			b["username"] = "root" + string(rune('a'+i))
			statuses[i] = e.client(t).do("POST", "/setup", b, nil).status
		}(i)
	}
	wg.Wait()
	created := 0
	for _, s := range statuses {
		switch s {
		case 201:
			created++
		case 404:
		default:
			t.Fatalf("statuses = %v, want one 201 and the rest 404", statuses)
		}
	}
	if created != 1 {
		t.Fatalf("statuses = %v, want exactly one 201", statuses)
	}
	all, _ := e.actors.List(context.Background())
	if admins, _ := e.access.AdminHumans(context.Background()); len(all) != 1 || len(admins) != 1 {
		t.Fatalf("actors = %+v, want exactly one admin", all)
	}
}

func TestSetup_LoginFailureDisablesOrphanAdmin(t *testing.T) {
	e := newEnv(t, "ABC123")
	e.logins.upsertBroken.Store(true)
	r := e.client(t).do("POST", "/setup", setupBody("ABC123"), nil)
	wantErr(t, r, 500, "internal")
	all, _ := e.actors.List(context.Background())
	if len(all) != 1 || !all[0].Disabled() {
		t.Fatalf("actors = %+v, want the orphan admin disabled", all)
	}
	e.logins.upsertBroken.Store(false)
	// The orphan was renamed, so the retry can use the same (unique) name.
	r = e.client(t).do("POST", "/setup", setupBody("ABC123"), nil)
	wantStatus(t, r, 201)
}
