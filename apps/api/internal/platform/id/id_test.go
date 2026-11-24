package id_test

import (
	"regexp"
	"testing"

	"agent-verdicts/internal/platform/id"
)

var uuidV4Pattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

func TestNew_MatchesUUIDv4Format(t *testing.T) {
	got := id.New()
	if !uuidV4Pattern.MatchString(got) {
		t.Fatalf("id.New() = %q, does not match UUID v4 format", got)
	}
}

func TestNew_ReturnsUniqueValues(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		got := id.New()
		if seen[got] {
			t.Fatalf("id.New() returned duplicate value %q on iteration %d", got, i)
		}
		seen[got] = true
	}
}
