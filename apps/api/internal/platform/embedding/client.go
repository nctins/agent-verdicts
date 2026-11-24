// Package embedding is a minimal client for OpenAI-compatible /embeddings.
package embedding

import (
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

// Embed returns one vector per text, in input order.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	data, err := providerhttp.PostJSON(ctx, c.hc, c.url+"/embeddings", c.apiKey, map[string]any{"model": c.model, "input": texts})
	if err != nil {
		return nil, fmt.Errorf("embedding: %w", err)
	}
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("embedding: bad response: %w", err)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embedding: got %d vectors for %d inputs", len(out.Data), len(texts))
	}
	vecs := make([][]float32, len(texts))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(texts) || vecs[d.Index] != nil {
			return nil, fmt.Errorf("embedding: invalid or duplicate index %d", d.Index)
		}
		if len(d.Embedding) == 0 || len(d.Embedding) != len(out.Data[0].Embedding) {
			return nil, fmt.Errorf("embedding: empty or inconsistent vector length at index %d", d.Index)
		}
		vecs[d.Index] = d.Embedding
	}
	return vecs, nil
}
