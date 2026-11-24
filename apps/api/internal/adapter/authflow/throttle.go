package authflow

import (
	"net"
	"sync"
	"time"
)

// loginThrottle caps password attempts on three levels (spec
// 2026-11-15-audit-tokens-sessions D9), each locking for lockout (default loginLockout) once
// its limit is reached within loginWindow:
//
//   - pair: pairMaxFailures for one (username, client IP);
//   - ip: ipMaxFailures from one client IP across any usernames;
//   - user: userMaxFailures for one username across all IPs.
//
// A stranger therefore cannot lock a known username by guessing from his own
// address, short of a distributed attack of userMaxFailures attempts. An
// attempt is reserved *before* the slow password check (Begin), so parallel
// requests cannot all slip past the limits; a correct password clears the
// pair's count and hands back the attempt's share of the other two
// (Succeed); an attempt that failed for an infrastructure reason is handed
// back everywhere (Release).
//
// ponytail: in-memory and per instance, so a restart clears every count and
// two instances do not share them; at most maxThrottleEntries live counters.
// Move the counters to Postgres (or a shared cache) when the api runs on
// more than one instance or is exposed beyond a small team.
type loginThrottle struct {
	mu      sync.Mutex
	fails   map[string]*throttleEntry
	now     func() time.Time
	lockout time.Duration
}

type throttleEntry struct {
	count int
	start time.Time // of the current counting window
	until time.Time // lockout end; zero = not locked
}

const (
	pairMaxFailures    = 5
	ipMaxFailures      = 30
	userMaxFailures    = 100
	loginWindow        = 15 * time.Minute
	loginLockout       = 15 * time.Minute
	maxThrottleEntries = 10_000
)

// MaxFailures is the failures one (username, IP) pair may make before it is locked.
const MaxFailures = pairMaxFailures

func newLoginThrottle() *loginThrottle {
	return &loginThrottle{fails: map[string]*throttleEntry{}, now: time.Now, lockout: loginLockout}
}

// level is one counter: its key and the limit that locks it.
type level struct {
	key   string
	limit int
}

// host is addr without its port: RemoteAddr carries one, X-Real-IP does not.
func host(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

// levels are the counters one attempt touches. The "\x00" keeps them apart
// from each other and from setupThrottleKey's keys.
func levels(username, addr string) []level {
	ip := host(addr)
	return []level{
		{"p\x00" + username + "\x00" + ip, pairMaxFailures},
		{"i\x00" + ip, ipMaxFailures},
		{"u\x00" + username, userMaxFailures},
	}
}

// Begin reserves one attempt; false means the pair, the IP or the username is locked out.
func (t *loginThrottle) Begin(username, addr string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	ls := levels(username, addr)
	for _, l := range ls {
		if e, ok := t.fails[l.key]; ok && now.Before(e.until) {
			return false
		}
	}
	for _, l := range ls {
		e, ok := t.fails[l.key]
		if ok && ((e.until.IsZero() && now.Sub(e.start) >= loginWindow) || (!e.until.IsZero() && !now.Before(e.until))) {
			delete(t.fails, l.key) // window over or lockout served: start afresh
			ok = false
		}
		if !ok {
			if len(t.fails) >= maxThrottleEntries {
				t.pruneLocked(now)
			}
			e = &throttleEntry{start: now}
			t.fails[l.key] = e
		}
		e.count++
		if e.count >= l.limit {
			e.until = now.Add(t.lockout)
		}
	}
	return true
}

// pruneLocked bounds memory under spraying by dropping entries that are not
// an active lockout — never the lockouts themselves.
func (t *loginThrottle) pruneLocked(now time.Time) {
	for k, e := range t.fails {
		if e.until.IsZero() || now.After(e.until) {
			delete(t.fails, k)
		}
	}
}

// Succeed clears the pair's count after a correct password; the attempt no
// longer counts against the IP and the username either.
func (t *loginThrottle) Succeed(username, addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ls := levels(username, addr)
	delete(t.fails, ls[0].key)
	t.releaseLocked(ls[1:])
}

// Release hands back an attempt that never really tested the password.
func (t *loginThrottle) Release(username, addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.releaseLocked(levels(username, addr))
}

func (t *loginThrottle) releaseLocked(ls []level) {
	for _, l := range ls {
		if e, ok := t.fails[l.key]; ok && e.count > 0 {
			e.count--
			if e.count < l.limit {
				e.until = time.Time{}
			}
		}
	}
}
