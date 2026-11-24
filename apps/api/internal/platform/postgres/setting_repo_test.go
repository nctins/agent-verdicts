package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/setting"
)

func TestSettingRepo_GetPut(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewSettingRepo(pgtest.NewMigrated(t))
	if _, err := repo.Get(ctx, "search"); !errors.Is(err, setting.ErrNotFound) {
		t.Fatalf("Get missing err = %v, want ErrNotFound", err)
	}
	for _, v := range []string{`{"a":1}`, `{"a":2}`} {
		if err := repo.Put(ctx, "search", []byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.Get(ctx, "search")
	if err != nil || strings.ReplaceAll(string(got), " ", "") != `{"a":2}` {
		t.Fatalf("Get = %s, %v", got, err)
	}
}

func TestSettingRepo_SearchServiceStoresNoPlaintextKey(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewMigrated(t)
	c, err := setting.LoadKey(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tester := func(_ context.Context, s setting.Search) (setting.Search, error) { s.Embedding.Dim = 8; return s, nil }
	s, err := setting.NewService(ctx, postgres.NewSettingRepo(pool), c, tester)
	if err != nil {
		t.Fatal(err)
	}
	key := "sk-plaintext-SECRET"
	if _, err := s.Update(ctx, setting.ProviderUpdate{URL: "http://e", Model: "m", APIKey: &key}, setting.ProviderUpdate{}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM setting WHERE key = 'search' AND strpos(value::text, $1) > 0`, key).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("setting.value contains the plaintext key")
	}
	var last4 string
	if err := pool.QueryRow(ctx, `SELECT value->'embedding'->>'api_key_last4' FROM setting WHERE key = 'search'`).Scan(&last4); err != nil || last4 != "CRET" {
		t.Fatalf("api_key_last4 = %q, %v", last4, err)
	}
	reloaded, err := setting.NewService(ctx, postgres.NewSettingRepo(pool), c, tester)
	if err != nil || reloaded.Current().Embedding.APIKey != key {
		t.Fatalf("reload: %+v, %v", reloaded.Current().Embedding, err)
	}
}
