// Command eval measures search quality and latency on eval/dataset.json
// (spec §11):
//
//	go run ./eval -database-url URL [-embed-url U -embed-model M]
//	              [-rerank-url U -rerank-model M] [-filler N]
//
// It loads the dataset into a temporary schema (dropped at the end), runs
// BM25, BM25+vector and BM25+vector+rerank (each only when its providers
// are given), and prints R@1/R@5/MRR overall, R@5 by kind and language, and
// p50 latency per search call and per step. With -filler N it then adds N
// copies of the distractors and measures latency again; quality is always
// measured on the dataset alone. Not run by go test: it needs real models.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/embedding"
	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/platform/rerank"
	"agent-verdicts/internal/project"
	"agent-verdicts/internal/search"
	"agent-verdicts/internal/setting"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed dataset.json
var datasetJSON []byte

const projectID = "eval"

type contrib struct {
	ID, Title, Body, Lang string
	Distractor            bool
}

type query struct {
	Query   string   `json:"query"`
	GoldIDs []string `json:"gold_ids"`
	Kind    string   `json:"kind"`
	Lang    string   `json:"lang"`
}

type dataset struct {
	Note          string    `json:"note"`
	Contributions []contrib `json:"contributions"`
	Queries       []query   `json:"queries"`
}

// score: r1 = top hit is gold; r5 = share of gold in the top 5; mrr =
// 1/rank of the first gold hit in ranked (0 if none).
func score(ranked, gold []string) (r1, r5, mrr float64) {
	found := 0
	for i, id := range ranked {
		if !slices.Contains(gold, id) {
			continue
		}
		if mrr == 0 {
			mrr = 1 / float64(i+1)
		}
		if i == 0 {
			r1 = 1
		}
		if i < 5 {
			found++
		}
	}
	return r1, float64(found) / float64(len(gold)), mrr
}

// isViVi: a Vietnamese query whose gold contains Vietnamese content.
func isViVi(q query, byID map[string]contrib) bool {
	if q.Lang != "vi" {
		return false
	}
	for _, g := range q.GoldIDs {
		if byID[g].Lang == "en" {
			return false
		}
	}
	return true
}

func main() {
	var f flags
	flag.StringVar(&f.dbURL, "database-url", os.Getenv("AGENTVERDICTS_DATABASE_URL"), "Postgres URL (ParadeDB)")
	flag.StringVar(&f.embedURL, "embed-url", "", "OpenAI-compatible embeddings base URL; empty skips vector configs")
	flag.StringVar(&f.embedModel, "embed-model", "", "embedding model")
	flag.StringVar(&f.rerankURL, "rerank-url", "", "rerank base URL; empty skips the rerank config")
	flag.StringVar(&f.rerankModel, "rerank-model", "", "rerank model")
	flag.IntVar(&f.filler, "filler", 0, "add N copies of the distractors and measure latency again")
	flag.Parse()
	if f.dbURL == "" {
		log.Fatal("eval: -database-url or AGENTVERDICTS_DATABASE_URL is required")
	}
	if err := run(context.Background(), f, os.Stdout); err != nil {
		log.Fatalf("eval: %v", err)
	}
}

type flags struct {
	dbURL, embedURL, embedModel, rerankURL, rerankModel string
	filler                                              int
}

