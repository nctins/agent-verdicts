package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const actorID = "00000000-0000-4000-8000-000000000001"

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// migrationFiles counts migrations/*.sql, i.e. the rows a full run records.
func migrationFiles(t *testing.T) int {
	t.Helper()
	m, err := filepath.Glob("migrations/*.sql")
	if err != nil || len(m) == 0 {
		t.Fatalf("migrations: %v %v", m, err)
	}
	return len(m)
}

func TestMigrate_Idempotent(t *testing.T) {
	pool := pgtest.New(t)
	ctx := context.Background()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("second run: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migration`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if want := migrationFiles(t); n != want {
		t.Fatalf("schema_migration rows = %d, want %d", n, want)
	}
	// 0001 is multi-statement: reaching here proves Exec accepted it in one call.
	var tables int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 17 { // 16 domain tables + schema_migration
		t.Fatalf("tables = %d, want 17", tables)
	}
}

// 0004 re-slugs project ids that are not slugs, keeping children and the old id as name.
func TestMigrate_0004ReslugsProjectIDs(t *testing.T) {
	pool := pgtest.New(t)
	ctx := context.Background()
	mustExec(t, pool, `CREATE TABLE schema_migration (version int PRIMARY KEY, applied_at timestamptz NOT NULL)`)
	for i, f := range []string{"0001_init.sql", "0002_views.sql", "0003_access.sql"} {
		b, err := os.ReadFile(filepath.Join("migrations", f))
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, pool, string(b))
		mustExec(t, pool, `INSERT INTO schema_migration VALUES ($1, now())`, i+1)
	}
	mustExec(t, pool, `INSERT INTO actor (id,type,name,created_at) VALUES ($1,'Agent','a',now())`, actorID)
	// "My Project" and "my-project!" both become my-project; "ok" is already a slug; "***" is empty after trimming.
	mustExec(t, pool, `INSERT INTO project VALUES ('my-project', now()-interval '4 days'), ('My Project', now()-interval '3 days'),
		('my-project!', now()-interval '2 days'), ('ok', now()), ('***', now()), ('_x', now())`)
	mustExec(t, pool, `INSERT INTO contribution (id,project_id,actor_id,kind,title,search_text,created_at)
		VALUES (gen_random_uuid(),'My Project',$1,'claim','t','t',now())`, actorID)
	mustExec(t, pool, `INSERT INTO role_assignment (id,actor_id,role_id,project_id,created_at)
		VALUES (gen_random_uuid(),$1,'viewer','my-project!',now())`, actorID)
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT id, name FROM project ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			t.Fatal(err)
		}
		got[name] = id
	}
	want := map[string]string{"my-project": "my-project", "My Project": "my-project-2", "my-project!": "my-project-3", "ok": "ok", "***": "project", "_x": "x"}
	if len(got) != len(want) {
		t.Fatalf("projects = %v, want %v", got, want)
	}
	for name, id := range want {
		if got[name] != id {
			t.Fatalf("projects = %v, want %v", got, want)
		}
	}
	var n int
	// 0005 backfills updated_at: no signals, so it equals created_at.
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM contribution_v WHERE updated_at = created_at`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("updated_at backfill: n=%d err=%v", n, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM contribution WHERE project_id = 'my-project-2'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("contribution follows: n=%d err=%v", n, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM role_assignment WHERE project_id = 'my-project-3'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("assignment follows: n=%d err=%v", n, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO project (id,name,created_at) VALUES ('Bad Id','x',now())`); err == nil {
		t.Fatal("non-slug id must violate the CHECK")
	}
}

