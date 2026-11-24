package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
)

func newActorRepo(t *testing.T) *postgres.ActorRepo {
	t.Helper()
	return postgres.NewActorRepo(pgtest.NewMigrated(t))
}

func TestActorRepo_CreateAndGet_Roundtrip(t *testing.T) {
	repo := newActorRepo(t)
	ctx := context.Background()

	want := actor.New(actor.TypeHuman)
	want.Name = "Test"

	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.ID != want.ID || got.Type != want.Type {
		t.Fatalf("Get() = %+v, want %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("Get().CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
}

func TestActorRepo_CreateAndGet_NameRoundtrip(t *testing.T) {
	repo := newActorRepo(t)
	ctx := context.Background()

	want := actor.New(actor.TypeAgent)
	want.Name = "Reviewer Bot"
	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Name != "Reviewer Bot" {
		t.Fatalf("Get().Name = %q, want %q", got.Name, "Reviewer Bot")
	}

}

func TestActorRepo_List_OrderedByCreatedAtThenID(t *testing.T) {
	repo := newActorRepo(t)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	second := actor.New(actor.TypeAgent)
	second.Name = "second"
	second.CreatedAt = base.Add(time.Second)
	firstB := actor.New(actor.TypeHuman)
	firstB.Name = "first-b"
	firstB.CreatedAt = base
	firstB.ID = "00000000-0000-4000-8000-00000000000b"
	firstA := actor.New(actor.TypeHuman)
	firstA.Name = "first-a"
	firstA.CreatedAt = base
	firstA.ID = "00000000-0000-4000-8000-00000000000a"

	for _, a := range []actor.Actor{second, firstB, firstA} {
		if err := repo.Create(ctx, a); err != nil {
			t.Fatalf("Create(%s) error = %v", a.Name, err)
		}
	}

	got, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	var names []string
	for _, a := range got {
		names = append(names, a.Name)
	}
	want := "first-a,first-b,second"
	if strings.Join(names, ",") != want {
		t.Fatalf("List() order = %q, want %q", strings.Join(names, ","), want)
	}
}

func TestActorRepo_List_EmptyIsEmptySlice(t *testing.T) {
	repo := newActorRepo(t)
	got, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("List() on empty table = %#v, want empty non-nil slice", got)
	}
}

func TestActorRepo_Get_NotFound(t *testing.T) {
	repo := newActorRepo(t)

	_, err := repo.Get(context.Background(), "does-not-exist")
	if !errors.Is(err, actor.ErrNotFound) {
		t.Fatalf("Get() error = %v, want actor.ErrNotFound", err)
	}
}

func TestActorRepo_Create_DuplicateID(t *testing.T) {
	repo := newActorRepo(t)
	ctx := context.Background()

	a := actor.New(actor.TypeAgent)
	a.Name = "Test"
	if err := repo.Create(ctx, a); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	if err := repo.Create(ctx, a); err == nil {
		t.Fatal("second Create() with duplicate ID: want error, got nil")
	}
}

func TestActorRepo_Create_InvalidType(t *testing.T) {
	repo := newActorRepo(t)

	invalid := actor.Actor{
		ID:        id.New(),
		Type:      actor.Type("Robot"),
		CreatedAt: time.Now(),
	}

	if err := repo.Create(context.Background(), invalid); err == nil {
		t.Fatal("Create() with invalid Type: want error, got nil")
	}
}

func TestActorRepo_Update_NameDisabled(t *testing.T) {
	repo := newActorRepo(t)
	ctx := context.Background()

	a := actor.New(actor.TypeHuman)
	a.Name = "Before"
	if err := repo.Create(ctx, a); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	disabledAt := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	a.Name = "After"
	a.DisabledAt = &disabledAt
	if err := repo.Update(ctx, a); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	got, err := repo.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Name != "After" || got.DisabledAt == nil || !got.DisabledAt.Equal(disabledAt) || !got.Disabled() {
		t.Fatalf("after Update: %+v", got)
	}

	got.DisabledAt = nil
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("re-enable Update() error = %v", err)
	}
	again, _ := repo.Get(ctx, a.ID)
	if again.Disabled() {
		t.Fatal("actor still disabled after clearing DisabledAt")
	}
}

func TestActorRepo_Update_NotFound(t *testing.T) {
	repo := newActorRepo(t)
	err := repo.Update(context.Background(), actor.Actor{ID: "missing", Type: actor.TypeHuman})
	if !errors.Is(err, actor.ErrNotFound) {
		t.Fatalf("Update(missing) error = %v, want ErrNotFound", err)
	}
}

// Field-specific writes must not undo each other (two admins editing at once).
func TestActorRepo_ProfileAndDisabledDoNotOverwriteEachOther(t *testing.T) {
	repo := newActorRepo(t)
	ctx := context.Background()
	a := actor.New(actor.TypeHuman)
	a.Name = "Bob"
	repo.Create(ctx, a)

	when := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	if err := repo.SetDisabled(ctx, a.ID, &when); err != nil {
		t.Fatalf("SetDisabled() error = %v", err)
	}
	if err := repo.UpdateProfile(ctx, a.ID, "Robert"); err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	got, _ := repo.Get(ctx, a.ID)
	if got.Name != "Robert" || !got.Disabled() {
		t.Fatalf("lost update: %+v", got)
	}
	if err := repo.SetDisabled(ctx, "missing", nil); !errors.Is(err, actor.ErrNotFound) {
		t.Fatalf("SetDisabled(missing) = %v", err)
	}
	if err := repo.UpdateProfile(ctx, "missing", "x"); !errors.Is(err, actor.ErrNotFound) {
		t.Fatalf("UpdateProfile(missing) = %v", err)
	}
}

// A non-UUID id names no actor: not-found, not a query error.
func TestActorRepo_NonUUIDID_IsNotFound(t *testing.T) {
	repo := newActorRepo(t)
	ctx := context.Background()
	if _, err := repo.Get(ctx, "nope"); !errors.Is(err, actor.ErrNotFound) {
		t.Fatalf("Get(nope) = %v, want ErrNotFound", err)
	}
	if err := repo.Update(ctx, actor.Actor{ID: "nope", Type: actor.TypeHuman}); !errors.Is(err, actor.ErrNotFound) {
		t.Fatalf("Update(nope) = %v, want ErrNotFound", err)
	}
	if err := repo.UpdateProfile(ctx, "nope", "x"); !errors.Is(err, actor.ErrNotFound) {
		t.Fatalf("UpdateProfile(nope) = %v, want ErrNotFound", err)
	}
	if err := repo.SetDisabled(ctx, "nope", nil); !errors.Is(err, actor.ErrNotFound) {
		t.Fatalf("SetDisabled(nope) = %v, want ErrNotFound", err)
	}
}

func TestActorRepo_TimesComeBackUTC(t *testing.T) {
	repo := newActorRepo(t)
	ctx := context.Background()
	a := actor.New(actor.TypeHuman)
	a.Name = "Test"
	a.CreatedAt = time.Date(2026, 1, 1, 7, 0, 0, 0, time.FixedZone("ICT", 7*3600))
	if err := repo.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.Get(ctx, a.ID)
	if got.CreatedAt.Location() != time.UTC || !got.CreatedAt.Equal(a.CreatedAt) {
		t.Fatalf("CreatedAt = %v, want %v in UTC", got.CreatedAt, a.CreatedAt)
	}
}
