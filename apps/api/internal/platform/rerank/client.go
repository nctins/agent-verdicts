// Package rerank is a minimal client for Cohere/Jina/TEI-style /rerank.
package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"agent-verdicts/internal/platform/providerhttp"
)

type Client struct {
	url, model, apiKey string
	hc                 *http.Client
}

// New returns a client; hc may be nil. Timeouts come from the caller's ctx.
func New(url, model, apiKey string, hc *http.Client) *Client {
	return &Client{url: strings.TrimRight(url, "/"), model: model, apiKey: apiKey, hc: providerhttp.NewClient(hc)}
}

// Rerank returns one score per doc, in docs order.
func (c *Client) Rerank(ctx context.Context, query string, docs []string) ([]float64, error) {
	if len(docs) == 0 {
		return []float64{}, nil
	}
	// "documents" is read by Cohere/Jina, "texts" by TEI; each ignores the other.
	data, err := providerhttp.PostJSON(ctx, c.hc, c.url+"/rerank", c.apiKey,
		map[string]any{"model": c.model, "query": query, "documents": docs, "texts": docs})
	if err != nil {
		return nil, fmt.Errorf("rerank: %w", err)
	}
	type item struct {
		Index          int      `json:"index"`
		RelevanceScore *float64 `json:"relevance_score"`
		Score          *float64 `json:"score"`
	}
	var items []item
	if t := bytes.TrimSpace(data); len(t) > 0 && t[0] == '[' {
		err = json.Unmarshal(data, &items) // TEI
	} else {
		var wrapped struct {
			Results []item `json:"results"` // Cohere/Jina
		}
		err = json.Unmarshal(data, &wrapped)
		items = wrapped.Results
	}
	if err != nil {
		return nil, fmt.Errorf("rerank: bad response: %w", err)
	}
	if len(items) != len(docs) {
		return nil, fmt.Errorf("rerank: got %d scores for %d docs", len(items), len(docs))
	}
	scores := make([]float64, len(docs))
	seen := make([]bool, len(docs))
	for _, it := range items {
		s := it.RelevanceScore
		if s == nil {
			s = it.Score
		}
		if it.Index < 0 || it.Index >= len(docs) || seen[it.Index] || s == nil {
			return nil, fmt.Errorf("rerank: invalid, duplicate or scoreless index %d", it.Index)
		}
		seen[it.Index] = true
		scores[it.Index] = *s
	}
	return scores, nil
}
