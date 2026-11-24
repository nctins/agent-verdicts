package httpapi

import (
	"strings"
	"testing"

	"agent-verdicts/internal/contribution"
)

// Spec 2026-11-15-role-play-fixes §A.

// #1, #5: the author reads and lists their own hidden contribution, with the
// reason; nobody else without content:Moderate does.
func TestHidden_AuthorSeesOwnWithReason(t *testing.T) {
	e := newEnv(t, "")
	boss, _ := e.admin(t)
	ann, a := loggedIn(t, e, "ann")
	bob, _ := loggedIn(t, e, "bob")
	cid := e.contrib(t, "p", a.ID, contribution.KindClaim, "Hidden claim", "")
	path := "/projects/p/contributions/" + cid

	wantErr(t, boss.do("PUT", path+"/hidden", map[string]any{"hidden": true, "reason": strings.Repeat("é", 501)}, nil), 400, "invalid_argument")
	wantStatus(t, boss.do("PUT", path+"/hidden", map[string]any{"hidden": true, "reason": " off topic "}, nil), 204)

	for name, c := range map[string]*client{"author": ann, "moderator": boss} {
		r := c.do("GET", path, nil, nil)
		wantStatus(t, r, 200)
		if r.body["hidden"] != true || r.body["hidden_reason"] != "off topic" {
			t.Fatalf("%s read = %s", name, r.raw)
		}
	}
	wantErr(t, bob.do("GET", path, nil, nil), 404, "not_found")

	listed := func(c *client, q string) []map[string]any {
		r := c.do("GET", "/projects/p/contributions?"+q, nil, nil)
		wantStatus(t, r, 200)
		return items(t, r)
	}
	if got := listed(ann, "actor=me"); len(got) != 1 || got[0]["hidden"] != true || got[0]["hidden_reason"] != "off topic" {
		t.Fatalf("author's actor=me list = %v", got)
	}
	if got := listed(ann, "actor="+a.ID); len(got) != 1 {
		t.Fatalf("author's list by own id = %v", got)
	}
	if got := listed(ann, ""); len(got) != 0 {
		t.Fatalf("author's unfiltered list = %v", got)
	}
	if got := listed(bob, "actor="+a.ID); len(got) != 0 {
		t.Fatalf("someone else's list of the author = %v", got)
	}

	evs := e.events(t) // newest first
	if d := evs[0].Detail; evs[0].Action != "content.hide" || d["title"] != "Hidden claim" || d["reason"] != "off topic" {
		t.Fatalf("content.hide event = %+v", evs[0])
	}

	wantStatus(t, boss.do("PUT", path+"/hidden", map[string]any{"hidden": false}, nil), 204)
	r := bob.do("GET", path, nil, nil)
	wantStatus(t, r, 200)
	if _, ok := r.body["hidden_reason"]; ok || r.body["hidden"] != nil {
		t.Fatalf("after unhide = %s", r.raw)
	}
	if ev := e.events(t)[0]; ev.Action != "content.unhide" || ev.Detail["title"] != "Hidden claim" {
		t.Fatalf("content.unhide event = %+v", ev)
	}
}

// #5: a hidden signal carries its reason for moderators.
func TestHiddenSignal_Reason(t *testing.T) {
	e := newEnv(t, "")
	boss, bossID := e.admin(t)
	cid := e.contrib(t, "p", bossID, contribution.KindClaim, "claim", "")
	path := "/projects/p/contributions/" + cid
	r := boss.do("POST", path+"/signals", map[string]string{"kind": "challenge", "stance": "unfavorable", "body": "spam"}, nil)
	wantStatus(t, r, 201)
	sid := r.body["id"].(string)
	wantStatus(t, boss.do("PUT", path+"/signals/"+sid+"/hidden", map[string]any{"hidden": true, "reason": "spam"}, nil), 204)
	h := boss.do("GET", path, nil, nil).body["signals"].(map[string]any)["history"].([]any)
	if len(h) != 1 || h[0].(map[string]any)["hidden_reason"] != "spam" {
		t.Fatalf("history = %v", h)
	}
	if ev := e.events(t)[0]; ev.Action != "content.hide" || ev.Detail["reason"] != "spam" || ev.Detail["contribution_id"] != cid {
		t.Fatalf("event = %+v", ev)
	}
	wantStatus(t, boss.do("PUT", path+"/signals/"+sid+"/hidden", map[string]any{"hidden": false}, nil), 204)
	if raw := boss.do("GET", path, nil, nil).raw; strings.Contains(raw, "hidden_reason") {
		t.Fatalf("reason survived unhide: %s", raw)
	}
}

