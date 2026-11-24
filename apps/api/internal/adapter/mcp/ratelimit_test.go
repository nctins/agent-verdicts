package mcp

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"agent-verdicts/internal/authentication"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
}

func callAs(h http.Handler, actorID string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/mcp/p1", nil)
	r = r.WithContext(authentication.WithActorID(r.Context(), actorID))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestRateLimit_429WithRetryAfterOverTheLimit(t *testing.T) {
	h := rateLimit(3, okHandler())
	for i := 0; i < 3; i++ {
		if w := callAs(h, "a1"); w.Code != 200 {
			t.Fatalf("request %d: %d", i+1, w.Code)
		}
	}
	w := callAs(h, "a1")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("over the limit: %d, want 429", w.Code)
	}
	secs, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil || secs < 1 || secs > 60 {
		t.Fatalf("Retry-After = %q", w.Header().Get("Retry-After"))
	}
	if w.Body.Len() == 0 {
		t.Fatal("empty 429 body")
	}
	if w := callAs(h, "a2"); w.Code != 200 { // per actor
		t.Fatalf("another actor limited: %d", w.Code)
	}
}

func TestRateLimit_ZeroDisables(t *testing.T) {
	h := rateLimit(0, okHandler())
	for i := 0; i < 1000; i++ {
		if w := callAs(h, "a1"); w.Code != 200 {
			t.Fatalf("request %d limited: %d", i+1, w.Code)
		}
	}
}

func TestRateLimiter_WindowResetsAndMapIsBounded(t *testing.T) {
	now := time.Now()
	l := newRateLimiter(2)
	l.now = func() time.Time { return now }
	l.allow("a")
	l.allow("a")
	if ok, wait := l.allow("a"); ok || wait <= 0 || wait > time.Minute {
		t.Fatalf("third request: ok=%v wait=%v", ok, wait)
	}
	now = now.Add(time.Minute + time.Second)
	if ok, _ := l.allow("a"); !ok {
		t.Fatal("still limited in the next window")
	}
	for i := 0; i < maxRateEntries*2; i++ {
		l.allow(strconv.Itoa(i))
		now = now.Add(time.Millisecond * 10)
	}
	if len(l.windows) > maxRateEntries+1 {
		t.Fatalf("%d windows tracked, bound is %d", len(l.windows), maxRateEntries)
	}
}
