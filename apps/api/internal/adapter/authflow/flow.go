// Package authflow is the password login and first-run setup behind the JSON
// API (/api/v1), which the web UI uses. One Flow is built in cmd and holds
// the single login throttle and setup guard (failures count toward one
// lockout; a finished setup stays closed).
package authflow

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/adapter/httpapi/session"
	"agent-verdicts/internal/authentication"
)

var (
	// ErrLocked: too many recent failures for this username (or setup client).
	ErrLocked = errors.New("too many failed attempts, try again later")
	// ErrSetupClosed: first-run setup is not (or no longer) available.
	ErrSetupClosed = errors.New("setup already complete")
	// ErrBadSetupCode: the setup code does not match.
	ErrBadSetupCode = errors.New("invalid setup code")
)

type Flow struct {
	actors    actor.Repository
	logins    authentication.LoginRepository
	access    access.Repository
	setupCode string
	throttle  *loginThrottle
	setupMu   sync.Mutex // serializes Setup so only one first admin is created
	setupDone atomic.Bool
}

// New builds the shared flow. A non-empty setupCode opens first-run setup.
func New(actors actor.Repository, logins authentication.LoginRepository, acc access.Repository, setupCode string) *Flow {
	return &Flow{actors: actors, logins: logins, access: acc, setupCode: setupCode, throttle: newLoginThrottle()}
}

// SetLockout replaces the default lockout served after too many wrong
// passwords or setup codes. Call it before serving.
func (f *Flow) SetLockout(d time.Duration) { f.throttle.lockout = d }

// Login checks a username and password. Wrong credentials are
// authentication.ErrUnauthenticated; a (username, clientAddr) pair, an
// address or a username with too many recent failures gets ErrLocked without
// its password being checked (see loginThrottle). Any other
// error is infrastructure trouble and is not counted as a strike.
func (f *Flow) Login(ctx context.Context, username, password, clientAddr string) (actor.Actor, authentication.Login, error) {
	// Only well-formed usernames get a throttle entry: anything else can't
	// match a login, and bounding the key keeps memory bounded.
	key, keyErr := authentication.NormalizeUsername(username)
	if keyErr == nil && !f.throttle.Begin(key, clientAddr) {
		return actor.Actor{}, authentication.Login{}, ErrLocked
	}
	l, err := authentication.VerifyPassword(ctx, f.actors, f.logins, username, password)
	if err != nil {
		if !errors.Is(err, authentication.ErrUnauthenticated) && keyErr == nil {
			f.throttle.Release(key, clientAddr)
		}
		return actor.Actor{}, authentication.Login{}, err
	}
	f.throttle.Succeed(key, clientAddr)
	// Only a sign of life for the admin pages: a failed write must not block the login.
	if err := f.logins.TouchLogin(ctx, l.ActorID, time.Now()); err != nil {
		log.Printf("agentverdicts: login: record last login: %v", err)
	}
	a, err := f.actors.Get(ctx, l.ActorID)
	if err != nil {
		return actor.Actor{}, authentication.Login{}, fmt.Errorf("login: read actor: %w", err)
	}
	return a, l, nil
}

// NeedsSetup reports whether no active Human admin can log in with a
// password yet — i.e. nobody can administer the instance.
func NeedsSetup(ctx context.Context, logins authentication.LoginRepository, acc access.Repository) (bool, error) {
	admins, err := acc.AdminHumans(ctx) // active Humans holding admin on every project
	if err != nil {
		return false, err
	}
	for _, id := range admins {
		if _, err := logins.GetByActor(ctx, id); err == nil {
			return false, nil
		} else if !errors.Is(err, authentication.ErrLoginNotFound) {
			return false, err
		}
	}
	return true, nil
}

// SetupActive reports whether first-run setup is still open. While open it
// re-checks the database, so an admin created another way (the CLI) closes
// it at once; once closed it stays closed without further queries.
func (f *Flow) SetupActive(ctx context.Context) bool {
	if f.setupCode == "" || f.setupDone.Load() {
		return false
	}
	need, err := NeedsSetup(ctx, f.logins, f.access)
	if err != nil {
		log.Printf("agentverdicts: setup check: %v", err)
		return true // fail closed: keep reporting setup as pending
	}
	if !need {
		f.setupDone.Store(true)
	}
	return need
}

