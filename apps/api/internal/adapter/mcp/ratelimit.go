package mcp

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"agent-verdicts/internal/authentication"
)

// DefaultRatePerMin is the requests a single actor may make to /mcp per minute.
const DefaultRatePerMin = 300

const (
	windowLength     = time.Minute
	maxRateEntries   = 10_000
	rateLimitMessage = "rate limit exceeded, retry later\n"
)

// rateLimiter is a fixed-window counter per actor (simpler than a token
// bucket; a burst of up to twice the limit is possible across a window
// edge, which is fine for protecting a project from a runaway loop).
//
// ponytail: in memory and per instance: a restart clears the counts and two
// instances each allow the full rate. At most maxRateEntries actors are
// tracked (expired windows are dropped when full). Move the counters to
// Postgres or a shared cache when the api runs on more than one instance.
type rateLimiter struct {
	mu      sync.Mutex
	perMin  int
	windows map[string]*rateWindow
	now     func() time.Time
}

type rateWindow struct {
	start time.Time
	count int
}

func newRateLimiter(perMin int) *rateLimiter {
	return &rateLimiter{perMin: perMin, windows: map[string]*rateWindow{}, now: time.Now}
}

// allow counts one request of actorID; when over the limit it returns how
// long until the window ends.
func (l *rateLimiter) allow(actorID string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w, found := l.windows[actorID]
	if found && now.Sub(w.start) >= windowLength {
		found = false
	}
	if !found {
		if len(l.windows) >= maxRateEntries {
			for k, o := range l.windows {
				if now.Sub(o.start) >= windowLength {
					delete(l.windows, k)
				}
			}
		}
		w = &rateWindow{start: now}
		l.windows[actorID] = w
	}
	if w.count >= l.perMin {
		return false, w.start.Add(windowLength).Sub(now)
	}
	w.count++
	return true, 0
}

// rateLimit answers 429 with Retry-After once an authenticated actor
// exceeds perMin requests in a minute; perMin <= 0 turns it off. It sits
// after the bearer check, so an anonymous caller never reaches it.
func rateLimit(perMin int, next http.Handler) http.Handler {
	if perMin <= 0 {
		return next
	}
	l := newRateLimiter(perMin)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := authentication.ActorID(r.Context()); ok {
			if ok, wait := l.allow(id); !ok {
				secs := int((wait + time.Second - 1) / time.Second)
				w.Header().Set("Retry-After", fmt.Sprint(max(secs, 1)))
				http.Error(w, rateLimitMessage, http.StatusTooManyRequests)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
