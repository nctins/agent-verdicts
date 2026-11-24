package authentication_test

import (
	"context"
	"testing"

	"agent-verdicts/internal/authentication"
)

func TestActorID_RoundtripsThroughContext(t *testing.T) {
	ctx := authentication.WithActorID(context.Background(), "actor-123")
	got, ok := authentication.ActorID(ctx)
	if !ok || got != "actor-123" {
		t.Fatalf("ActorID() = %q, %v, want %q, true", got, ok, "actor-123")
	}
}

func TestActorID_AbsentFromPlainContext(t *testing.T) {
	_, ok := authentication.ActorID(context.Background())
	if ok {
		t.Fatal("ActorID() on a context nobody injected into: want ok=false, got true")
	}
}