// #3: similar never names the question being answered or the claim being replaced.
func TestSimilar_ExcludesAnsweredAndSuperseded(t *testing.T) {
	e := newEnv(t, "")
	c, a := loggedIn(t, e, "ann")
	q := e.contrib(t, "p", a.ID, contribution.KindQuestion, "pgbouncer pool size for orders", "")
	old := e.contrib(t, "p", a.ID, contribution.KindClaim, "pgbouncer pool size is 20", "")
	similar := func(body map[string]string) string {
		r := c.do("POST", "/projects/p/contributions", body, nil)
		wantStatus(t, r, 201)
		var ids []string
		for _, it := range array(t, r, "similar") {
			ids = append(ids, it["id"].(string))
		}
		return strings.Join(ids, ",")
	}
	if got := similar(map[string]string{"kind": "claim", "title": "pgbouncer pool size 40", "answers_id": q}); got != old {
		t.Fatalf("similar of an answer = %q, want only %s", got, old)
	}
	if got := similar(map[string]string{"kind": "claim", "title": "pgbouncer pool size is 30", "supersedes_id": old}); strings.Contains(got, old) || got == "" {
		t.Fatalf("similar of a replacement = %q, must not contain %s", got, old)
	}
}

// #6: a signal on a superseded claim is accepted and says what replaced the claim.
func TestCreateSignal_SupersededBy(t *testing.T) {
	e := newEnv(t, "")
	c, a := loggedIn(t, e, "ann")
	old := e.contrib(t, "p", a.ID, contribution.KindClaim, "old claim", "")
	post := func() resp {
		r := c.do("POST", "/projects/p/contributions/"+old+"/signals", map[string]string{"kind": "assessment", "stance": "neutral"}, nil)
		wantStatus(t, r, 201)
		return r
	}
	if r := post(); strings.Contains(r.raw, "superseded_by") {
		t.Fatalf("live claim: %s", r.raw)
	}
	r := c.do("POST", "/projects/p/contributions", map[string]string{"kind": "claim", "title": "new claim", "supersedes_id": old}, nil)
	wantStatus(t, r, 201)
	if sb, _ := post().body["superseded_by"].(map[string]any); sb["id"] != r.body["id"] || sb["title"] != "new claim" {
		t.Fatalf("superseded_by = %v", sb)
	}
}

// #4: the actor search ignores accents on both sides, and đ matches d.
func TestAdminActors_SearchIgnoresAccents(t *testing.T) {
	e := newEnv(t, "")
	c, _ := e.admin(t)
	binh, do := e.human(t, "binh", false, false), e.human(t, "doq", false, false)
	wantStatus(t, c.do("PATCH", "/admin/actors/"+binh.ID, map[string]any{"name": "Bình (eval)"}, nil), 200)
	wantStatus(t, c.do("PATCH", "/admin/actors/"+do.ID, map[string]any{"name": "Đỗ Quyên"}, nil), 200)
	for q, want := range map[string]string{"binh": "Bình (eval)", "bi": "Bình (eval)", "BÌNH": "Bình (eval)", "do quyen": "Đỗ Quyên", "đỗ": "Đỗ Quyên"} {
		got := items(t, c.do("GET", "/admin/actors?q="+strings.ReplaceAll(q, " ", "+"), nil, nil))
		if len(got) != 1 || got[0]["name"] != want {
			t.Errorf("q=%s = %v, want %s", q, got, want)
		}
	}
	wantErr(t, c.do("GET", "/admin/actors?q=b", nil, nil), 400, "invalid_argument")
}

// #12: actor rules are reported as actor errors.
func TestAdminActors_ErrorsNameTheActorDomain(t *testing.T) {
	e := newEnv(t, "")
	c, bossID := e.admin(t)
	r := c.do("PATCH", "/admin/actors/"+bossID, map[string]any{"disabled": true}, nil)
	wantErr(t, r, 400, "invalid_argument")
	if r.body["message"] != "actor: invalid argument: you cannot disable yourself" {
		t.Fatalf("message = %v", r.body["message"])
	}
}

