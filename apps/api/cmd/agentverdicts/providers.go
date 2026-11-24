package main

import (
	"context"
	"fmt"
	"time"

	"agent-verdicts/internal/platform/embedding"
	"agent-verdicts/internal/platform/rerank"
	"agent-verdicts/internal/setting"
)

const providerTestTimeout = 10 * time.Second

// testProviders is the setting.Tester: one real call to each enabled
// provider, filling Embedding.Dim from the returned vector.
func testProviders(ctx context.Context, s setting.Search) (setting.Search, error) {
	if p := s.Embedding; p.URL != "" {
		c, cancel := context.WithTimeout(ctx, providerTestTimeout)
		vecs, err := embedding.New(p.URL, p.Model, p.APIKey, nil).Embed(c, []string{"kiểm tra / connection test"})
		cancel()
		if err != nil {
			return s, err // already prefixed "embedding:"
		}
		if len(vecs) != 1 || len(vecs[0]) == 0 {
			return s, fmt.Errorf("embedding: want 1 non-empty vector, got %d", len(vecs))
		}
		s.Embedding.Dim = len(vecs[0])
	}
	if p := s.Rerank; p.URL != "" {
		c, cancel := context.WithTimeout(ctx, providerTestTimeout)
		scores, err := rerank.New(p.URL, p.Model, p.APIKey, nil).Rerank(c, "kiểm tra", []string{"kiểm tra kết nối", "connection test"})
		cancel()
		if err != nil {
			return s, err // already prefixed "rerank:"
		}
		if len(scores) != 2 {
			return s, fmt.Errorf("rerank: want 2 scores, got %d", len(scores))
		}
	}
	return s, nil
}
