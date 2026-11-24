package search

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"sync/atomic"
	"time"

	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/setting"
)

var sweepBatch = 500 // var for tests

const (
	queueSize     = 1024
	sweepEvery    = 60 * time.Second
	embedBatch    = 32
	docEmbedLimit = 30 * time.Second // per batch; background, so longer than the 2s query timeout
	retryAfter    = 10 * time.Minute // a failed item is skipped by sweeps this long
)

// modelKey is what the vector index depends on: a change means wipe + rebuild.
type modelKey struct {
	model string
	dim   int
}

// Worker embeds contributions in the background (spec §6.4). Everything but
// Enqueue, Reindex and Reindexing runs on the Run goroutine.
type Worker struct {
	d     Deps
	log   *log.Logger
	queue chan string
	wake  chan struct{} // settings changed or reindex requested: sweep now

	last, ensured modelKey             // last configured model; model whose index exists
	repairTried   bool                 // startup dim-mismatch repair already attempted
	failed        map[string]time.Time // id -> retry after (sweep skips until then)

	// Reindex requests: reindexing while requested != completed. wiped is
	// the last request whose DeleteAll ran (Run goroutine only).
	requested, completed atomic.Int64
	wiped                int64
}

func NewWorker(d Deps, logger *log.Logger) *Worker {
	return &Worker{d: d, log: logger, queue: make(chan string, queueSize), wake: make(chan struct{}, 1)}
}

// Enqueue never blocks: on a full queue the id is dropped and the next sweep picks it up.
func (w *Worker) Enqueue(contributionID string) {
	select {
	case w.queue <- contributionID:
	default:
	}
}

func (w *Worker) poke() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Worker) Run(ctx context.Context) {
	w.d.Settings.OnChange(func(setting.Search) { w.poke() })
	if p, ok := w.provider(); ok {
		w.last = modelKey{p.Model, p.Dim} // the startup model is not a change: keep its chunks
	}
	w.sweep(ctx)
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-w.queue:
			w.process(ctx, id)
		case <-w.wake:
			w.sweep(ctx)
		case <-t.C:
			w.sweep(ctx)
		}
	}
}

// Reindex requests a full re-embed and returns at once: the next sweep on
// the Run goroutine drops every chunk and backfills. It never fails.
func (w *Worker) Reindex(context.Context) error {
	w.requested.Add(1)
	w.poke()
	return nil
}

func (w *Worker) Reindexing() bool { return w.requested.Load() != w.completed.Load() }

func (w *Worker) provider() (setting.Provider, bool) {
	p := w.d.Settings.Current().Embedding
	return p, p.URL != "" && w.d.NewEmbedder != nil
}

// prepare brings storage in line with p: on a model/dim change DeleteAll,
// then EnsureIndex, in that order (see ChunkStore.EnsureIndex).
func (w *Worker) prepare(ctx context.Context, p setting.Provider) error {
	k := modelKey{p.Model, p.Dim}
	if k != w.last {
		if err := w.d.Chunks.DeleteAll(ctx); err != nil {
			return err
		}
		w.last, w.ensured = k, modelKey{}
	}
	if k != w.ensured {
		err := w.d.Chunks.EnsureIndex(ctx, p.Model, p.Dim)
		if errors.Is(err, ErrDimMismatch) && !w.repairTried {
			// Stale rows of this model with another dim (restart between saving
			// new settings and the wipe): wipe and retry. One completed wipe
			// per process, so a mismatch the wipe cannot cure does not drop
			// every vector on every sweep; a failed wipe (DB blip) may retry.
			w.log.Printf("search: index for model %q: stored vectors have another dimension; dropping all chunks", p.Model)
			if err = w.d.Chunks.DeleteAll(ctx); err == nil {
				w.repairTried = true // only a completed wipe spends the one repair
				err = w.d.Chunks.EnsureIndex(ctx, p.Model, p.Dim)
			}
		}
		if err != nil {
			return err
		}
		w.ensured = k
	}
	return nil
}

// sweep backfills one batch of Pending; a full batch pokes Run for the next.
func (w *Worker) sweep(ctx context.Context) {
	gen := w.requested.Load()
	if gen != w.wiped {
		if err := w.d.Chunks.DeleteAll(ctx); err != nil {
			w.log.Printf("search: reindex: %v", err)
			return
		}
		w.wiped = gen
	}
	p, ok := w.provider()
	if !ok {
		w.completed.Store(gen)
		return
	}
	if err := w.prepare(ctx, p); err != nil {
		w.log.Printf("search: prepare index for model %q: %v", p.Model, err)
		return
	}
	now := time.Now()
	for id, at := range w.failed {
		if now.After(at) {
			delete(w.failed, id)
		}
	}
	pending, err := w.d.Chunks.Pending(ctx, p.Model, sweepBatch+len(w.failed))
	if err != nil {
		w.log.Printf("search: sweep: %v", err)
		return
	}
	var ids []string // pending minus recently failed, so they cannot block newer items
	for _, id := range pending {
		if _, skip := w.failed[id]; !skip && len(ids) < sweepBatch {
			ids = append(ids, id)
		}
	}
	failed := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if err := w.embed(ctx, p, id); err != nil {
			w.log.Printf("search: sweep: %v", err)
			if w.failed == nil {
				w.failed = map[string]time.Time{}
			}
			w.failed[id] = time.Now().Add(retryAfter)
			if failed++; failed == 3 {
				return // provider likely down; next tick retries
			}
			continue // one bad item must not block newer ones
		}
		failed = 0
		delete(w.failed, id)
	}
	if len(ids) == sweepBatch {
		w.poke() // more pending: continue right after queued items
		return
	}
	// The pass is done when nothing but recently failed items is pending:
	// they are retried after retryAfter, but must not keep Reindexing true.
	if ids, err = w.d.Chunks.Pending(ctx, p.Model, len(w.failed)+1); err != nil {
		w.log.Printf("search: sweep: %v", err)
	} else if !slices.ContainsFunc(ids, func(id string) bool { _, f := w.failed[id]; return !f }) {
		w.completed.Store(gen) // a request newer than gen keeps Reindexing true
	}
}

func (w *Worker) process(ctx context.Context, id string) {
	p, ok := w.provider()
	if !ok {
		return
	}
	err := w.prepare(ctx, p)
	if err == nil {
		err = w.embed(ctx, p, id)
	}
	if err != nil {
		w.log.Printf("search: embed: %v", err)
	}
}

// embed chunks one contribution and stores its vectors; a vanished id is not an error.
func (w *Worker) embed(ctx context.Context, p setting.Provider, id string) error {
	c, err := w.d.Contributions.GetByID(ctx, id)
	if errors.Is(err, contribution.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	chunks := Chunk(c.Title, c.Body)
	emb := w.d.NewEmbedder(p)
	vecs := make([][]float32, 0, len(chunks))
	for i := 0; i < len(chunks); i += embedBatch {
		batch := chunks[i:min(i+embedBatch, len(chunks))]
		bctx, cancel := context.WithTimeout(ctx, docEmbedLimit)
		vs, err := emb.Embed(bctx, batch)
		cancel()
		if err == nil && len(vs) != len(batch) {
			err = fmt.Errorf("%d vectors for %d chunks", len(vs), len(batch))
		}
		if err != nil {
			return fmt.Errorf("contribution %s: %w", id, err)
		}
		vecs = append(vecs, vs...)
	}
	return w.d.Chunks.Replace(ctx, id, p.Model, chunks, vecs)
}
