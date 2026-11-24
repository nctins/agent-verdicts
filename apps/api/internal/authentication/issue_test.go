package authentication_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/postgres"
)

func TestIssue_ReturnsTokenBoundToExistingActor(t *testing.T) {
	db, repo := newTestRepos(t)
	a := seedActor(t, db)
	actors := postgres.NewActorRepo(db)

	token, err := authentication.Issue(context.Background(), actors, repo, a.ID)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		t.Fatalf("token = %q, want two non-empty '.'-separated parts", token)
	}

	cred, err := repo.Get(context.Background(), parts[0])
	if err != nil {
		t.Fatalf("Get() persisted credential error = %v", err)
	}
	if cred.ActorID != a.ID {
		t.Fatalf("persisted credential ActorID = %q, want %q", cred.ActorID, a.ID)
	}
}

func TestIssue_UnknownActor_ReturnsErrActorNotFound(t *testing.T) {
	db, repo := newTestRepos(t)
	actors := postgres.NewActorRepo(db)

	_, err := authentication.Issue(context.Background(), actors, repo, "does-not-exist")
	if !errors.Is(err, authentication.ErrActorNotFound) {
		t.Fatalf("Issue() error = %v, want authentication.ErrActorNotFound", err)
	}
}

// TestIssue_StoresOnlyHash_NotRawSecret is the direct proof of Decision #9
// §5: storage must not let a holder of the DB row impersonate the Actor.
func TestIssue_StoresOnlyHash_NotRawSecret(t *testing.T) {
	db, repo := newTestRepos(t)
	a := seedActor(t, db)
	actors := postgres.NewActorRepo(db)

	token, err := authentication.Issue(context.Background(), actors, repo, a.ID)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	parts := strings.SplitN(token, ".", 2)
	credentialID, secret := parts[0], parts[1]

	var storedHash string
	if err := db.QueryRow(context.Background(), `SELECT secret_hash FROM credential WHERE id = $1`, credentialID).Scan(&storedHash); err != nil {
		t.Fatalf("read stored secret_hash: %v", err)
	}
	if storedHash == secret {
		t.Fatal("secret_hash column stores the raw secret verbatim — must store only a one-way hash")
	}
	if strings.Contains(storedHash, secret) {
		t.Fatal("secret_hash column contains the raw secret as a substring")
	}
}

func TestIssue_TwoCalls_ProduceDifferentTokens(t *testing.T) {
	db, repo := newTestRepos(t)
	a := seedActor(t, db)
	actors := postgres.NewActorRepo(db)

	t1, err := authentication.Issue(context.Background(), actors, repo, a.ID)
	if err != nil {
		t.Fatalf("Issue() #1 error = %v", err)
	}
	t2, err := authentication.Issue(context.Background(), actors, repo, a.ID)
	if err != nil {
		t.Fatalf("Issue() #2 error = %v", err)
	}
	if t1 == t2 {
		t.Fatal("two Issue() calls for the same Actor returned identical tokens — must be independently random")
	}
}
