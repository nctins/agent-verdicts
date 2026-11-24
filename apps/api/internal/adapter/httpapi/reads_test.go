package httpapi

import "testing"

func TestReads_ManagersOnly(t *testing.T) {
	e := newEnv(t, "")
	e.project(t, "p1", "p2")
	mona, monaID := e.loggedIn(t, "mona")
	e.grant(t, monaID, "manager", "p1")
	e.grant(t, monaID, "contributor", "p2")

	r := mona.do("GET", "/projects/p1/reads", nil, nil)
	wantStatus(t, r, 200)
	if r.body["detail_days"] != float64(180) || r.body["never_read"] != float64(0) || r.body["from"] == r.body["to"] {
		t.Fatalf("reads = %s", r.raw)
	}
	wantStatus(t, mona.do("GET", "/projects/p1/reads?from=2026-01-01&to=2026-01-31", nil, nil), 200)
	wantErr(t, mona.do("GET", "/projects/p1/reads?from=2026-02-01&to=2026-01-31", nil, nil), 400, "invalid_argument")
	wantErr(t, mona.do("GET", "/projects/p1/reads?from=yesterday", nil, nil), 400, "invalid_argument")
	// A reader without member:Manage gets 403; an unreadable project is 404.
	wantErr(t, mona.do("GET", "/projects/p2/reads", nil, nil), 403, "forbidden")
	wantErr(t, mona.do("PUT", "/projects/p2/reads/retention", map[string]any{"detail_days": 60}, nil), 403, "forbidden")
	wantErr(t, mona.do("GET", "/projects/nope/reads", nil, nil), 404, "not_found")
	wantErr(t, mona.do("PUT", "/projects/nope/reads/retention", map[string]any{"detail_days": 60}, nil), 404, "not_found")
}

func TestReads_SetRetention(t *testing.T) {
	e := newEnv(t, "")
	e.project(t, "p1")
	mona, monaID := e.loggedIn(t, "mona")
	e.grant(t, monaID, "manager", "p1")

	r := mona.do("PUT", "/projects/p1/reads/retention", map[string]any{"detail_days": 365}, nil)
	wantStatus(t, r, 200)
	if r.body["detail_days"] != float64(365) || r.body["estimated_bytes"] != float64(0) || r.body["rows_per_day"] != float64(0) {
		t.Fatalf("retention = %s", r.raw)
	}
	if r := mona.do("GET", "/projects/p1/reads", nil, nil); r.body["detail_days"] != float64(365) {
		t.Fatalf("reads = %s", r.raw)
	}
	for _, body := range []map[string]any{{"detail_days": 29}, {"detail_days": 3651}, {}, {"detail_days": "60"}} {
		wantErr(t, mona.do("PUT", "/projects/p1/reads/retention", body, nil), 400, "invalid_argument")
	}
}
