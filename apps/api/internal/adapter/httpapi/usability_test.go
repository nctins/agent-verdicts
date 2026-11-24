package httpapi

import (
	"context"
	"strings"
	"testing"

	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/postgres"
	"agent-verdicts/internal/signal"
)

// Spec 2026-11-15-web-usability-2 §1: GET /projects/{p}/authors.
func TestAuthors_SortedAndHiddenOnlyAuthorsLeftOut(t *testing.T) {
	e := newEnv(t, "")
	adm, bossID := e.admin(t)
	zed, amy, bob := e.human(t, "zed", false, false), e.human(t, "amy", false, false), e.human(t, "bob", false, false)
	e.contrib(t, "p", zed.ID, contribution.KindClaim, "by zed", "")
	e.contrib(t, "p", amy.ID, contribution.KindClaim, "by amy one", "")
	e.contrib(t, "p", amy.ID, contribution.KindClaim, "by amy two", "")
	hidden := e.contrib(t, "p", bob.ID, contribution.KindClaim, "by bob", "")
	e.contrib(t, "other", bossID, contribution.KindClaim, "elsewhere", "")
	if err := postgres.NewContributionRepo(e.pool).SetHidden(context.Background(), hidden, bossID, "", true); err != nil {
		t.Fatal(err)
	}
	names := func(c *client) string {
		r := c.do("GET", "/projects/p/authors", nil, nil)
		wantStatus(t, r, 200)
		var out []string
		for _, it := range items(t, r) {
			if it["id"] == "" || it["name"] == "" {
				t.Fatalf("author without id or name: %v", it)
			}
			out = append(out, it["name"].(string))
		}
		return strings.Join(out, ",")
	}
	if got := names(e.reader(t)); got != "Name amy,Name zed" {
		t.Fatalf("reader sees authors %q, want the visible ones by name", got)
	}
	if got := names(adm); got != "Name amy,Name bob,Name zed" {
		t.Fatalf("moderator sees authors %q, want all three", got)
	}
	c, _ := e.loggedIn(t, "nobody")
	wantErr(t, c.do("GET", "/projects/p/authors", nil, nil), 404, "not_found")
}

// Spec §2: replies.
func TestReplyTo(t *testing.T) {
	e := newEnv(t, "")
	c, a := loggedIn(t, e, "ann")
	claim := e.contrib(t, "p", a.ID, contribution.KindClaim, "claim", "")
	other := e.contrib(t, "p", a.ID, contribution.KindClaim, "other claim", "")
	path := func(id string) string { return "/projects/p/contributions/" + id + "/signals" }
	post := func(cid string, body map[string]string) resp { return c.do("POST", path(cid), body, nil) }
	root := post(claim, map[string]string{"kind": "challenge", "stance": "unfavorable", "body": "wrong"})
	wantStatus(t, root, 201)
	rootID := root.body["id"].(string)
	elsewhere := post(other, map[string]string{"kind": "challenge", "stance": "unfavorable", "body": "wrong too"}).body["id"].(string)

	state := func() any { return c.do("GET", "/projects/p/contributions/"+claim, nil, nil).body["state"] }
	before := state()
	r := post(claim, map[string]string{"kind": "challenge", "stance": "neutral", "body": "why?", "reply_to": rootID})
	wantStatus(t, r, 201)
	replyID := r.body["id"].(string)
	// A reply to a reply is stored under the root.
	r = post(claim, map[string]string{"kind": "challenge", "stance": "neutral", "body": "because", "reply_to": replyID})
	wantStatus(t, r, 201)
	deepID := r.body["id"].(string)
	replyTo := map[string]any{}
	for _, s := range c.do("GET", "/projects/p/contributions/"+claim, nil, nil).body["signals"].(map[string]any)["neutral"].([]any) {
		m := s.(map[string]any)
		replyTo[m["id"].(string)] = m["reply_to"]
	}
	if len(replyTo) != 2 || replyTo[replyID] != rootID || replyTo[deepID] != rootID {
		t.Fatalf("reply_to = %v, want both under %s", replyTo, rootID)
	}
	if got := state(); got != before {
		t.Fatalf("state %v -> %v after replies, want unchanged", before, got)
	}

	wantErr(t, post(claim, map[string]string{"kind": "challenge", "stance": "neutral", "body": "x", "reply_to": elsewhere}), 400, "invalid_argument")
	wantErr(t, post(claim, map[string]string{"kind": "challenge", "stance": "neutral", "body": "x", "reply_to": "nope"}), 400, "invalid_argument")
	wantErr(t, post(claim, map[string]string{"kind": "challenge", "stance": "unfavorable", "body": "x", "reply_to": rootID}), 400, "invalid_argument")
	wantErr(t, post(claim, map[string]string{"kind": "assessment", "stance": "neutral", "reply_to": rootID}), 400, "invalid_argument")

	// A withdrawn signal cannot be replied to.
	wantStatus(t, c.do("DELETE", path(claim)+"/"+rootID, nil, nil), 204)
	wantErr(t, post(claim, map[string]string{"kind": "challenge", "stance": "neutral", "body": "x", "reply_to": rootID}), 400, "invalid_argument")
}

