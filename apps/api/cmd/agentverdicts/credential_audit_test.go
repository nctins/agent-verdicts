package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"agent-verdicts/internal/audit"
	"agent-verdicts/internal/platform/postgres"
)

func cliOK(t *testing.T, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := runCredentialCLI(context.Background(), args, &stdout, &stderr); code != 0 {
		t.Fatalf("%v: code %d, stderr %q", args, code, stderr.String())
	}
	return stdout.String()
}

// Every credential subcommand logs one event with a null actor and never the secret.
func TestCredentialCLI_LogsOperatorEvents(t *testing.T) {
	db := useTestDB(t)
	ctx := context.Background()
	events := func() []audit.Event {
		p, err := postgres.NewAuditRepo(db).List(ctx, audit.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		return p.Items
	}

	out := cliOK(t, "bootstrap", "-actor-type", "Agent", "-name", "Bot")
	botID, bootToken := parseActorIDAndToken(t, out)
	issued := parseToken(t, cliOK(t, "issue", "-actor", botID, "-label", "ci", "-expires-in-days", "30"))
	credID, _, _ := strings.Cut(issued, ".")
	cliOK(t, "revoke", "-credential", credID)
	cliOK(t, "revoke", "-credential", credID) // already revoked: no second event
	human := cliOK(t, "bootstrap", "-actor-type", "Human", "-name", "Root", "-username", "root", "-admin")
	rootPW := lineValue(t, human, "Password:")
	pw := lineValue(t, cliOK(t, "password", "-username", "root"), "Password:")

	got := map[string]int{}
	var raw []byte
	for _, e := range events() {
		got[e.Action]++
		if e.ActorID != "" {
			t.Errorf("%s has actor %q, want the CLI (none)", e.Action, e.ActorID)
		}
		b, _ := json.Marshal(e)
		raw = append(raw, b...)
	}
	want := map[string]int{"actor.create": 2, "credential.issue": 1, "credential.revoke": 1, "assignment.create": 1, "actor.password_reset": 1}
	for a, n := range want {
		if got[a] != n {
			t.Errorf("%s events = %d, want %d", a, got[a], n)
		}
	}
	if len(got) != len(want) {
		t.Errorf("events = %v", got)
	}
	for _, secret := range []string{bootToken, strings.SplitN(bootToken, ".", 2)[1], issued, strings.SplitN(issued, ".", 2)[1], rootPW, pw} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("a secret reached the audit log: %q", secret)
		}
	}
}

func TestCredentialCLI_IssueExpiry(t *testing.T) {
	db := useTestDB(t)
	ctx := context.Background()
	botID, _ := parseActorIDAndToken(t, cliOK(t, "bootstrap", "-actor-type", "Agent", "-name", "Bot"))
	repo := postgres.NewCredentialRepo(db)
	expiry := func(token string) *time.Time {
		id, _, _ := strings.Cut(token, ".")
		c, err := repo.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return c.ExpiresAt
	}

	if exp := expiry(parseToken(t, cliOK(t, "issue", "-actor", botID))); exp == nil || exp.Sub(time.Now().AddDate(0, 0, 90)).Abs() > time.Minute {
		t.Fatalf("default expiry = %v, want 90 days", exp)
	}
	if exp := expiry(parseToken(t, cliOK(t, "issue", "-actor", botID, "-expires-in-days", "0"))); exp != nil {
		t.Fatalf("-expires-in-days 0 = %v, want never", exp)
	}
	creds, _ := repo.ListByActor(ctx, botID)
	if creds[0].ExpiresAt != nil { // bootstrap
		t.Fatalf("bootstrap token expires at %v, want never", creds[0].ExpiresAt)
	}
	var stdout, stderr bytes.Buffer
	if code := runCredentialCLI(ctx, []string{"issue", "-actor", botID, "-expires-in-days", "7"}, &stdout, &stderr); code != 2 {
		t.Fatalf("bad expiry: code %d", code)
	}
	if code := runCredentialCLI(ctx, []string{"issue", "-actor", botID, "-label", strings.Repeat("x", 65)}, &stdout, &stderr); code != 1 {
		t.Fatalf("bad label: code %d", code)
	}
}
