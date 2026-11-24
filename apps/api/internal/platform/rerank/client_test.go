package rerank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, body string, status int) (*httptest.Server, *http.Request, *map[string]any) {
	t.Helper()
	var got http.Request
	var payload map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = *r.Clone(r.Context())
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s, &got, &payload
}

func TestRerankCohereShapeAndRequest(t *testing.T) {
	s, req, payload := serve(t, `{"results":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.1}]}`, 200)
	out, err := New(s.URL+"/", "m", "secret", nil).Rerank(context.Background(), "q", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if out[0] != 0.1 || out[1] != 0.9 {
		t.Fatalf("out: %v", out)
	}
	if req.URL.Path != "/rerank" || req.Header.Get("Authorization") != "Bearer secret" {
		t.Fatalf("req: %v %v", req.URL.Path, req.Header)
	}
	p := *payload
	if p["model"] != "m" || p["query"] != "q" || len(p["documents"].([]any)) != 2 || len(p["texts"].([]any)) != 2 {
		t.Fatalf("payload: %v", p)
	}
}

func TestRerankTEIShape(t *testing.T) {
	s, _, _ := serve(t, `[{"index":0,"score":0.3},{"index":1,"score":0.7}]`, 200)
	out, err := New(s.URL, "m", "", nil).Rerank(context.Background(), "q", []string{"a", "b"})
	if err != nil || out[0] != 0.3 || out[1] != 0.7 {
		t.Fatalf("%v %v", out, err)
	}
}

func TestRerankNoKeyNoHeader(t *testing.T) {
	s, req, _ := serve(t, `[{"index":0,"score":1}]`, 200)
	if _, err := New(s.URL, "m", "", nil).Rerank(context.Background(), "q", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := req.Header["Authorization"]; ok {
		t.Fatal("Authorization sent without key")
	}
}

func TestRerankEmptyNoCall(t *testing.T) {
	out, err := New("http://127.0.0.1:1", "m", "", nil).Rerank(context.Background(), "q", nil)
	if err != nil || len(out) != 0 {
		t.Fatalf("%v %v", out, err)
	}
}

func TestRerankErrors(t *testing.T) {
	cases := map[string]struct {
		body   string
		status int
	}{
		"500":     {strings.Repeat("x", 1000), 500},
		"badjson": {`nope`, 200},
		"shape":   {`"str"`, 200},
		"count":   {`[{"index":0,"score":1}]`, 200},
		"dup":     {`[{"index":0,"score":1},{"index":0,"score":1}]`, 200},
		"missing": {`{"results":[{"index":0,"relevance_score":1},{"index":2,"relevance_score":1}]}`, 200},
		"neg":     {`[{"index":-1,"score":1},{"index":0,"score":1}]`, 200},
		"noscore": {`[{"index":0},{"index":1}]`, 200},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s, _, _ := serve(t, tc.body, tc.status)
			_, err := New(s.URL, "m", "secret", nil).Rerank(context.Background(), "q", []string{"a", "b"})
			if err == nil {
				t.Fatal("want error")
			}
			if strings.Contains(err.Error(), "secret") || len(err.Error()) > 450 {
				t.Fatalf("bad error text: %d bytes", len(err.Error()))
			}
		})
	}
}
