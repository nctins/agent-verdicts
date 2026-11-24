package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/setting"
	"agent-verdicts/internal/trust"

	"golang.org/x/text/unicode/norm"
)

// ---- fakes ----

type memStore struct {
	mu sync.Mutex
	v  []byte
}

func (m *memStore) Get(context.Context, string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.v == nil {
		return nil, setting.ErrNotFound
	}
	return m.v, nil
}

func (m *memStore) Put(_ context.Context, _ string, v []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.v = v
	return nil
}

// newSettings loads a setting.Service with the given providers (no keys).
// Update's tester reports dim *dim.
func newSettings(t *testing.T, emb, rer setting.Provider, dim *int) *setting.Service {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{
		"embedding": map[string]any{"url": emb.URL, "model": emb.Model, "dim": emb.Dim},
		"rerank":    map[string]any{"url": rer.URL, "model": rer.Model},
	})
	tester := func(_ context.Context, s setting.Search) (setting.Search, error) {
		if dim != nil {
			s.Embedding.Dim = *dim
		}
		return s, nil
	}
	s, err := setting.NewService(context.Background(), &memStore{v: raw}, nil, tester)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var (
	embOn = setting.Provider{URL: "http://embed", Model: "m1", Dim: 3}
	rerOn = setting.Provider{URL: "http://rerank", Model: "r1"}
)

type fakeLex struct {
	ids   []string
	query atomic.Value // string
}

func (f *fakeLex) Search(_ context.Context, _, q string, _ Filters, n int) ([]Hit, error) {
	f.query.Store(q)
	var out []Hit
	for _, id := range f.ids[:min(n, len(f.ids))] {
		out = append(out, Hit{ContributionID: id})
	}
	return out, nil
}

type fakeVec struct {
	hits  []ChunkHit
	calls atomic.Int32
	model atomic.Value // string; searches may run concurrently
}

func (f *fakeVec) Search(_ context.Context, _ string, _ []float32, model string, _ Filters, n int) ([]ChunkHit, error) {
	f.calls.Add(1)
	f.model.Store(model)
	return f.hits[:min(n, len(f.hits))], nil
}

type fakeEmb struct {
	calls atomic.Int32
	mu    sync.Mutex
	fn    func(ctx context.Context, texts []string) ([][]float32, error)
}

func (f *fakeEmb) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	f.calls.Add(1)
	f.mu.Lock()
	fn := f.fn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, texts)
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}

func (f *fakeEmb) set(fn func(ctx context.Context, texts []string) ([][]float32, error)) {
	f.mu.Lock()
	f.fn = fn
	f.mu.Unlock()
}

type fakeRer struct {
	calls atomic.Int32
	fn    func(ctx context.Context, q string, docs []string) ([]float64, error)
}

func (f *fakeRer) Rerank(ctx context.Context, q string, docs []string) ([]float64, error) {
	f.calls.Add(1)
	if f.fn != nil {
		return f.fn(ctx, q, docs)
	}
	return nil, errors.New("no scores")
}

// fakeChunks keeps chunks per model and records the order of state changes.
type fakeChunks struct {
	mu     sync.Mutex
	all    []string                       // every contribution id, oldest first
	chunks map[string]map[string][]string // model -> id -> chunks
	ops    []string
	best   map[string]ChunkHit

	ensureErrs []error // EnsureIndex returns these first, in order
	onPending  func()  // called (unlocked) at each Pending
}

func (f *fakeChunks) op(s string) { f.ops = append(f.ops, s) }

func (f *fakeChunks) Replace(_ context.Context, id, model string, chunks []string, vecs [][]float32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(chunks) != len(vecs) {
		return fmt.Errorf("%d chunks, %d vecs", len(chunks), len(vecs))
	}
	if f.chunks == nil {
		f.chunks = map[string]map[string][]string{}
	}
	if f.chunks[model] == nil {
		f.chunks[model] = map[string][]string{}
	}
	f.chunks[model][id] = chunks
	f.op("replace:" + model)
	return nil
}

func (f *fakeChunks) Pending(_ context.Context, model string, limit int) ([]string, error) {
	if f.onPending != nil {
		f.onPending()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, id := range f.all {
		if _, ok := f.chunks[model][id]; !ok && len(out) < limit {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f *fakeChunks) DeleteAll(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chunks = nil
	f.op("deleteall")
	return nil
}

func (f *fakeChunks) Count(_ context.Context, model string) (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.chunks[model]), len(f.all), nil
}

func (f *fakeChunks) EnsureIndex(_ context.Context, model string, dim int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.ensureErrs) > 0 {
		err := f.ensureErrs[0]
		f.ensureErrs = f.ensureErrs[1:]
		f.op("ensurefail")
		return err
	}
	f.op(fmt.Sprintf("ensure:%s:%d", model, dim))
	return nil
}

func (f *fakeChunks) BestChunk(_ context.Context, id, _ string, _ []float32) (ChunkHit, error) {
	return f.best[id], nil
}

func (f *fakeChunks) has(model, id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.chunks[model][id]
	return ok
}

func (f *fakeChunks) opsCopy() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ops)
}

