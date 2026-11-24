package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/audit"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/signal"
)

// events is everything logged so far, newest first.
func (e *env) events(t *testing.T) []audit.Event {
	t.Helper()
	p, err := postgres.NewAuditRepo(e.pool).List(context.Background(), audit.Filter{Limit: audit.MaxLimit})
	if err != nil {
		t.Fatal(err)
	}
	return p.Items
}

func count(evs []audit.Event, action string) int {
	n := 0
	for _, ev := range evs {
		if ev.Action == action {
			n++
		}
	}
	return n
}

// Every operation in the audit table produces exactly one event, and no
// event holds a secret the same response returned.
func TestAudit_EveryOperationLogsOnceWithoutSecrets(t *testing.T) {
	e := newEnv(t, "")
	c, bossID := e.admin(t)
	secrets := []string{}
	keep := func(r resp, keys ...string) resp {
		for _, k := range keys {
			if s, _ := r.body[k].(string); s != "" {
				secrets = append(secrets, s)
				if k == "token" {
					_, secret, _ := strings.Cut(s, ".")
					secrets = append(secrets, secret)
				}
			}
		}
		return r
	}

	human := keep(c.do("POST", "/admin/actors", map[string]any{"type": "Human", "name": "Hana", "username": "hana"}, nil), "password")
	wantStatus(t, human, 201)
	hanaID := human.body["id"].(string)
	agent := keep(c.do("POST", "/admin/actors", map[string]any{"type": "Agent", "name": "Bot"}, nil), "token")
	wantStatus(t, agent, 201)
	agentID := agent.body["id"].(string)
	wantStatus(t, c.do("PATCH", "/admin/actors/"+agentID, map[string]any{"name": "Bot 2"}, nil), 200)
	wantStatus(t, c.do("PATCH", "/admin/actors/"+agentID, map[string]any{"disabled": true}, nil), 200)
	wantStatus(t, c.do("PATCH", "/admin/actors/"+agentID, map[string]any{"disabled": false}, nil), 200)
	wantStatus(t, keep(c.do("POST", "/admin/actors/"+hanaID+"/password", map[string]any{"password": "a-chosen-password-9"}, nil), "password"), 200)
	secrets = append(secrets, "a-chosen-password-9")
	issued := keep(c.do("POST", "/admin/actors/"+agentID+"/credentials", map[string]any{"label": "ci"}, nil), "token")
	wantStatus(t, issued, 201)
	wantStatus(t, c.do("DELETE", "/admin/credentials/"+issued.body["id"].(string), nil, nil), 204)
	wantStatus(t, c.do("DELETE", "/admin/credentials/"+issued.body["id"].(string), nil, nil), 204) // a no-op: no second event

	e.project(t, "p1")
	wantStatus(t, c.do("POST", "/admin/projects", map[string]string{"id": "p2"}, nil), 201)
	wantStatus(t, c.do("PATCH", "/admin/projects/p2", map[string]any{"name": "Two", "min_favorable": 2}, nil), 200)
	pol := c.do("POST", "/admin/policies", map[string]any{"name": "Rd", "actions": []string{"contribution:Read"}}, nil)
	polID := pol.body["id"].(string)
	wantStatus(t, c.do("PUT", "/admin/policies/"+polID, map[string]any{"name": "Rd2", "actions": []string{"contribution:Read"}}, nil), 200)
	role := c.do("POST", "/admin/roles", map[string]any{"name": "rd", "policy_ids": []string{polID}}, nil)
	roleID := role.body["id"].(string)
	wantStatus(t, c.do("PUT", "/admin/roles/"+roleID, map[string]any{"name": "rd2", "policy_ids": []string{polID}}, nil), 200)
	as := c.do("POST", "/admin/assignments", map[string]any{"actor_id": hanaID, "role_id": roleID, "project_id": "p1"}, nil)
	wantStatus(t, as, 201)
	wantStatus(t, c.do("DELETE", "/admin/assignments/"+as.body["id"].(string), nil, nil), 204)
	wantStatus(t, c.do("DELETE", "/admin/roles/"+roleID, nil, nil), 204)
	wantStatus(t, c.do("DELETE", "/admin/policies/"+polID, nil, nil), 204)
	wantStatus(t, c.do("PUT", "/admin/settings/search", map[string]any{"embedding": map[string]any{"url": "http://emb.example/v1", "model": "m", "api_key": "sk-very-secret-1234"}}, nil), 200)
	secrets = append(secrets, "sk-very-secret-1234")
	wantStatus(t, c.do("POST", "/admin/settings/search/reindex", nil, nil), 202)

	cid := e.contrib(t, "p1", bossID, contribution.KindClaim, "A claim", "")
	sig, err := signal.Create(context.Background(), postgres.NewContributionRepo(e.pool), e.actors, postgres.NewSignalRepo(e.pool),
		signal.NewInput{ProjectID: "p1", ContributionID: cid, ActorID: hanaID, Kind: signal.KindAssessment, Stance: signal.StanceFavorable, Body: "why"})
	if err != nil {
		t.Fatal(err)
	}
	cp := "/projects/p1/contributions/" + cid
	wantStatus(t, c.do("PUT", cp+"/hidden", map[string]bool{"hidden": true}, nil), 204)
	wantStatus(t, c.do("PUT", cp+"/hidden", map[string]bool{"hidden": false}, nil), 204)
	wantStatus(t, c.do("PUT", cp+"/signals/"+sig.Signal.ID+"/hidden", map[string]bool{"hidden": true}, nil), 204)

	evs := e.events(t)
	want := map[string]int{
		"actor.create": 2, "actor.rename": 1, "actor.disable": 1, "actor.enable": 1, "actor.password_reset": 1,
		"credential.issue": 1, "credential.revoke": 1, "assignment.create": 1, "assignment.delete": 1,
		"role.create": 1, "role.update": 1, "role.delete": 1, "policy.create": 1, "policy.update": 1, "policy.delete": 1,
		"project.create": 1, "project.update": 1, "settings.search.update": 1, "settings.search.reindex": 1,
		"content.hide": 2, "content.unhide": 1,
	}
	total := 0
	for action, n := range want {
		if got := count(evs, action); got != n {
			t.Errorf("%s events = %d, want %d", action, got, n)
		}
		total += n
	}
	if len(evs) != total {
		t.Errorf("%d events in all, want %d", len(evs), total)
	}
	raw, _ := json.Marshal(evs)
	for _, s := range secrets {
		if strings.Contains(string(raw), s) {
			t.Errorf("a secret reached the audit log: %q", s)
		}
	}
	for _, ev := range evs {
		if ev.ActorID != bossID {
			t.Errorf("%s by %q, want the caller", ev.Action, ev.ActorID)
		}
		if ev.Action == "content.hide" && ev.ProjectID != "p1" || ev.Action == "project.update" && ev.ProjectID != "p2" {
			t.Errorf("%s: project %q", ev.Action, ev.ProjectID)
		}
	}
}

