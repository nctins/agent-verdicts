package authentication_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/postgres"
)

func spyHandler(called *bool, sawActorID *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		if id, ok := authentication.ActorID(r.Context()); ok {
			*sawActorID = id
		}
		w.WriteHeader(http.StatusOK)
	})
}

func TestMiddleware_MissingCredential_RejectsBeforeNext(t *testing.T) {
	db, repo := newTestRepos(t)
	actors := postgres.NewActorRepo(db)

	var called bool
	var sawActorID string
	handler := authentication.Middleware(actors, repo)(spyHandler(&called, &sawActorID))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("next handler was called despite missing credential — fail-closed violated")
	}
	if rec.Code == http.StatusOK {
		t.Fatalf("response code = %d, want a rejection status, not 200", rec.Code)
	}
}

func TestMiddleware_MalformedCredential_RejectsBeforeNext(t *testing.T) {
	db, repo := newTestRepos(t)
	actors := postgres.NewActorRepo(db)

	var called bool
	var sawActorID string
	handler := authentication.Middleware(actors, repo)(spyHandler(&called, &sawActorID))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "not-a-bearer-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("next handler was called despite malformed credential — fail-closed violated")
	}
}

func TestMiddleware_InvalidCredential_RejectsBeforeNext(t *testing.T) {
	db, repo := newTestRepos(t)
	actors := postgres.NewActorRepo(db)

	var called bool
	var sawActorID string
	handler := authentication.Middleware(actors, repo)(spyHandler(&called, &sawActorID))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer does-not-exist.some-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("next handler was called despite invalid credential — fail-closed violated")
	}
}

func TestMiddleware_RevokedCredential_RejectsBeforeNext(t *testing.T) {
	db, repo := newTestRepos(t)
	a := seedActor(t, db)
	actors := postgres.NewActorRepo(db)
	ctx := context.Background()

	token, err := authentication.Issue(ctx, actors, repo, a.ID)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	credentialID := strings.SplitN(token, ".", 2)[0]
	if err := authentication.Revoke(ctx, repo, credentialID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	var called bool
	var sawActorID string
	handler := authentication.Middleware(actors, repo)(spyHandler(&called, &sawActorID))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("next handler was called despite revoked credential — fail-closed violated")
	}
}

func TestMiddleware_ValidCredential_CallsNextWithActorIDInContext(t *testing.T) {
	db, repo := newTestRepos(t)
	a := seedActor(t, db)
	actors := postgres.NewActorRepo(db)

	token, err := authentication.Issue(context.Background(), actors, repo, a.ID)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	var called bool
	var sawActorID string
	handler := authentication.Middleware(actors, repo)(spyHandler(&called, &sawActorID))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler was not called despite a valid credential")
	}
	if sawActorID != a.ID {
		t.Fatalf("next handler saw ActorID = %q, want %q", sawActorID, a.ID)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response code = %d, want 200 (from spy handler)", rec.Code)
	}
}