type fakeRepo struct {
	contribution.Repository
	m       map[string]contribution.Contribution
	qiCalls [][]string
}

func (f *fakeRepo) Get(_ context.Context, project, id string) (contribution.Contribution, error) {
	c, ok := f.m[id]
	if !ok || c.ProjectID != project {
		return contribution.Contribution{}, contribution.ErrNotFound
	}
	return c, nil
}

func (f *fakeRepo) GetByID(_ context.Context, id string) (contribution.Contribution, error) {
	c, ok := f.m[id]
	if !ok {
		return contribution.Contribution{}, contribution.ErrNotFound
	}
	return c, nil
}

func (f *fakeRepo) QuestionInfo(_ context.Context, ids []string) (map[string]contribution.QuestionInfo, error) {
	f.qiCalls = append(f.qiCalls, slices.Clone(ids))
	out := map[string]contribution.QuestionInfo{}
	for _, id := range ids {
		out[id] = contribution.QuestionInfo{Status: contribution.QuestionOpen}
	}
	return out, nil
}

type fix struct {
	lex    *fakeLex
	vec    *fakeVec
	emb    *fakeEmb
	rer    *fakeRer
	chunks *fakeChunks
	repo   *fakeRepo
	deps   Deps
}

// newFix: contributions "a".."f" are claims in project "p" with body "body of x".
func newFix(t *testing.T, emb, rer setting.Provider) *fix {
	f := &fix{lex: &fakeLex{}, vec: &fakeVec{}, emb: &fakeEmb{}, rer: &fakeRer{}, chunks: &fakeChunks{},
		repo: &fakeRepo{m: map[string]contribution.Contribution{}}}
	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		f.add(contribution.Contribution{ID: id, ProjectID: "p", Kind: contribution.KindClaim, Title: "title " + id, Body: "body of " + id})
	}
	f.deps = Deps{Lexical: f.lex, Vector: f.vec, Chunks: f.chunks, Contributions: f.repo,
		Settings:    newSettings(t, emb, rer, nil),
		NewEmbedder: func(setting.Provider) Embedder { return f.emb },
		NewReranker: func(setting.Provider) Reranker { return f.rer }}
	return f
}

func (f *fix) add(c contribution.Contribution) {
	f.repo.m[c.ID] = c
	f.chunks.all = append(f.chunks.all, c.ID)
}

func ids(r Response) []string {
	var out []string
	for _, x := range r.Results {
		out = append(out, x.ID)
	}
	return out
}

