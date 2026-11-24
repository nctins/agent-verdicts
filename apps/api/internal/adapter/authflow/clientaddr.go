package authflow

import (
	"net"
	"net/http"
	"strings"
)

// ClientAddr is the client's address for throttling: X-Real-IP when it holds
// an IP (nginx in docker/ui overwrites it with $remote_addr), else RemoteAddr.
// Trustworthy only while the api port is not published (deploy/compose.yaml).
func ClientAddr(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(ip) != nil {
		return ip
	}
	return r.RemoteAddr
}
