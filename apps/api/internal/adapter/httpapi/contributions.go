package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/adapter/httpapi/session"
	"agent-verdicts/internal/adapter/view"
	"agent-verdicts/internal/audit"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/signal"
	"agent-verdicts/internal/trust"
)

// queryInt parses an optional integer query parameter (0 when absent).
func queryInt(r *http.Request, name string) (int, error) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%w: %s must be an integer", errInvalidArgument, name)
	}
	return n, nil
}

// queryBool parses an optional true/false query parameter (def when absent).
func queryBool(r *http.Request, name string, def bool) (bool, error) {
	switch r.URL.Query().Get(name) {
	case "":
		return def, nil
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	}
	return false, fmt.Errorf("%w: %s must be true or false", errInvalidArgument, name)
}

type listItem struct {
	view.ListItem
	ActorID string `json:"actor_id"`
}

// GET /projects/{p}/contributions. An unknown project is just empty.
func (h *handler) listContributions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := queryInt(r, "limit")
	if err != nil {
		fail(w, r, err)
		return
	}
	f := contribution.ListFilter{
		Kind: contribution.Kind(q.Get("kind")), ActorID: q.Get("actor"), Sort: contribution.Sort(q.Get("sort")),
		Cursor: q.Get("cursor"), Limit: limit,
	}
	if f.IncludeSuperseded, err = queryBool(r, "include_superseded", false); err != nil {
		fail(w, r, err)
		return
	}
	if f.ActorID == "me" {
		f.ActorID = me(r)
	}
	f.IncludeHidden = view.SeesHidden(f, me(r), canModerate(r))
	if s := q.Get("since"); s != "" {
		if f.Since, err = time.Parse(time.RFC3339, s); err != nil {
			fail(w, r, fmt.Errorf("%w: since must be an RFC 3339 timestamp", contribution.ErrInvalidArgument))
			return
		}
	}
	for _, s := range q["state"] {
		f.States = append(f.States, trust.State(s))
	}
	for _, s := range q["question_status"] {
		f.QuestionStatus = append(f.QuestionStatus, contribution.QuestionStatus(s))
	}
	page, err := h.d.Contributions.List(r.Context(), r.PathValue("p"), f)
	if err != nil {
		fail(w, r, err)
		return
	}
	shaped, err := view.ListItems(r.Context(), h.d.Contributions, h.d.Actors, page.Items)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := make([]listItem, len(shaped))
	for i, it := range shaped {
		out[i] = listItem{ListItem: it, ActorID: page.Items[i].ActorID}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "next_cursor": view.NextCursor(page.NextCursor)})
}