func TestSchema_Constraints(t *testing.T) {
	pool := pgtest.NewMigrated(t)
	ctx := context.Background()
	mustExec(t, pool, `INSERT INTO actor (id,type,name,created_at) VALUES ($1,'Agent','a',now())`, actorID)
	mustExec(t, pool, `INSERT INTO project (id,name,created_at) VALUES ('p','p',now())`)

	if _, err := pool.Exec(ctx, `INSERT INTO contribution (id,project_id,actor_id,kind,title,answers_id,search_text,created_at,updated_at)
		VALUES (gen_random_uuid(),'p',$1,'question','q',gen_random_uuid(),'q',now(),now())`, actorID); err == nil {
		t.Fatal("question with answers_id must be rejected")
	}

	var cid string
	if err := pool.QueryRow(ctx, `INSERT INTO contribution (id,project_id,actor_id,kind,title,search_text,created_at,updated_at)
		VALUES (gen_random_uuid(),'p',$1,'claim','c','c',now(),now()) RETURNING id`, actorID).Scan(&cid); err != nil {
		t.Fatal(err)
	}
	ins := `INSERT INTO signal (id,contribution_id,actor_id,kind,stance,body,created_at)
		VALUES (gen_random_uuid(),$1,$2,$3,'favorable',$4,now())`
	mustExec(t, pool, ins, cid, actorID, "assessment", "") // empty assessment body is fine
	if _, err := pool.Exec(ctx, ins, cid, actorID, "assessment", ""); err == nil {
		t.Fatal("second active assessment by same actor must violate signal_one_active_assessment")
	}
	if _, err := pool.Exec(ctx, ins, cid, actorID, "challenge", ""); err == nil {
		t.Fatal("challenge with empty body must be rejected")
	}
	mustExec(t, pool, ins, cid, actorID, "challenge", "why")

	// embedding without dimension is accepted
	mustExec(t, pool, `INSERT INTO contribution_chunk VALUES ($1,0,'x','m','[1,2,3]')`, cid)
}

