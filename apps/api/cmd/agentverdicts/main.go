// Command agentverdicts is the single binary of Agent Verdicts.
//
//	agentverdicts [serve]          HTTP server on AGENTVERDICTS_HTTP_ADDR (default)
//	agentverdicts credential …     operator CLI for actors, tokens and passwords
//	agentverdicts list|show …      read CLI for contributions and signals
//	agentverdicts migrate          apply database migrations
//
// The server exposes GET /healthz, /mcp and the JSON API under /api/v1.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"agent-verdicts/internal/adapter/authflow"
	"agent-verdicts/internal/adapter/cli"
	"agent-verdicts/internal/adapter/httpapi"
	"agent-verdicts/internal/adapter/httpapi/session"
	"agent-verdicts/internal/adapter/mcp"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/embedding"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/platform/rerank"
	"agent-verdicts/internal/reads"
	"agent-verdicts/internal/search"
	"agent-verdicts/internal/setting"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultHTTPAddr  = ":8080"
	defaultSecretDir = "/secrets"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	args := normalizeHelpArgs(os.Args[1:])
	switch {
	case len(args) > 0 && isHelpArg(args[0]):
		printUsage(os.Stdout)
	case len(args) > 0 && args[0] == "credential":
		os.Exit(runCredentialCLI(ctx, args[1:], os.Stdout, os.Stderr))
	case len(args) == 0 || (len(args) == 1 && args[0] == "serve"):
		if err := run(ctx); err != nil {
			log.Fatalf("agentverdicts: %v", err)
		}
	default:
		os.Exit(runCLI(ctx, args, os.Stdout, os.Stderr))
	}
}

// isHelpArg reports whether arg asks for usage text.
func isHelpArg(arg string) bool {
	return arg == "help" || arg == "-h" || arg == "--help"
}

// hasHelpArg reports whether any argument asks for usage text — used to
// skip opening the database on a help-only invocation.
func hasHelpArg(args []string) bool {
	for _, a := range args {
		if isHelpArg(a) {
			return true
		}
	}
	return false
}

// normalizeHelpArgs rewrites `help <cmd> …` to `<cmd> … -h` so both
// spellings reach the subcommand's own flag usage.
func normalizeHelpArgs(args []string) []string {
	if len(args) < 2 || args[0] != "help" {
		return args
	}
	out := append([]string{}, args[1:]...)
	return append(out, "-h")
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `Agent Verdicts — agent-native forum for trust signals on Contributions.

Usage:
  agentverdicts [serve]                         start the HTTP server (default)
  agentverdicts credential bootstrap -actor-type Agent -name <name>
                                                create an agent and its MCP token
  agentverdicts credential bootstrap -actor-type Human -name <name> -username <u> [-admin]
                                                create a person with a temporary password
  agentverdicts credential password -username <u> [-actor <actor-id>]
                                                set a new temporary login password
  agentverdicts credential issue  -actor <actor-id> [-label <text>] [-expires-in-days 30|90|365|0]
  agentverdicts credential revoke -credential <credential-id>
  agentverdicts list -project <id> [-kind claim|question] [-state <State> ...]
                                                one line per contribution, newest first
  agentverdicts show -project <id> -id <contribution-id>
                                                a contribution with its signals or answers
  agentverdicts migrate                         apply database migrations and exit
  agentverdicts help [command]                  this text, or a command's flags

Environment:
  AGENTVERDICTS_DATABASE_URL  Postgres URL (required)
  AGENTVERDICTS_HTTP_ADDR     listen address (default %s)
  AGENTVERDICTS_SECRET_DIR    directory of the settings key (default %s)
  AGENTVERDICTS_MCP_RATE_PER_MIN  requests per minute one actor may make to /mcp (default %d, 0 = no limit)
`, defaultHTTPAddr, defaultSecretDir, mcp.DefaultRatePerMin)
}

// runCLI handles every argument that is not serve/credential/help.
func runCLI(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	switch args[0] {
	case "list", "show", "migrate":
	default:
		fmt.Fprintf(stderr, "agentverdicts: unknown command %q\n", args[0])
		printUsage(stderr)
		return 2
	}
	if hasHelpArg(args) { // flag usage only; no database needed
		return cli.Run(ctx, args, stdout, stderr, cli.Deps{})
	}
	pool, err := openDB(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "agentverdicts %s: %v\n", args[0], err)
		return 2
	}
	defer pool.Close()
	if args[0] == "migrate" { // openDB already migrated
		fmt.Fprintln(stdout, "migrations applied")
		return 0
	}
	return cli.Run(ctx, args, stdout, stderr, cli.Deps{
		Contributions: postgres.NewContributionRepo(pool), Signals: postgres.NewSignalRepo(pool), Actors: postgres.NewActorRepo(pool),
	})
}

