package search_test

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/embedding"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/platform/textfold"
	"agent-verdicts/internal/search"
	"agent-verdicts/internal/setting"
	"agent-verdicts/internal/signal"
	"agent-verdicts/internal/trust"
)

const embDim = 16

// hashEmbed is a bag-of-words vector: each folded word bumps one bucket.
func hashEmbed(text string) []float32 {
	v := make([]float32, embDim)
	v[0] = 0.01 // never the zero vector
	for _, w := range strings.Fields(textfold.Fold(text)) {
		h := fnv.New32a()
		h.Write([]byte(w))
		v[h.Sum32()%embDim]++
	}
	return v
}

func embedServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Input []string }
		if r.URL.Path != "/embeddings" || json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "bad request", 400)
			return
		}
		type item struct {
			Embedding []float32 `json:"embedding"`
		}
		var out struct {
			Data []item `json:"data"`
		}
		for _, in := range req.Input {
			out.Data = append(out.Data, item{hashEmbed(in)})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

type store struct{ v []byte }

func (s *store) Get(context.Context, string) ([]byte, error) {
	if s.v == nil {
		return nil, setting.ErrNotFound
	}
	return s.v, nil
}
func (s *store) Put(_ context.Context, _ string, v []byte) error { s.v = v; return nil }

func TestIntegration_VectorPathOnPostgres(t *testing.T) {
	pool := pgtest.NewMigrated(t)
	ctx := context.Background()
	const actorID = "00000000-0000-4000-8000-000000000001"
	for _, q := range []string{
		`INSERT INTO actor (id,type,name,created_at) VALUES ('` + actorID + `','Agent','a',now())`,
		`INSERT INTO project (id,name,created_at) VALUES ('p','p', now())`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	srv := embedServer(t)
	raw, _ := json.Marshal(map[string]any{"embedding": map[string]any{"url": srv.URL, "model": "hash", "dim": embDim}})
	settings, err := setting.NewService(ctx, &store{v: raw}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	repo := postgres.NewContributionRepo(pool)
	deps := search.Deps{
		Lexical: postgres.NewLexicalIndex(pool), Vector: postgres.NewVectorIndex(pool), Chunks: postgres.NewChunkRepo(pool),
		Contributions: repo, Settings: settings,
		NewEmbedder: func(p setting.Provider) search.Embedder { return embedding.New(p.URL, p.Model, p.APIKey, nil) },
	}
	w := search.NewWorker(deps, log.New(io.Discard, "", 0))
	deps.Reindexing = w.Reindexing
	svc := search.NewService(deps)

	create := func(title, body string) string {
		c, err := contribution.Create(ctx, postgres.NewProjectRepo(pool), postgres.NewActorRepo(pool), repo,
			contribution.NewInput{ProjectID: "p", ActorID: actorID, Kind: contribution.KindClaim, Title: title, Body: body})
		if err != nil {
			t.Fatal(err)
		}
		w.Enqueue(c.ID)
		return c.ID
	}
	match := create("Cache invalidation", "Redis cache invalidation needs a TTL.")
	other := create("Unrelated", "Gardening tomatoes in spring.")

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { w.Run(runCtx); close(done) }()
	defer func() { cancel(); <-done }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		st, err := svc.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.EmbeddedCount == 2 && st.TotalCount == 2 {
			if st.Pipeline != "bm25+vector" {
				t.Fatalf("pipeline = %q", st.Pipeline)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not embedded: %+v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}

	r, err := svc.Search(ctx, search.Query{ProjectID: "p", Text: "cache invalidation", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if r.Degraded || len(r.Results) != 2 || r.Results[0].ID != match || r.Results[1].ID != other {
		t.Fatalf("results = %+v degraded=%v", r.Results, r.Degraded)
	}
	// "other" has no lexical match, so it came from the vector path; vector
	// snippets are the head of the best chunk without the title it starts with.
	if r.Results[1].Snippet != "Gardening tomatoes in spring." || r.Results[0].Snippet != "Redis cache invalidation needs a TTL." {
		t.Errorf("snippets = %q, %q", r.Results[0].Snippet, r.Results[1].Snippet)
	}
}

// Spec 2026-11-15-web-usability-2 §1: a state filter picks from every match,
// so a filtered search returns `limit` matching results even when more than
// `limit` unfiltered hits rank above them.
func TestIntegration_StateFilterFillsLimit(t *testing.T) {
	pool := pgtest.NewMigrated(t)
	ctx := context.Background()
	const authorID, voterID = "00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"
	for _, q := range []string{
		`INSERT INTO actor (id,type,name,created_at) VALUES ('` + authorID + `','Agent','author',now()), ('` + voterID + `','Agent','voter',now())`,
		`INSERT INTO project (id,name,created_at) VALUES ('p','p', now())`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	settings, err := setting.NewService(ctx, &store{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	repo := postgres.NewContributionRepo(pool)
	svc := search.NewService(search.Deps{
		Lexical: postgres.NewLexicalIndex(pool), Vector: postgres.NewVectorIndex(pool), Chunks: postgres.NewChunkRepo(pool),
		Contributions: repo, Settings: settings,
	})
	create := func(title, body string) string {
		c, err := contribution.Create(ctx, postgres.NewProjectRepo(pool), postgres.NewActorRepo(pool), repo,
			contribution.NewInput{ProjectID: "p", ActorID: authorID, Kind: contribution.KindClaim, Title: title, Body: body})
		if err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	for i := 0; i < 8; i++ { // strong matches, no signals: InsufficientEvidence
		create("cache cache cache", "cache cache cache cache")
	}
	favorable := map[string]bool{}
	for i := 0; i < 5; i++ { // weak matches, each Favorable
		id := create("notes", "one mention of cache among "+strings.Repeat("filler words ", 30))
		favorable[id] = true
		if _, err := signal.Create(ctx, repo, postgres.NewActorRepo(pool), postgres.NewSignalRepo(pool),
			signal.NewInput{ProjectID: "p", ContributionID: id, ActorID: voterID, Kind: signal.KindAssessment, Stance: signal.StanceFavorable}); err != nil {
			t.Fatal(err)
		}
	}
	const limit = 3
	r, err := svc.Search(ctx, search.Query{ProjectID: "p", Text: "cache", Limit: limit, Filters: search.Filters{States: []trust.State{trust.StateFavorable}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Results) != limit {
		t.Fatalf("filtered search returned %d results, want %d", len(r.Results), limit)
	}
	for _, res := range r.Results {
		if !favorable[res.ID] || res.State == nil || *res.State != trust.StateFavorable {
			t.Fatalf("result %+v is not a Favorable match", res)
		}
	}
	if r, _ = svc.Search(ctx, search.Query{ProjectID: "p", Text: "cache", Limit: limit}); len(r.Results) != limit || favorable[r.Results[0].ID] {
		t.Fatalf("unfiltered top results should be the strong, non-Favorable ones: %+v", r.Results)
	}
}

// Spec 2026-11-15-role-play-fixes §A.2: Vietnamese function words in the
// query do not pull an unrelated title above the real match.
func TestIntegration_VietnameseStopwords(t *testing.T) {
	pool := pgtest.NewMigrated(t)
	ctx := context.Background()
	const actorID = "00000000-0000-4000-8000-000000000001"
	for _, q := range []string{
		`INSERT INTO actor (id,type,name,created_at) VALUES ('` + actorID + `','Agent','a',now())`,
		`INSERT INTO project (id,name,created_at) VALUES ('p','p', now())`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	settings, err := setting.NewService(ctx, &store{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	repo := postgres.NewContributionRepo(pool)
	svc := search.NewService(search.Deps{
		Lexical: postgres.NewLexicalIndex(pool), Vector: postgres.NewVectorIndex(pool), Chunks: postgres.NewChunkRepo(pool),
		Contributions: repo, Settings: settings,
	})
	create := func(kind contribution.Kind, title, body string) string {
		c, err := contribution.Create(ctx, postgres.NewProjectRepo(pool), postgres.NewActorRepo(pool), repo,
			contribution.NewInput{ProjectID: "p", ActorID: actorID, Kind: kind, Title: title, Body: body})
		if err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	timeout := create(contribution.KindQuestion, "Timeout mặc định của HTTP client nội bộ là bao nhiêu?", "Khi gọi dịch vụ nội bộ thì timeout là bao nhiêu?")
	pool20 := create(contribution.KindClaim, "PgBouncer pool_size cho orders: 20 ở ≤400 rps, 40 ở 1200 rps", "Đo trên staging với pgbench.")
	found := func(q string) []string {
		r, err := svc.Search(ctx, search.Query{ProjectID: "p", Text: q})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, x := range r.Results {
			out = append(out, x.ID)
		}
		return out
	}
	if got := found("pool bao nhiêu thì đủ khi tải cao"); len(got) == 0 || got[0] != pool20 {
		t.Fatalf("results = %v, want the pool claim %s first (timeout question is %s)", got, pool20, timeout)
	}
	if got := found("là bao nhiêu"); len(got) != 1 || got[0] != timeout {
		t.Fatalf("all-stopword query = %v, want [%s]", got, timeout)
	}
}