// Spec §2 D3: a new favorable challenge is refused with a pointer to assessments.
func TestFavorableChallengeRefused(t *testing.T) {
	e := newEnv(t, "")
	c, a := loggedIn(t, e, "ann")
	claim := e.contrib(t, "p", a.ID, contribution.KindClaim, "claim", "")
	r := c.do("POST", "/projects/p/contributions/"+claim+"/signals", map[string]string{"kind": "challenge", "stance": "favorable", "body": "yes"}, nil)
	wantErr(t, r, 400, "invalid_argument")
	if !strings.Contains(string(r.raw), "use an assessment with a body") {
		t.Fatalf("message does not point at assessments: %s", r.raw)
	}
	wantStatus(t, c.do("POST", "/projects/p/contributions/"+claim+"/signals", map[string]string{"kind": "assessment", "stance": "favorable", "body": "yes"}, nil), 201)
}

// A favorable challenge from before D3 still counts (written straight to the database).
func TestLegacyFavorableChallengeStillCounts(t *testing.T) {
	e := newEnv(t, "")
	c, a := loggedIn(t, e, "ann")
	claim := e.contrib(t, "p", a.ID, contribution.KindClaim, "claim", "")
	b := e.human(t, "bea", false, false) // not the author, whose own support would not count
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO signal (id, contribution_id, actor_id, kind, stance, body, created_at)
		VALUES (gen_random_uuid(), $1, $2, 'challenge', 'favorable', 'old', now())`, claim, b.ID); err != nil {
		t.Fatal(err)
	}
	// The counters are rebuilt by the next signal on the claim.
	if _, err := signal.Create(context.Background(), postgres.NewContributionRepo(e.pool), e.actors, postgres.NewSignalRepo(e.pool),
		signal.NewInput{ProjectID: "p", ContributionID: claim, ActorID: b.ID, Kind: signal.KindAssessment, Stance: signal.StanceNeutral}); err != nil {
		t.Fatal(err)
	}
	r := c.do("GET", "/projects/p/contributions/"+claim, nil, nil)
	if r.body["state"] != "Favorable" {
		t.Fatalf("state = %v, want Favorable from the old favorable challenge: %s", r.body["state"], r.raw)
	}
}

// Spec §4 D8: names are unique ignoring case.
func TestActorName_UniqueIgnoringCase(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	body := func(name, user string) map[string]string {
		return map[string]string{"type": "Human", "name": name, "username": user, "password": testPassword}
	}
	r := c.do("POST", "/admin/actors", body("Dana", "dana1"), nil)
	wantStatus(t, r, 201)
	wantErr(t, c.do("POST", "/admin/actors", body("  dana ", "dana2"), nil), 409, "conflict")
	wantErr(t, c.do("POST", "/admin/actors", map[string]string{"type": "Agent", "name": "DANA"}, nil), 409, "conflict")
	r2 := c.do("POST", "/admin/actors", body("Eve", "eve1"), nil)
	wantStatus(t, r2, 201)
	eve := r2.body["id"].(string)
	wantErr(t, c.do("PATCH", "/admin/actors/"+eve, map[string]string{"name": "dANA"}, nil), 409, "conflict")
	wantStatus(t, c.do("PATCH", "/admin/actors/"+eve, map[string]string{"name": "EVE"}, nil), 200) // own name, other case
}

// Spec §4 D9: a manager without actor:Manage sees only members of its projects, or a name search.
func TestManager_ActorList(t *testing.T) {
	e := newEnv(t, "")
	e.project(t, "p1", "p2")
	mgr, mid := e.loggedIn(t, "mia")
	e.grant(t, mid, "manager", "p1")
	member := e.human(t, "mel", false, false)
	e.grant(t, member.ID, "viewer", "p1")
	stranger := e.human(t, "sam", false, false)
	e.grant(t, stranger.ID, "viewer", "p2")
	everywhere := e.human(t, "eva", false, false)
	e.grant(t, everywhere.ID, "viewer", "")
	e.human(t, "nan", false, false) // no assignment at all

	names := func(r resp) []string {
		wantStatus(t, r, 200)
		var out []string
		for _, it := range items(t, r) {
			out = append(out, it["name"].(string))
		}
		return out
	}
	// mia herself and mel hold roles on p1; sam is on p2 only; eva's assignment is on every project.
	for _, it := range items(t, mgr.do("GET", "/admin/actors", nil, nil)) {
		if it["username"] != nil {
			t.Fatalf("a manager sees how people log in: %v", it)
		}
	}
	if got := strings.Join(names(mgr.do("GET", "/admin/actors", nil, nil)), ","); got != "Name mia,Name mel" {
		t.Fatalf("without q: %q, want only the members of p1", got)
	}
	if got := strings.Join(names(mgr.do("GET", "/admin/actors?q=%20NAN%20", nil, nil)), ","); got != "Name nan" {
		t.Fatalf("q=NAN: %q, want the actor without a role found by name", got)
	}
	wantErr(t, mgr.do("GET", "/admin/actors?q=n", nil, nil), 400, "invalid_argument")
	wantErr(t, mgr.do("GET", "/admin/actors?q=%20n%20", nil, nil), 400, "invalid_argument")
	for i := 0; i < 25; i++ {
		e.human(t, "bulk"+strings.Repeat("x", i), false, false)
	}
	if got := names(mgr.do("GET", "/admin/actors?q=bulk", nil, nil)); len(got) != 20 {
		t.Fatalf("q=bulk returned %d actors, want at most 20", len(got))
	}
	// An admin still gets everything; q narrows it without the cap.
	adm, _ := e.admin(t)
	if got := names(adm.do("GET", "/admin/actors?q=bulk", nil, nil)); len(got) != 25 {
		t.Fatalf("admin q=bulk returned %d actors, want 25", len(got))
	}
	wantErr(t, adm.do("GET", "/admin/actors?q=b", nil, nil), 400, "invalid_argument")

	// A manager of every project also sees the actor whose assignment is on every project.
	all, aid := e.loggedIn(t, "max")
	e.grant(t, aid, "manager", "")
	got := strings.Join(names(all.do("GET", "/admin/actors", nil, nil)), ",")
	if !strings.Contains(got, "Name eva") || !strings.Contains(got, "Name sam") || strings.Contains(got, "Name nan") {
		t.Fatalf("manager of every project sees %q", got)
	}
}

// Spec §4 D10: a manager's overview covers only the projects it manages, not the ones it merely reads.
func TestManager_OverviewScopedToManagedProjects(t *testing.T) {
	e := newEnv(t, "")
	boss := e.human(t, "boss", true, false)
	e.contrib(t, "p1", boss.ID, contribution.KindClaim, "managed one", "")
	e.contrib(t, "p1", boss.ID, contribution.KindClaim, "managed two", "")
	e.contrib(t, "p2", boss.ID, contribution.KindClaim, "only read", "")
	e.contrib(t, "p3", boss.ID, contribution.KindClaim, "not mine", "")
	c, me := e.loggedIn(t, "mia")
	e.grant(t, me, "manager", "p1")
	e.grant(t, me, "viewer", "p2")
	r := c.do("GET", "/admin/overview", nil, nil)
	wantStatus(t, r, 200)
	recent, _ := r.body["recent"].([]any)
	if r.body["project_count"] != float64(1) || r.body["contribution_count"] != float64(2) || len(recent) != 2 {
		t.Fatalf("overview = %s", r.raw)
	}
	for _, it := range recent {
		if it.(map[string]any)["project_id"] != "p1" {
			t.Fatalf("overview lists another project: %s", r.raw)
		}
	}
	// Reading alone opens nothing.
	v, vid := e.loggedIn(t, "vera")
	e.grant(t, vid, "viewer", "")
	wantErr(t, v.do("GET", "/admin/overview", nil, nil), 403, "forbidden")
}
