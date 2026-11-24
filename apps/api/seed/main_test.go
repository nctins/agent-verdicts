package main

import (
	"context"
	"io"
	"testing"

	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
)

func TestSeedTwiceDoesNotDuplicate(t *testing.T) {
	pool := pgtest.NewMigrated(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := seed(ctx, pool, demoJSON, io.Discard); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	var contributions, actors, signals, logins int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM contribution), (SELECT count(*) FROM actor),
		(SELECT count(*) FROM signal), (SELECT count(*) FROM login)`).Scan(&contributions, &actors, &signals, &logins); err != nil {
		t.Fatal(err)
	}
	d, err := parse(demoJSON)
	if err != nil {
		t.Fatal(err)
	}
	if contributions != len(d.Contributions) || actors != len(d.Actors) || signals != len(d.Signals) || logins != 1 {
		t.Errorf("got contributions=%d actors=%d signals=%d logins=%d; want %d %d %d 1",
			contributions, actors, signals, logins, len(d.Contributions), len(d.Actors), len(d.Signals))
	}
	if admins, err := postgres.NewAccessRepo(pool).AdminHumans(ctx); err != nil || len(admins) != 1 {
		t.Errorf("seeded admins = %v, %v; want one admin role holder", admins, err)
	}
	counts, err := postgres.NewContributionRepo(pool).CountByProject(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts["payments-api"].OpenQuestions != 1 { // q6 has no answers; q1 is answered
		t.Errorf("payments-api open questions = %d, want 1", counts["payments-api"].OpenQuestions)
	}
}
