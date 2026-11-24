package httpapi

import (
	"net/http"
	"time"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/adapter/httpapi/session"
	"agent-verdicts/internal/adapter/view"
	"agent-verdicts/internal/audit"
)

// record logs e after a successful operation; the actor defaults to the
// caller. A failed write is logged, never returned (audit.Record, D2).
func (h *handler) record(r *http.Request, e audit.Event) {
	if e.ActorID == "" {
		e.ActorID = me(r)
	}
	audit.Record(r.Context(), h.d.Audit, e)
}

type auditItem struct {
	ID         string         `json:"id"`
	At         string         `json:"at"`
	ActorID    *string        `json:"actor_id"` // null: the operator CLI
	ActorName  string         `json:"actor_name"`
	Action     string         `json:"action"`
	TargetType string         `json:"target_type"`
	TargetID   string         `json:"target_id"`
	ProjectID  *string        `json:"project_id"`
	Detail     map[string]any `json:"detail"`
}

// GET /admin/audit?actor=&project=&action=&from=&to=&cursor=&limit=: newest first.
// actor:Manage sees everything; a member manager only the assignment.* and
// content.* events of the projects it manages.
func (h *handler) listAudit(w http.ResponseWriter, r *http.Request) {
	ctx, q := r.Context(), r.URL.Query()
	limit, err := queryInt(r, "limit")
	if err != nil {
		fail(w, r, err)
		return
	}
	from, err := queryDate(r, "from", time.Time{})
	if err != nil {
		fail(w, r, err)
		return
	}
	to, err := queryDate(r, "to", time.Time{})
	if err != nil {
		fail(w, r, err)
		return
	}
	f := audit.Filter{ActorID: q.Get("actor"), ProjectID: q.Get("project"), Action: q.Get("action"), From: from, To: to, Cursor: q.Get("cursor"), Limit: limit}
	if g := session.Grants(ctx); !g.Can(access.ActorManage, "") {
		f.Restrict = true
		f.AllProjects, f.Projects = g.Managed()
	}
	page, err := h.d.Audit.List(ctx, f)
	if err != nil {
		fail(w, r, err)
		return
	}
	names := actor.NewNameResolver(h.d.Actors)
	items := make([]auditItem, len(page.Items))
	for i, e := range page.Items {
		it := auditItem{ID: e.ID, At: view.TS(e.At), Action: e.Action, TargetType: e.TargetType, TargetID: e.TargetID, Detail: e.Detail}
		if e.ActorID != "" {
			it.ActorID, it.ActorName = &e.ActorID, names.Name(ctx, e.ActorID)
		}
		if e.ProjectID != "" {
			it.ProjectID = &e.ProjectID
		}
		items[i] = it
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": view.NextCursor(page.NextCursor)})
}