func run(ctx context.Context, f flags, out io.Writer) (err error) {
	var d dataset
	if err := json.Unmarshal(datasetJSON, &d); err != nil {
		return err
	}
	pool, drop, err := tempSchema(ctx, f.dbURL)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, drop()) }()

	l, err := newLoader(ctx, pool, f)
	if err != nil {
		return err
	}
	for _, c := range d.Contributions {
		if err := l.add(ctx, c.ID, c.Title, c.Body); err != nil {
			return err
		}
	}
	if err := l.finish(ctx); err != nil {
		return err
	}

	cfgs := []config{{name: "bm25"}}
	if f.embedURL != "" {
		cfgs = append(cfgs, config{name: "bm25+vector", embed: true})
		if f.rerankURL != "" {
			cfgs = append(cfgs, config{name: "bm25+vector+rerank", embed: true, rerank: true})
		}
	}
	byID := map[string]contrib{}
	for _, c := range d.Contributions {
		byID[c.ID] = c
	}
	fmt.Fprintf(out, "dataset: %d contributions, %d queries (%s)\n", len(d.Contributions), len(d.Queries), d.Note)
	var runs []*result
	for _, c := range cfgs {
		r, err := measure(ctx, pool, l, f, c, d.Queries)
		if err != nil {
			return err
		}
		runs = append(runs, r)
	}
	report(out, runs, d.Queries, byID)
	latency(out, fmt.Sprintf("latency p50 (ms), %d contributions", len(d.Contributions)), runs)

	if f.filler > 0 {
		var noise []contrib
		for _, c := range d.Contributions {
			if c.Distractor {
				noise = append(noise, c)
			}
		}
		for i := 0; i < f.filler; i++ {
			c := noise[i%len(noise)]
			if err := l.add(ctx, fmt.Sprintf("filler-%d", i), c.Title, c.Body); err != nil {
				return err
			}
		}
		if err := l.finish(ctx); err != nil {
			return err
		}
		runs = runs[:0]
		for _, c := range cfgs {
			r, err := measure(ctx, pool, l, f, c, d.Queries)
			if err != nil {
				return err
			}
			runs = append(runs, r)
		}
		latency(out, fmt.Sprintf("latency p50 (ms), %d contributions (+%d filler)", len(d.Contributions)+f.filler, f.filler), runs)
	}
	return nil
}

// tempSchema creates a fresh schema, migrates it and returns a pool bound to
// it plus a func that closes the pool and drops the schema.
func tempSchema(ctx context.Context, url string) (*pgxpool.Pool, func() error, error) {
	schema := "eval_" + strings.ReplaceAll(id.New(), "-", "")[:16]
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		return nil, nil, err
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		return nil, nil, err
	}
	var pool *pgxpool.Pool
	drop := func() error {
		if pool != nil {
			pool.Close()
		}
		c, err := pgx.Connect(context.Background(), url)
		if err != nil {
			return fmt.Errorf("drop schema %s: %w", schema, err)
		}
		defer c.Close(context.Background())
		_, err = c.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		return err
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err == nil {
		cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
		pool, err = pgxpool.NewWithConfig(ctx, cfg)
	}
	if err == nil {
		err = postgres.Migrate(ctx, pool)
	}
	if err != nil {
		return nil, nil, errors.Join(err, drop())
	}
	return pool, drop, nil
}

// loader creates contributions through the domain and embeds them the way
// search.Worker does (search.Chunk + ChunkStore.Replace), caching vectors by
// chunk text so filler copies cost no extra embedding calls.
type loader struct {
	projects *postgres.ProjectRepo
	actors   *postgres.ActorRepo
	repo     *postgres.ContributionRepo
	chunks   *postgres.ChunkRepo
	actorID  string
	emb      search.Embedder
	model    string
	dim      int
	ids      map[string]string // dataset id -> contribution id
	cache    map[string][]float32
	pending  []string // contribution ids waiting for vectors
}

func newLoader(ctx context.Context, pool *pgxpool.Pool, f flags) (*loader, error) {
	l := &loader{
		projects: postgres.NewProjectRepo(pool), actors: postgres.NewActorRepo(pool),
		repo: postgres.NewContributionRepo(pool), chunks: postgres.NewChunkRepo(pool),
		ids: map[string]string{}, cache: map[string][]float32{}, model: f.embedModel,
	}
	if _, err := project.Create(ctx, l.projects, projectID, "", ""); err != nil {
		return nil, err
	}
	a := actor.New(actor.TypeAgent)
	a.Name = "eval"
	if err := l.actors.Create(ctx, a); err != nil {
		return nil, err
	}
	l.actorID = a.ID
	if f.embedURL != "" {
		l.emb = embedding.New(f.embedURL, f.embedModel, "", nil)
		vs, err := l.emb.Embed(ctx, []string{"kiểm tra / connection test"})
		if err != nil {
			return nil, err
		}
		l.dim = len(vs[0])
	}
	return l, nil
}

func (l *loader) add(ctx context.Context, key, title, body string) error {
	c, err := contribution.Create(ctx, l.projects, l.actors, l.repo, contribution.NewInput{
		ProjectID: projectID, ActorID: l.actorID, Title: title, Body: body,
	})
	if err != nil {
		return fmt.Errorf("contribution %s: %w", key, err)
	}
	l.ids[key] = c.ID
	l.pending = append(l.pending, c.ID)
	return nil
}

