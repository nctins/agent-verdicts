package actor_test

import (
	"errors"
	"strings"
	"testing"

	"agent-verdicts/internal/actor"
)

func TestNew_PanicsOnInvalidType(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("New() with invalid type: want panic, got none")
		}
	}()
	actor.New(actor.Type("Robot"))
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr error
	}{
		{"Alice", "Alice", nil},
		{"  Bob Builder  ", "Bob Builder", nil},
		{"Nguyễn Văn A", "Nguyễn Văn A", nil},
		{"", "", actor.ErrNameRequired},
		{"   ", "", actor.ErrNameRequired},
		{strings.Repeat("é", actor.MaxNameLen), strings.Repeat("é", actor.MaxNameLen), nil},
		{strings.Repeat("x", actor.MaxNameLen+1), "", actor.ErrNameTooLong},
	}
	for _, tt := range tests {
		got, err := actor.NormalizeName(tt.in)
		if !errors.Is(err, tt.wantErr) {
			t.Errorf("NormalizeName(%q) error = %v, want %v", tt.in, err, tt.wantErr)
		}
		if got != tt.want {
			t.Errorf("NormalizeName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNew_ValidTypesDoNotPanic(t *testing.T) {
	for _, typ := range []actor.Type{actor.TypeHuman, actor.TypeAgent} {
		a := actor.New(typ)
		if a.Type != typ {
			t.Fatalf("New(%q).Type = %q, want %q", typ, a.Type, typ)
		}
		if a.ID == "" {
			t.Fatal("New() returned Actor with empty ID")
		}
	}
}
