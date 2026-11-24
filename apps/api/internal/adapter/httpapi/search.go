package httpapi

import (
	"net/http"

	"agent-verdicts/internal/adapter/view"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/search"
	"agent-verdicts/internal/trust"
)

// GET /projects/{p}/search?q=&kind=&limit=&rerank= (rerank defaults to true).
func (h *handler) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := queryInt(r, "limit")
	if err != nil {
		fail(w, r, err)
		return
	}
	rerank, err := queryBool(r, "rerank", true)
	if err != nil {
		fail(w, r, err)
		return
	}
	superseded, err := queryBool(r, "include_superseded", false)
	if err != nil {
		fail(w, r, err)
		return
	}
	f := search.Filters{Kind: contribution.Kind(q.Get("kind")), IncludeSuperseded: superseded, IncludeHidden: canModerate(r)}
	for _, s := range q["state"] {
		f.States = append(f.States, trust.State(s))
	}
	for _, s := range q["question_status"] {
		f.QuestionStatus = append(f.QuestionStatus, contribution.QuestionStatus(s))
	}
	results, degraded, err := view.Search(r.Context(), h.d.Search, search.Query{
		ProjectID: r.PathValue("p"), Text: q.Get("q"), Limit: limit, Rerank: rerank, Filters: f,
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "degraded": degraded})
}
