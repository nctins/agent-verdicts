package authflow

import (
	"net/http"
	"net/url"
	"strings"
)

// OriginMatches reports whether r's Origin header names this server: its
// host equals the request Host or, behind a proxy that rewrites Host, the
// first X-Forwarded-Host. A missing or unparsable Origin does not match.
// Login and setup have no session (so no CSRF token) yet; this is their
// cross-site defence on the JSON API.
func OriginMatches(r *http.Request) bool {
	u, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || u.Host == "" {
		return false
	}
	fwd, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Host"), ",")
	fwd = strings.TrimSpace(fwd)
	return u.Host == r.Host || (fwd != "" && u.Host == fwd)
}
