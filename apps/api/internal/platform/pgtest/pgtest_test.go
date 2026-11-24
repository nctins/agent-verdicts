package pgtest_test

import (
	"context"
	"testing"

	"agent-verdicts/internal/platform/pgtest"
)

func TestNew_IsolatedSchema(t *testing.T) {
	a := pgtest.New(t)
	b := pgtest.New(t)
	ctx := context.Background()
	if _, err := a.Exec(ctx, `CREATE TABLE x (id int)`); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Exec(ctx, `CREATE TABLE x (id int)`); err != nil {
		t.Fatalf("second schema must not see first schema's table: %v", err)
	}
	var one int
	if err := a.QueryRow(ctx, `SELECT 1 FROM pg_extension WHERE extname = 'vector'`).Scan(&one); err != nil {
		t.Fatalf("public extensions must be visible: %v", err)
	}
}
