package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"agent-verdicts/internal/platform/pgtest"

	"github.com/jackc/pgx/v5/pgxpool"
)

// useTestDB points AGENTVERDICTS_DATABASE_URL at a fresh, migrated per-test
// schema and returns a pool on that schema for seeding and assertions.
func useTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := pgtest.NewMigrated(t)
	u, err := url.Parse(os.Getenv("AGENTVERDICTS_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", pool.Config().ConnConfig.RuntimeParams["search_path"])
	u.RawQuery = q.Encode()
	t.Setenv("AGENTVERDICTS_DATABASE_URL", u.String())
	return pool
}

func TestIsHelpArg(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		if !isHelpArg(arg) {
			t.Errorf("isHelpArg(%q) = false, want true", arg)
		}
	}
	for _, arg := range []string{"list", "show", "credential", "", "-help-me"} {
		if isHelpArg(arg) {
			t.Errorf("isHelpArg(%q) = true, want false", arg)
		}
	}
}

func TestPrintUsage_CoversEveryMode(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	for _, want := range []string{"serve", "agentverdicts credential", "bootstrap", "issue", "revoke",
		"migrate", "list", "show", "AGENTVERDICTS_DATABASE_URL", "AGENTVERDICTS_HTTP_ADDR", defaultHTTPAddr} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("printUsage() output missing %q", want)
		}
	}
}

func TestNormalizeHelpArgs(t *testing.T) {
	tests := []struct{ in, want []string }{
		{[]string{"help", "credential"}, []string{"credential", "-h"}},
		{[]string{"help"}, []string{"help"}},
		{[]string{"credential", "issue"}, []string{"credential", "issue"}},
	}
	for _, tt := range tests {
		if got := normalizeHelpArgs(tt.in); strings.Join(got, " ") != strings.Join(tt.want, " ") {
			t.Errorf("normalizeHelpArgs(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestRunCLI_UnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runCLI(context.Background(), []string{"bogus"}, &stdout, &stderr); code != 2 {
		t.Errorf("bogus: code = %d, want 2", code)
	}
}

func TestRunCLI_MissingDatabaseURL(t *testing.T) {
	t.Setenv("AGENTVERDICTS_DATABASE_URL", "")
	for _, cmd := range []string{"list", "show", "migrate"} {
		var stdout, stderr bytes.Buffer
		if code := runCLI(context.Background(), []string{cmd, "-project", "x", "-id", "y"}, &stdout, &stderr); code != 2 {
			t.Errorf("%s: code = %d, want 2", cmd, code)
		}
		if !strings.Contains(stderr.String(), "AGENTVERDICTS_DATABASE_URL") {
			t.Errorf("%s: stderr = %q, want the variable named", cmd, stderr.String())
		}
	}
}

func TestRunCLI_MigrateIsIdempotent(t *testing.T) {
	useTestDB(t)
	for i := 0; i < 2; i++ {
		var stdout, stderr bytes.Buffer
		if code := runCLI(context.Background(), []string{"migrate"}, &stdout, &stderr); code != 0 {
			t.Fatalf("run %d: code = %d, stderr = %q", i, code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "migrations applied") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	}
}

func TestRunCLI_ListOnEmptyProject(t *testing.T) {
	useTestDB(t)
	var stdout, stderr bytes.Buffer
	if code := runCLI(context.Background(), []string{"list", "-project", "p"}, &stdout, &stderr); code != 0 || stdout.Len() != 0 {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if code := runCLI(context.Background(), []string{"show", "-project", "p", "-id", "nope"}, &stdout, &stderr); code != 1 {
		t.Fatalf("show missing: code = %d, want 1", code)
	}
}

// Help never needs the database, even when none is configured.
func TestRunCredentialCLI_Help_PrintsUsageWithoutDB(t *testing.T) {
	t.Setenv("AGENTVERDICTS_DATABASE_URL", "")
	for _, args := range [][]string{{"-h"}, {"--help"}, {"help"}} {
		var stdout, stderr bytes.Buffer
		if code := runCredentialCLI(context.Background(), args, &stdout, &stderr); code != 0 {
			t.Fatalf("runCredentialCLI(%v) code = %d, want 0", args, code)
		}
		for _, want := range []string{"bootstrap", "issue", "revoke"} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("runCredentialCLI(%v) stdout missing %q", args, want)
			}
		}
	}
	var stdout, stderr bytes.Buffer
	runCredentialCLI(context.Background(), []string{"bootstrap", "-h"}, &stdout, &stderr)
	if !strings.Contains(stderr.String(), "-actor-type") {
		t.Fatalf("bootstrap -h stderr = %q, want bootstrap flags", stderr.String())
	}
}

func TestMissingDatabaseURL_NamesTheVariable(t *testing.T) {
	t.Setenv("AGENTVERDICTS_DATABASE_URL", "")
	var stdout, stderr bytes.Buffer
	if code := runCredentialCLI(context.Background(), []string{"issue", "-actor", "x"}, &stdout, &stderr); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "AGENTVERDICTS_DATABASE_URL") {
		t.Fatalf("stderr = %q, want the variable named", stderr.String())
	}
	if err := run(context.Background()); err == nil || !strings.Contains(err.Error(), "AGENTVERDICTS_DATABASE_URL") {
		t.Fatalf("run() = %v, want error naming AGENTVERDICTS_DATABASE_URL", err)
	}
}

func TestMux_Healthz(t *testing.T) {
	rec := httptest.NewRecorder()
	mcp := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session" {
			t.Errorf("api path = %q, want prefix stripped", r.URL.Path)
		}
		w.WriteHeader(http.StatusAccepted)
	})
	newMux(mcp, api).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want 200", rec.Code)
	}
	for _, p := range []string{"/", "/console/"} {
		rec = httptest.NewRecorder()
		newMux(mcp, api).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404 (no web surface on the api)", p, rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	newMux(mcp, api).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/p", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("POST /mcp/p = %d, want the MCP handler", rec.Code)
	}
	for _, p := range []string{"/mcp", "/mcp/", "/mcp/p/x"} {
		rec = httptest.NewRecorder()
		newMux(mcp, api).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("POST %s = %d, want 404", p, rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	newMux(mcp, api).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/session", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("GET /api/v1/session = %d, want the API handler", rec.Code)
	}
}

func TestNewHTTPServer_HasReadHeaderTimeout(t *testing.T) {
	if srv := newHTTPServer(":0", http.NotFoundHandler()); srv.ReadHeaderTimeout <= 0 {
		t.Fatalf("ReadHeaderTimeout = %v, want > 0", srv.ReadHeaderTimeout)
	}
}

func TestRun_OpensDBAndStopsOnCancel(t *testing.T) {
	useTestDB(t)
	t.Setenv("AGENTVERDICTS_HTTP_ADDR", "127.0.0.1:0")
	t.Setenv("AGENTVERDICTS_SECRET_DIR", t.TempDir()+"/secrets")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return within 5s after cancel")
	}
}
