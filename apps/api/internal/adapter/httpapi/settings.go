package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"agent-verdicts/internal/audit"
	"agent-verdicts/internal/setting"
)

// Search settings (spec §6.6). Responses carry only api_key_last4, never a
// key. The provider URLs are admin-entered and the server calls them: an
// accepted SSRF risk, since only admins can set them (spec §8).

type providerView struct {
	URL         string `json:"url"`
	Model       string `json:"model"`
	APIKeyLast4 string `json:"api_key_last4"`
}

type statusView struct {
	Pipeline      string   `json:"pipeline"`
	EmbeddedCount int      `json:"embedded_count"`
	TotalCount    int      `json:"total_count"`
	Reindexing    bool     `json:"reindexing"`
	Warnings      []string `json:"warnings"`
}

type settingsView struct {
	Embedding struct {
		providerView
		Dim int `json:"dim"`
	} `json:"embedding"`
	Rerank providerView `json:"rerank"`
	Status statusView   `json:"status"`
}

func (h *handler) writeSettings(w http.ResponseWriter, r *http.Request) {
	cur := h.d.Settings.Current()
	st, err := h.d.Search.Status(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	var v settingsView
	v.Embedding.providerView = providerView{cur.Embedding.URL, cur.Embedding.Model, cur.Embedding.KeyLast4}
	v.Embedding.Dim = cur.Embedding.Dim
	v.Rerank = providerView{cur.Rerank.URL, cur.Rerank.Model, cur.Rerank.KeyLast4}
	v.Status = statusView{st.Pipeline, st.EmbeddedCount, st.TotalCount, st.Reindexing, st.Warnings}
	if v.Status.Warnings == nil {
		v.Status.Warnings = []string{}
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *handler) getSettings(w http.ResponseWriter, r *http.Request) { h.writeSettings(w, r) }

type providerIn struct {
	URL    string          `json:"url"`
	Model  string          `json:"model"`
	APIKey json.RawMessage `json:"api_key"` // absent or blank = keep, null = delete, string = set (spec §6.6)
}

func (p providerIn) update() (setting.ProviderUpdate, error) {
	u := setting.ProviderUpdate{URL: p.URL, Model: p.Model}
	switch {
	case p.APIKey == nil:
	case bytes.Equal(p.APIKey, []byte("null")):
		u.APIKey = new(string)
	default:
		var k string
		if err := json.Unmarshal(p.APIKey, &k); err != nil {
			return u, fmt.Errorf("%w: api_key must be a string or null", setting.ErrInvalidArgument)
		}
		if k = strings.TrimSpace(k); k != "" { // blank keeps the key; only null deletes
			u.APIKey = &k
		}
	}
	return u, nil
}

// decodeSettings reads a PUT/test body; on failure it has written the error.
func decodeSettings(w http.ResponseWriter, r *http.Request) (emb, rer setting.ProviderUpdate, ok bool) {
	var in struct {
		Embedding providerIn `json:"embedding"`
		Rerank    providerIn `json:"rerank"`
	}
	if !decode(w, r, &in) {
		return emb, rer, false
	}
	var err error
	if emb, err = in.Embedding.update(); err == nil {
		rer, err = in.Rerank.update()
	}
	if err != nil {
		fail(w, r, err)
		return emb, rer, false
	}
	return emb, rer, true
}

// PUT /admin/settings/search: test the providers, then save.
func (h *handler) putSettings(w http.ResponseWriter, r *http.Request) {
	emb, rer, ok := decodeSettings(w, r)
	if !ok {
		return
	}
	if _, err := h.d.Settings.Update(r.Context(), emb, rer); err != nil {
		fail(w, r, err)
		return
	}
	cur := h.d.Settings.Current()
	h.record(r, audit.Event{Action: "settings.search.update", TargetType: "settings", TargetID: "search",
		Detail: map[string]any{"embedding_model": cur.Embedding.Model, "rerank_model": cur.Rerank.Model}})
	h.writeSettings(w, r)
}

// POST /admin/settings/search/test: test only, save nothing.
func (h *handler) testSettings(w http.ResponseWriter, r *http.Request) {
	emb, rer, ok := decodeSettings(w, r)
	if !ok {
		return
	}
	if err := h.d.Settings.Test(r.Context(), emb, rer); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /admin/settings/search/reindex: re-embed everything in the background.
func (h *handler) reindex(w http.ResponseWriter, r *http.Request) {
	if err := h.d.Worker.Reindex(r.Context()); err != nil {
		fail(w, r, err)
		return
	}
	h.record(r, audit.Event{Action: "settings.search.reindex", TargetType: "settings", TargetID: "search"})
	writeJSON(w, http.StatusAccepted, map[string]bool{"reindexing": true})
}
