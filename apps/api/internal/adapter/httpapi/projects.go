package httpapi

import (
	"net/http"
	"slices"
	"strings"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/adapter/httpapi/session"
	"agent-verdicts/internal/adapter/view"
	"agent-verdicts/internal/project"
	"agent-verdicts/internal/trust"
)

type projectItem struct {
	ID                string              `json:"id"`
	Name              string              `json:"name"`
	Description       string              `json:"description"`
	MinFavorable      int                 `json:"min_favorable"`
	ContributionCount int                 `json:"contribution_count"`
	OpenQuestionCount int                 `json:"open_question_count"`
	LastActivityAt    *string             `json:"last_activity_at"`
	States            map[trust.State]int `json:"states"`
	CreatedAt         string              `json:"created_at"`
	Archived          bool                `json:"archived"`
}

// readableProjects is every project the caller may read.
func (h *handler) readableProjects(r *http.Request) ([]project.Project, error) {
	ps, err := h.d.Projects.List(r.Context())
	if err != nil {
		return nil, err
	}
	g := session.Grants(r.Context())
	return slices.DeleteFunc(ps, func(p project.Project) bool { return !g.Can(access.ContributionRead, p.ID) }), nil
}

// GET /projects and GET /admin/projects: every project the caller may read,
// with its counts, sorted by id. Archived ones are included and flagged, so
// a client can tell "archived" from "not found"; the home page leaves them out.
func (h *handler) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := h.readableProjects(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	counts, err := h.d.Contributions.CountByProject(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	out := []projectItem{}
	for _, p := range ps {
		n := counts[p.ID]
		states := map[trust.State]int{trust.StateFavorable: 0, trust.StateUnfavorable: 0, trust.StateConflict: 0, trust.StateInsufficientEvidence: 0}
		for st, c := range n.States {
			states[st] = c
		}
		var last *string
		if n.LastActivity != nil {
			ts := view.TS(*n.LastActivity)
			last = &ts
		}
		out = append(out, projectItem{LastActivityAt: last, ID: p.ID, Name: p.Name, Description: p.Description, MinFavorable: p.MinFavorable, ContributionCount: n.Contributions, OpenQuestionCount: n.OpenQuestions, States: states, CreatedAt: view.TS(p.CreatedAt), Archived: p.Archived()})
	}
	slices.SortFunc(out, func(a, b projectItem) int { return strings.Compare(a.ID, b.ID) })
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
