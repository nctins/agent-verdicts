package project_test

import (
	"testing"

	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
)

func newTestRepo(t *testing.T) *postgres.ProjectRepo {
	t.Helper()
	return postgres.NewProjectRepo(pgtest.NewMigrated(t))
}
