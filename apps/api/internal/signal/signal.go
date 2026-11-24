// Package signal defines Signal: a challenge or assessment on a claim, with
// a declared stance (favorable / unfavorable / neutral). Each signal updates
// the contribution's counters in the same transaction; each actor has at
// most one active assessment per contribution.
package signal

import (
	"fmt"
	"time"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/contribution"
)

type Kind string

const (
	KindChallenge  Kind = "challenge"
	KindAssessment Kind = "assessment"
)

type Stance string

const (
	StanceFavorable   Stance = "favorable"
	StanceUnfavorable Stance = "unfavorable"
	StanceNeutral     Stance = "neutral"
)

type Signal struct {
	ID, ContributionID, ActorID string
	Kind                        Kind
	Stance                      Stance
	Body                        string
	Sources                     []string
	ReplyTo                     string // challenges only: the root signal on the same claim this answers ("" = not a reply)
	CreatedAt                   time.Time
	SupersededAt                *time.Time // assessments only: replaced by a newer one from the same actor
	WithdrawnAt                 *time.Time // the author took it back; never restored
	HiddenAt                    *time.Time // hidden by a moderator
	HiddenReason                string     // why, as the moderator put it
	ActorDisabled               bool       // its sender is disabled now (read-only, set by ListByContribution and Get)
}

// Active reports whether the signal counts toward the contribution's trust
// state (spec 2026-11-15-retract-supersede-moderate D1).
func (s Signal) Active() bool {
	return s.SupersededAt == nil && s.WithdrawnAt == nil && s.HiddenAt == nil && !s.ActorDisabled
}

var (
	ErrInvalidArgument = contribution.ErrInvalidArgument // shared sentinel
	// ErrActorNotFound: the actor is empty, unknown or disabled.
	ErrActorNotFound = contribution.ErrActorNotFound
	// ErrNotFound: no such signal on that contribution (or hidden from the caller).
	ErrNotFound = contribution.ErrNotFound
	// ErrNotAuthor: only the sender can withdraw a signal.
	ErrNotAuthor = fmt.Errorf("%w: only the author can withdraw a signal", access.ErrForbidden)
)