func TestAudit_SetupCompleteLogsOnceWithoutPassword(t *testing.T) {
	e := newEnv(t, "ABC123")
	c := e.client(t)
	r := c.do("POST", "/setup", setupBody("ABC123"), nil)
	wantStatus(t, r, 201)
	evs := e.events(t)
	raw, _ := json.Marshal(evs)
	if len(evs) != 1 || evs[0].Action != "setup.complete" || evs[0].ActorID != r.body["actor"].(map[string]any)["id"] || strings.Contains(string(raw), testPassword) {
		t.Fatalf("events = %s", raw)
	}
}

func TestAudit_FailedOperationLogsNothing(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	wantErr(t, c.do("POST", "/admin/actors", map[string]any{"type": "Agent", "name": ""}, nil), 400, "invalid_argument")
	wantErr(t, c.do("DELETE", "/admin/assignments/00000000-0000-4000-8000-000000000000", nil, nil), 404, "not_found")
	if evs := e.events(t); len(evs) != 0 {
		t.Fatalf("events = %v", evs)
	}
}

func TestAudit_ListOutputAndFilters(t *testing.T) {
	e := newEnv(t, "")
	c, bossID := e.admin(t)
	wantStatus(t, c.do("POST", "/admin/projects", map[string]string{"id": "p1"}, nil), 201)
	wantStatus(t, c.do("POST", "/admin/projects", map[string]string{"id": "p2"}, nil), 201)
	wantStatus(t, c.do("POST", "/admin/actors", map[string]any{"type": "Agent", "name": "Bot"}, nil), 201)

	all := items(t, c.do("GET", "/admin/audit", nil, nil))
	if len(all) != 3 || all[0]["action"] != "actor.create" || all[2]["action"] != "project.create" { // newest first
		t.Fatalf("audit = %v", all)
	}
	if all[0]["actor_id"] != bossID || all[0]["actor_name"] != "Name boss" || all[0]["project_id"] != nil || all[0]["detail"] == nil {
		t.Fatalf("item = %v", all[0])
	}
	if got := items(t, c.do("GET", "/admin/audit?project=p2", nil, nil)); len(got) != 1 || got[0]["target_id"] != "p2" {
		t.Fatalf("project filter = %v", got)
	}
	if got := items(t, c.do("GET", "/admin/audit?action=actor.create&actor="+bossID, nil, nil)); len(got) != 1 {
		t.Fatalf("action+actor filter = %v", got)
	}
	if got := items(t, c.do("GET", "/admin/audit?actor="+e.actorOther(t), nil, nil)); len(got) != 0 {
		t.Fatalf("other actor filter = %v", got)
	}

	// Paging with the cursor.
	r := c.do("GET", "/admin/audit?limit=2", nil, nil)
	next, _ := r.body["next_cursor"].(string)
	if len(items(t, r)) != 2 || next == "" {
		t.Fatalf("page 1 = %s", r.raw)
	}
	r = c.do("GET", "/admin/audit?limit=2&cursor="+next, nil, nil)
	if got := items(t, r); len(got) != 1 || got[0]["action"] != "project.create" || r.body["next_cursor"] != nil {
		t.Fatalf("page 2 = %s", r.raw)
	}
	// Days are UTC and both ends count (spec 2026-11-22-admin-ui-redesign A3).
	day := func(d int) string { return time.Now().UTC().AddDate(0, 0, d).Format(time.DateOnly) }
	if got := items(t, c.do("GET", "/admin/audit?from="+day(0)+"&to="+day(0), nil, nil)); len(got) != 3 {
		t.Fatalf("today = %v", got)
	}
	if got := items(t, c.do("GET", "/admin/audit?to="+day(-1), nil, nil)); len(got) != 0 {
		t.Fatalf("until yesterday = %v", got)
	}
	if got := items(t, c.do("GET", "/admin/audit?from="+day(1), nil, nil)); len(got) != 0 {
		t.Fatalf("from tomorrow = %v", got)
	}
	wantErr(t, c.do("GET", "/admin/audit?from=22-11-2026", nil, nil), 400, "invalid_argument")
	wantErr(t, c.do("GET", "/admin/audit?cursor=bad", nil, nil), 400, "invalid_argument")
	wantErr(t, c.do("GET", "/admin/audit?limit=abc", nil, nil), 400, "invalid_argument")
}

