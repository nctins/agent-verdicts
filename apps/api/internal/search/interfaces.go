package search

import (
	"context"
	"errors"

	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/trust"
)

// Filters narrows a search with the same semantics as contribution.ListFilter:
// States applies to claims only, QuestionStatus to questions only (union
// when both are set), Kind narrows further.
type Filters struct {
	Kind           contribution.Kind
	States         []trust.State
	QuestionStatus []contribution.QuestionStatus
	// Superseded and hidden claims are left out unless asked for.
	IncludeSuperseded bool
	IncludeHidden     bool // moderators only
}

// ErrDimMismatch: stored vectors and the index/vector disagree on dimension
// (ChunkStore.EnsureIndex, Replace). Repairable by DeleteAll + EnsureIndex.
var ErrDimMismatch = errors.New("search: vector dimension mismatch")

type Hit struct{ ContributionID string }

type ChunkHit struct {
	ContributionID string
	Idx            int
	Text           string
}

type LexicalIndex interface {
	Search(ctx context.Context, projectID, foldedQuery string, f Filters, n int) ([]Hit, error)
}

// VectorIndex returns at most one hit per contribution (its best chunk).
type VectorIndex interface {
	Search(ctx context.Context, projectID string, v []float32, model string, f Filters, n int) ([]ChunkHit, error)
}

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type Reranker interface {
	Rerank(ctx context.Context, query string, docs []string) ([]float64, error)
}

type ChunkStore interface {
	Replace(ctx context.Context, contributionID, model string, chunks []string, vecs [][]float32) error
	// Pending: contributions with no chunk for model, oldest first.
	Pending(ctx context.Context, model string, limit int) ([]string, error)
	DeleteAll(ctx context.Context) error
	Count(ctx context.Context, model string) (embedded, total int, err error)
	// EnsureIndex builds the HNSW index for model (CREATE INDEX CONCURRENTLY
	// … WHERE model = …) and drops the indexes of other models.
	EnsureIndex(ctx context.Context, model string, dim int) error
	// BestChunk: the chunk of contributionID nearest to v; zero ChunkHit when
	// the contribution has no chunk for model.
	BestChunk(ctx context.Context, contributionID, model string, v []float32) (ChunkHit, error)
}