func mustSearch(t *testing.T, s *Service, q Query) Response {
	t.Helper()
	if q.ProjectID == "" {
		q.ProjectID = "p"
	}
	if q.Limit == 0 {
		q.Limit = 20
	}
	r, err := s.Search(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func status(t *testing.T, s *Service) setting.Status {
	t.Helper()
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// ---- Service ----

func TestSearch_LexicalOnlyWhenUnconfigured(t *testing.T) {
	f := newFix(t, setting.Provider{}, setting.Provider{})
	f.lex.ids = []string{"b", "a"}
	s := NewService(f.deps)
	r := mustSearch(t, s, Query{Text: "  Đá Ổn  ", Rerank: true})
	if !slices.Equal(ids(r), []string{"b", "a"}) || r.Degraded {
		t.Fatalf("got %v degraded=%v", ids(r), r.Degraded)
	}
	if q := f.lex.query.Load(); q != "da on" {
		t.Errorf("lexical query = %q, want folded", q)
	}
	if f.vec.calls.Load()+f.emb.calls.Load()+f.rer.calls.Load() != 0 {
		t.Error("vector/embed/rerank called while unconfigured")
	}
	if p := status(t, s).Pipeline; p != "bm25" {
		t.Errorf("pipeline = %q", p)
	}
}

func TestSearch_HybridFusesLexicalAndVector(t *testing.T) {
	f := newFix(t, embOn, setting.Provider{})
	f.lex.ids = []string{"a", "b", "c"}
	f.vec.hits = []ChunkHit{{ContributionID: "c", Text: "chunk c"}, {ContributionID: "d", Text: "chunk d"}}
	s := NewService(f.deps)
	r := mustSearch(t, s, Query{Text: "q"})
	want := Fuse([]string{"a", "b", "c"}, []string{"c", "d"}, 0.5, 1.0)
	if !slices.Equal(ids(r), want) || r.Degraded {
		t.Fatalf("got %v degraded=%v, want %v", ids(r), r.Degraded, want)
	}
	if want[0] != "c" || f.vec.model.Load() != "m1" {
		t.Fatalf("fixture: fused %v, vector model %v", want, f.vec.model.Load())
	}
	if p := status(t, s).Pipeline; p != "bm25+vector" {
		t.Errorf("pipeline = %q", p)
	}
}

func TestSearch_RerankReordersTop(t *testing.T) {
	f := newFix(t, embOn, rerOn)
	f.lex.ids = []string{"a", "b", "c"}
	f.vec.hits = []ChunkHit{{ContributionID: "a", Text: "chunk a"}}
	var docs []string
	f.rer.fn = func(_ context.Context, q string, d []string) ([]float64, error) {
		docs = d
		return []float64{0.1, 0.9, 0.5}, nil // a, b, c
	}
	s := NewService(f.deps)
	r := mustSearch(t, s, Query{Text: "q", Rerank: true})
	if !slices.Equal(ids(r), []string{"b", "c", "a"}) || r.Degraded {
		t.Fatalf("got %v degraded=%v", ids(r), r.Degraded)
	}
	if docs[0] != "title a\nchunk a" || docs[1] != "title b\nbody of b" {
		t.Errorf("rerank docs = %q", docs)
	}
	if p := status(t, s).Pipeline; p != "bm25+vector+rerank" {
		t.Errorf("pipeline = %q", p)
	}

	f.rer.calls.Store(0)
	r = mustSearch(t, s, Query{Text: "q", Rerank: false})
	if f.rer.calls.Load() != 0 || ids(r)[0] != "a" {
		t.Fatalf("Rerank=false: calls=%d order=%v", f.rer.calls.Load(), ids(r))
	}
}

func TestSearch_RerankOnlyTop20(t *testing.T) {
	f := newFix(t, setting.Provider{}, rerOn)
	for i := range 30 {
		id := fmt.Sprintf("x%02d", i)
		f.add(contribution.Contribution{ID: id, ProjectID: "p", Kind: contribution.KindClaim, Title: id})
		f.lex.ids = append(f.lex.ids, id)
	}
	var n int
	f.rer.fn = func(_ context.Context, _ string, d []string) ([]float64, error) {
		n = len(d)
		out := make([]float64, len(d))
		for i := range out {
			out[i] = float64(i) // reverse
		}
		return out, nil
	}
	r := mustSearch(t, NewService(f.deps), Query{Text: "q", Rerank: true, Limit: 20})
	if n != 20 || ids(r)[0] != "x19" || len(r.Results) != 20 {
		t.Fatalf("docs=%d results=%v", n, ids(r))
	}
	if p := status(t, NewService(f.deps)).Pipeline; p != "bm25+rerank" {
		t.Errorf("pipeline = %q", p)
	}
}

func TestSearch_EmbedTimeoutDegradesToLexical(t *testing.T) {
	f := newFix(t, embOn, setting.Provider{})
	f.lex.ids = []string{"a", "b"}
	f.emb.fn = func(ctx context.Context, _ []string) ([][]float32, error) {
		<-ctx.Done()
		return nil, errors.New("flattened provider error") // like a redacted client error
	}
	start := time.Now()
	r := mustSearch(t, NewService(f.deps), Query{Text: "q"})
	if el := time.Since(start); el > 2300*time.Millisecond || el < 1900*time.Millisecond {
		t.Errorf("took %v, want ~2s", el)
	}
	if !slices.Equal(ids(r), []string{"a", "b"}) || !r.Degraded || f.vec.calls.Load() != 0 {
		t.Fatalf("got %v degraded=%v vec=%d", ids(r), r.Degraded, f.vec.calls.Load())
	}
}

func TestSearch_RerankErrorKeepsFusedOrder(t *testing.T) {
	f := newFix(t, setting.Provider{}, rerOn)
	f.lex.ids = []string{"c", "a", "b"}
	r := mustSearch(t, NewService(f.deps), Query{Text: "q", Rerank: true})
	if !slices.Equal(ids(r), []string{"c", "a", "b"}) || !r.Degraded || f.rer.calls.Load() != 1 {
		t.Fatalf("got %v degraded=%v calls=%d", ids(r), r.Degraded, f.rer.calls.Load())
	}
}

func TestSearch_RerankWrongScoreCountDegrades(t *testing.T) {
	f := newFix(t, setting.Provider{}, rerOn)
	f.lex.ids = []string{"c", "a"}
	f.rer.fn = func(context.Context, string, []string) ([]float64, error) { return []float64{1}, nil }
	r := mustSearch(t, NewService(f.deps), Query{Text: "q", Rerank: true})
	if !slices.Equal(ids(r), []string{"c", "a"}) || !r.Degraded {
		t.Fatalf("got %v degraded=%v", ids(r), r.Degraded)
	}
}

func TestSearch_RerankSemaphore(t *testing.T) {
	f := newFix(t, setting.Provider{}, rerOn)
	f.lex.ids = []string{"a", "b"}
	f.rer.fn = func(ctx context.Context, _ string, d []string) ([]float64, error) {
		select {
		case <-time.After(1500 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return []float64{0, 1}, nil
	}
	s := NewService(f.deps)
	type out struct {
		r  Response
		el time.Duration
	}
	res := make(chan out, 3)
	start := time.Now()
	for range 3 {
		go func() {
			r, err := s.Search(context.Background(), Query{ProjectID: "p", Text: "q", Rerank: true})
			if err != nil {
				t.Error(err)
			}
			res <- out{r, time.Since(start)}
		}()
	}
	var degraded []time.Duration
	for range 3 {
		o := <-res
		if o.r.Degraded {
			degraded = append(degraded, o.el)
			if !slices.Equal(ids(o.r), []string{"a", "b"}) {
				t.Errorf("degraded result order %v", ids(o.r))
			}
		} else if !slices.Equal(ids(o.r), []string{"b", "a"}) {
			t.Errorf("reranked order %v", ids(o.r))
		}
	}
	if len(degraded) != 1 || degraded[0] < 900*time.Millisecond || degraded[0] > 1400*time.Millisecond {
		t.Fatalf("degraded calls at %v, want exactly one at ~1s", degraded)
	}
	if f.rer.calls.Load() != 2 {
		t.Errorf("rerank calls = %d", f.rer.calls.Load())
	}
}

func TestSearch_LimitAndEmptyQuery(t *testing.T) {
	f := newFix(t, setting.Provider{}, setting.Provider{})
	for i := range 30 {
		id := fmt.Sprintf("x%02d", i)
		f.add(contribution.Contribution{ID: id, ProjectID: "p", Kind: contribution.KindClaim, Title: id})
		f.lex.ids = append(f.lex.ids, id)
	}
	s := NewService(f.deps)
	for _, tc := range []struct{ limit, want int }{{0, 5}, {-1, 5}, {7, 7}, {50, 20}} {
		r, err := s.Search(context.Background(), Query{ProjectID: "p", Text: "q", Limit: tc.limit})
		if err != nil || len(r.Results) != tc.want {
			t.Errorf("Limit %d: %d results, err %v", tc.limit, len(r.Results), err)
		}
	}
	for _, q := range []string{"", "  \n\t"} {
		_, err := s.Search(context.Background(), Query{ProjectID: "p", Text: q})
		if !errors.Is(err, ErrEmptyQuery) || !errors.Is(err, contribution.ErrInvalidArgument) {
			t.Errorf("Text %q: err = %v", q, err)
		}
	}
	for _, q := range []string{"a\x00b", "bad \xff"} {
		if _, err := s.Search(context.Background(), Query{ProjectID: "p", Text: q}); !errors.Is(err, contribution.ErrInvalidArgument) {
			t.Errorf("Text %q: err = %v, want ErrInvalidArgument", q, err)
		}
	}
}

func TestSearch_Snippets(t *testing.T) {
	long := strings.Repeat("filler words here ", 30) + "the needle sits here " + strings.Repeat("more text ", 30)
	t.Run("lexical", func(t *testing.T) {
		f := newFix(t, setting.Provider{}, setting.Provider{})
		f.add(contribution.Contribution{ID: "n", ProjectID: "p", Kind: contribution.KindClaim, Title: "T", Body: long})
		f.add(contribution.Contribution{ID: "e", ProjectID: "p", Kind: contribution.KindClaim, Title: "Empty body title"})
		f.lex.ids = []string{"n", "e"}
		r := mustSearch(t, NewService(f.deps), Query{Text: "Needle x"})
		if want := Snippet(long, []string{"needle"}, 200); r.Results[0].Snippet != want || !strings.Contains(want, "needle") {
			t.Errorf("snippet = %q, want %q", r.Results[0].Snippet, want)
		}
		if r.Results[1].Snippet != "" { // the title is already in the result
			t.Errorf("empty-body snippet = %q, want empty", r.Results[1].Snippet)
		}
	})
	t.Run("vector", func(t *testing.T) {
		f := newFix(t, embOn, setting.Provider{})
		chunk := "T\n\n" + long
		f.add(contribution.Contribution{ID: "a", ProjectID: "p", Kind: contribution.KindClaim, Title: "T", Body: long})
		f.add(contribution.Contribution{ID: "t", ProjectID: "p", Kind: contribution.KindClaim, Title: "Only title"})
		f.lex.ids = []string{"a", "b", "c", "t"}
		f.vec.hits = []ChunkHit{{ContributionID: "a", Text: chunk}, {ContributionID: "t", Text: "Only title"}}
		f.chunks.best = map[string]ChunkHit{"b": {ContributionID: "b", Text: "best chunk of b"}}
		r := mustSearch(t, NewService(f.deps), Query{Text: "needle"})
		got := map[string]string{}
		for _, x := range r.Results {
			got[x.ID] = x.Snippet
		}
		if want := Snippet(long, nil, 200); got["a"] != want { // chunk 0 minus the title the result already has
			t.Errorf("a snippet = %q, want head of chunk without title %q", got["a"], want)
		}
		if got["t"] != "" {
			t.Errorf("title-only snippet = %q, want empty", got["t"])
		}
		if got["b"] != "best chunk of b" {
			t.Errorf("b snippet = %q (BestChunk)", got["b"])
		}
		if got["c"] != "body of c" {
			t.Errorf("c snippet = %q (no chunk → body)", got["c"])
		}
	})
}

func TestSearch_QuestionInfoAndState(t *testing.T) {
	f := newFix(t, setting.Provider{}, setting.Provider{})
	f.add(contribution.Contribution{ID: "q1", ProjectID: "p", Kind: contribution.KindQuestion, Title: "Q"})
	f.add(contribution.Contribution{ID: "fav", ProjectID: "p", Kind: contribution.KindClaim, Title: "F", Counts: trust.Counts{Favorable: 3, FavorableActors: 3}, MinFavorable: 1})
	f.lex.ids = []string{"q1", "fav"}
	r := mustSearch(t, NewService(f.deps), Query{Text: "q"})
	q, c := r.Results[0], r.Results[1]
	if q.Kind != contribution.KindQuestion || q.Question == nil || q.Question.Status != contribution.QuestionOpen || q.State != nil {
		t.Errorf("question result = %+v", q)
	}
	if c.Kind != contribution.KindClaim || c.State == nil || *c.State != trust.StateFavorable || c.Question != nil {
		t.Errorf("claim result = %+v", c)
	}
	if len(f.repo.qiCalls) != 1 || !slices.Equal(f.repo.qiCalls[0], []string{"q1"}) {
		t.Errorf("QuestionInfo calls = %v", f.repo.qiCalls)
	}
	if c.Title != "F" {
		t.Errorf("title = %q", c.Title)
	}
}

func TestSearch_HydrationSkipsVanished(t *testing.T) {
	f := newFix(t, setting.Provider{}, setting.Provider{})
	f.lex.ids = []string{"a", "gone", "b", "c"}
	r := mustSearch(t, NewService(f.deps), Query{Text: "q", Limit: 2})
	if !slices.Equal(ids(r), []string{"a", "b"}) {
		t.Fatalf("got %v", ids(r))
	}
}

func TestStatus_CountsReindexingWarnings(t *testing.T) {
	f := newFix(t, embOn, setting.Provider{})
	f.chunks.chunks = map[string]map[string][]string{"m1": {"a": {"x"}}, "old": {"b": {"y"}}}
	f.deps.Reindexing = func() bool { return true }
	st := status(t, NewService(f.deps))
	if st.EmbeddedCount != 1 || st.TotalCount != 6 || !st.Reindexing || st.Warnings != nil {
		t.Fatalf("status = %+v", st)
	}

	f = newFix(t, setting.Provider{}, setting.Provider{})
	f.chunks.chunks = map[string]map[string][]string{"m1": {"a": {"x"}}}
	st = status(t, NewService(f.deps))
	if st.EmbeddedCount != 0 || st.TotalCount != 6 || st.Reindexing {
		t.Fatalf("unconfigured status = %+v", st)
	}
}

// ---- Worker ----

func newWorker(f *fix) *Worker { return NewWorker(f.deps, log.New(io.Discard, "", 0)) }

func TestWorker_FailedEmbedRecoveredBySweep(t *testing.T) {
	f := newFix(t, embOn, setting.Provider{})
	f.emb.set(func(context.Context, []string) ([][]float32, error) { return nil, errors.New("down") })
	w := newWorker(f)
	ctx := context.Background()
	w.Enqueue("a")
	w.process(ctx, <-w.queue)
	if f.chunks.has("m1", "a") {
		t.Fatal("chunk written although embedder failed")
	}
	f.emb.set(nil)
	w.sweep(ctx)
	for _, id := range f.chunks.all {
		if !f.chunks.has("m1", id) {
			t.Errorf("%s not embedded after sweep", id)
		}
	}
}

func TestWorker_ChunksAndBatches(t *testing.T) {
	f := newFix(t, embOn, setting.Provider{})
	body := strings.Repeat("A sentence that is long enough to fill some space. ", 1500) // ~75k runes → >32 chunks
	f.add(contribution.Contribution{ID: "big", ProjectID: "p", Kind: contribution.KindClaim, Title: "Big", Body: body})
	var maxBatch atomic.Int32
	f.emb.set(func(_ context.Context, texts []string) ([][]float32, error) {
		if int32(len(texts)) > maxBatch.Load() {
			maxBatch.Store(int32(len(texts)))
		}
		out := make([][]float32, len(texts))
		for i := range out {
			out[i] = []float32{1, 0, 0}
		}
		return out, nil
	})
	w := newWorker(f)
	w.process(context.Background(), "big")
	f.chunks.mu.Lock()
	got := f.chunks.chunks["m1"]["big"]
	f.chunks.mu.Unlock()
	if want := Chunk("Big", body); !slices.Equal(got, want) || len(want) <= 32 {
		t.Fatalf("stored %d chunks, want %d (>32)", len(got), len(want))
	}
	if maxBatch.Load() > 32 {
		t.Errorf("batch size %d > 32", maxBatch.Load())
	}
}

func TestWorker_ModelChangeWipesEnsuresThenReembeds(t *testing.T) {
	f := newFix(t, embOn, setting.Provider{})
	dim := 4
	f.deps.Settings = newSettings(t, embOn, setting.Provider{}, &dim)
	w := newWorker(f)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	waitAll := func(model string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for _, id := range f.chunks.all {
			for !f.chunks.has(model, id) {
				if time.Now().After(deadline) {
					t.Fatalf("%s not embedded with %s; ops %v", id, model, f.chunks.opsCopy())
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	}
	waitAll("m1")
	if ops := f.chunks.opsCopy(); ops[0] != "ensure:m1:3" || slices.Contains(ops, "deleteall") {
		t.Fatalf("startup ops = %v, want EnsureIndex first and no wipe", ops)
	}
	if _, err := f.deps.Settings.Update(ctx, setting.ProviderUpdate{URL: "http://embed", Model: "m2"}, setting.ProviderUpdate{}); err != nil {
		t.Fatal(err)
	}
	waitAll("m2")
	ops := f.chunks.opsCopy()
	del, ens, rep := slices.Index(ops, "deleteall"), slices.Index(ops, "ensure:m2:4"), slices.Index(ops, "replace:m2")
	if del < 0 || !(del < ens && ens < rep) {
		t.Fatalf("ops = %v, want deleteall < ensure:m2:4 < replace:m2", ops)
	}
	if f.chunks.has("m1", "a") {
		t.Error("old model chunks survived the change")
	}
}

func TestWorker_EnqueueNeverBlocks(t *testing.T) {
	w := newWorker(newFix(t, embOn, setting.Provider{}))
	done := make(chan struct{})
	go func() {
		for range 1024 + 10 {
			w.Enqueue("a")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Enqueue blocked on a full queue")
	}
	if len(w.queue) != 1024 {
		t.Errorf("queue len = %d", len(w.queue))
	}
}

func TestWorker_ReindexUntilPendingEmpty(t *testing.T) {
	f := newFix(t, embOn, setting.Provider{})
	w := newWorker(f)
	f.deps.Reindexing = w.Reindexing
	s := NewService(f.deps)
	ctx := context.Background()
	w.sweep(ctx)
	if status(t, s).Reindexing || status(t, s).EmbeddedCount != 6 {
		t.Fatalf("before reindex: %+v", status(t, s))
	}
	if err := w.Reindex(ctx); err != nil {
		t.Fatal(err)
	}
	if st := status(t, s); !st.Reindexing {
		t.Fatalf("after Reindex: %+v", st)
	}
	before := len(f.chunks.opsCopy())
	w.sweep(ctx)
	if st := status(t, s); st.Reindexing || st.EmbeddedCount != 6 {
		t.Fatalf("after sweep: %+v", st)
	}
	if ops := f.chunks.opsCopy()[before:]; ops[0] != "deleteall" || !slices.Contains(ops, "replace:m1") {
		t.Fatalf("sweep ops = %v, want wipe then re-embed", ops)
	}
}

// A Reindex requested while a sweep runs must survive that sweep's end.
func TestWorker_ReindexDuringSweepKeepsFlag(t *testing.T) {
	f := newFix(t, embOn, setting.Provider{})
	w := newWorker(f)
	ctx := context.Background()
	w.sweep(ctx)
	_ = w.Reindex(ctx)
	requested := false
	f.chunks.onPending = func() {
		if !requested {
			requested = true
			_ = w.Reindex(ctx) // arrives mid-sweep, after this sweep's wipe
		}
	}
	w.sweep(ctx)
	if !w.Reindexing() {
		t.Fatal("flag cleared although a reindex arrived during the sweep")
	}
	f.chunks.onPending = nil
	n := len(f.chunks.opsCopy())
	w.sweep(ctx)
	if w.Reindexing() {
		t.Fatal("flag still set after a full pass")
	}
	if ops := f.chunks.opsCopy()[n:]; ops[0] != "deleteall" {
		t.Fatalf("second request not wiped: %v", ops)
	}
}

func TestWorker_SweepSkipsPoisonItem(t *testing.T) {
	f := newFix(t, embOn, setting.Provider{})
	f.emb.set(func(_ context.Context, texts []string) ([][]float32, error) {
		if strings.Contains(texts[0], "body of a") { // oldest: always rejected
			return nil, errors.New("413")
		}
		return [][]float32{{1, 0, 0}}, nil
	})
	w := newWorker(f)
	w.sweep(context.Background())
	for _, id := range f.chunks.all[1:] {
		if !f.chunks.has("m1", id) {
			t.Errorf("%s blocked by failing item a", id)
		}
	}
}

func TestWorker_SweepStopsAfterThreeConsecutiveFailures(t *testing.T) {
	f := newFix(t, embOn, setting.Provider{})
	f.emb.set(func(context.Context, []string) ([][]float32, error) { return nil, errors.New("down") })
	newWorker(f).sweep(context.Background())
	if n := f.emb.calls.Load(); n != 3 {
		t.Fatalf("embed calls = %d, want 3", n)
	}
}

func TestWorker_FullBatchContinuesWithoutTicker(t *testing.T) {
	old := sweepBatch
	sweepBatch = 2
	defer func() { sweepBatch = old }()
	f := newFix(t, embOn, setting.Provider{})
	w := newWorker(f)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(2 * time.Second)
	for _, id := range f.chunks.all {
		for !f.chunks.has("m1", id) {
			if time.Now().After(deadline) {
				t.Fatalf("%s not embedded: batches of %d stopped after one sweep", id, sweepBatch)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// Same model, stale dim on disk at startup: ErrDimMismatch → wipe, retry once.
func TestWorker_StartupEnsureFailureWipesAndRetries(t *testing.T) {
	f := newFix(t, embOn, setting.Provider{})
	f.chunks.ensureErrs = []error{fmt.Errorf("wrapped: %w", ErrDimMismatch)}
	w := newWorker(f)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(2 * time.Second)
	for !f.chunks.has("m1", "f") {
		if time.Now().After(deadline) {
			t.Fatalf("not embedded; ops %v", f.chunks.opsCopy())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if ops := f.chunks.opsCopy(); !slices.Equal(ops[:3], []string{"ensurefail", "deleteall", "ensure:m1:3"}) {
		t.Fatalf("ops = %v", ops)
	}
}

func TestWorker_SkipsWhenEmbeddingUnconfigured(t *testing.T) {
	f := newFix(t, setting.Provider{}, setting.Provider{})
	w := newWorker(f)
	w.process(context.Background(), "a")
	w.sweep(context.Background())
	if f.emb.calls.Load() != 0 || len(f.chunks.opsCopy()) != 0 {
		t.Fatalf("embed calls %d, ops %v", f.emb.calls.Load(), f.chunks.opsCopy())
	}
}

func TestWorker_GenericEnsureErrorDoesNotWipe(t *testing.T) {
	f := newFix(t, embOn, setting.Provider{})
	f.chunks.ensureErrs = []error{errors.New("lock timeout")}
	w := newWorker(f)
	w.last = modelKey{"m1", 3} // as after Run's startup
	ctx := context.Background()
	w.sweep(ctx)
	w.sweep(ctx)
	ops := f.chunks.opsCopy()
	if slices.Contains(ops, "deleteall") || !slices.Equal(ops[:2], []string{"ensurefail", "ensure:m1:3"}) {
		t.Fatalf("ops = %v, want retry without wipe", ops)
	}
	if !f.chunks.has("m1", "f") {
		t.Error("not embedded after the index came up")
	}
}

func TestWorker_RecentlyFailedItemsAreSkipped(t *testing.T) {
	f := newFix(t, embOn, setting.Provider{})
	f.emb.set(func(_ context.Context, texts []string) ([][]float32, error) {
		for _, bad := range []string{"body of a", "body of b", "body of c"} {
			if strings.Contains(texts[0], bad) {
				return nil, errors.New("413")
			}
		}
		return [][]float32{{1, 0, 0}}, nil
	})
	w := newWorker(f)
	ctx := context.Background()
	w.sweep(ctx) // a, b, c fail → stop
	w.sweep(ctx) // skips them
	for _, id := range []string{"d", "e", "f"} {
		if !f.chunks.has("m1", id) {
			t.Errorf("%s blocked by three failing head items", id)
		}
	}
	// After retry-after they are tried again.
	calls := f.emb.calls.Load()
	w.sweep(ctx)
	if f.emb.calls.Load() != calls {
		t.Fatal("skipped items retried before retry-after")
	}
	for id := range w.failed {
		w.failed[id] = time.Now().Add(-time.Second)
	}
	w.sweep(ctx)
	if f.emb.calls.Load() != calls+3 {
		t.Fatalf("embed calls %d, want %d (a, b, c retried)", f.emb.calls.Load(), calls+3)
	}
}

// A dim mismatch that the wipe does not cure must not wipe every vector on
// every sweep: the repair runs once per process.
func TestWorker_DimRepairWipesOnce(t *testing.T) {
	f := newFix(t, embOn, setting.Provider{})
	mismatch := fmt.Errorf("wrapped: %w", ErrDimMismatch)
	f.chunks.ensureErrs = []error{mismatch, mismatch, mismatch, mismatch, mismatch, mismatch}
	w := newWorker(f)
	w.last = modelKey{"m1", 3} // as after Run's startup
	for range 3 {
		w.sweep(context.Background())
	}
	n := 0
	for _, op := range f.chunks.opsCopy() {
		if op == "deleteall" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("DeleteAll ran %d times, want 1; ops %v", n, f.chunks.opsCopy())
	}
}

// An item that keeps failing must not keep Reindexing true forever: the
// pass is done once only recently failed items are pending (they are still
// retried after retryAfter).
func TestWorker_ReindexCompletesWithPoisonItem(t *testing.T) {
	f := newFix(t, embOn, setting.Provider{})
	f.emb.set(func(_ context.Context, texts []string) ([][]float32, error) {
		if strings.Contains(texts[0], "body of a") {
			return nil, errors.New("413")
		}
		return [][]float32{{1, 0, 0}}, nil
	})
	w := newWorker(f)
	ctx := context.Background()
	_ = w.Reindex(ctx)
	w.sweep(ctx)
	w.sweep(ctx)
	if w.Reindexing() {
		t.Fatal("Reindexing stuck on a permanently failing item")
	}
}

// Public search bounds query embedding like rerank: with every slot busy a
// search waits up to embedWait, then skips the vector leg (degraded).
func TestSearch_EmbedSemaphore(t *testing.T) {
	f := newFix(t, embOn, setting.Provider{})
	f.lex.ids = []string{"a"}
	f.emb.set(func(ctx context.Context, _ []string) ([][]float32, error) {
		select {
		case <-time.After(1500 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return [][]float32{{1, 0, 0}}, nil
	})
	s := NewService(f.deps)
	start := time.Now()
	res := make(chan time.Duration, embedSlots+1)
	for range embedSlots + 1 {
		go func() {
			r, err := s.Search(context.Background(), Query{ProjectID: "p", Text: "q"})
			if err != nil {
				t.Error(err)
			}
			if r.Degraded {
				res <- time.Since(start)
			} else {
				res <- 0
			}
		}()
	}
	var degraded []time.Duration
	for range embedSlots + 1 {
		if d := <-res; d > 0 {
			degraded = append(degraded, d)
		}
	}
	if len(degraded) != 1 || degraded[0] < embedWait-100*time.Millisecond || degraded[0] > embedWait+2*time.Second {
		t.Fatalf("degraded searches at %v, want exactly one at ~%v", degraded, embedWait)
	}
	if n := f.emb.calls.Load(); n != embedSlots {
		t.Errorf("embed calls = %d, want %d", n, embedSlots)
	}
}

// ---- accent-exact lexical matches (spec 2026-11-15-role-play-fixes §C5) ----

func accentFix(t *testing.T, emb setting.Provider) *fix {
	f := newFix(t, emb, setting.Provider{})
	f.add(contribution.Contribution{ID: "x", ProjectID: "p", Kind: contribution.KindClaim, Title: "PgBouncer pool_size cho orders", Body: "Bản cập nhật sau phản biện."})
	f.add(contribution.Contribution{ID: "y", ProjectID: "p", Kind: contribution.KindClaim, Title: "Ghi đè bằng biến môi trường"})
	f.add(contribution.Contribution{ID: "w", ProjectID: "p", Kind: contribution.KindClaim, Title: "Biếng nhác"})
	f.add(contribution.Contribution{ID: "z", ProjectID: "p", Kind: contribution.KindClaim, Title: norm.NFD.String("Tên BIẾN lạ")})
	return f
}

func TestSearch_AccentedTokensNeedAnExactMatch(t *testing.T) {
	for name, tc := range map[string]struct {
		lex   []string
		query string
		drop  bool
		want  []string
	}{
		"fold collision is ranked last":        {[]string{"x", "y"}, "biến môi trường", false, []string{"y", "x"}},
		"fold collision is dropped":            {[]string{"x", "y"}, "biến môi trường", true, []string{"y"}},
		"unaccented query is untouched":        {[]string{"x", "y"}, "bien moi truong", true, []string{"x", "y"}},
		"NFD query":                            {[]string{"x", "y"}, norm.NFD.String("biến môi trường"), true, []string{"y"}},
		"NFD and upper-case stored text":       {[]string{"x", "z", "y"}, "biến", true, []string{"z", "y"}},
		"one unaccented token keeps its place": {[]string{"x", "y"}, "pool biến", true, []string{"x", "y"}},
		"no match inside another word":         {[]string{"w", "y"}, "biến", false, []string{"y", "w"}},
		"stopwords do not count as a match":    {[]string{"x", "y"}, "cho biến", true, []string{"y"}},
		"BM25 order kept among exact matches":  {[]string{"y", "x", "z"}, "biến", false, []string{"y", "z", "x"}},
	} {
		f := accentFix(t, setting.Provider{})
		f.lex.ids = tc.lex
		r := mustSearch(t, NewService(f.deps), Query{Text: tc.query, DropAccentMismatch: tc.drop})
		if got := ids(r); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}

// The check is for the lexical branch only: what the vector branch found stays.
func TestSearch_AccentCheckLeavesVectorHitsAlone(t *testing.T) {
	f := accentFix(t, embOn)
	f.lex.ids = []string{"x", "y"}
	f.vec.hits = []ChunkHit{{ContributionID: "x", Text: "chunk"}}
	r := mustSearch(t, NewService(f.deps), Query{Text: "biến môi trường", DropAccentMismatch: true})
	if got := ids(r); !slices.Equal(got, []string{"x", "y"}) || r.Degraded { // x: vector rank 1 (weight 1.0) beats y: lexical rank 1 (0.5)
		t.Fatalf("got %v degraded=%v", got, r.Degraded)
	}
}
