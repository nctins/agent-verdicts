package mcp_test

import (
	"net/http"
	"strconv"
	"testing"
)

// The limit sits after the bearer check and before the project gate: an
// anonymous caller gets 401 and no count; an authenticated one gets 429 with
// Retry-After once over the limit, whatever the project says.
func TestRateLimit_AfterBearerBeforeProjectGate(t *testing.T) {
	e := newEnvRate(t, 3)
	_, token := e.agent(t, "noisy") // no role: the project gate answers 404
	post := func(tok string) *http.Response {
		req, _ := http.NewRequest("POST", e.ts.URL+"/mcp/p", nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	for i := 0; i < 10; i++ {
		if got := post("").StatusCode; got != 401 {
			t.Fatalf("anonymous = %d, want 401", got)
		}
	}
	for i := 0; i < 3; i++ {
		if got := post(token).StatusCode; got != 404 {
			t.Fatalf("request %d = %d, want 404 from the project gate", i+1, got)
		}
	}
	res := post(token)
	if secs, err := strconv.Atoi(res.Header.Get("Retry-After")); res.StatusCode != 429 || err != nil || secs < 1 {
		t.Fatalf("over the limit = %d, Retry-After %q", res.StatusCode, res.Header.Get("Retry-After"))
	}
	// The other agent is not affected.
	if got := e.call(t, "list_contributions", map[string]any{}); got == nil {
		t.Fatal("other actor limited")
	}
}

func TestRateLimit_ZeroMeansUnlimited(t *testing.T) {
	e := newEnvRate(t, 0)
	_, token := e.agent(t, "busy")
	for i := 0; i < 400; i++ {
		req, _ := http.NewRequest("POST", e.ts.URL+"/mcp/p", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode == 429 {
			t.Fatalf("request %d limited with the limit off", i+1)
		}
	}
}
