package authentication_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/postgres"
)

func TestHashPassword_RoundtripAndSalted(t *testing.T) {
	h1, err := authentication.HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	h2, _ := authentication.HashPassword("correct horse battery")
	if h1 == h2 {
		t.Fatal("two hashes of the same password are identical — salt missing")
	}
	if !strings.HasPrefix(h1, "pbkdf2-sha256$") {
		t.Fatalf("hash %q lacks self-describing prefix", h1)
	}
	if !authentication.CheckPassword(h1, "correct horse battery") {
		t.Fatal("CheckPassword(right) = false")
	}
	if authentication.CheckPassword(h1, "correct horse batterY") {
		t.Fatal("CheckPassword(wrong) = true")
	}
	for _, bad := range []string{"", "garbage", "pbkdf2-sha256$x$y$z", "md5$1$a$b"} {
		if authentication.CheckPassword(bad, "anything") {
			t.Fatalf("CheckPassword(malformed %q) = true", bad)
		}
	}
}

func TestNormalizeUsername(t *testing.T) {
	ok := map[string]string{"Alice": "alice", " bob.smith ": "bob.smith", "a_b-c": "a_b-c", "abc": "abc"}
	for in, want := range ok {
		got, err := authentication.NormalizeUsername(in)
		if err != nil || got != want {
			t.Errorf("NormalizeUsername(%q) = (%q, %v), want (%q, nil)", in, got, err, want)
		}
	}
	for _, in := range []string{"", "ab", "has space", "ümlaut", strings.Repeat("a", 33), "semi;colon"} {
		if _, err := authentication.NormalizeUsername(in); !errors.Is(err, authentication.ErrInvalidUsername) {
			t.Errorf("NormalizeUsername(%q) error = %v, want ErrInvalidUsername", in, err)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if err := authentication.ValidatePassword("short"); !errors.Is(err, authentication.ErrPasswordTooShort) {
		t.Errorf("short: %v", err)
	}
	if err := authentication.ValidatePassword(strings.Repeat("x", 129)); !errors.Is(err, authentication.ErrPasswordTooLong) {
		t.Errorf("long: %v", err)
	}
	if err := authentication.ValidatePassword("ten chars!"); err != nil {
		t.Errorf("10 chars: %v", err)
	}
}

func TestGeneratePassword_ReadableAndValid(t *testing.T) {
	pat := regexp.MustCompile(`^[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}$`)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		p := authentication.GeneratePassword()
		if !pat.MatchString(p) || strings.ContainsAny(p, "01lio") {
			t.Fatalf("GeneratePassword() = %q, want 4 groups of unambiguous chars", p)
		}
		if err := authentication.ValidatePassword(p); err != nil {
			t.Fatalf("generated password fails policy: %v", err)
		}
		if seen[p] {
			t.Fatal("duplicate generated password")
		}
		seen[p] = true
	}
}

func newLoginFixture(t *testing.T) (*postgres.LoginRepo, actor.Repository, actor.Actor) {
	t.Helper()
	db, _ := newTestRepos(t)
	actors := postgres.NewActorRepo(db)
	h := actor.New(actor.TypeHuman)
	h.Name = "Alice"
	if err := actors.Create(context.Background(), h); err != nil {
		t.Fatalf("seed human: %v", err)
	}
	return postgres.NewLoginRepo(db), actors, h
}

func TestResetAndVerifyPassword(t *testing.T) {
	logins, actors, h := newLoginFixture(t)
	ctx := context.Background()

	pw, err := authentication.ResetPassword(ctx, logins, actors, h.ID, "Alice")
	if err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}
	l, err := authentication.VerifyPassword(ctx, actors, logins, "ALICE", pw)
	if err != nil {
		t.Fatalf("VerifyPassword(right, other case) error = %v", err)
	}
	if l.ActorID != h.ID || l.Username != "alice" {
		t.Fatalf("VerifyPassword() = %+v", l)
	}
	for _, c := range [][2]string{{"alice", "wrong-password"}, {"nobody", pw}, {"", ""}} {
		if _, err := authentication.VerifyPassword(ctx, actors, logins, c[0], c[1]); !errors.Is(err, authentication.ErrUnauthenticated) {
			t.Errorf("VerifyPassword(%q, …) error = %v, want ErrUnauthenticated", c[0], err)
		}
	}

	// A second reset replaces the password and moves ChangedAt forward.
	time.Sleep(2 * time.Millisecond)
	pw2, err := authentication.ResetPassword(ctx, logins, actors, h.ID, "")
	if err != nil {
		t.Fatalf("second ResetPassword(keep username) error = %v", err)
	}
	if _, err := authentication.VerifyPassword(ctx, actors, logins, "alice", pw); err == nil {
		t.Fatal("old password still works after reset")
	}
	l2, err := authentication.VerifyPassword(ctx, actors, logins, "alice", pw2)
	if err != nil || !l2.ChangedAt.After(l.ChangedAt) {
		t.Fatalf("after reset: %+v, %v", l2, err)
	}
}

