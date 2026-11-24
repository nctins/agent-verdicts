package search

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/textfold"
	"agent-verdicts/internal/project"
	"agent-verdicts/internal/setting"
	"agent-verdicts/internal/trust"
)

const (
	defaultLimit  = 5
	maxLimit      = 20
	candidates    = 50 // lexical and vector candidates each
	rerankTop     = 20
	wLex, wVec    = 0.5, 1.0
	embedTimeout  = 2 * time.Second
	rerankTimeout = 5 * time.Second
	rerankWait    = time.Second // max wait for a rerank slot
	rerankSlots   = 2
	// Query embedding on the public route is bounded the same way, so a
	// flood of searches cannot saturate the provider the worker also uses.
	// ponytail: 4 slots / 1s wait are a judgment call (spec §6.1 names only
	// the rerank limit); tune from real traffic.
	embedSlots = 4
	embedWait  = time.Second
	snippetLen = 200
)

var ErrEmptyQuery = fmt.Errorf("%w: query must not be empty", contribution.ErrInvalidArgument)

type Result struct {
	ID                 string
	Kind               contribution.Kind
	Title              string
	Snippet            string
	State              *trust.State
	Question           *contribution.QuestionInfo
	Superseded, Hidden bool
	HiddenReason       string
}

type Response struct {
	Results  []Result
	Degraded bool
}

type Query struct {
	ProjectID, Text string
	Filters         Filters
	Limit           int
	Rerank          bool
	LexicalOnly     bool // skip the vector provider too
	// DropAccentMismatch drops lexical hits that match the query only once
	// accents are stripped, instead of ranking them last (see exactFirst).
	DropAccentMismatch bool
}

type Deps struct {
	Lexical       LexicalIndex
	Vector        VectorIndex
	Chunks        ChunkStore
	Contributions contribution.Repository
	Settings      *setting.Service
	// Only called for a provider with URL != ""; nil means that provider is off.
	NewEmbedder func(setting.Provider) Embedder
	NewReranker func(setting.Provider) Reranker
	// Reindexing reports Worker.Reindexing for Status; nil → false.
	Reindexing func() bool
}

type Service struct {
	d        Deps
	sem      chan struct{} // rerank slots
	embedSem chan struct{} // query embedding slots
}

func NewService(d Deps) *Service {
	return &Service{d: d, sem: make(chan struct{}, rerankSlots), embedSem: make(chan struct{}, embedSlots)}
}

// providers returns the embedding and rerank providers that are on.
func (s *Service) providers() (emb, rer *setting.Provider) {
	cfg := s.d.Settings.Current()
	if cfg.Embedding.URL != "" && s.d.NewEmbedder != nil {
		emb = &cfg.Embedding
	}
	if cfg.Rerank.URL != "" && s.d.NewReranker != nil {
		rer = &cfg.Rerank
	}
	return emb, rer
}

// hydrated is a fused candidate loaded from the repository.
type hydrated struct {
	c     contribution.Contribution
	chunk string // best chunk from the vector search, "" if none
}

func (s *Service) Search(ctx context.Context, q Query) (Response, error) {
	if err := project.ValidateID(q.ProjectID); err != nil {
		return Response{}, err
	}
	text := strings.TrimSpace(q.Text)
	if text == "" {
		return Response{}, ErrEmptyQuery
	}
	if !textfold.Valid(text) {
		return Response{}, fmt.Errorf("%w: query must be valid UTF-8 without NUL bytes", contribution.ErrInvalidArgument)
	}
	limit := q.Limit
	switch {
	case limit <= 0:
		limit = defaultLimit
	case limit > maxLimit:
		limit = maxLimit
	}
	emb, rer := s.providers()
	if !q.Rerank {
		rer = nil
	}
	if q.LexicalOnly {
		emb, rer = nil, nil
	}
	folded := stripStopwords(textfold.Fold(text)) // the lexical query and the snippet terms
	var resp Response

	lex, err := s.d.Lexical.Search(ctx, q.ProjectID, folded, q.Filters, candidates)
	if err != nil {
		return Response{}, err
	}
	lexIDs := make([]string, len(lex))
	for i, h := range lex {
		lexIDs[i] = h.ContributionID
	}
	lexIDs, loaded, err := s.exactFirst(ctx, q, lexIDs)
	if err != nil {
		return Response{}, err
	}

	var qv []float32
	var vecIDs []string
	chunks := map[string]string{}
	if emb != nil {
		qv = s.embedQuery(ctx, *emb, text)
		var hits []ChunkHit
		if qv != nil {
			hits, err = s.d.Vector.Search(ctx, q.ProjectID, qv, emb.Model, q.Filters, candidates)
		}
		if qv == nil || err != nil {
			qv, resp.Degraded = nil, true
		}
		for _, h := range hits {
			vecIDs = append(vecIDs, h.ContributionID)
			chunks[h.ContributionID] = h.Text
		}
	}
	fused := Fuse(lexIDs, vecIDs, wLex, wVec)

	need := limit
	if rer != nil {
		need = max(limit, rerankTop)
	}
	var top []hydrated
	for _, id := range fused {
		if len(top) == need {
			break
		}
		c, ok := loaded[id]
		if !ok {
			c, err = s.d.Contributions.Get(ctx, q.ProjectID, id)
			if errors.Is(err, contribution.ErrNotFound) {
				continue // deleted since indexing
			}
			if err != nil {
				return Response{}, err
			}
		}
		top = append(top, hydrated{c: c, chunk: chunks[id]})
	}

	if rer != nil && len(top) > 0 {
		cut := top[:min(rerankTop, len(top))]
		if !s.rerank(ctx, *rer, text, cut) {
			resp.Degraded = true
		}
	}
	top = top[:min(limit, len(top))]
	return s.results(ctx, resp, top, folded, emb, qv)
}

