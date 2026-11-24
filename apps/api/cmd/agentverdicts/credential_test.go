package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRunCredentialCLI_Bootstrap_PrintsActorIDAndToken(t *testing.T) {
	useTestDB(t)

	var stdout, stderr bytes.Buffer
	code := runCredentialCLI(context.Background(), []string{"bootstrap", "-actor-type", "Agent", "-name", "Bot"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runCredentialCLI(bootstrap) code = %d, want 0, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Actor ID:") || !strings.Contains(stdout.String(), "Credential:") {
		t.Fatalf("stdout = %q, want to contain both 'Actor ID:' and 'Credential:'", stdout.String())
	}
}

// People get a username and temporary password, never a token.
func TestRunCredentialCLI_Bootstrap_Human_NoTokenAndUsernameRequired(t *testing.T) {
	db := useTestDB(t)

	var stdout, stderr bytes.Buffer
	if code := runCredentialCLI(context.Background(), []string{"bootstrap", "-actor-type", "Human", "-name", "Alice"}, &stdout, &stderr); code != 2 {
		t.Fatalf("Human without -username: code = %d, want 2", code)
	}
	stdout.Reset()
	if code := runCredentialCLI(context.Background(), []string{"bootstrap", "-actor-type", "Human", "-name", "Alice", "-username", "alice"}, &stdout, &stderr); code != 0 {
		t.Fatalf("Human with -username: code = %d, stderr = %q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "Credential:") {
		t.Fatalf("a token was printed for a person: %q", stdout.String())
	}
	pw := lineValue(t, stdout.String(), "Password:")
	l, err := authentication.VerifyPassword(context.Background(), postgres.NewActorRepo(db), postgres.NewLoginRepo(db), "alice", pw)
	if err != nil || !l.MustChange {
		t.Fatalf("bootstrap password: %+v, %v (want valid and must-change)", l, err)
	}
}

func TestRunCredentialCLI_Bootstrap_MissingName_UsageError(t *testing.T) {
	useTestDB(t)

	for _, args := range [][]string{
		{"bootstrap", "-actor-type", "Human"},
		{"bootstrap", "-actor-type", "Human", "-name", "   "},
	} {
		var stdout, stderr bytes.Buffer
		code := runCredentialCLI(context.Background(), args, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("runCredentialCLI(%v) code = %d, want 2", args, code)
		}
		if !strings.Contains(stderr.String(), "-name") {
			t.Fatalf("runCredentialCLI(%v) stderr = %q, want mention of -name", args, stderr.String())
		}
	}
}

func TestRunCredentialCLI_Bootstrap_PersistsTrimmedName(t *testing.T) {
	db := useTestDB(t)

	for _, admin := range []bool{false, true} {
		args := []string{"bootstrap", "-actor-type", "Human", "-name", "  Alice Smith" + map[bool]string{true: " 2", false: ""}[admin] + "  ", "-username", "alice" + map[bool]string{true: "-admin", false: ""}[admin]}
		if admin {
			args = append(args, "-admin")
		}
		var stdout, stderr bytes.Buffer
		if code := runCredentialCLI(context.Background(), args, &stdout, &stderr); code != 0 {
			t.Fatalf("runCredentialCLI(%v) code = %d, stderr=%q", args, code, stderr.String())
		}
		actorID := lineValue(t, stdout.String(), "Actor ID:")

		a, err := postgres.NewActorRepo(db).Get(context.Background(), actorID)
		if err != nil {
			t.Fatalf("actors.Get() error = %v", err)
		}
		want := "Alice Smith" + map[bool]string{true: " 2", false: ""}[admin]
		if a.Name != want {
			t.Fatalf("admin=%v: Actor.Name = %q, want %q", admin, a.Name, want)
		}
	}
}

// TestRunCredentialCLI_Bootstrap_Admin_GrantsAdminRole proves the -admin
// flag gives the new Actor the admin role on every project and still
// issues a working credential, without touching authentication.Bootstrap's
// own default (no-flag) code path.
func TestRunCredentialCLI_Bootstrap_Admin_GrantsAdminRole(t *testing.T) {
	db := useTestDB(t)

	var stdout, stderr bytes.Buffer
	code := runCredentialCLI(context.Background(), []string{"bootstrap", "-actor-type", "Human", "-name", "Admin", "-username", "admin", "-admin"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runCredentialCLI(bootstrap -admin) code = %d, want 0, stderr=%q", code, stderr.String())
	}
	actorID := lineValue(t, stdout.String(), "Actor ID:")

	if g, err := postgres.NewAccessRepo(db).Grants(context.Background(), actorID); err != nil || !g.IsAdmin() {
		t.Fatalf("bootstrapped actor is not admin with -admin: %+v, %v", g, err)
	}

	l, err := authentication.VerifyPassword(context.Background(), postgres.NewActorRepo(db), postgres.NewLoginRepo(db), "admin", lineValue(t, stdout.String(), "Password:"))
	if err != nil || l.ActorID != actorID {
		t.Fatalf("admin password: %+v, %v", l, err)
	}
}

// TestRunCredentialCLI_Bootstrap_AdminWithAgent_UsageError proves -admin is
// rejected outright for -actor-type Agent (D10.7: only valid with Human).
func TestRunCredentialCLI_Bootstrap_AdminWithAgent_UsageError(t *testing.T) {
	useTestDB(t)

	var stdout, stderr bytes.Buffer
	code := runCredentialCLI(context.Background(), []string{"bootstrap", "-actor-type", "Agent", "-name", "Bot", "-admin"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("runCredentialCLI(bootstrap -actor-type Agent -admin) code = %d, want 2", code)
	}
}

// TestRunCredentialCLI_Bootstrap_WithoutAdminFlag_NoRole proves the
// default (no -admin) path gives the new Actor no role at all.
func TestRunCredentialCLI_Bootstrap_WithoutAdminFlag_NoRole(t *testing.T) {
	db := useTestDB(t)

	var stdout, stderr bytes.Buffer
	code := runCredentialCLI(context.Background(), []string{"bootstrap", "-actor-type", "Human", "-name", "Alice", "-username", "alice"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runCredentialCLI(bootstrap) code = %d, want 0, stderr=%q", code, stderr.String())
	}
	actorID := lineValue(t, stdout.String(), "Actor ID:")

	if g, err := postgres.NewAccessRepo(db).Grants(context.Background(), actorID); err != nil || g.IsAdmin() || g.Can("contribution:Read", "p") {
		t.Fatalf("bootstrapped actor has grants without -admin: %+v, %v", g, err)
	}
}

func TestRunCredentialCLI_Bootstrap_InvalidActorType_UsageError(t *testing.T) {
	useTestDB(t)

	var stdout, stderr bytes.Buffer
	code := runCredentialCLI(context.Background(), []string{"bootstrap", "-actor-type", "Robot", "-name", "x"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("runCredentialCLI(bootstrap, bad actor-type) code = %d, want 2", code)
	}
}

func TestRunCredentialCLI_Issue_ForExistingActor(t *testing.T) {
	db := useTestDB(t)

	a := actor.New(actor.TypeAgent)
	a.Name = "Test"
	if err := postgres.NewActorRepo(db).Create(context.Background(), a); err != nil {
		t.Fatalf("seed actor: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runCredentialCLI(context.Background(), []string{"issue", "-actor", a.ID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runCredentialCLI(issue) code = %d, want 0, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Credential:") {
		t.Fatalf("stdout = %q, want to contain 'Credential:'", stdout.String())
	}
}

func TestRunCredentialCLI_Issue_UnknownActor_RuntimeError(t *testing.T) {
	useTestDB(t)

	var stdout, stderr bytes.Buffer
	code := runCredentialCLI(context.Background(), []string{"issue", "-actor", "does-not-exist"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("runCredentialCLI(issue, unknown actor) code = %d, want 1", code)
	}
}

func TestRunCredentialCLI_Revoke_ThenVerifyFails(t *testing.T) {
	db := useTestDB(t)

	var bootstrapOut, stderr bytes.Buffer
	if code := runCredentialCLI(context.Background(), []string{"bootstrap", "-actor-type", "Agent", "-name", "Bot"}, &bootstrapOut, &stderr); code != 0 {
		t.Fatalf("bootstrap setup failed: code=%d stderr=%q", code, stderr.String())
	}

	var credentialID string
	for _, line := range strings.Split(bootstrapOut.String(), "\n") {
		if strings.HasPrefix(line, "Credential: ") {
			token := strings.TrimPrefix(line, "Credential: ")
			credentialID = strings.SplitN(token, ".", 2)[0]
		}
	}
	if credentialID == "" {
		t.Fatalf("could not parse credential_id out of bootstrap stdout: %q", bootstrapOut.String())
	}

	var revokeOut bytes.Buffer
	code := runCredentialCLI(context.Background(), []string{"revoke", "-credential", credentialID}, &revokeOut, &stderr)
	if code != 0 {
		t.Fatalf("runCredentialCLI(revoke) code = %d, want 0, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(revokeOut.String(), "revoked") {
		t.Fatalf("revoke stdout = %q, want to mention 'revoked'", revokeOut.String())
	}

	credentials := postgres.NewCredentialRepo(db)
	got, err := credentials.Get(context.Background(), credentialID)
	if err != nil {
		t.Fatalf("Get() after revoke error = %v", err)
	}
	if got.RevokedAt == nil {
		t.Fatal("credential.RevokedAt = nil after revoke, want set")
	}
}

func TestRunCredentialCLI_UnknownSubcommand_UsageError(t *testing.T) {
	useTestDB(t)

	var stdout, stderr bytes.Buffer
	code := runCredentialCLI(context.Background(), []string{"bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("runCredentialCLI(bogus) code = %d, want 2", code)
	}
}

// --- Additional tests requested this round: actor-binding and rotation/no-lockout proof. ---

// TestRunCredentialCLI_Bootstrap_TokenResolvesToBootstrappedActor proves a
// bootstrap-issued token actually resolves, via authentication.Verify, to
// the Actor ID Bootstrap printed — not just that *some* token was printed.
func TestRunCredentialCLI_Bootstrap_TokenResolvesToBootstrappedActor(t *testing.T) {
	db := useTestDB(t)

	var stdout, stderr bytes.Buffer
	if code := runCredentialCLI(context.Background(), []string{"bootstrap", "-actor-type", "Agent", "-name", "Bot"}, &stdout, &stderr); code != 0 {
		t.Fatalf("bootstrap failed: code=%d stderr=%q", code, stderr.String())
	}

	actorID, token := parseActorIDAndToken(t, stdout.String())

	resolvedActorID, err := authentication.Verify(context.Background(), postgres.NewActorRepo(db), postgres.NewCredentialRepo(db), token)
	if err != nil {
		t.Fatalf("authentication.Verify() error = %v", err)
	}
	if resolvedActorID != actorID {
		t.Fatalf("Verify() resolved actor = %q, want %q (the bootstrapped actor)", resolvedActorID, actorID)
	}
}

// TestRunCredentialCLI_Issue_TokenResolvesToExistingActor proves a token
// from the "issue" subcommand (for a pre-seeded existing Actor) resolves,
// via authentication.Verify, to that exact Actor's ID.
func TestRunCredentialCLI_Issue_TokenResolvesToExistingActor(t *testing.T) {
	db := useTestDB(t)

	a := actor.New(actor.TypeAgent)
	a.Name = "Test"
	if err := postgres.NewActorRepo(db).Create(context.Background(), a); err != nil {
		t.Fatalf("seed actor: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := runCredentialCLI(context.Background(), []string{"issue", "-actor", a.ID}, &stdout, &stderr); code != 0 {
		t.Fatalf("issue failed: code=%d stderr=%q", code, stderr.String())
	}
	token := parseToken(t, stdout.String())

	resolvedActorID, err := authentication.Verify(context.Background(), postgres.NewActorRepo(db), postgres.NewCredentialRepo(db), token)
	if err != nil {
		t.Fatalf("authentication.Verify() error = %v", err)
	}
	if resolvedActorID != a.ID {
		t.Fatalf("Verify() resolved actor = %q, want %q (the pre-seeded actor)", resolvedActorID, a.ID)
	}
}

// TestRunCredentialCLI_Issue_TwoCredentialsForSameActor_BothValid_DifferentIDs
// proves issuing a second credential for the same Actor produces two
// independently-valid tokens (both verify successfully, both resolve to
// the same Actor ID) with different credential_id prefixes.
func TestRunCredentialCLI_Issue_TwoCredentialsForSameActor_BothValid_DifferentIDs(t *testing.T) {
	db := useTestDB(t)

	a := actor.New(actor.TypeAgent)
	a.Name = "Test"
	if err := postgres.NewActorRepo(db).Create(context.Background(), a); err != nil {
		t.Fatalf("seed actor: %v", err)
	}

	var out1, out2, stderr bytes.Buffer
	if code := runCredentialCLI(context.Background(), []string{"issue", "-actor", a.ID}, &out1, &stderr); code != 0 {
		t.Fatalf("first issue failed: code=%d stderr=%q", code, stderr.String())
	}
	if code := runCredentialCLI(context.Background(), []string{"issue", "-actor", a.ID}, &out2, &stderr); code != 0 {
		t.Fatalf("second issue failed: code=%d stderr=%q", code, stderr.String())
	}

	token1 := parseToken(t, out1.String())
	token2 := parseToken(t, out2.String())

	credentialID1 := strings.SplitN(token1, ".", 2)[0]
	credentialID2 := strings.SplitN(token2, ".", 2)[0]
	if credentialID1 == credentialID2 {
		t.Fatalf("expected two distinct credential IDs, got the same: %q", credentialID1)
	}

	actors := postgres.NewActorRepo(db)
	credentials := postgres.NewCredentialRepo(db)

	resolved1, err := authentication.Verify(context.Background(), actors, credentials, token1)
	if err != nil {
		t.Fatalf("Verify(token1) error = %v", err)
	}
	resolved2, err := authentication.Verify(context.Background(), actors, credentials, token2)
	if err != nil {
		t.Fatalf("Verify(token2) error = %v", err)
	}
	if resolved1 != a.ID || resolved2 != a.ID {
		t.Fatalf("Verify(token1) = %q, Verify(token2) = %q, want both = %q", resolved1, resolved2, a.ID)
	}
}

// TestRunCredentialCLI_IssueNew_DoesNotInvalidateExisting proves the
// no-lockout rotation property: issuing a new credential for an Actor does
// not revoke or otherwise break an existing credential for that same
// Actor — the old credential is proven still valid via authentication.Verify
// AFTER the new one is issued.
func TestRunCredentialCLI_IssueNew_DoesNotInvalidateExisting(t *testing.T) {
	db := useTestDB(t)

	var bootstrapOut, stderr bytes.Buffer
	if code := runCredentialCLI(context.Background(), []string{"bootstrap", "-actor-type", "Agent", "-name", "Bot"}, &bootstrapOut, &stderr); code != 0 {
		t.Fatalf("bootstrap failed: code=%d stderr=%q", code, stderr.String())
	}
	actorID, tokenA := parseActorIDAndToken(t, bootstrapOut.String())

	var issueOut bytes.Buffer
	if code := runCredentialCLI(context.Background(), []string{"issue", "-actor", actorID}, &issueOut, &stderr); code != 0 {
		t.Fatalf("issue (credential B) failed: code=%d stderr=%q", code, stderr.String())
	}
	tokenB := parseToken(t, issueOut.String())

	actors := postgres.NewActorRepo(db)
	credentials := postgres.NewCredentialRepo(db)

	// Credential A (issued first, via bootstrap) must still verify after
	// credential B was issued — issuing B must not have touched A.
	resolvedA, err := authentication.Verify(context.Background(), actors, credentials, tokenA)
	if err != nil {
		t.Fatalf("Verify(tokenA) after issuing tokenB error = %v, want no-lockout (A still valid)", err)
	}
	if resolvedA != actorID {
		t.Fatalf("Verify(tokenA) resolved = %q, want %q", resolvedA, actorID)
	}

	resolvedB, err := authentication.Verify(context.Background(), actors, credentials, tokenB)
	if err != nil {
		t.Fatalf("Verify(tokenB) error = %v", err)
	}
	if resolvedB != actorID {
		t.Fatalf("Verify(tokenB) resolved = %q, want %q", resolvedB, actorID)
	}
}

// parseActorIDAndToken extracts "Actor ID: <id>" and "Credential: <token>"
// lines out of runCredentialBootstrap's stdout.
func parseActorIDAndToken(t *testing.T, stdout string) (actorID, token string) {
	t.Helper()
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "Actor ID: ") {
			actorID = strings.TrimPrefix(line, "Actor ID: ")
		}
		if strings.HasPrefix(line, "Credential: ") {
			token = strings.TrimPrefix(line, "Credential: ")
		}
	}
	if actorID == "" || token == "" {
		t.Fatalf("could not parse Actor ID and Credential out of stdout: %q", stdout)
	}
	return actorID, token
}

// parseToken extracts "Credential: <token>" out of runCredentialIssue's
// stdout.
func parseToken(t *testing.T, stdout string) string {
	t.Helper()
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "Credential: ") {
			return strings.TrimPrefix(line, "Credential: ")
		}
	}
	t.Fatalf("could not parse Credential out of stdout: %q", stdout)
	return ""
}

func lineValue(t *testing.T, out, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	t.Fatalf("no %q line in output:\n%s", prefix, out)
	return ""
}

func verifyPasswordAt(t *testing.T, db *pgxpool.Pool, username, password string) error {
	t.Helper()
	_, err := authentication.VerifyPassword(context.Background(), postgres.NewActorRepo(db), postgres.NewLoginRepo(db), username, password)
	return err
}

func TestRunCredentialCLI_Bootstrap_Username_PrintsWorkingPassword(t *testing.T) {
	db := useTestDB(t)

	var stdout, stderr bytes.Buffer
	code := runCredentialCLI(context.Background(), []string{"bootstrap", "-actor-type", "Human", "-name", "Admin", "-username", "Admin", "-admin"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if got := lineValue(t, stdout.String(), "Username:"); got != "admin" {
		t.Fatalf("Username = %q, want normalized %q", got, "admin")
	}
	pw := lineValue(t, stdout.String(), "Password:")
	if err := verifyPasswordAt(t, db, "admin", pw); err != nil {
		t.Fatalf("printed password does not verify: %v", err)
	}
}

func TestRunCredentialCLI_Bootstrap_UsernameRules(t *testing.T) {
	db := useTestDB(t)

	run := func(args ...string) int {
		var stdout, stderr bytes.Buffer
		return runCredentialCLI(context.Background(), args, &stdout, &stderr)
	}
	if code := run("bootstrap", "-actor-type", "Agent", "-name", "Bot", "-username", "bot"); code != 2 {
		t.Errorf("agent with -username: code = %d, want 2", code)
	}
	if code := run("bootstrap", "-actor-type", "Human", "-name", "X", "-username", "a b"); code != 2 {
		t.Errorf("invalid username: code = %d, want 2", code)
	}
	if code := run("bootstrap", "-actor-type", "Human", "-name", "First", "-username", "taken"); code != 0 {
		t.Fatalf("first bootstrap: code = %d", code)
	}
	if code := run("bootstrap", "-actor-type", "Human", "-name", "Second", "-username", "taken"); code != 2 {
		t.Errorf("taken username: code = %d, want 2", code)
	}

	all, _ := postgres.NewActorRepo(db).List(context.Background())
	if len(all) != 1 {
		t.Fatalf("actors = %d, want 1 (rejected bootstraps must not create actors)", len(all))
	}
}

func TestRunCredentialCLI_Password_ResetsAndAssigns(t *testing.T) {
	db := useTestDB(t)

	var out bytes.Buffer
	if code := runCredentialCLI(context.Background(), []string{"bootstrap", "-actor-type", "Human", "-name", "Legacy", "-username", "old-name"}, &out, &bytes.Buffer{}); code != 0 {
		t.Fatalf("bootstrap code = %d", code)
	}
	actorID := lineValue(t, out.String(), "Actor ID:")

	var stdout, stderr bytes.Buffer
	if code := runCredentialCLI(context.Background(), []string{"password", "-username", "legacy"}, &stdout, &stderr); code != 1 {
		t.Fatalf("unknown username without -actor: code = %d, want 1", code)
	}

	stdout.Reset()
	if code := runCredentialCLI(context.Background(), []string{"password", "-actor", actorID, "-username", "legacy"}, &stdout, &stderr); code != 0 {
		t.Fatalf("assign: code = %d, stderr = %q", code, stderr.String())
	}
	first := lineValue(t, stdout.String(), "Password:")

	stdout.Reset()
	if code := runCredentialCLI(context.Background(), []string{"password", "-username", "legacy"}, &stdout, &stderr); code != 0 {
		t.Fatalf("reset: code = %d, stderr = %q", code, stderr.String())
	}
	second := lineValue(t, stdout.String(), "Password:")
	if err := verifyPasswordAt(t, db, "legacy", first); err == nil {
		t.Fatal("old password still valid after reset")
	}
	if err := verifyPasswordAt(t, db, "legacy", second); err != nil {
		t.Fatalf("new password rejected: %v", err)
	}
}
