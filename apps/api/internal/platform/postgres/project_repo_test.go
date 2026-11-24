package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/project"
)

func newProjectRepo(t *testing.T) *postgres.ProjectRepo {
	t.Helper()
	return postgres.NewProjectRepo(pgtest.NewMigrated(t))
}

func TestProjectRepo_CreateAndGet_Roundtrip(t *testing.T) {
	repo := newProjectRepo(t)
	ctx := context.Background()

	want := project.New()

	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.ID != want.ID {
		t.Fatalf("Get().ID = %q, want %q", got.ID, want.ID)
	}
	if got.Name != want.Name {
		t.Fatalf("Get().Name = %q, want %q", got.Name, want.Name)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("Get().CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
}

func TestProjectRepo_Get_NotFound(t *testing.T) {
	repo := newProjectRepo(t)

	_, err := repo.Get(context.Background(), "does-not-exist")
	if !errors.Is(err, project.ErrNotFound) {
		t.Fatalf("Get() error = %v, want project.ErrNotFound", err)
	}
}

func TestProjectRepo_Create_DuplicateID(t *testing.T) {
	repo := newProjectRepo(t)
	ctx := context.Background()

	p := project.New()
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	if err := repo.Create(ctx, p); err == nil {
		t.Fatal("second Create() with duplicate ID: want error, got nil")
	}
}

func TestProjectRepo_List_NewestFirstThenID(t *testing.T) {
	repo := newProjectRepo(t)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range []project.Project{
		{ID: "old", Name: "old", CreatedAt: base},
		{ID: "new-b", Name: "new-b", CreatedAt: base.Add(time.Hour)},
		{ID: "new-a", Name: "new-a", CreatedAt: base.Add(time.Hour)},
	} {
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("Create(%s) error = %v", p.ID, err)
		}
	}

	got, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	var ids []string
	for _, p := range got {
		ids = append(ids, p.ID)
	}
	if want := "new-a,new-b,old"; strings.Join(ids, ",") != want {
		t.Fatalf("List() order = %q, want %q", strings.Join(ids, ","), want)
	}
}

func TestProjectRepo_List_Empty(t *testing.T) {
	repo := newProjectRepo(t)
	got, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("List() on empty table = %#v, want empty non-nil slice", got)
	}
}
