package authentication

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"agent-verdicts/internal/actor"
)

var (
	ErrLoginNotFound = errors.New("authentication: login not found")
	ErrUsernameTaken = errors.New("authentication: username already in use")
	ErrNotHuman      = errors.New("authentication: only Human actors have password logins")
)

// Login is a Human actor's username + password hash. ChangedAt moves on
// every password change; sessions remember it so a change ends
// every other session.
type Login struct {
	ActorID      string
	Username     string
	PasswordHash string
	ChangedAt    time.Time
	// MustChange makes the UI send the user to change their password
	// before anything else (set by an admin when creating / resetting).
	MustChange bool
}

type LoginRepository interface {
	// Upsert creates or replaces the login of l.ActorID; ErrUsernameTaken if
	// another actor already has l.Username.
	Upsert(ctx context.Context, l Login) error
	GetByUsername(ctx context.Context, username string) (Login, error)
	GetByActor(ctx context.Context, actorID string) (Login, error)
	// TouchLogin records a successful password login of actorID at at.
	TouchLogin(ctx context.Context, actorID string, at time.Time) error
}

// CreateHuman creates the Human actor a and its login, returning the
// password (generated when password == ""). Callers check the username
// first; a create racing for the same name still loses on the login table's
// UNIQUE username, and then, like any failure to write the login, a is
// renamed and disabled so no enabled person without a login stays behind
// and the name is free again.
func CreateHuman(ctx context.Context, logins LoginRepository, actors actor.Repository, a actor.Actor, username, password string, mustChange bool) (string, error) {
	if err := actors.Create(ctx, a); err != nil {
		if errors.Is(err, actor.ErrNameTaken) {
			return "", err // a caller-facing message, unwrapped
		}
		return "", fmt.Errorf("authentication: create human: %w", err)
	}
	password, err := SetLogin(ctx, logins, actors, a.ID, username, password, mustChange)
	if err != nil {
		// Names are unique: give the orphan's name back so the retry can use it.
		cleanup := context.WithoutCancel(ctx)
		base := []rune(a.Name)
		if len(base) > 50 {
			base = base[:50]
		}
		if rerr := actors.UpdateProfile(cleanup, a.ID, fmt.Sprintf("%s (failed %.8s)", string(base), a.ID)); rerr != nil {
			log.Printf("agentverdicts: rename login-less actor %s: %v", a.ID, rerr)
		}
		now := time.Now()
		if derr := actors.SetDisabled(cleanup, a.ID, &now); derr != nil {
			log.Printf("agentverdicts: disable login-less actor %s: %v", a.ID, derr)
		}
		return "", err
	}
	return password, nil
}

// ResetPassword gives a Human actor a fresh generated password and returns
// it. username may be "" to keep the actor's current username.
func ResetPassword(ctx context.Context, logins LoginRepository, actors actor.Repository, actorID, username string) (string, error) {
	return SetLogin(ctx, logins, actors, actorID, username, "", false)
}

// SetLogin sets a Human actor's username and password and returns the
// password. password == "" generates one; otherwise it must pass
// ValidatePassword. username == "" keeps the current username. mustChange
// makes the UI demand a new password at the next login.
func SetLogin(ctx context.Context, logins LoginRepository, actors actor.Repository, actorID, username, password string, mustChange bool) (string, error) {
	a, err := actors.Get(ctx, actorID)
	if errors.Is(err, actor.ErrNotFound) {
		return "", ErrActorNotFound
	}
	if err != nil {
		return "", fmt.Errorf("authentication: set login: %w", err)
	}
	if a.Type != actor.TypeHuman {
		return "", ErrNotHuman
	}
	if username == "" {
		existing, err := logins.GetByActor(ctx, actorID)
		if errors.Is(err, ErrLoginNotFound) {
			return "", ErrInvalidUsername
		}
		if err != nil {
			return "", fmt.Errorf("authentication: set login: %w", err)
		}
		username = existing.Username
	}
	username, err = NormalizeUsername(username)
	if err != nil {
		return "", err
	}
	if password == "" {
		password = GeneratePassword()
	} else if err := ValidatePassword(password); err != nil {
		return "", err
	}
	if err := setPassword(ctx, logins, actorID, username, password, mustChange); err != nil {
		return "", err
	}
	return password, nil
}

// ChangePassword replaces actorID's password after checking current.
func ChangePassword(ctx context.Context, logins LoginRepository, actorID, current, next string) error {
	l, err := logins.GetByActor(ctx, actorID)
	if errors.Is(err, ErrLoginNotFound) {
		return ErrUnauthenticated
	}
	if err != nil {
		return fmt.Errorf("authentication: change password: %w", err)
	}
	if !checkPassword(l.PasswordHash, current) {
		return ErrUnauthenticated
	}
	if err := ValidatePassword(next); err != nil {
		return err
	}
	return setPassword(ctx, logins, actorID, l.Username, next, false)
}

func setPassword(ctx context.Context, logins LoginRepository, actorID, username, pw string, mustChange bool) error {
	hash, err := HashPassword(pw)
	if err != nil {
		return err
	}
	return logins.Upsert(ctx, Login{ActorID: actorID, Username: username, PasswordHash: hash, ChangedAt: time.Now(), MustChange: mustChange})
}

// VerifyPassword checks a login. Every credential failure —
// unknown or malformed username, wrong password, disabled or missing actor
// — is the same ErrUnauthenticated, and an unknown username still pays the
// hashing cost. Infrastructure errors come back wrapped, distinct from
// ErrUnauthenticated, so callers do not count them as a wrong password.
func VerifyPassword(ctx context.Context, actors actor.Repository, logins LoginRepository, username, password string) (Login, error) {
	username, err := NormalizeUsername(username)
	if err != nil {
		checkPassword(dummyHash, password)
		return Login{}, ErrUnauthenticated
	}
	l, err := logins.GetByUsername(ctx, username)
	if errors.Is(err, ErrLoginNotFound) {
		checkPassword(dummyHash, password)
		return Login{}, ErrUnauthenticated
	}
	if err != nil {
		return Login{}, fmt.Errorf("authentication: verify password: %w", err)
	}
	if !checkPassword(l.PasswordHash, password) {
		return Login{}, ErrUnauthenticated
	}
	a, err := actors.Get(ctx, l.ActorID)
	if errors.Is(err, actor.ErrNotFound) {
		return Login{}, ErrUnauthenticated
	}
	if err != nil {
		return Login{}, fmt.Errorf("authentication: verify password: resolve actor: %w", err)
	}
	if a.Disabled() {
		return Login{}, ErrUnauthenticated
	}
	return l, nil
}
