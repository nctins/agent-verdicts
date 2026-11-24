package authentication_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/postgres"
)

func TestIssueWith_StoresLabelAndExpiry(t *testing.T) {
	db, repo := newTestRepos(t)
	a := seedActor(t, db)
	ctx := context.Background()
	exp := time.Now().Add(30 * 24 * time.Hour)

	token, err := authentication.IssueWith(ctx, postgres.NewActorRepo(db), repo, a.ID, "  ci runner ", &exp)
	if err != nil {
		t.Fatal(err)
	}
	id, _, _ := strings.Cut(token, ".")
	c, err := repo.Get(ctx, id)
	if err != nil || c.Label != "ci runner" || c.ExpiresAt == nil || c.ExpiresAt.Sub(exp).Abs() > time.Millisecond || c.LastUsedAt != nil {
		t.Fatalf("credential = %+v, %v", c, err)
	}
	// Issue (no options) keeps today's behaviour: no label, never expires.
	token, _ = authentication.Issue(ctx, postgres.NewActorRepo(db), repo, a.ID)
	id, _, _ = strings.Cut(token, ".")
	if c, _ := repo.Get(ctx, id); c.Label != "" || c.ExpiresAt != nil {
		t.Fatalf("Issue() credential = %+v", c)
	}
}

func TestIssueWith_RejectsBadLabel(t *testing.T) {
	db, repo := newTestRepos(t)
	a := seedActor(t, db)
	for _, label := range []string{strings.Repeat("x", 65), "a\nb", "tab\there"} {
		if _, err := authentication.IssueWith(context.Background(), postgres.NewActorRepo(db), repo, a.ID, label, nil); !errors.Is(err, authentication.ErrInvalidLabel) {
			t.Errorf("label %q: err = %v, want ErrInvalidLabel", label, err)
		}
	}
	if _, err := authentication.IssueWith(context.Background(), postgres.NewActorRepo(db), repo, a.ID, strings.Repeat("é", 64), nil); err != nil {
		t.Errorf("64 runes refused: %v", err)
	}
}

func TestVerify_ExpiredTokenIsUnauthenticated(t *testing.T) {
	db, repo := newTestRepos(t)
	a := seedActor(t, db)
	actors := postgres.NewActorRepo(db)
	ctx := context.Background()

	past, future := time.Now().Add(-time.Second), time.Now().Add(time.Hour)
	expired, _ := authentication.IssueWith(ctx, actors, repo, a.ID, "", &past)
	live, _ := authentication.IssueWith(ctx, actors, repo, a.ID, "", &future)

	if _, err := authentication.Verify(ctx, actors, repo, expired); !errors.Is(err, authentication.ErrUnauthenticated) {
		t.Fatalf("expired token: err = %v, want ErrUnauthenticated", err)
	}
	if got, err := authentication.Verify(ctx, actors, repo, live); err != nil || got != a.ID {
		t.Fatalf("live token: %q, %v", got, err)
	}
}

// last_used_at moves on first use, then at most once a minute.
func TestVerify_LastUsedWrittenAtMostOncePerMinute(t *testing.T) {
	db, repo := newTestRepos(t)
	a := seedActor(t, db)
	actors := postgres.NewActorRepo(db)
	ctx := context.Background()
	token, _ := authentication.Issue(ctx, actors, repo, a.ID)
	id, _, _ := strings.Cut(token, ".")
	lastUsed := func() *time.Time {
		c, err := repo.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return c.LastUsedAt
	}
	if lastUsed() != nil {
		t.Fatal("last_used_at set before use")
	}
	verify := func() {
		t.Helper()
		if _, err := authentication.Verify(ctx, actors, repo, token); err != nil {
			t.Fatal(err)
		}
	}
	verify()
	first := lastUsed()
	if first == nil {
		t.Fatal("last_used_at not written on first use")
	}
	verify()
	verify()
	if got := lastUsed(); got == nil || !got.Equal(*first) {
		t.Fatalf("last_used_at rewritten within a minute: %v -> %v", first, got)
	}

	// Two minutes later it moves again.
	if _, err := db.Exec(ctx, `UPDATE credential SET last_used_at = now() - interval '2 minutes' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	stale := lastUsed()
	verify()
	if got := lastUsed(); got == nil || !got.After(*stale) {
		t.Fatalf("last_used_at not refreshed after a minute: %v -> %v", stale, got)
	}
}
