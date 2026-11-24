package postgres_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/platform/pgtest"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/project"

	"github.com/jackc/pgx/v5/pgxpool"
)

type accessEnv struct {
	pool   *pgxpool.Pool
	repo   *postgres.AccessRepo
	actors *postgres.ActorRepo
}

func newAccessEnv(t *testing.T) accessEnv {
	t.Helper()
	pool := pgtest.NewMigrated(t)
	return accessEnv{pool: pool, repo: postgres.NewAccessRepo(pool), actors: postgres.NewActorRepo(pool)}
}

func (e accessEnv) actor(t *testing.T, typ actor.Type) string {
	t.Helper()
	a := actor.New(typ)
	a.Name = "a" + a.ID[:6]
	if err := e.actors.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a.ID
}

func (e accessEnv) project(t *testing.T, ids ...string) {
	t.Helper()
	repo := postgres.NewProjectRepo(e.pool)
	for _, p := range ids {
		if err := repo.Create(context.Background(), project.Project{ID: p, Name: p, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
}

func (e accessEnv) assign(t *testing.T, actorID, roleID string, projectID *string) access.Assignment {
	t.Helper()
	a := access.NewAssignment(actorID, roleID, projectID, actorID)
	if err := e.repo.CreateAssignment(context.Background(), a); err != nil {
		t.Fatalf("CreateAssignment: %v", err)
	}
	return a
}

func ptr(s string) *string { return &s }

func TestAccessRepo_ManagedSeeded(t *testing.T) {
	e := newAccessEnv(t)
	ctx := context.Background()
	roles, err := e.repo.ListRoles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var managed []string
	for _, r := range roles {
		if r.Managed {
			managed = append(managed, r.ID)
		}
	}
	slices.Sort(managed)
	if !slices.Equal(managed, []string{"admin", "contributor", "manager", "viewer"}) {
		t.Fatal(managed)
	}
	got, err := e.repo.RoleActions(ctx, "contributor")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []access.Action{access.ContributionRead, access.ContributionCreate, access.SignalCreate}) {
		t.Fatal(got)
	}
	if got, _ := e.repo.RoleActions(ctx, "admin"); !slices.Equal(got, []access.Action{access.All}) {
		t.Fatal(got)
	}
}

func TestAccessRepo_GrantsByScope(t *testing.T) {
	e := newAccessEnv(t)
	ctx := context.Background()
	e.project(t, "p1", "p2")
	a := e.actor(t, actor.TypeAgent)
	e.assign(t, a, "viewer", nil)
	e.assign(t, a, "contributor", ptr("p1"))
	g, err := e.repo.Grants(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if !g.Can(access.ContributionCreate, "p1") || g.Can(access.ContributionCreate, "p2") || !g.Can(access.ContributionRead, "p2") {
		t.Fatalf("grants = %+v", g)
	}
	if g, _ := e.repo.Grants(ctx, id.New()); g.Can(access.ContributionRead, "p1") {
		t.Fatal("unknown actor has grants")
	}
}

func TestAccessRepo_DisabledActorHasNoGrants(t *testing.T) {
	e := newAccessEnv(t)
	ctx := context.Background()
	h := e.actor(t, actor.TypeHuman)
	if err := access.GrantAdmin(ctx, e.repo, h); err != nil {
		t.Fatal(err)
	}
	if ids, _ := e.repo.AdminHumans(ctx); !slices.Equal(ids, []string{h}) {
		t.Fatal(ids)
	}
	now := time.Now()
	if err := e.actors.SetDisabled(ctx, h, &now); !errors.Is(err, access.ErrLastAdmin) {
		t.Fatalf("disabling the last admin: err = %v, want ErrLastAdmin", err)
	}
	if err := access.GrantAdmin(ctx, e.repo, e.actor(t, actor.TypeHuman)); err != nil { // a second admin
		t.Fatal(err)
	}
	if err := e.actors.SetDisabled(ctx, h, &now); err != nil {
		t.Fatal(err)
	}
	if g, _ := e.repo.Grants(ctx, h); g.IsAdmin() {
		t.Fatal("disabled actor keeps grants")
	}
	if ids, _ := e.repo.AdminHumans(ctx); slices.Contains(ids, h) {
		t.Fatal(ids)
	}
}

func TestAccessRepo_AdminHumansExcludesAgents(t *testing.T) {
	e := newAccessEnv(t)
	ctx := context.Background()
	ag := e.actor(t, actor.TypeAgent)
	if err := access.GrantAdmin(ctx, e.repo, ag); err != nil {
		t.Fatal(err)
	}
	if ids, _ := e.repo.AdminHumans(ctx); len(ids) != 0 {
		t.Fatal(ids)
	}
}

func TestAccessRepo_DuplicateAssignment(t *testing.T) {
	e := newAccessEnv(t)
	ctx := context.Background()
	e.project(t, "p1")
	a := e.actor(t, actor.TypeAgent)
	e.assign(t, a, "viewer", nil)
	e.assign(t, a, "viewer", ptr("p1"))
	for _, p := range []*string{nil, ptr("p1")} {
		if err := e.repo.CreateAssignment(ctx, access.NewAssignment(a, "viewer", p, a)); !errors.Is(err, access.ErrConflict) {
			t.Errorf("duplicate on %v: err = %v", p, err)
		}
	}
	if err := e.repo.CreateAssignment(ctx, access.NewAssignment(a, "viewer", ptr("nope"), a)); !errors.Is(err, access.ErrNotFound) {
		t.Errorf("unknown project: err = %v", err)
	}
	if err := e.repo.CreateAssignment(ctx, access.NewAssignment(a, "nope", nil, a)); !errors.Is(err, access.ErrNotFound) {
		t.Errorf("unknown role: err = %v", err)
	}
}

func TestAccessRepo_Assignments(t *testing.T) {
	e := newAccessEnv(t)
	ctx := context.Background()
	e.project(t, "p1", "p2")
	a, b := e.actor(t, actor.TypeAgent), e.actor(t, actor.TypeHuman)
	x := e.assign(t, a, "viewer", ptr("p1"))
	e.assign(t, a, "viewer", ptr("p2"))
	e.assign(t, b, "contributor", ptr("p1"))
	got, err := e.repo.ListAssignments(ctx, access.AssignmentFilter{ProjectID: "p1"})
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	if got, _ := e.repo.ListAssignments(ctx, access.AssignmentFilter{ActorID: a}); len(got) != 2 {
		t.Fatal(got)
	}
	g, err := e.repo.GetAssignment(ctx, x.ID)
	if err != nil || g.RoleID != "viewer" || g.ProjectID == nil || *g.ProjectID != "p1" {
		t.Fatal(g, err)
	}
	if err := e.repo.DeleteAssignment(ctx, x.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.repo.GetAssignment(ctx, x.ID); !errors.Is(err, access.ErrNotFound) {
		t.Fatal(err)
	}
	if err := e.repo.DeleteAssignment(ctx, x.ID); !errors.Is(err, access.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestAccessRepo_CustomRoundtrip(t *testing.T) {
	e := newAccessEnv(t)
	ctx := context.Background()
	p := access.Policy{ID: id.New(), Name: "Reviewer", Actions: []access.Action{access.ContributionRead, access.SignalCreate}, CreatedAt: time.Now()}
	if err := e.repo.CreatePolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.CreatePolicy(ctx, access.Policy{ID: id.New(), Name: "Reviewer", Actions: p.Actions, CreatedAt: time.Now()}); !errors.Is(err, access.ErrConflict) {
		t.Fatalf("duplicate name: %v", err)
	}
	p.Name, p.Actions = "Reviewer2", []access.Action{access.ContributionRead}
	if err := e.repo.UpdatePolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err := e.repo.GetPolicy(ctx, p.ID)
	if err != nil || got.Name != "Reviewer2" || !slices.Equal(got.Actions, p.Actions) || got.Managed {
		t.Fatal(got, err)
	}
	r := access.Role{ID: id.New(), Name: "reviewer", PolicyIDs: []string{p.ID, "ReadOnlyAccess"}, CreatedAt: time.Now()}
	if err := e.repo.CreateRole(ctx, r); err != nil {
		t.Fatal(err)
	}
	gr, err := e.repo.GetRole(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := slices.Clone(r.PolicyIDs)
	slices.Sort(want)
	if !slices.Equal(gr.PolicyIDs, want) {
		t.Fatal(gr.PolicyIDs)
	}
	r.PolicyIDs = []string{"ContributorAccess"}
	if err := e.repo.UpdateRole(ctx, r); err != nil {
		t.Fatal(err)
	}
	if acts, _ := e.repo.RoleActions(ctx, r.ID); !slices.Equal(acts, []access.Action{access.ContributionRead, access.ContributionCreate, access.SignalCreate}) {
		t.Fatal(acts)
	}
	if err := e.repo.CreateRole(ctx, access.Role{ID: id.New(), Name: "bad", PolicyIDs: []string{"nope"}, CreatedAt: time.Now()}); !errors.Is(err, access.ErrNotFound) {
		t.Fatalf("unknown policy: %v", err)
	}
}

func TestAccessRepo_DeleteInUse(t *testing.T) {
	e := newAccessEnv(t)
	ctx := context.Background()
	p := access.Policy{ID: id.New(), Name: "P", Actions: []access.Action{access.ContributionRead}, CreatedAt: time.Now()}
	r := access.Role{ID: id.New(), Name: "R", PolicyIDs: []string{p.ID}, CreatedAt: time.Now()}
	if err := e.repo.CreatePolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.CreateRole(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.DeletePolicy(ctx, p.ID); !errors.Is(err, access.ErrConflict) {
		t.Fatalf("policy in a role: %v", err)
	}
	a := e.actor(t, actor.TypeAgent)
	x := e.assign(t, a, r.ID, nil)
	if err := e.repo.DeleteRole(ctx, r.ID); !errors.Is(err, access.ErrConflict) {
		t.Fatalf("assigned role: %v", err)
	}
	if err := e.repo.DeleteAssignment(ctx, x.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.DeleteRole(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.DeletePolicy(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.repo.GetPolicy(ctx, p.ID); !errors.Is(err, access.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestAccessRepo_ManagedReadOnly(t *testing.T) {
	e := newAccessEnv(t)
	ctx := context.Background()
	ro, err := e.repo.GetPolicy(ctx, "ReadOnlyAccess")
	if err != nil || !ro.Managed {
		t.Fatal(ro, err)
	}
	ro.Actions = []access.Action{access.ContributionRead, access.SignalCreate}
	if err := e.repo.UpdatePolicy(ctx, ro); !errors.Is(err, access.ErrForbidden) {
		t.Errorf("UpdatePolicy managed: %v", err)
	}
	if err := e.repo.DeletePolicy(ctx, "ReadOnlyAccess"); !errors.Is(err, access.ErrForbidden) {
		t.Errorf("DeletePolicy managed: %v", err)
	}
	if err := e.repo.UpdateRole(ctx, access.Role{ID: "viewer", Name: "viewer", PolicyIDs: []string{"AdministratorAccess"}}); !errors.Is(err, access.ErrForbidden) {
		t.Errorf("UpdateRole managed: %v", err)
	}
	if err := e.repo.DeleteRole(ctx, "viewer"); !errors.Is(err, access.ErrForbidden) {
		t.Errorf("DeleteRole managed: %v", err)
	}
	if err := e.repo.DeleteRole(ctx, id.New()); !errors.Is(err, access.ErrNotFound) {
		t.Errorf("DeleteRole unknown: %v", err)
	}
	if got, _ := e.repo.RoleActions(ctx, "viewer"); !slices.Equal(got, []access.Action{access.ContributionRead}) {
		t.Fatal("managed role changed:", got)
	}
}

// 0003 turns is_admin into an admin assignment and drops the column.
func TestMigrate_0003MovesAdminFlag(t *testing.T) {
	pool := pgtest.New(t)
	ctx := context.Background()
	if err := postgres.Migrate(ctx, pool); err != nil { // creates extensions + schema_migration
		t.Fatal(err)
	}
	// Rebuild the pre-0003 state: drop everything 0003 made and restore the flag.
	mustExec(t, pool, `DROP TABLE role_assignment, role_policy, role, policy`)
	mustExec(t, pool, `ALTER TABLE actor ADD COLUMN is_admin boolean NOT NULL DEFAULT false`)
	mustExec(t, pool, `DELETE FROM schema_migration WHERE version = 3`)
	admin, other := id.New(), id.New()
	mustExec(t, pool, `INSERT INTO actor (id, type, name, is_admin, created_at) VALUES ($1,'Human','A',true,now()), ($2,'Agent','B',false,now())`, admin, other)
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	repo := postgres.NewAccessRepo(pool)
	if g, _ := repo.Grants(ctx, admin); !g.IsAdmin() {
		t.Fatal("former admin is not admin")
	}
	if g, _ := repo.Grants(ctx, other); g.Can(access.ContributionRead, "p") {
		t.Fatal("non-admin got grants")
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'actor' AND column_name = 'is_admin'`).Scan(&n); err != nil || n != 0 {
		t.Fatal("is_admin column still exists", n, err)
	}
}

// Two admins removing each other's admin rights at the same moment, by any
// mix of "delete the assignment" and "disable the actor": exactly one wins
// and one active Human admin is always left.
func TestAccessRepo_ConcurrentLastAdminRemovals(t *testing.T) {
	e := newAccessEnv(t)
	ctx := context.Background()
	remove := map[string]func(actorID, assignmentID string) error{
		"delete": func(_, assignmentID string) error { return e.repo.DeleteAssignment(ctx, assignmentID) },
		"disable": func(actorID, _ string) error {
			now := time.Now()
			return e.actors.SetDisabled(ctx, actorID, &now)
		},
	}
	for _, combo := range [][2]string{{"delete", "delete"}, {"disable", "disable"}, {"delete", "disable"}} {
		for round := 0; round < 12; round++ {
			if _, err := e.pool.Exec(ctx, `DELETE FROM role_assignment WHERE role_id = 'admin'`); err != nil {
				t.Fatal(err)
			}
			if _, err := e.pool.Exec(ctx, `UPDATE actor SET disabled_at = NULL`); err != nil {
				t.Fatal(err)
			}
			ids := [2]string{e.actor(t, actor.TypeHuman), e.actor(t, actor.TypeHuman)}
			as := [2]access.Assignment{e.assign(t, ids[0], access.AdminRole, nil), e.assign(t, ids[1], access.AdminRole, nil)}

			var errs [2]error
			var wg sync.WaitGroup
			start := make(chan struct{})
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					// Goroutine i removes admin i; either both succeed (the bug) or one is refused.
					errs[i] = remove[combo[i]](ids[i], as[i].ID)
				}()
			}
			close(start)
			wg.Wait()

			won, last := 0, 0
			for _, err := range errs {
				switch {
				case err == nil:
					won++
				case errors.Is(err, access.ErrLastAdmin):
					last++
				default:
					t.Fatalf("%v round %d: unexpected error %v", combo, round, err)
				}
			}
			if left, _ := e.repo.AdminHumans(ctx); won != 1 || last != 1 || len(left) != 1 {
				t.Fatalf("%v round %d: %d won, %d refused, %d admins left (errs %v)", combo, round, won, last, len(left), errs)
			}
		}
	}
}
