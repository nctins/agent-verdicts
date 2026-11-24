package authentication_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/postgres"
)

func TestVerify_ValidToken_ResolvesActor(t *testing.T) {
	db, repo := newTestRepos(t)
	a := seedActor(t, db)
	actors := postgres.NewActorRepo(db)
	ctx := context.Background()

	token, err := authentication.Issue(ctx, actors, repo, a.ID)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	gotActorID, err := authentication.Verify(ctx, actors, repo, token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if gotActorID != a.ID {
		t.Fatalf("Verify() actorID = %q, want %q", gotActorID, a.ID)
	}
}

func TestVerify_MalformedToken_ReturnsErrUnauthenticated(t *testing.T) {
	db, repo := newTestRepos(t)
	actors := postgres.NewActorRepo(db)
	for _, tok := range []string{"", "no-separator", ".", "id.", ".secret"} {
		if _, err := authentication.Verify(context.Background(), actors, repo, tok); !errors.Is(err, authentication.ErrUnauthenticated) {
			t.Errorf("Verify(%q) error = %v, want ErrUnauthenticated", tok, err)
		}
	}
}

func TestVerify_UnknownCredentialID_ReturnsErrUnauthenticated(t *testing.T) {
	db, repo := newTestRepos(t)
	actors := postgres.NewActorRepo(db)
	_, err := authentication.Verify(context.Background(), actors, repo, "does-not-exist.some-secret")
	if !errors.Is(err, authentication.ErrUnauthenticated) {
		t.Fatalf("Verify() error = %v, want ErrUnauthenticated", err)
	}
}

// TestVerify_TamperedSecret_CannotImpersonate is the direct proof that a
// caller who guesses/observes a credential_id but not its secret cannot
// authenticate as that credential's Actor (Decision #9 §4).
func TestVerify_TamperedSecret_CannotImpersonate(t *testing.T) {
	db, repo := newTestRepos(t)
	a := seedActor(t, db)
	actors := postgres.NewActorRepo(db)
	ctx := context.Background()

	token, err := authentication.Issue(ctx, actors, repo, a.ID)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	credentialID := strings.SplitN(token, ".", 2)[0]
	tampered := credentialID + ".attacker-guessed-secret"

	if _, err := authentication.Verify(ctx, actors, repo, tampered); !errors.Is(err, authentication.ErrUnauthenticated) {
		t.Fatalf("Verify() with tampered secret error = %v, want ErrUnauthenticated", err)
	}
}

func TestVerify_RevokedCredential_ReturnsErrUnauthenticated(t *testing.T) {
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
	if _, err := authentication.Verify(ctx, actors, repo, token); !errors.Is(err, authentication.ErrUnauthenticated) {
		t.Fatalf("Verify() after revoke error = %v, want ErrUnauthenticated", err)
	}
}

// TestVerify_CredentialResolvesOnlyItsBoundActor proves a credential never
// resolves to any Actor other than the one it was issued for.
func TestVerify_CredentialResolvesOnlyItsBoundActor(t *testing.T) {
	db, repo := newTestRepos(t)
	a1 := seedActor(t, db)
	a2 := seedActor(t, db)
	actors := postgres.NewActorRepo(db)
	ctx := context.Background()

	token1, err := authentication.Issue(ctx, actors, repo, a1.ID)
	if err != nil {
		t.Fatalf("Issue() a1 error = %v", err)
	}
	token2, err := authentication.Issue(ctx, actors, repo, a2.ID)
	if err != nil {
		t.Fatalf("Issue() a2 error = %v", err)
	}

	got1, err := authentication.Verify(ctx, actors, repo, token1)
	if err != nil || got1 != a1.ID {
		t.Fatalf("Verify(token1) = %q, %v, want %q, nil", got1, err, a1.ID)
	}
	got2, err := authentication.Verify(ctx, actors, repo, token2)
	if err != nil || got2 != a2.ID {
		t.Fatalf("Verify(token2) = %q, %v, want %q, nil", got2, err, a2.ID)
	}
}

// TestRotation_IssueNewBeforeRevokeOld_NoLockout proves Decision #9 §2's
// stated purpose: issuing a replacement credential before revoking the old
// one means the Actor is never left without a working credential.
func TestRotation_IssueNewBeforeRevokeOld_NoLockout(t *testing.T) {
	db, repo := newTestRepos(t)
	a := seedActor(t, db)
	actors := postgres.NewActorRepo(db)
	ctx := context.Background()

	oldToken, err := authentication.Issue(ctx, actors, repo, a.ID)
	if err != nil {
		t.Fatalf("Issue() old error = %v", err)
	}
	newToken, err := authentication.Issue(ctx, actors, repo, a.ID)
	if err != nil {
		t.Fatalf("Issue() new error = %v", err)
	}

	// Between issuing new and revoking old, BOTH must authenticate — this
	// is the no-lockout window.
	if _, err := authentication.Verify(ctx, actors, repo, oldToken); err != nil {
		t.Fatalf("old token must still verify before revocation: %v", err)
	}
	if _, err := authentication.Verify(ctx, actors, repo, newToken); err != nil {
		t.Fatalf("new token must verify immediately after issuance: %v", err)
	}

	oldCredentialID := strings.SplitN(oldToken, ".", 2)[0]
	if err := authentication.Revoke(ctx, repo, oldCredentialID); err != nil {
		t.Fatalf("Revoke() old error = %v", err)
	}

	if _, err := authentication.Verify(ctx, actors, repo, oldToken); !errors.Is(err, authentication.ErrUnauthenticated) {
		t.Fatalf("old token after revoke: error = %v, want ErrUnauthenticated", err)
	}
	gotActorID, err := authentication.Verify(ctx, actors, repo, newToken)
	if err != nil || gotActorID != a.ID {
		t.Fatalf("new token after old revoked: = %q, %v, want %q, nil (no lockout)", gotActorID, err, a.ID)
	}
}

func TestVerify_DisabledActor_Unauthenticated(t *testing.T) {
	db, repo := newTestRepos(t)
	actors := postgres.NewActorRepo(db)
	ctx := context.Background()

	id, token, err := authentication.Bootstrap(ctx, actors, repo, actor.TypeAgent, "bot")
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	a, _ := actors.Get(ctx, id)
	now := time.Now()
	a.DisabledAt = &now
	if err := actors.Update(ctx, a); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if _, err := authentication.Verify(ctx, actors, repo, token); !errors.Is(err, authentication.ErrUnauthenticated) {
		t.Fatalf("Verify(disabled actor) error = %v, want ErrUnauthenticated", err)
	}
}