// actorOther is the id of a throwaway actor with no events.
func (e *env) actorOther(t *testing.T) string { return e.human(t, "idle", false, false).ID }

// A member manager sees only assignment.* and content.* events of the
// projects it manages; an actor with neither right gets 403.
func TestAudit_MemberManagerSeesOnlyOwnProjects(t *testing.T) {
	e := newEnv(t, "")
	boss, bossID := e.admin(t)
	e.project(t, "p1", "p2")
	mona, monaID := e.loggedIn(t, "mona")
	e.grant(t, monaID, "manager", "p1")
	tom := e.human(t, "tom", false, false)

	for _, p := range []string{"p1", "p2"} {
		as := boss.do("POST", "/admin/assignments", map[string]any{"actor_id": tom.ID, "role_id": "viewer", "project_id": p}, nil)
		wantStatus(t, as, 201)
	}
	wantStatus(t, boss.do("POST", "/admin/assignments", map[string]any{"actor_id": tom.ID, "role_id": "viewer", "project_id": nil}, nil), 201) // every project: not hers
	cid := e.contrib(t, "p1", bossID, contribution.KindClaim, "A claim", "")
	c2 := e.contrib(t, "p2", bossID, contribution.KindClaim, "Another", "")
	wantStatus(t, boss.do("PUT", "/projects/p1/contributions/"+cid+"/hidden", map[string]bool{"hidden": true}, nil), 204)
	wantStatus(t, boss.do("PUT", "/projects/p2/contributions/"+c2+"/hidden", map[string]bool{"hidden": true}, nil), 204)
	wantStatus(t, boss.do("PATCH", "/admin/actors/"+tom.ID, map[string]any{"name": "Tom"}, nil), 200) // not an assignment/content event

	got := items(t, mona.do("GET", "/admin/audit", nil, nil))
	if len(got) != 2 {
		t.Fatalf("manager of p1 sees %v", got)
	}
	for _, it := range got {
		a, _ := it["action"].(string)
		if it["project_id"] != "p1" || !(strings.HasPrefix(a, "assignment.") || strings.HasPrefix(a, "content.")) {
			t.Fatalf("manager sees %v", it)
		}
	}
	if got := items(t, mona.do("GET", "/admin/audit?project=p2", nil, nil)); len(got) != 0 {
		t.Fatalf("p2 filter leaks: %v", got)
	}
	if got := items(t, mona.do("GET", "/admin/audit?action=actor.rename", nil, nil)); len(got) != 0 {
		t.Fatalf("action filter leaks: %v", got)
	}
	if n := len(items(t, boss.do("GET", "/admin/audit", nil, nil))); n < 6 {
		t.Fatalf("admin sees %d events", n)
	}

	// A manager on every project also sees the assignments on every project.
	wide, wideID := e.loggedIn(t, "wide")
	e.grant(t, wideID, "manager", "")
	if n := len(items(t, wide.do("GET", "/admin/audit", nil, nil))); n != 5 { // 3 assignments + 2 hides; wide's own grant is not logged
		t.Fatalf("manager on every project sees %d", n)
	}

	nobody, _ := e.loggedIn(t, "nina")
	wantErr(t, nobody.do("GET", "/admin/audit", nil, nil), 403, "forbidden")
	_ = access.All
}

