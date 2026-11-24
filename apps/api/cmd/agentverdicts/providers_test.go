package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-verdicts/internal/setting"
)

func fakeProvider(t *testing.T, embedResp, rerankResp string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/embeddings":
			_, _ = w.Write([]byte(embedResp))
		case "/rerank":
			_, _ = w.Write([]byte(rerankResp))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestTestProviders(t *testing.T) {
	ok := fakeProvider(t, `{"data":[{"index":0,"embedding":[0.1,0.2,0.3]}]}`, `[{"index":0,"score":0.9},{"index":1,"score":0.1}]`)
	got, err := testProviders(context.Background(), setting.Search{
		Embedding: setting.Provider{URL: ok, Model: "m"}, Rerank: setting.Provider{URL: ok, Model: "r"},
	})
	if err != nil || got.Embedding.Dim != 3 {
		t.Fatalf("ok providers: dim=%d err=%v", got.Embedding.Dim, err)
	}

	if _, err := testProviders(context.Background(), setting.Search{}); err != nil {
		t.Fatalf("disabled providers: %v", err)
	}

	for name, tc := range map[string]struct {
		url string
		s   func(string) setting.Search
	}{
		"empty vector": {fakeProvider(t, `{"data":[{"index":0,"embedding":[]}]}`, ``),
			func(u string) setting.Search { return setting.Search{Embedding: setting.Provider{URL: u}} }},
		"no vectors": {fakeProvider(t, `{"data":[]}`, ``),
			func(u string) setting.Search { return setting.Search{Embedding: setting.Provider{URL: u}} }},
		"rerank one score": {fakeProvider(t, ``, `[{"index":0,"score":0.9}]`),
			func(u string) setting.Search { return setting.Search{Rerank: setting.Provider{URL: u}} }},
		"embedding down": {"http://127.0.0.1:1",
			func(u string) setting.Search { return setting.Search{Embedding: setting.Provider{URL: u}} }},
	} {
		if _, err := testProviders(context.Background(), tc.s(tc.url)); err == nil {
			t.Errorf("%s: want error", name)
		} else if !strings.Contains(err.Error(), "embedding") && !strings.Contains(err.Error(), "rerank") {
			t.Errorf("%s: error %q does not name the provider", name, err)
		}
	}
}