func TestSchema_BM25ExactFilter(t *testing.T) {
	pool := pgtest.NewMigrated(t)
	ctx := context.Background()
	mustExec(t, pool, `INSERT INTO actor (id,type,name,created_at) VALUES ($1,'Agent','a',now())`, actorID)
	mustExec(t, pool, `INSERT INTO project (id,name,created_at) VALUES ('my-proj','a',now()), ('my','b',now())`)
	for _, p := range []string{"my-proj", "my"} {
		mustExec(t, pool, `INSERT INTO contribution (id,project_id,actor_id,kind,title,search_text,created_at,updated_at)
			VALUES (gen_random_uuid(),$1,$2,'claim','t','running dogs',now(),now())`, p, actorID)
	}
	var got []string
	rows, err := pool.Query(ctx, `SELECT project_id FROM contribution WHERE search_text @@@ 'run' AND project_id = 'my'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var p string
		rows.Scan(&p)
		got = append(got, p)
	}
	if len(got) != 1 || got[0] != "my" {
		t.Fatalf("got %v, want [my]", got)
	}
}

func TestMigrate_Concurrent(t *testing.T) {
	pool := pgtest.New(t)
	ctx := context.Background()
	const n = 5
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = postgres.Migrate(ctx, pool) }()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("migrator %d: %v", i, err)
		}
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migration`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if want := migrationFiles(t); rows != want {
		t.Fatalf("schema_migration rows = %d, want %d", rows, want)
	}
}

// A bare ParadeDB database (the stock image, or a managed one) has no
// extensions yet: the first migration creates them.
func TestMigrate_CreatesExtensionsOnBareDatabase(t *testing.T) {
	url := os.Getenv("AGENTVERDICTS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AGENTVERDICTS_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) }) // after the DROP below (cleanups run last-in first-out)
	name := "t_bare_" + strings.ReplaceAll(id.New(), "-", "")[:12]
	mustExecConn(t, admin, "CREATE DATABASE "+name+" TEMPLATE template0")
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)") })

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate on a bare database: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_extension WHERE extname IN ('pg_search','vector')`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("extensions = %d, %v; want 2", n, err)
	}
}

func mustExecConn(t *testing.T, c *pgx.Conn, sql string) {
	t.Helper()
	if _, err := c.Exec(context.Background(), sql); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// 0009 makes actor names unique ignoring case: later duplicates (by created_at, id)
// get " (2)", " (3)"..., and the base is truncated to stay within 64 characters.
func TestMigrate_0009DeduplicatesActorNames(t *testing.T) {
	pool := pgtest.New(t)
	ctx := context.Background()
	mustExec(t, pool, `CREATE TABLE schema_migration (version int PRIMARY KEY, applied_at timestamptz NOT NULL)`)
	files, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	for i, f := range files {
		if strings.Contains(f, "0009_") {
			files = files[:i]
			break
		}
	}
	if len(files) != 8 {
		t.Fatalf("migrations before 0009 = %d, want 8", len(files))
	}
	for i, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, pool, string(b))
		mustExec(t, pool, `INSERT INTO schema_migration VALUES ($1, now())`, i+1)
	}
	long := strings.Repeat("é", 64) // 64 characters, 128 bytes
	ins := `INSERT INTO actor (id, type, name, created_at) VALUES ($1, 'Human', $2, now() - $3::int * interval '1 minute')`
	for i, a := range []struct{ id, name string }{
		{"00000000-0000-4000-8000-000000000001", "Dana"},     // oldest: keeps its name
		{"00000000-0000-4000-8000-000000000002", "dana"},     // case variant
		{"00000000-0000-4000-8000-000000000003", "DANA"},     // second case variant
		{"00000000-0000-4000-8000-000000000004", "Dana (2)"}, // already holds the first suffix
		{"00000000-0000-4000-8000-000000000005", long},
		{"00000000-0000-4000-8000-000000000006", strings.ToUpper(long)},
		{"00000000-0000-4000-8000-000000000007", "Solo"},
	} {
		mustExec(t, pool, ins, a.id, a.name, 100-i)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	rows, err := pool.Query(ctx, `SELECT id::text, name FROM actor`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			t.Fatal(err)
		}
		got[id[len(id)-1:]] = name
	}
	if got["1"] != "Dana" || got["7"] != "Solo" || got["5"] != long {
		t.Fatalf("first holders must keep their names: %v", got)
	}
	// "dana" (older) takes "dana (2)" first, so the pre-existing "Dana (2)" moves on.
	if got["2"] != "dana (2)" || got["3"] != "DANA (3)" || got["4"] != "Dana (2) (2)" {
		t.Fatalf("suffixes = %v", got)
	}
	seen := map[string]bool{}
	for k, name := range got {
		if n := len([]rune(name)); n < 1 || n > 64 {
			t.Fatalf("actor %s: name %q has %d characters", k, name, n)
		}
		if l := strings.ToLower(name); seen[l] {
			t.Fatalf("names still collide: %v", got)
		} else {
			seen[l] = true
		}
	}
	if want := strings.Repeat("é", 60) + " (2)"; !strings.EqualFold(got["6"], want) {
		t.Fatalf("64-character duplicate = %q, want the base cut to 60 and \" (2)\" appended", got["6"])
	}
	if _, err := pool.Exec(ctx, `INSERT INTO actor (id, type, name, created_at) VALUES (gen_random_uuid(), 'Agent', 'SOLO', now())`); err == nil {
		t.Fatal("a name differing only by case must violate the unique index")
	}
}

// 0012 recounts with the author's favorable signals left out; re-running it
// on counters from the old rule fixes them.
func TestMigrate_0012IgnoresSelfSupport(t *testing.T) {
	pool := pgtest.NewMigrated(t)
	mustExec(t, pool, `INSERT INTO actor (id,type,name,created_at) VALUES ($1,'Agent','a',now()), ($2,'Agent','b',now())`, actorID, "00000000-0000-4000-8000-000000000002")
	mustExec(t, pool, `INSERT INTO project (id,name,created_at) VALUES ('p','p', now())`)
	mustExec(t, pool, `INSERT INTO contribution (id,project_id,actor_id,kind,title,search_text,created_at,updated_at,favorable_n,favorable_actor_n,unfavorable_n)
		VALUES ('00000000-0000-4000-8000-0000000000c1','p',$1,'claim','t','t',now(),now(),2,2,1)`, actorID)
	for _, s := range [][3]string{{actorID, "assessment", "favorable"}, {"00000000-0000-4000-8000-000000000002", "assessment", "favorable"}, {actorID, "challenge", "unfavorable"}} {
		mustExec(t, pool, `INSERT INTO signal (id,contribution_id,actor_id,kind,stance,body,created_at)
			VALUES (gen_random_uuid(),'00000000-0000-4000-8000-0000000000c1',$1,$2,$3,'x',now())`, s[0], s[1], s[2])
	}
	b, err := os.ReadFile(filepath.Join("migrations", "0012_ignore_self_support.sql"))
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, string(b))
	var f, fa, u int
	if err := pool.QueryRow(context.Background(), `SELECT favorable_n, favorable_actor_n, unfavorable_n FROM contribution`).Scan(&f, &fa, &u); err != nil {
		t.Fatal(err)
	}
	if f != 1 || fa != 1 || u != 1 {
		t.Fatalf("counts = %d %d %d, want 1 1 1", f, fa, u)
	}
}