// embedQuery returns nil when the provider fails or times out, or no slot
// frees within embedWait.
func (s *Service) embedQuery(ctx context.Context, p setting.Provider, text string) []float32 {
	if !acquire(ctx, s.embedSem, embedWait) {
		return nil
	}
	defer func() { <-s.embedSem }()
	ctx, cancel := context.WithTimeout(ctx, embedTimeout)
	defer cancel()
	vs, err := s.d.NewEmbedder(p).Embed(ctx, []string{text})
	if err != nil || len(vs) != 1 || len(vs[0]) == 0 {
		return nil
	}
	return vs[0]
}

// rerank reorders top in place by provider score; false (order untouched)
// when no slot frees within rerankWait, or the call fails or times out.
func (s *Service) rerank(ctx context.Context, p setting.Provider, query string, top []hydrated) bool {
	if !acquire(ctx, s.sem, rerankWait) {
		return false
	}
	defer func() { <-s.sem }()
	docs := make([]string, len(top))
	for i, h := range top {
		text := h.chunk
		if text == "" {
			text = truncRunes(h.c.Body, chunkMax)
		}
		docs[i] = h.c.Title + "\n" + text
	}
	ctx, cancel := context.WithTimeout(ctx, rerankTimeout)
	defer cancel()
	scores, err := s.d.NewReranker(p).Rerank(ctx, query, docs)
	if err != nil || len(scores) != len(top) {
		return false
	}
	idx := make([]int, len(top))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int { return cmp.Compare(scores[b], scores[a]) })
	sorted := make([]hydrated, len(top))
	for i, j := range idx {
		sorted[i] = top[j]
	}
	copy(top, sorted)
	return true
}

func (s *Service) results(ctx context.Context, resp Response, top []hydrated, folded string, emb *setting.Provider, qv []float32) (Response, error) {
	var terms []string
	for _, t := range strings.Fields(folded) {
		if utf8.RuneCountInString(t) > 1 {
			terms = append(terms, t)
		}
	}
	var questions []string
	resp.Results = make([]Result, len(top))
	for i, h := range top {
		c := h.c
		r := Result{ID: c.ID, Kind: c.Kind, Title: c.Title, State: c.State(), Superseded: c.SupersededByID != "", Hidden: c.HiddenAt != nil, HiddenReason: c.HiddenReason}
		if c.Kind == contribution.KindQuestion {
			questions = append(questions, c.ID)
		}
		chunk := h.chunk
		if chunk == "" && qv != nil {
			if best, err := s.d.Chunks.BestChunk(ctx, c.ID, emb.Model, qv); err == nil && best.ContributionID != "" {
				chunk = best.Text
			}
		}
		// Chunk 0 starts with the title (Chunk), which the result already
		// carries: drop it so the snippet spends its budget on the body.
		if chunk = strings.TrimPrefix(chunk, c.Title+"\n\n"); chunk == c.Title {
			chunk = ""
		}
		switch {
		case chunk != "":
			r.Snippet = Snippet(chunk, nil, snippetLen) // head of the best chunk
		case strings.TrimSpace(c.Body) != "":
			r.Snippet = Snippet(c.Body, terms, snippetLen)
		} // no body: empty snippet, the title says it all
		resp.Results[i] = r
	}
	if len(questions) > 0 {
		info, err := s.d.Contributions.QuestionInfo(ctx, questions)
		if err != nil {
			return Response{}, err
		}
		for i := range resp.Results {
			if qi, ok := info[resp.Results[i].ID]; ok && resp.Results[i].Kind == contribution.KindQuestion {
				resp.Results[i].Question = &qi
			}
		}
	}
	return resp, nil
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func (s *Service) Status(ctx context.Context) (setting.Status, error) {
	emb, rer := s.providers()
	st := setting.Status{Pipeline: "bm25", Warnings: s.d.Settings.Warnings()}
	model := ""
	if emb != nil {
		st.Pipeline += "+vector"
		model = emb.Model
	}
	if rer != nil {
		st.Pipeline += "+rerank"
	}
	var err error
	if st.EmbeddedCount, st.TotalCount, err = s.d.Chunks.Count(ctx, model); err != nil {
		return setting.Status{}, err
	}
	if emb == nil {
		st.EmbeddedCount = 0
	}
	if s.d.Reindexing != nil {
		st.Reindexing = s.d.Reindexing()
	}
	return st, nil
}

// acquire takes a slot of sem, waiting at most wait; the caller releases it.
func acquire(ctx context.Context, sem chan struct{}, wait time.Duration) bool {
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case sem <- struct{}{}:
		return true
	case <-t.C:
		return false
	case <-ctx.Done():
		return false
	}
}
