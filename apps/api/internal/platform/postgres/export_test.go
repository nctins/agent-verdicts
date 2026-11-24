package postgres

import "agent-verdicts/internal/search"

// VectorQueryForTest exposes the vector search SQL so tests can EXPLAIN it.
func VectorQueryForTest(projectID string, v []float32, model string, f search.Filters, n int) (string, []any) {
	return vectorQuery(projectID, v, model, f, n, n*vectorWindowPerHit)
}
