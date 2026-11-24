package embedding

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

func TestEmbedRequestAndOrder(t *testing.T) {
	s, req, payload := serve(t, `{"data":[{"index":1,"embedding":[3,4]},{"index":0,"embedding":[1,2]}]}`, 200)
	c := New(s.URL+"/v1/", "m", "secret", nil)
	out, err := c.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if out[0][0] != 1 || out[1][0] != 3 {
		t.Fatalf("order: %v", out)
	}
	if req.URL.Path != "/v1/embeddings" || req.Method != "POST" || req.Header.Get("Authorization") != "Bearer secret" {
		t.Fatalf("req: %v %v %v", req.Method, req.URL.Path, req.Header)
	}
	if (*payload)["model"] != "m" || len((*payload)["input"].([]any)) != 2 {
		t.Fatalf("payload: %v", *payload)
	}
}

func TestEmbedNoKeyNoHeader(t *testing.T) {
	s, req, _ := serve(t, `{"data":[{"index":0,"embedding":[1]}]}`, 200)
	if _, err := New(s.URL, "m", "", nil).Embed(context.Background(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := req.Header["Authorization"]; ok {
		t.Fatal("Authorization sent without key")
	}
}

func TestEmbedEmptyNoCall(t *testing.T) {
	c := New("http://127.0.0.1:1", "m", "", nil)
	out, err := c.Embed(context.Background(), nil)
	if err != nil || len(out) != 0 {
		t.Fatalf("%v %v", out, err)
	}
}

func TestEmbedErrors(t *testing.T) {
	long := strings.Repeat("x", 1000)
	cases := map[string]struct {
		body   string
		status int
	}{
		"500":        {long, 500},
		"badjson":    {`not json`, 200},
		"wrongshape": {`[1,2]`, 200},
		"count":      {`{"data":[{"index":0,"embedding":[1]}]}`, 200},
		"dup":        {`{"data":[{"index":0,"embedding":[1]},{"index":0,"embedding":[1]}]}`, 200},
		"missing":    {`{"data":[{"index":0,"embedding":[1]},{"index":5,"embedding":[1]}]}`, 200},
		"lens":       {`{"data":[{"index":0,"embedding":[1]},{"index":1,"embedding":[1,2]}]}`, 200},
		"zerolen":    {`{"data":[{"index":0,"embedding":[]},{"index":1,"embedding":[]}]}`, 200},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s, _, _ := serve(t, tc.body, tc.status)
			_, err := New(s.URL, "m", "secret", nil).Embed(context.Background(), []string{"a", "b"})
			if err == nil {
				t.Fatal("want error")
			}
			if strings.Contains(err.Error(), "secret") || len(err.Error()) > 450 {
				t.Fatalf("bad error text: %d bytes", len(err.Error()))
			}
		})
	}
}
