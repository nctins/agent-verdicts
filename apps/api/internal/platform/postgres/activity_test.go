package postgres_test

import (
	"context"
	"testing"
	"time"

	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/platform/postgres"
)

// Spec 2026-11-22-admin-ui-redesign A1, A2.
func TestCredentialRepo_Activity(t *testing.T) {
	db, creds := newCredentialRepos(t)
	logins := postgres.NewLoginRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	at := func(h int) *time.Time { x := now.Add(time.Duration(h) * time.Hour); return &x }

	quiet := seedActor(t, db) // nothing at all
	agent := seedActor(t, db)
	human := seedActor(t, db)
	// Create stores neither revocation nor use: those go through Revoke and Touch, as in production.
	add := func(actorID string, c authentication.Credential) {
		c.ID, c.ActorID, c.SecretHash, c.CreatedAt = id.New(), actorID, id.New(), now.Add(-100*time.Hour)
		if err := creds.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
		if c.RevokedAt != nil {
			if err := creds.Revoke(ctx, c.ID, *c.RevokedAt); err != nil {
				t.Fatal(err)
			}
		}
		if c.LastUsedAt != nil {
			if err := creds.Touch(ctx, c.ID, *c.LastUsedAt); err != nil {
				t.Fatal(err)
			}
		}
	}
	add(agent.ID, authentication.Credential{ExpiresAt: at(48)})                     // live: next expiry
	add(agent.ID, authentication.Credential{ExpiresAt: at(24), RevokedAt: at(-1)})  // revoked: not an expiry
	add(agent.ID, authentication.Credential{ExpiresAt: at(-2), LastUsedAt: at(-3)}) // expired, but its use counts
	add(agent.ID, authentication.Credential{})                                      // never expires
	if err := logins.Upsert(ctx, authentication.Login{ActorID: human.ID, Username: "h" + human.ID[:8], PasswordHash: "x", ChangedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := logins.TouchLogin(ctx, human.ID, now.Add(-5*time.Hour)); err != nil {
		t.Fatal(err)
	}
	add(human.ID, authentication.Credential{LastUsedAt: at(-30)}) // older than the login

	got, err := creds.Activity(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	eq := func(p *time.Time, want *time.Time) bool {
		return (p == nil) == (want == nil) && (p == nil || p.Equal(*want))
	}
	if a := got[agent.ID]; !eq(a.LastActive, at(-3)) || !eq(a.NextExpiry, at(48)) {
		t.Errorf("agent = %+v", a)
	}
	if h := got[human.ID]; !eq(h.LastActive, at(-5)) || h.NextExpiry != nil {
		t.Errorf("human = %+v", h)
	}
	if q, ok := got[quiet.ID]; ok && (q.LastActive != nil || q.NextExpiry != nil) {
		t.Errorf("quiet = %+v", q)
	}
}
