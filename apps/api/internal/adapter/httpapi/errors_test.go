package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/adapter/authflow"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/project"
	"agent-verdicts/internal/setting"
)

// Every row of spec §8's error table, plus the domain sentinels that feed it.
func TestErrorFrom_SpecTable(t *testing.T) {
	cases := []struct {
		err    error
		code   string
		status int
	}{
		{contribution.ErrInvalidArgument, "invalid_argument", 400},
		{fmt.Errorf("wrap: %w", contribution.ErrBadCursor), "invalid_argument", 400},
		{setting.ErrInvalidArgument, "invalid_argument", 400},
		{actor.ErrNameRequired, "invalid_argument", 400},
		{actor.ErrNameTooLong, "invalid_argument", 400},
		{authentication.ErrInvalidUsername, "invalid_argument", 400},
		{authentication.ErrPasswordTooShort, "invalid_argument", 400},
		{authentication.ErrPasswordTooLong, "invalid_argument", 400},
		{authentication.ErrNotHuman, "invalid_argument", 400},
		{authentication.ErrUnauthenticated, "unauthenticated", 401},
		{errForbidden, "forbidden", 403},
		{contribution.ErrNotFound, "not_found", 404},
		{project.ErrNotFound, "not_found", 404},
		{actor.ErrNotFound, "not_found", 404},
		{authentication.ErrNotFound, "not_found", 404},
		{authentication.ErrActorNotFound, "not_found", 404},
		{contribution.ErrActorNotFound, "not_found", 404},
		{setting.ErrNotFound, "not_found", 404},
		{project.ErrAlreadyExists, "conflict", 409},
		{authentication.ErrUsernameTaken, "conflict", 409},
		{authflow.ErrLocked, "too_many_requests", 429},
		{setting.ErrUpstream, "upstream_failed", 502},
		{errors.New("pq: connection refused"), "internal", 500},
	}
	for _, c := range cases {
		code, status := errorFrom(c.err)
		if code != c.code || status != c.status {
			t.Errorf("errorFrom(%v) = %s %d, want %s %d", c.err, code, status, c.code, c.status)
		}
	}
}

func TestFail_InternalHidesDetail(t *testing.T) {
	w := httptest.NewRecorder()
	fail(w, httptest.NewRequest("GET", "/x", nil), errors.New("pq: secret driver text"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "internal" || body["message"] != "internal error" {
		t.Fatalf("body = %v", body)
	}
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatal("internal detail leaked to the client")
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
}
