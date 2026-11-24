package main

import "net/http"

// newMux is the server's HTTP surface: health check, MCP, the JSON API
// (served under /api/v1 with the prefix stripped). The web UI is a separate
// image; anything else is a 404.
func newMux(mcp, api http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("/mcp/{project}", mcp)
	notFound := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "MCP endpoint is /mcp/<project>", http.StatusNotFound)
	}
	mux.HandleFunc("/mcp", notFound)
	mux.HandleFunc("/mcp/", notFound)
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", api))
	return mux
}