func within(t *testing.T, got any, days int) {
	t.Helper()
	s, _ := got.(string)
	at, err := time.Parse(time.RFC3339Nano, s)
	want := time.Now().AddDate(0, 0, days)
	if err != nil || at.Sub(want).Abs() > time.Minute {
		t.Fatalf("expires_at = %v, want about %d days from now", got, days)
	}
}

func TestCredentials_LabelExpiryAndDefault(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	agent := c.do("POST", "/admin/actors", map[string]any{"type": "Agent", "name": "Bot"}, nil)
	agentID := agent.body["id"].(string)
	issue := func(body any) resp { return c.do("POST", "/admin/actors/"+agentID+"/credentials", body, nil) }

	r := issue(nil) // no body: the default
	wantStatus(t, r, 201)
	within(t, r.body["expires_at"], 90)
	if r.body["label"] != "" {
		t.Fatalf("label = %v", r.body["label"])
	}
	r = issue(map[string]any{"label": " deploy bot ", "expires_in_days": 30})
	wantStatus(t, r, 201)
	within(t, r.body["expires_at"], 30)
	if r.body["label"] != "deploy bot" {
		t.Fatalf("label = %v", r.body["label"])
	}
	within(t, issue(map[string]any{"expires_in_days": 365}).body["expires_at"], 365)
	within(t, issue(map[string]any{"label": "x"}).body["expires_at"], 90) // absent = 90
	r = issue(`{"expires_in_days": null}`)
	wantStatus(t, r, 201)
	if r.body["expires_at"] != nil {
		t.Fatalf("null expiry = %v, want never", r.body["expires_at"])
	}
	for _, bad := range []any{`{"expires_in_days": 7}`, `{"expires_in_days": "90"}`, `{"expires_in_days": 0}`, map[string]any{"label": strings.Repeat("x", 65)}, map[string]any{"label": "a\nb"}} {
		wantErr(t, issue(bad), 400, "invalid_argument")
	}

	// The first token of a new Agent expires after 90 days too; the list shows all three fields.
	creds := c.do("GET", "/admin/actors/"+agentID, nil, nil).body["credentials"].([]any)
	if len(creds) != 6 {
		t.Fatalf("credentials = %v", creds)
	}
	first := creds[0].(map[string]any)
	within(t, first["expires_at"], 90)
	if _, ok := first["last_used_at"]; !ok || first["last_used_at"] != nil || first["label"] != "" {
		t.Fatalf("credential item = %v", first)
	}
	if creds[2].(map[string]any)["label"] != "deploy bot" || creds[5].(map[string]any)["expires_at"] != nil {
		t.Fatalf("credential items = %v", creds)
	}
}
