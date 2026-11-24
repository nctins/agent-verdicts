package providerhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPostJSONHeadersAndBody(t *testing.T) {
	var auth, ct, path string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, ct, path = r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.URL.Path
		_, _ = w.Write([]byte("ok"))
	}))
	defer s.Close()
	out, err := PostJSON(context.Background(), NewClient(nil), s.URL+"/x", "secret", map[string]any{"a": 1})
	if err != nil || string(out) != "ok" || auth != "Bearer secret" || ct != "application/json" || path != "/x" {
		t.Fatalf("%q %v %q %q %q", out, err, auth, ct, path)
	}
}

func TestPostJSONNoKeyNoHeader(t *testing.T) {
	var has bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, has = r.Header["Authorization"]
	}))
	defer s.Close()
	if _, err := PostJSON(context.Background(), NewClient(nil), s.URL, "", 1); err != nil || has {
		t.Fatalf("%v %v", err, has)
	}
}

func TestPostJSONStatusExcerptAndRedaction(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte("bad key " + r.Header.Get("Authorization")[7:] + strings.Repeat("x", 1000)))
	}))
	defer s.Close()
	_, err := PostJSON(context.Background(), NewClient(nil), s.URL, "sk-topsecret", 1)
	if err == nil || !strings.Contains(err.Error(), "status 401") || strings.Contains(err.Error(), "sk-topsecret") || !strings.Contains(err.Error(), "***") || len(err.Error()) > 350 {
		t.Fatalf("err: %v", err)
	}
}

func TestPostJSONRedactsTransportError(t *testing.T) {
	// The key in the URL is echoed by net/http's *url.Error.
	_, err := PostJSON(context.Background(), NewClient(nil), "http://127.0.0.1:1/sk-topsecret", "sk-topsecret", 1)
	if err == nil || strings.Contains(err.Error(), "sk-topsecret") {
		t.Fatalf("err: %v", err)
	}
}

func TestPostJSONBodyCap(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := []byte(strings.Repeat("1", 1<<20))
		for i := 0; i < 33; i++ {
			_, _ = w.Write(chunk)
		}
	}))
	defer s.Close()
	_, err := PostJSON(context.Background(), NewClient(nil), s.URL, "", 1)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("got %v", err)
	}
}

func TestPostJSONContextCancel(t *testing.T) {
	block := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block }))
	defer s.Close()
	defer close(block)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := PostJSON(ctx, NewClient(nil), s.URL, "", 1); err == nil || time.Since(start) > time.Second {
		t.Fatalf("err=%v elapsed=%v", err, time.Since(start))
	}
}

func redirectPair(t *testing.T) (first *httptest.Server, leaked *string, hit *bool) {
	var l string
	var h bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, l = true, r.Header.Get("Authorization")
	}))
	first = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/t", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(func() { other.Close(); first.Close() })
	return first, &l, &h
}

func TestRedirectOtherPortDoesNotLeakKey(t *testing.T) {
	first, leaked, hit := redirectPair(t)
	if _, err := PostJSON(context.Background(), NewClient(nil), first.URL, "secret", 1); err != nil {
		t.Fatal(err)
	}
	if !*hit {
		t.Fatal("redirect target not reached; test is vacuous")
	}
	if *leaked != "" {
		t.Fatalf("key leaked: %q", *leaked)
	}
}

func TestNewClientKeepsCallerCheckRedirect(t *testing.T) {
	called := false
	hc := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { called = true; return http.ErrUseLastResponse }}
	if got := NewClient(hc); got != hc {
		t.Fatal("caller client replaced")
	}
	first, _, _ := redirectPair(t)
	_, _ = PostJSON(context.Background(), hc, first.URL, "", 1)
	if !called {
		t.Fatal("caller policy not used")
	}
}