// GET /projects/{p}/contributions/{id}: the same view as reference_contribution.
func (h *handler) getContribution(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, err := view.Load(ctx, h.d.Contributions, r.PathValue("p"), r.PathValue("id"), me(r), canModerate(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	ref, err := view.NewReference(ctx, h.d.Contributions, h.d.Signals, h.d.Actors, c, canModerate(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ref)
}

// GET /projects/{p}/authors: who wrote the contributions the caller can see, by name.
func (h *handler) listAuthors(w http.ResponseWriter, r *http.Request) {
	as, err := h.d.Contributions.Authors(r.Context(), r.PathValue("p"), canModerate(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	type item struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	out := make([]item, len(as))
	for i, a := range as {
		out[i] = item{a.ID, a.Name}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// me is the logged-in actor; requireLogin guarantees it is set.
func me(r *http.Request) string { id, _ := session.ActorID(r.Context()); return id }

func (h *handler) created(r *http.Request, id string) view.Created {
	return view.Created{ID: id, ActorID: me(r), ActorName: actor.NewNameResolver(h.d.Actors).Name(r.Context(), me(r))}
}

// POST /projects/{p}/contributions.
func (h *handler) createContribution(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind         string   `json:"kind"`
		Title        string   `json:"title"`
		Body         string   `json:"body"`
		AnswersID    string   `json:"answers_id"`
		Sources      []string `json:"sources"`
		SupersedesID string   `json:"supersedes_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	similar := view.Similar(r.Context(), h.d.Search, r.PathValue("p"), in.Title, in.AnswersID, in.SupersedesID)
	c, err := contribution.Create(r.Context(), h.d.Projects, h.d.Actors, h.d.Contributions, contribution.NewInput{
		ProjectID: r.PathValue("p"), ActorID: me(r), Kind: contribution.Kind(in.Kind),
		Title: in.Title, Body: in.Body, AnswersID: in.AnswersID, Sources: in.Sources, SupersedesID: in.SupersedesID, CanModerate: canModerate(r),
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	h.d.Worker.Enqueue(c.ID)
	writeJSON(w, http.StatusCreated, view.CreatedContribution{Created: h.created(r, c.ID), Similar: similar})
}

// POST /projects/{p}/contributions/{id}/signals
func (h *handler) createSignal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind    string   `json:"kind"`
		Stance  string   `json:"stance"`
		Body    string   `json:"body"`
		Sources []string `json:"sources"`
		ReplyTo string   `json:"reply_to"`
	}
	if !decode(w, r, &in) {
		return
	}
	res, err := signal.Create(r.Context(), h.d.Contributions, h.d.Actors, h.d.Signals, signal.NewInput{
		ProjectID: r.PathValue("p"), ContributionID: r.PathValue("id"), ActorID: me(r),
		Kind: signal.Kind(in.Kind), Stance: signal.Stance(in.Stance), Body: in.Body, Sources: in.Sources, ReplyTo: in.ReplyTo, CanModerate: canModerate(r),
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, view.NewCreatedSignal(r.Context(), h.d.Contributions, r.PathValue("p"), h.created(r, res.Signal.ID), res, canModerate(r)))
}

// DELETE /projects/{p}/contributions/{id}/signals/{sid}: the author takes the signal back (204).
func (h *handler) withdrawSignal(w http.ResponseWriter, r *http.Request) {
	err := signal.Withdraw(r.Context(), h.d.Contributions, h.d.Actors, h.d.Signals, signal.WithdrawInput{
		ProjectID: r.PathValue("p"), ContributionID: r.PathValue("id"), SignalID: r.PathValue("sid"),
		ActorID: me(r), CanModerate: canModerate(r),
	})
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /projects/{p}/contributions/{id}/supersession: undo a supersession (204).
func (h *handler) deleteSupersession(w http.ResponseWriter, r *http.Request) {
	if err := contribution.Unsupersede(r.Context(), h.d.Contributions, r.PathValue("p"), r.PathValue("id"), me(r), canModerate(r)); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func hideAction(hidden bool) string {
	if hidden {
		return "content.hide"
	}
	return "content.unhide"
}

// hideInput is the body of both PUT …/hidden routes. reason is optional and
// only kept when hiding.
type hideInput struct {
	Hidden bool   `json:"hidden"`
	Reason string `json:"reason"`
}

// decodeHide reads and validates a hideInput; false when it already answered.
func decodeHide(w http.ResponseWriter, r *http.Request) (in hideInput, ok bool) {
	if !decode(w, r, &in) {
		return in, false
	}
	if !in.Hidden {
		in.Reason = ""
	}
	var err error
	if in.Reason, err = contribution.NormalizeHiddenReason(in.Reason); err != nil {
		fail(w, r, err)
		return in, false
	}
	return in, true
}

// withReason adds a non-empty hide reason to an audit detail.
func withReason(detail map[string]any, reason string) map[string]any {
	if reason != "" {
		detail["reason"] = reason
	}
	return detail
}

// PUT /projects/{p}/contributions/{id}/hidden {hidden, reason} (204).
func (h *handler) hideContribution(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeHide(w, r)
	if !ok {
		return
	}
	c, err := h.d.Contributions.Get(r.Context(), r.PathValue("p"), r.PathValue("id"))
	if err == nil {
		err = h.d.Contributions.SetHidden(r.Context(), c.ID, me(r), in.Reason, in.Hidden)
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	h.record(r, audit.Event{Action: hideAction(in.Hidden), TargetType: "contribution", TargetID: c.ID, ProjectID: r.PathValue("p"),
		Detail: withReason(map[string]any{"title": c.Title}, in.Reason)})
	w.WriteHeader(http.StatusNoContent)
}

// PUT /projects/{p}/contributions/{id}/signals/{sid}/hidden {hidden, reason} (204).
func (h *handler) hideSignal(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeHide(w, r)
	if !ok {
		return
	}
	if err := signal.SetHidden(r.Context(), h.d.Contributions, h.d.Signals, r.PathValue("p"), r.PathValue("id"), r.PathValue("sid"), me(r), in.Reason, in.Hidden); err != nil {
		fail(w, r, err)
		return
	}
	h.record(r, audit.Event{Action: hideAction(in.Hidden), TargetType: "signal", TargetID: r.PathValue("sid"), ProjectID: r.PathValue("p"),
		Detail: withReason(map[string]any{"contribution_id": r.PathValue("id")}, in.Reason)})
	w.WriteHeader(http.StatusNoContent)
}
