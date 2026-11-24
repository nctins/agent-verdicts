package session_test

import (
	"context"
	"testing"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/adapter/httpapi/session"
)

func TestActorID_NoValue_ReturnsFalse(t *testing.T) {
	_, ok := session.ActorID(context.Background())
	if ok {
		t.Fatal("ActorID() ok = true on a bare context, want false")
	}
}

func TestGrants_NoValue_GrantsNothing(t *testing.T) {
	if g := session.Grants(context.Background()); g.IsAdmin() || g.Can(access.ContributionRead, "p") {
		t.Fatal("bare context grants something")
	}
}

func TestWithActor_RoundTrip(t *testing.T) {
	var g access.Grants
	g.Add("p1", access.ContributionRead)
	ctx := session.WithActor(context.Background(), "actor-1", g)

	id, ok := session.ActorID(ctx)
	if !ok || id != "actor-1" {
		t.Fatalf("ActorID() = %q, %v", id, ok)
	}
	if got := session.Grants(ctx); !got.Can(access.ContributionRead, "p1") || got.Can(access.ContributionRead, "p2") {
		t.Fatalf("Grants() = %+v", got)
	}
}