// finish embeds every pending contribution and builds the vector index.
func (l *loader) finish(ctx context.Context) error {
	if l.emb == nil {
		return nil
	}
	for _, cid := range l.pending {
		c, err := l.repo.GetByID(ctx, cid)
		if err != nil {
			return err
		}
		chunks := search.Chunk(c.Title, c.Body)
		var missing []string
		for _, t := range chunks {
			if _, ok := l.cache[t]; !ok && !slices.Contains(missing, t) {
				missing = append(missing, t)
			}
		}
		for i := 0; i < len(missing); i += 32 {
			batch := missing[i:min(i+32, len(missing))]
			vs, err := l.emb.Embed(ctx, batch)
			if err != nil {
				return err
			}
			for j, t := range batch {
				l.cache[t] = vs[j]
			}
		}
		vecs := make([][]float32, len(chunks))
		for i, t := range chunks {
			vecs[i] = l.cache[t]
		}
		if err := l.chunks.Replace(ctx, cid, l.model, chunks, vecs); err != nil {
			return err
		}
	}
	l.pending = nil
	return l.chunks.EnsureIndex(ctx, l.model, l.dim)
}

type config struct {
	name          string
	embed, rerank bool
}

type result struct {
	cfg      config
	ranked   [][]string // dataset ids per query
	degraded int
	timings  map[string][]time.Duration // step -> durations
}

// memStore is a setting.Store holding only setting['search'].
type memStore struct{ v []byte }

func (m memStore) Get(context.Context, string) ([]byte, error) {
	if m.v == nil {
		return nil, setting.ErrNotFound
	}
	return m.v, nil
}
func (memStore) Put(context.Context, string, []byte) error { return errors.New("read-only") }

func measure(ctx context.Context, pool *pgxpool.Pool, l *loader, f flags, c config, qs []query) (*result, error) {
	var stored struct {
		Embedding struct {
			URL   string `json:"url"`
			Model string `json:"model"`
			Dim   int    `json:"dim"`
		} `json:"embedding"`
		Rerank struct {
			URL   string `json:"url"`
			Model string `json:"model"`
		} `json:"rerank"`
	}
	if c.embed {
		stored.Embedding.URL, stored.Embedding.Model, stored.Embedding.Dim = f.embedURL, f.embedModel, l.dim
	}
	if c.rerank {
		stored.Rerank.URL, stored.Rerank.Model = f.rerankURL, f.rerankModel
	}
	raw, _ := json.Marshal(stored)
	settings, err := setting.NewService(ctx, memStore{raw}, nil, nil)
	if err != nil {
		return nil, err
	}
	r := &result{cfg: c, timings: map[string][]time.Duration{}}
	rec := func(step string, start time.Time) { // searches run one at a time
		r.timings[step] = append(r.timings[step], time.Since(start))
	}
	svc := search.NewService(search.Deps{
		Lexical:       timedLexical{postgres.NewLexicalIndex(pool), rec},
		Vector:        timedVector{postgres.NewVectorIndex(pool), rec},
		Chunks:        l.chunks,
		Contributions: l.repo,
		Settings:      settings,
		NewEmbedder: func(p setting.Provider) search.Embedder {
			return timedEmbedder{embedding.New(p.URL, p.Model, p.APIKey, nil), rec}
		},
		NewReranker: func(p setting.Provider) search.Reranker {
			return timedReranker{rerank.New(p.URL, p.Model, p.APIKey, nil), rec}
		},
	})
	back := map[string]string{}
	for k, v := range l.ids {
		back[v] = k
	}
	for _, q := range qs {
		start := time.Now()
		resp, err := svc.Search(ctx, search.Query{ProjectID: projectID, Text: q.Query, Rerank: c.rerank})
		if err != nil {
			return nil, fmt.Errorf("%s: %q: %w", c.name, q.Query, err)
		}
		rec("search", start)
		if resp.Degraded {
			r.degraded++
		}
		ids := make([]string, len(resp.Results))
		for i, res := range resp.Results {
			ids[i] = back[res.ID]
		}
		r.ranked = append(r.ranked, ids)
	}
	return r, nil
}

type timedLexical struct {
	search.LexicalIndex
	rec func(string, time.Time)
}

func (t timedLexical) Search(ctx context.Context, p, q string, f search.Filters, n int) ([]search.Hit, error) {
	defer t.rec("lexical", time.Now())
	return t.LexicalIndex.Search(ctx, p, q, f, n)
}

type timedVector struct {
	search.VectorIndex
	rec func(string, time.Time)
}

