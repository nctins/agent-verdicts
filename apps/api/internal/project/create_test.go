package project_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agent-verdicts/internal/project"
)

func TestCreate_NewID_Succeeds(t *testing.T) {
	repo := newTestRepo(t)

	p, err := project.Create(context.Background(), repo, "my-project", "", "")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if p.ID != "my-project" {
		t.Fatalf("Create().ID = %q, want %q", p.ID, "my-project")
	}
	if p.CreatedAt.IsZero() {
		t.Fatal("Create().CreatedAt is zero, want set")
	}

	got, err := repo.Get(context.Background(), "my-project")
	if err != nil {
		t.Fatalf("Get() after Create() error = %v", err)
	}
	if got.ID != "my-project" {
		t.Fatalf("Get().ID = %q, want %q", got.ID, "my-project")
	}
}

func TestCreate_DuplicateID_ReturnsErrAlreadyExists(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	if _, err := project.Create(ctx, repo, "dup-project", "", ""); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	_, err := project.Create(ctx, repo, "dup-project", "", "")
	if !errors.Is(err, project.ErrAlreadyExists) {
		t.Fatalf("second Create() error = %v, want project.ErrAlreadyExists", err)
	}
}

func TestCreate_EmptyID_ReturnsValidationError(t *testing.T) {
	repo := newTestRepo(t)

	_, err := project.Create(context.Background(), repo, "", "", "")
	if err == nil {
		t.Fatal("Create(\"\") error = nil, want non-nil validation error")
	}
	if errors.Is(err, project.ErrAlreadyExists) {
		t.Fatal("Create(\"\") must not return ErrAlreadyExists")
	}
}

func TestValidateID(t *testing.T) {
	for _, ok := range []string{"p", "my-project", "a_b", "0", "9x", strings.Repeat("a", 64)} {
		if err := project.ValidateID(ok); err != nil {
			t.Errorf("ValidateID(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "-a", "_a", "A", "My", "a b", "a.b", "..", ".", "a/b", "dự-án", "a\x00b", "demo\n", strings.Repeat("a", 65)} {
		if err := project.ValidateID(bad); !errors.Is(err, project.ErrInvalidID) {
			t.Errorf("ValidateID(%q) = %v, want ErrInvalidID", bad, err)
		}
	}
}

func TestNormalize(t *testing.T) {
	if got, err := project.NormalizeName("  Dự án 1 "); err != nil || got != "Dự án 1" {
		t.Errorf("NormalizeName = %q, %v", got, err)
	}
	for _, bad := range []string{"", "  ", "a\nb", "a\x00b", strings.Repeat("é", 101), "\xff"} {
		if _, err := project.NormalizeName(bad); !errors.Is(err, project.ErrInvalidField) {
			t.Errorf("NormalizeName(%q) = %v, want ErrInvalidField", bad, err)
		}
	}
	if got, err := project.NormalizeDescription("line1\nline2"); err != nil || got != "line1\nline2" {
		t.Errorf("NormalizeDescription = %q, %v", got, err)
	}
	if _, err := project.NormalizeDescription(""); err != nil {
		t.Errorf("empty description: %v", err)
	}
	for _, bad := range []string{"a\tb", "a\x00b", strings.Repeat("é", 501)} {
		if _, err := project.NormalizeDescription(bad); !errors.Is(err, project.ErrInvalidField) {
			t.Errorf("NormalizeDescription(%q) = %v, want ErrInvalidField", bad, err)
		}
	}
}

func TestCreate_DefaultsNameToID(t *testing.T) {
	repo := newTestRepo(t)
	p, err := project.Create(context.Background(), repo, "abc", " ", "")
	if err != nil || p.Name != "abc" || p.Description != "" {
		t.Fatalf("Create = %+v, %v", p, err)
	}
}

func TestUpdate_MinFavorable(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if _, err := project.Create(ctx, repo, "abc", "Name", ""); err != nil {
		t.Fatal(err)
	}
	three := 3
	if p, err := project.Update(ctx, repo, "abc", nil, nil, &three, nil); err != nil || p.MinFavorable != 3 {
		t.Fatalf("Update = %+v, %v", p, err)
	}
	if got, _ := repo.Get(ctx, "abc"); got.MinFavorable != 3 {
		t.Fatalf("stored = %+v", got)
	}
	for _, bad := range []int{0, 11, -1} {
		if _, err := project.Update(ctx, repo, "abc", nil, nil, &bad, nil); !errors.Is(err, project.ErrInvalidField) {
			t.Errorf("min_favorable %d: err = %v", bad, err)
		}
	}
}

func TestUpdate(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if _, err := project.Create(ctx, repo, "abc", "Name", "desc"); err != nil {
		t.Fatal(err)
	}
	d := "new desc"
	p, err := project.Update(ctx, repo, "abc", nil, &d, nil, nil)
	if err != nil || p.Name != "Name" || p.Description != "new desc" {
		t.Fatalf("Update = %+v, %v", p, err)
	}
	got, _ := repo.Get(ctx, "abc")
	if got.Name != "Name" || got.Description != "new desc" {
		t.Fatalf("stored = %+v", got)
	}
	empty := " "
	if _, err := project.Update(ctx, repo, "abc", &empty, nil, nil, nil); !errors.Is(err, project.ErrInvalidField) {
		t.Fatalf("empty name: %v", err)
	}
	if _, err := project.Update(ctx, repo, "nope", nil, &d, nil, nil); !errors.Is(err, project.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestCreate_InvalidID_NoWrite(t *testing.T) {
	repo := newTestRepo(t)
	if _, err := project.Create(context.Background(), repo, "a\x00b", "", ""); !errors.Is(err, project.ErrInvalidID) {
		t.Fatalf("err = %v, want ErrInvalidID", err)
	}
}
