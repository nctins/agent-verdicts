package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"agent-verdicts/internal/audit"
	"agent-verdicts/internal/reads"
)

type readCount struct {
	Reads   int `json:"reads"`
	Readers int `json:"readers"`
}

type readItem struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Kind       string    `json:"kind"`
	State      *string   `json:"state"`
	Superseded bool      `json:"superseded"`
	Observe    readCount `json:"observe"`
	Reference  readCount `json:"reference"`
	SearchHit  struct {
		Reads int `json:"reads"`
	} `json:"search_hit"`
}

type readPeriod struct {
	Day       string `json:"day,omitempty"`
	Month     string `json:"month,omitempty"`
	Observe   int    `json:"observe"`
	Reference int    `json:"reference"`
	SearchHit int    `json:"search_hit"`
}

func utcToday() time.Time {
	t, _ := time.Parse(time.DateOnly, time.Now().UTC().Format(time.DateOnly))
	return t
}

// queryDate parses ?name=YYYY-MM-DD, or returns def when it is absent.
func queryDate(r *http.Request, name string, def time.Time) (time.Time, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, nil
	}
	t, err := time.Parse(time.DateOnly, v)
	if err != nil {
		return t, fmt.Errorf("%w: %s must be YYYY-MM-DD", errInvalidArgument, name)
	}
	return t, nil
}

// GET /projects/{p}/reads: how often agents read the project's
// contributions over MCP, for its managers. No per-actor fields.
func (h *handler) getReads(w http.ResponseWriter, r *http.Request) {
	today := utcToday()
	to, err := queryDate(r, "to", today)
	if err != nil {
		fail(w, r, err)
		return
	}
	from, err := queryDate(r, "from", to.AddDate(0, 0, -29))
	if err != nil {
		fail(w, r, err)
		return
	}
	if from.After(to) {
		fail(w, r, fmt.Errorf("%w: from must not be after to", errInvalidArgument))
		return
	}
	s, err := h.d.Reads.Stats(r.Context(), r.PathValue("p"), from, to)
	if err != nil {
		fail(w, r, err)
		return
	}
	period := func(p reads.Period, month bool) readPeriod {
		out := readPeriod{Observe: p.Observe, Reference: p.Reference, SearchHit: p.SearchHit}
		if month {
			out.Month = p.Start.Format(time.DateOnly)
		} else {
			out.Day = p.Start.Format(time.DateOnly)
		}
		return out
	}
	daily, monthly, items := []readPeriod{}, []readPeriod{}, []readItem{}
	for _, p := range s.Daily {
		daily = append(daily, period(p, false))
	}
	for _, p := range s.Monthly {
		monthly = append(monthly, period(p, true))
	}
	for _, it := range s.Items {
		x := readItem{ID: it.ID, Title: it.Title, Kind: it.Kind, State: it.State, Superseded: it.Superseded,
			Observe: readCount(it.Observe), Reference: readCount(it.Reference)}
		x.SearchHit.Reads = it.SearchHits
		items = append(items, x)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": from.Format(time.DateOnly), "to": to.Format(time.DateOnly),
		"detail_days": s.DetailDays, "detail_from": reads.DetailFrom(today, s.DetailDays).Format(time.DateOnly),
		"daily": daily, "monthly": monthly, "items": items, "never_read": s.NeverRead,
	})
}

// PUT /projects/{p}/reads/retention: how long daily detail is kept, with the size it implies.
func (h *handler) putReadRetention(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DetailDays *int `json:"detail_days"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.DetailDays == nil {
		fail(w, r, fmt.Errorf("%w: detail_days is required", errInvalidArgument))
		return
	}
	if err := reads.ValidateDetailDays(*in.DetailDays); err != nil {
		fail(w, r, err)
		return
	}
	p := r.PathValue("p")
	ret, err := h.d.Reads.SetDetailDays(r.Context(), p, *in.DetailDays, utcToday())
	if err != nil {
		fail(w, r, err)
		return
	}
	h.record(r, audit.Event{Action: "project.reads_retention", TargetType: "project", TargetID: p, ProjectID: p, Detail: map[string]any{"detail_days": ret.DetailDays}})
	writeJSON(w, http.StatusOK, map[string]any{"detail_days": ret.DetailDays, "estimated_bytes": ret.EstimatedBytes, "rows_per_day": ret.RowsPerDay})
}