// §C1: search results carry the flags the list carries.
func TestSearch_FlagsHiddenAndSuperseded(t *testing.T) {
	e := newEnv(t, "")
	boss, bossID := e.admin(t)
	ann, _ := loggedIn(t, e, "ann")
	hid := e.contrib(t, "p", bossID, contribution.KindClaim, "quokka hidden", "")
	old := e.contrib(t, "p", bossID, contribution.KindClaim, "quokka old", "")
	path := "/projects/p/contributions"
	wantStatus(t, boss.do("PUT", path+"/"+hid+"/hidden", map[string]any{"hidden": true, "reason": "off topic"}, nil), 204)
	wantStatus(t, boss.do("POST", path, map[string]string{"kind": "claim", "title": "numbat new", "supersedes_id": old}, nil), 201)
	found := func(c *client) map[string]map[string]any {
		r := c.do("GET", "/projects/p/search?q=quokka&include_superseded=true", nil, nil)
		wantStatus(t, r, 200)
		out := map[string]map[string]any{}
		for _, it := range array(t, r, "results") {
			out[it["id"].(string)] = it
		}
		return out
	}
	got := found(boss)
	if got[hid]["hidden"] != true || got[hid]["hidden_reason"] != "off topic" || got[old]["superseded"] != true || got[old]["hidden"] != nil {
		t.Fatalf("moderator search = %v", got)
	}
	if got := found(ann); len(got) != 1 || got[old]["superseded"] != true {
		t.Fatalf("contributor search = %v", got)
	}
}

// §C2: errors of the shared query-parameter parsing name no domain.
func TestQueryParameterErrors_NameNoDomain(t *testing.T) {
	e := newEnv(t, "")
	boss, _ := e.admin(t)
	e.project(t, "p")
	for path, want := range map[string]string{
		"/admin/audit?limit=x":                            "invalid argument: limit must be an integer",
		"/projects/p/contributions?limit=x":               "invalid argument: limit must be an integer",
		"/projects/p/search?q=a&rerank=maybe":             "invalid argument: rerank must be true or false",
		"/projects/p/contributions?include_superseded=no": "invalid argument: include_superseded must be true or false",
	} {
		r := boss.do("GET", path, nil, nil)
		wantErr(t, r, 400, "invalid_argument")
		if r.body["message"] != want {
			t.Errorf("%s: message = %v, want %q", path, r.body["message"], want)
		}
	}
}

// §C5: a title is not "similar" to a text that only shares a word once the
// accents are stripped (biến / biện); typing without accents still finds it.
func TestSimilar_AccentCollisionDropped(t *testing.T) {
	e := newEnv(t, "")
	c, _ := loggedIn(t, e, "ann")
	e.project(t, "p")
	post := func(body map[string]string) resp {
		r := c.do("POST", "/projects/p/contributions", body, nil)
		wantStatus(t, r, 201)
		return r
	}
	pool := post(map[string]string{"kind": "claim", "title": "PgBouncer pool_size cho orders: 20 ở ≤400 rps, 40 ở 1200 rps", "body": "Bản cập nhật sau phản biện."}).body["id"]
	first := post(map[string]string{"kind": "claim", "title": "Timeout HTTP client nội bộ có thể ghi đè bằng biến môi trường"})
	if s := array(t, first, "similar"); len(s) != 0 {
		t.Fatalf("similar = %v, want none (biến is not biện)", s)
	}
	if s := array(t, post(map[string]string{"kind": "claim", "title": "Dùng biến môi trường cho timeout"}), "similar"); len(s) != 1 || s[0]["id"] != first.body["id"] {
		t.Fatalf("similar of a real near-duplicate = %v", s)
	}
	ids := func(q string) []any {
		r := c.do("GET", "/projects/p/search?"+q, nil, nil)
		wantStatus(t, r, 200)
		var out []any
		for _, it := range array(t, r, "results") {
			out = append(out, it["id"])
		}
		return out
	}
	// Unaccented typing matches both, as before; the accented word puts the collision last.
	if got := ids("q=bien"); len(got) != 3 {
		t.Fatalf("q=bien = %v, want all three", got)
	}
	if got := ids("q=bi%E1%BA%BFn"); len(got) != 3 || got[2] != pool {
		t.Fatalf("q=biến = %v, want the pool claim %v last", got, pool)
	}
}