type SetupInput struct {
	Code, Name, Username, Password string
	// ClientAddr is authflow.ClientAddr(r); wrong codes are throttled per host.
	ClientAddr string
}

// setupThrottleKey limits wrong setup codes per client host, so a stranger
// guessing codes cannot lock the operator out. The "\x00" prefix keeps it
// apart from username keys in the shared throttle.
func setupThrottleKey(addr string) string { return "\x00setup:" + host(addr) }

// Setup creates the first admin and its password login. Errors:
// ErrSetupClosed, ErrLocked, ErrBadSetupCode, the actor/authentication
// validation errors, authentication.ErrUsernameTaken, or infrastructure.
func (f *Flow) Setup(ctx context.Context, in SetupInput) (actor.Actor, authentication.Login, error) {
	var none actor.Actor
	// One setup at a time, end to end: concurrent correct submissions see
	// the first one's result (ErrSetupClosed) instead of creating a second admin.
	f.setupMu.Lock()
	defer f.setupMu.Unlock()
	if !f.SetupActive(ctx) {
		return none, authentication.Login{}, ErrSetupClosed
	}
	key := setupThrottleKey(in.ClientAddr)
	if !f.throttle.Begin(key, in.ClientAddr) {
		return none, authentication.Login{}, ErrLocked
	}
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	if subtle.ConstantTimeCompare([]byte(code), []byte(strings.ToUpper(f.setupCode))) != 1 {
		return none, authentication.Login{}, ErrBadSetupCode
	}
	f.throttle.Release(key, in.ClientAddr) // a right code is not a strike

	name, err := actor.NormalizeName(in.Name)
	if err != nil {
		return none, authentication.Login{}, err
	}
	username, err := authentication.NormalizeUsername(in.Username)
	if err != nil {
		return none, authentication.Login{}, err
	}
	if err := authentication.ValidatePassword(in.Password); err != nil {
		return none, authentication.Login{}, err
	}
	// Check before creating anything so a taken name never leaves a
	// login-less admin behind.
	switch _, err := f.logins.GetByUsername(ctx, username); {
	case err == nil:
		return none, authentication.Login{}, authentication.ErrUsernameTaken
	case !errors.Is(err, authentication.ErrLoginNotFound):
		return none, authentication.Login{}, fmt.Errorf("setup: check username: %w", err)
	}

	a := actor.New(actor.TypeHuman)
	a.Name = name
	if _, err := authentication.CreateHuman(ctx, f.logins, f.actors, a, username, in.Password, false); err != nil {
		if errors.Is(err, actor.ErrNameTaken) {
			return none, authentication.Login{}, err
		}
		return none, authentication.Login{}, fmt.Errorf("setup: create admin: %w", err)
	}
	if err := access.GrantAdmin(ctx, f.access, a.ID); err != nil {
		return none, authentication.Login{}, fmt.Errorf("setup: grant admin: %w", err)
	}
	l, err := f.logins.GetByActor(ctx, a.ID)
	if err != nil {
		return none, authentication.Login{}, fmt.Errorf("setup: read login: %w", err)
	}
	f.setupDone.Store(true)
	log.Printf("agentverdicts: setup complete — first admin %q created", username)
	return a, l, nil
}

// sessionCookieName is the cookie carrying the session ID, used by the
// API (one session store, Path=/).
const sessionCookieName = "av_session"

// IsHTTPS reports whether the client reached us over HTTPS, directly or
// via a proxy saying so in X-Forwarded-Proto. Trusting the header is safe
// here: a forged value only makes the client's own browser withhold the
// cookie over plain HTTP.
func IsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func writeSessionCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: IsHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
}

func SetSessionCookie(w http.ResponseWriter, r *http.Request, sessionID string) {
	writeSessionCookie(w, r, sessionID, int(session.MaxLifetime.Seconds()))
}

func ClearSessionCookie(w http.ResponseWriter, r *http.Request) { writeSessionCookie(w, r, "", -1) }

// SessionID reads the av_session cookie value, if present.
func SessionID(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}