func TestResetPassword_Rules(t *testing.T) {
	logins, actors, h := newLoginFixture(t)
	ctx := context.Background()

	agent := actor.New(actor.TypeAgent)
	agent.Name = "Bot"
	actors.Create(ctx, agent)
	if _, err := authentication.ResetPassword(ctx, logins, actors, agent.ID, "bot"); !errors.Is(err, authentication.ErrNotHuman) {
		t.Errorf("agent: %v, want ErrNotHuman", err)
	}
	if _, err := authentication.ResetPassword(ctx, logins, actors, h.ID, ""); !errors.Is(err, authentication.ErrInvalidUsername) {
		t.Errorf("no username yet and none given: %v, want ErrInvalidUsername", err)
	}
	if _, err := authentication.ResetPassword(ctx, logins, actors, "missing", "x-user"); !errors.Is(err, authentication.ErrActorNotFound) {
		t.Errorf("missing actor: %v, want ErrActorNotFound", err)
	}

	other := actor.New(actor.TypeHuman)
	other.Name = "Other"
	actors.Create(ctx, other)
	if _, err := authentication.ResetPassword(ctx, logins, actors, h.ID, "taken"); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := authentication.ResetPassword(ctx, logins, actors, other.ID, "Taken"); !errors.Is(err, authentication.ErrUsernameTaken) {
		t.Errorf("duplicate username: %v, want ErrUsernameTaken", err)
	}
}

func TestVerifyPassword_DisabledActorRejected(t *testing.T) {
	logins, actors, h := newLoginFixture(t)
	ctx := context.Background()
	pw, _ := authentication.ResetPassword(ctx, logins, actors, h.ID, "alice")
	now := time.Now()
	h.DisabledAt = &now
	actors.Update(ctx, h)
	if _, err := authentication.VerifyPassword(ctx, actors, logins, "alice", pw); !errors.Is(err, authentication.ErrUnauthenticated) {
		t.Fatalf("disabled actor: %v, want ErrUnauthenticated", err)
	}
}

func TestChangePassword(t *testing.T) {
	logins, actors, h := newLoginFixture(t)
	ctx := context.Background()
	pw, _ := authentication.ResetPassword(ctx, logins, actors, h.ID, "alice")

	if err := authentication.ChangePassword(ctx, logins, h.ID, "wrong-current", "new password 1"); !errors.Is(err, authentication.ErrUnauthenticated) {
		t.Errorf("wrong current: %v", err)
	}
	if err := authentication.ChangePassword(ctx, logins, h.ID, pw, "short"); !errors.Is(err, authentication.ErrPasswordTooShort) {
		t.Errorf("short new: %v", err)
	}
	if err := authentication.ChangePassword(ctx, logins, h.ID, pw, "new password 1"); err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	if _, err := authentication.VerifyPassword(ctx, actors, logins, "alice", "new password 1"); err != nil {
		t.Fatalf("new password rejected: %v", err)
	}
}

type failingActors struct{ actor.Repository }

func (failingActors) Get(context.Context, string) (actor.Actor, error) {
	return actor.Actor{}, errors.New("database is locked")
}

// An infrastructure error is not "wrong password": callers must be able to
// tell them apart (no lockout strike, log it).
func TestVerifyPassword_InfraErrorIsNotUnauthenticated(t *testing.T) {
	logins, actors, h := newLoginFixture(t)
	ctx := context.Background()
	pw, _ := authentication.ResetPassword(ctx, logins, actors, h.ID, "alice")
	_, err := authentication.VerifyPassword(ctx, failingActors{actors}, logins, "alice", pw)
	if err == nil || errors.Is(err, authentication.ErrUnauthenticated) {
		t.Fatalf("infra error = %v, want a non-ErrUnauthenticated error", err)
	}
}

func TestSetLogin_ChosenPasswordAndMustChange(t *testing.T) {
	logins, actors, h := newLoginFixture(t)
	ctx := context.Background()

	if _, err := authentication.SetLogin(ctx, logins, actors, h.ID, "alice", "short", false); !errors.Is(err, authentication.ErrPasswordTooShort) {
		t.Fatalf("short chosen password: %v", err)
	}
	pw, err := authentication.SetLogin(ctx, logins, actors, h.ID, "alice", "chosen password 1", true)
	if err != nil || pw != "chosen password 1" {
		t.Fatalf("SetLogin(chosen) = %q, %v", pw, err)
	}
	l, err := authentication.VerifyPassword(ctx, actors, logins, "alice", "chosen password 1")
	if err != nil || !l.MustChange {
		t.Fatalf("after SetLogin(mustChange): %+v, %v", l, err)
	}
	if err := authentication.ChangePassword(ctx, logins, h.ID, "chosen password 1", "my own password"); err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	l, _ = logins.GetByActor(ctx, h.ID)
	if l.MustChange {
		t.Fatal("ChangePassword did not clear MustChange")
	}

	gen, err := authentication.SetLogin(ctx, logins, actors, h.ID, "", "", false)
	if err != nil || len(gen) != 19 {
		t.Fatalf("SetLogin(generate, keep username) = %q, %v", gen, err)
	}
}

func TestGenerateCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		c := authentication.GenerateCode(6)
		if len(c) != 6 || strings.ContainsAny(c, "01lio-") {
			t.Fatalf("GenerateCode(6) = %q", c)
		}
		seen[c] = true
	}
	if len(seen) < 19 {
		t.Fatal("codes repeat too often")
	}
}