func (t timedVector) Search(ctx context.Context, p string, v []float32, m string, f search.Filters, n int) ([]search.ChunkHit, error) {
	defer t.rec("vector", time.Now())
	return t.VectorIndex.Search(ctx, p, v, m, f, n)
}

type timedEmbedder struct {
	search.Embedder
	rec func(string, time.Time)
}

func (t timedEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	defer t.rec("embed", time.Now())
	return t.Embedder.Embed(ctx, texts)
}

type timedReranker struct {
	search.Reranker
	rec func(string, time.Time)
}

func (t timedReranker) Rerank(ctx context.Context, q string, docs []string) ([]float64, error) {
	defer t.rec("rerank", time.Now())
	return t.Reranker.Rerank(ctx, q, docs)
}

func report(out io.Writer, runs []*result, qs []query, byID map[string]contrib) {
	type group struct {
		name string
		in   func(query) bool
	}
	groups := func(names []string, in func(string, query) bool) []group {
		var gs []group
		for _, n := range names {
			gs = append(gs, group{n, func(q query) bool { return in(n, q) }})
		}
		return gs
	}
	table := func(title string, gs []group, metrics bool) {
		fmt.Fprintf(out, "\n%s\n%-20s", title, "config")
		if metrics {
			fmt.Fprintf(out, " | %5s | %5s | %5s", "R@1", "R@5", "MRR")
		}
		for _, g := range gs {
			n := 0
			for _, q := range qs {
				if g.in(q) {
					n++
				}
			}
			fmt.Fprintf(out, " | %s (n=%d)", g.name, n)
		}
		fmt.Fprintln(out, " | degraded")
		for _, r := range runs {
			fmt.Fprintf(out, "%-20s", r.cfg.name)
			if metrics {
				var s1, s5, sm float64
				for i, q := range qs {
					a, b, c := score(r.ranked[i], q.GoldIDs)
					s1, s5, sm = s1+a, s5+b, sm+c
				}
				n := float64(len(qs))
				fmt.Fprintf(out, " | %.3f | %.3f | %.3f", s1/n, s5/n, sm/n)
			}
			for _, g := range gs {
				var s5 float64
				n := 0
				for i, q := range qs {
					if g.in(q) {
						_, b, _ := score(r.ranked[i], q.GoldIDs)
						s5 += b
						n++
					}
				}
				w := utf8.RuneCountInString(fmt.Sprintf("%s (n=%d)", g.name, n))
				fmt.Fprintf(out, " | %-*s", w, fmt.Sprintf("%.3f", s5/float64(max(n, 1))))
			}
			fmt.Fprintf(out, " | %d\n", r.degraded)
		}
	}
	table("quality (MRR over the top 5 returned)", nil, true)
	table("R@5 by kind", groups([]string{"K", "P", "X", "T"}, func(n string, q query) bool { return q.Kind == n }), false)
	byLang := groups([]string{"en", "vi"}, func(n string, q query) bool { return q.Lang == n })
	byLang = append(byLang, group{"vi→vi", func(q query) bool { return isViVi(q, byID) }})
	table("R@5 by query language (vi→vi: Vietnamese query, Vietnamese gold)", byLang, false)

	last := runs[len(runs)-1]
	fmt.Fprintf(out, "\nmisses at R@5 (%s):\n", last.cfg.name)
	for i, q := range qs {
		if _, b, _ := score(last.ranked[i], q.GoldIDs); b < 1 {
			fmt.Fprintf(out, "  [%s/%s] %q gold=%v got=%v\n", q.Kind, q.Lang, q.Query, q.GoldIDs, last.ranked[i])
		}
	}
}

func latency(out io.Writer, title string, runs []*result) {
	steps := []string{"search", "lexical", "embed", "vector", "rerank"}
	fmt.Fprintf(out, "\n%s\n%-20s", title, "config")
	for _, s := range steps {
		fmt.Fprintf(out, " | %7s", s)
	}
	fmt.Fprintln(out)
	for _, r := range runs {
		fmt.Fprintf(out, "%-20s", r.cfg.name)
		for _, s := range steps {
			if d := r.timings[s]; len(d) > 0 {
				fmt.Fprintf(out, " | %7.1f", p50(d))
			} else {
				fmt.Fprintf(out, " | %7s", "-")
			}
		}
		fmt.Fprintln(out)
	}
}

func p50(ds []time.Duration) float64 {
	s := slices.Clone(ds)
	slices.Sort(s)
	return float64(s[len(s)/2].Microseconds()) / 1000
}
