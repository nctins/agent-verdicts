package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/postgres"
)

// newRuntime serves the JSON API and nothing at / (the UI is its own image).
func TestRuntime_ServesAPIOnly(t *testing.T) {
	pool := useTestDB(t)
	t.Setenv("AGENTVERDICTS_SECRET_DIR", t.TempDir())
	ctx := context.Background()
	actors, logins := postgres.NewActorRepo(pool), postgres.NewLoginRepo(pool)
	a := actor.New(actor.TypeHuman)
	a.Name = "Boss"
	if err := actors.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := access.GrantAdmin(ctx, postgres.NewAccessRepo(pool), a.ID); err != nil {
		t.Fatal(err)
	}
	const pw = "correct-horse-1"
	if _, err := authentication.SetLogin(ctx, logins, actors, a.ID, "boss", pw, false); err != nil {
		t.Fatal(err)
	}
	h, _, err := newRuntime(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/session", strings.NewReader(`{"username":"boss","password":"`+pw+`"}`))
	req.Header.Set("Origin", ts.URL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("API login = %d, want 200", resp.StatusCode)
	}
	for _, p := range []string{"/", "/console/"} {
		resp, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("GET %s = %d, want 404", p, resp.StatusCode)
		}
	}
}