// openDB opens and migrates the database named by AGENTVERDICTS_DATABASE_URL.
func openDB(ctx context.Context) (*pgxpool.Pool, error) {
	url := os.Getenv("AGENTVERDICTS_DATABASE_URL")
	if url == "" {
		return nil, errors.New("AGENTVERDICTS_DATABASE_URL is required")
	}
	pool, err := postgres.Open(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// newHTTPServer is the one place server-wide limits live. ReadHeaderTimeout
// stops a slow client from holding a connection open indefinitely.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
}

// newRuntime wires settings, search, MCP and the JSON API on pool and
// returns the server's whole HTTP handler. The returned worker is not
// started.
func newRuntime(ctx context.Context, pool *pgxpool.Pool) (http.Handler, *search.Worker, error) {
	dir := os.Getenv("AGENTVERDICTS_SECRET_DIR")
	if dir == "" {
		dir = defaultSecretDir
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("secret dir: %w", err)
	}
	cipher, err := setting.LoadKey(dir)
	if err != nil {
		return nil, nil, err
	}
	settings, err := setting.NewService(ctx, postgres.NewSettingRepo(pool), cipher, testProviders)
	if err != nil {
		return nil, nil, err
	}
	for _, w := range settings.Warnings() {
		log.Printf("agentverdicts: settings warning: %s", w)
	}

	contributions := postgres.NewContributionRepo(pool)
	d := search.Deps{
		Lexical: postgres.NewLexicalIndex(pool), Vector: postgres.NewVectorIndex(pool), Chunks: postgres.NewChunkRepo(pool),
		Contributions: contributions, Settings: settings,
		NewEmbedder: func(p setting.Provider) search.Embedder { return embedding.New(p.URL, p.Model, p.APIKey, nil) },
		NewReranker: func(p setting.Provider) search.Reranker { return rerank.New(p.URL, p.Model, p.APIKey, nil) },
	}
	worker := search.NewWorker(d, log.Default())
	d.Reindexing = worker.Reindexing
	projects, actors, signals := postgres.NewProjectRepo(pool), postgres.NewActorRepo(pool), postgres.NewSignalRepo(pool)
	credentials, logins := postgres.NewCredentialRepo(pool), postgres.NewLoginRepo(pool)
	acc := postgres.NewAccessRepo(pool)
	readsRepo := postgres.NewReadsRepo(pool)
	searchSvc := search.NewService(d)

	// First-run setup: while no Human admin can log in, a one-time code in
	// the log lets the operator create one from the browser.
	setupCode := ""
	if need, err := authflow.NeedsSetup(ctx, logins, acc); err != nil {
		return nil, nil, fmt.Errorf("check first-run setup: %w", err)
	} else if need {
		setupCode = strings.ToUpper(authentication.GenerateCode(6))
		log.Printf("agentverdicts: first-run setup — open /setup/ in the web UI (or POST /api/v1/setup) and enter code %s", setupCode)
	}
	// One session store and one login/setup flow for the JSON API.
	sessions, err := session.NewStore(ctx, postgres.NewSessionRepo(pool))
	if err != nil {
		return nil, nil, err
	}
	ratePerMin, err := mcpRatePerMin(os.Getenv("AGENTVERDICTS_MCP_RATE_PER_MIN"))
	if err != nil {
		return nil, nil, err
	}
	auth := authflow.New(actors, logins, acc, setupCode)
	if v := os.Getenv("AGENTVERDICTS_LOGIN_LOCKOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, nil, fmt.Errorf("AGENTVERDICTS_LOGIN_LOCKOUT must be a positive duration such as 30s or 15m, got %q", v)
		}
		auth.SetLockout(d)
	}

	mcpHandler := mcp.NewHandler(mcp.Deps{
		Projects: projects, Actors: actors, Contributions: contributions,
		Signals: signals, Credentials: credentials, Access: acc, Search: searchSvc, Worker: worker, Reads: readsRepo,
		RatePerMin: ratePerMin,
	})
	api := httpapi.New(httpapi.Deps{
		Actors: actors, Projects: projects, Contributions: contributions, Signals: signals,
		Credentials: credentials, Logins: logins, Access: acc, Audit: postgres.NewAuditRepo(pool), Sessions: sessions, Auth: auth,
		Search: searchSvc, Worker: worker, Settings: settings, Reads: readsRepo,
	})
	return newMux(mcpHandler, api), worker, nil
}

// rollupReads rolls old daily read counts into monthly totals at startup
// and then once a day, until ctx is cancelled.
func rollupReads(ctx context.Context, repo reads.Repository) {
	for {
		today, _ := time.Parse(time.DateOnly, time.Now().UTC().Format(time.DateOnly))
		if err := repo.Rollup(ctx, today); err != nil && ctx.Err() == nil {
			log.Printf("agentverdicts: reads rollup: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(24 * time.Hour):
		}
	}
}

// mcpRatePerMin parses AGENTVERDICTS_MCP_RATE_PER_MIN: empty = the default,
// 0 = no limit.
func mcpRatePerMin(v string) (int, error) {
	if v == "" {
		return mcp.DefaultRatePerMin, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("AGENTVERDICTS_MCP_RATE_PER_MIN must be a number of requests per minute (0 = no limit), got %q", v)
	}
	return n, nil
}

// run opens the database, serves HTTP until ctx is cancelled, then shuts
// the server down before closing the pool.
func run(ctx context.Context) error {
	// Startup uses a context decoupled from ctx: a shutdown signal arriving
	// mid-startup must not turn a clean shutdown into an error.
	pool, err := openDB(context.Background())
	if err != nil {
		return err
	}
	defer pool.Close()

	handler, worker, err := newRuntime(context.Background(), pool)
	if err != nil {
		return err
	}
	workerCtx, stopWorker := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() { worker.Run(workerCtx); close(workerDone) }()
	rollupDone := make(chan struct{})
	go func() { rollupReads(workerCtx, postgres.NewReadsRepo(pool)); close(rollupDone) }()
	// Deferred: stops the worker and the rollup after srv.Shutdown below, before pool.Close.
	defer func() { stopWorker(); <-workerDone; <-rollupDone }()

	addr := os.Getenv("AGENTVERDICTS_HTTP_ADDR")
	if addr == "" {
		addr = defaultHTTPAddr
	}
	srv := newHTTPServer(addr, handler)

	serveErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()
	log.Printf("agentverdicts: listening on %s", addr)

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
	case <-ctx.Done():
	}
	log.Println("agentverdicts: shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http server shutdown: %w", err)
	}
	return nil
}
