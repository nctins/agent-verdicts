package authflow_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/adapter/authflow"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
)

const pw = "correct-horse-1"

func repos(t *testing.T) (*postgres.ActorRepo, *postgres.LoginRepo, *postgres.AccessRepo) {
	pool := pgtest.NewMigrated(t)
	return postgres.NewActorRepo(pool), postgres.NewLoginRepo(pool), postgres.NewAccessRepo(pool)
}

func human(t *testing.T, actors actor.Repository, logins authentication.LoginRepository, acc access.Repository, username string, admin bool) actor.Actor {
	t.Helper()
	a := actor.New(actor.TypeHuman)
	a.Name = username
	if err := actors.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if admin {
		if err := access.GrantAdmin(context.Background(), acc, a.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := authentication.SetLogin(context.Background(), logins, actors, a.ID, username, pw, false); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestLogin_SuccessAndLockout(t *testing.T) {
	actors, logins, acc := repos(t)
	a := human(t, actors, logins, acc, "alice", false)
	f := authflow.New(actors, logins, acc, "")
	ctx := context.Background()

	got, l, err := f.Login(ctx, "alice", pw, "203.0.113.7:1")
	if err != nil || got.ID != a.ID || l.ActorID != a.ID {
		t.Fatalf("Login = %v, %v, %v", got, l, err)
	}
	for i := 0; i < authflow.MaxFailures; i++ {
		if _, _, err := f.Login(ctx, "alice", "wrong-password", "203.0.113.7:1"); !errors.Is(err, authentication.ErrUnauthenticated) {
			t.Fatalf("attempt %d: err = %v, want ErrUnauthenticated", i, err)
		}
	}
	if _, _, err := f.Login(ctx, "alice", pw, "203.0.113.7:1"); !errors.Is(err, authflow.ErrLocked) {
		t.Fatalf("after lockout err = %v, want ErrLocked", err)
	}
}

func TestSetup_CodeGuardAndOneAdmin(t *testing.T) {
	actors, logins, acc := repos(t)
	f := authflow.New(actors, logins, acc, "ABC123")
	ctx := context.Background()
	if !f.SetupActive(ctx) {
		t.Fatal("setup should be active on an empty database")
	}
	in := authflow.SetupInput{Code: "nope", Name: "Root", Username: "root", Password: pw, ClientAddr: "203.0.113.7:1"}
	if _, _, err := f.Setup(ctx, in); !errors.Is(err, authflow.ErrBadSetupCode) {
		t.Fatalf("wrong code err = %v", err)
	}

	const n = 8
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			in := in
			in.Code, in.Username = " abc123 ", "root"+string(rune('a'+i))
			_, _, errs[i] = f.Setup(ctx, in)
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, authflow.ErrSetupClosed):
			t.Fatalf("errs = %v", errs)
		}
	}
	all, _ := actors.List(ctx)
	admins, _ := acc.AdminHumans(ctx)
	if ok != 1 || len(all) != 1 || len(admins) != 1 || admins[0] != all[0].ID {
		t.Fatalf("ok = %d, actors = %+v; want exactly one admin", ok, all)
	}
	if f.SetupActive(ctx) {
		t.Fatal("setup still active after the first admin")
	}
}

func TestSetup_ValidationAndTakenUsername(t *testing.T) {
	actors, logins, acc := repos(t)
	human(t, actors, logins, acc, "taken", false)
	f := authflow.New(actors, logins, acc, "ABC123")
	ctx := context.Background()
	base := authflow.SetupInput{Code: "ABC123", Name: "Root", Username: "root", Password: pw}
	for name, mod := range map[string]func(*authflow.SetupInput){
		"name":     func(in *authflow.SetupInput) { in.Name = " " },
		"username": func(in *authflow.SetupInput) { in.Username = "A B" },
		"password": func(in *authflow.SetupInput) { in.Password = "short" },
		"taken":    func(in *authflow.SetupInput) { in.Username = "taken" },
	} {
		in := base
		mod(&in)
		if _, _, err := f.Setup(ctx, in); err == nil {
			t.Errorf("%s: Setup succeeded", name)
		}
	}
	if _, _, err := f.Setup(ctx, authflow.SetupInput{Code: "ABC123", Name: "Root", Username: "taken", Password: pw}); !errors.Is(err, authentication.ErrUsernameTaken) {
		t.Fatalf("taken err = %v", err)
	}
	all, _ := actors.List(ctx)
	if len(all) != 1 {
		t.Fatalf("failed setups left actors behind: %+v", all)
	}
}

func TestSetup_ClosedWithoutCode(t *testing.T) {
	actors, logins, acc := repos(t)
	f := authflow.New(actors, logins, acc, "")
	if f.SetupActive(context.Background()) {
		t.Fatal("setup active without a code")
	}
	if _, _, err := f.Setup(context.Background(), authflow.SetupInput{}); !errors.Is(err, authflow.ErrSetupClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestNeedsSetup(t *testing.T) {
	actors, logins, acc := repos(t)
	ctx := context.Background()
	need := func() bool {
		n, err := authflow.NeedsSetup(ctx, logins, acc)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if !need() {
		t.Fatal("empty DB should need setup")
	}
	human(t, actors, logins, acc, "plain", false)
	if !need() {
		t.Fatal("non-admin login should not end setup")
	}
	a := human(t, actors, logins, acc, "boss", true)
	if need() {
		t.Fatal("admin with login should end setup")
	}
	now := time.Now()
	a.DisabledAt = &now
	if err := actors.Update(ctx, a); err != nil { // SetDisabled refuses to disable the last admin
		t.Fatal(err)
	}
	if !need() {
		t.Fatal("disabled admin should not count")
	}
}

func TestSetupThrottleKey_PerClientAddress(t *testing.T) {
	actors, logins, acc := repos(t)
	f := authflow.New(actors, logins, acc, "ABC123")
	ctx := context.Background()
	wrong := func(addr string) error {
		_, _, err := f.Setup(ctx, authflow.SetupInput{Code: "ZZZ", ClientAddr: addr})
		return err
	}
	for i := 0; i < authflow.MaxFailures; i++ {
		wrong("203.0.113.7:51000")
	}
	if err := wrong("203.0.113.7:60000"); !errors.Is(err, authflow.ErrLocked) {
		t.Fatalf("same host, other port: err = %v, want ErrLocked", err)
	}
	if err := wrong("198.51.100.2:40000"); !errors.Is(err, authflow.ErrBadSetupCode) {
		t.Fatalf("other client: err = %v, want ErrBadSetupCode", err)
	}
}

func TestSessionCookie_Attributes(t *testing.T) {
	for _, https := range []bool{false, true} {
		r := httptest.NewRequest("POST", "/api/v1/session", nil)
		if https {
			r.Header.Set("X-Forwarded-Proto", "https")
		}
		for _, set := range []func(http.ResponseWriter, *http.Request){
			func(w http.ResponseWriter, r *http.Request) { authflow.SetSessionCookie(w, r, "sid") },
			authflow.ClearSessionCookie,
		} {
			w := httptest.NewRecorder()
			set(w, r)
			cs := w.Result().Cookies()
			if len(cs) != 1 || cs[0].Name != "av_session" || cs[0].Path != "/" || !cs[0].HttpOnly ||
				cs[0].SameSite != http.SameSiteLaxMode || cs[0].Secure != https {
				t.Errorf("https=%v: cookie = %+v", https, cs)
			}
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "av_session", Value: "abc"})
	if sid, ok := authflow.SessionID(r); !ok || sid != "abc" {
		t.Fatalf("SessionID = %q, %v", sid, ok)
	}
}
