package signal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/platform/textfold"
	"agent-verdicts/internal/project"
)

const maxBodyBytes = 65536

type NewInput struct {
	ProjectID, ContributionID, ActorID string
	Kind                               Kind
	Stance                             Stance
	Body                               string
	Sources                            []string
	ReplyTo                            string // optional: a signal on the same claim this answers (spec 2026-11-15-web-usability-2 §2)
	CanModerate                        bool   // the caller holds content:Moderate: hidden contributions are visible to it
}

type Result struct {
	Signal         Signal
	ReplacedStance *Stance // stance of the assessment just replaced; nil if none
	// SupersededByID names the newer claim when the signal landed on a
	// superseded one (still accepted; the sender is told where to go).
	SupersededByID string
	// NotCounted: the claim's author supported it; kept, but not counted
	// toward its state (spec 2026-11-21-ignore-self-support).
	NotCounted bool
}

// Create validates the input, confirms the contribution (a claim in the
// same project) and the actor (exists, not disabled), then persists. Every
// check runs before the write.
func Create(ctx context.Context, contributions contribution.Repository, actors actor.Repository, repo Repository, in NewInput) (Result, error) {
	s := Signal{
		ID: id.New(), ContributionID: in.ContributionID, ActorID: in.ActorID,
		Kind: in.Kind, Stance: in.Stance, Body: in.Body,
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond), // what timestamptz stores
	}
	switch {
	case s.Kind != KindChallenge && s.Kind != KindAssessment:
		return Result{}, fmt.Errorf("%w: kind must be challenge or assessment", ErrInvalidArgument)
	case s.Stance != StanceFavorable && s.Stance != StanceUnfavorable && s.Stance != StanceNeutral:
		return Result{}, fmt.Errorf("%w: stance must be favorable, unfavorable or neutral", ErrInvalidArgument)
	case len(s.Body) > maxBodyBytes:
		return Result{}, fmt.Errorf("%w: body must be at most 65536 bytes", ErrInvalidArgument)
	case !textfold.Valid(s.Body):
		return Result{}, fmt.Errorf("%w: body must be valid UTF-8 without NUL bytes", ErrInvalidArgument)
	case s.Kind == KindChallenge && s.Body == "":
		return Result{}, fmt.Errorf("%w: a challenge needs a body", ErrInvalidArgument)
	case s.Kind == KindChallenge && s.Stance == StanceFavorable:
		// Only new writes: favorable challenges from before this rule stay valid and keep counting.
		return Result{}, fmt.Errorf("%w: a challenge is unfavorable or neutral; to support a claim with an argument, use an assessment with a body", ErrInvalidArgument)
	case in.ReplyTo != "" && (s.Kind != KindChallenge || s.Stance != StanceNeutral):
		return Result{}, fmt.Errorf("%w: a reply must be a challenge with stance neutral", ErrInvalidArgument)
	}
	var err error
	if s.Sources, err = contribution.NormalizeSources(in.Sources); err != nil {
		return Result{}, err
	}
	if err := project.ValidateID(in.ProjectID); err != nil {
		return Result{}, err
	}
	c, err := contributions.Get(ctx, in.ProjectID, in.ContributionID)
	if err != nil {
		return Result{}, err
	}
	if !c.VisibleTo(in.CanModerate) {
		return Result{}, contribution.ErrNotFound
	}
	if c.Kind == contribution.KindQuestion {
		return Result{}, fmt.Errorf("%w: questions cannot be challenged or assessed; answer it instead", ErrInvalidArgument)
	}
	if err := requireActiveActor(ctx, actors, s.ActorID); err != nil {
		return Result{}, err
	}
	if in.ReplyTo != "" {
		if s.ReplyTo, err = replyRoot(ctx, repo, in.ReplyTo, in.ContributionID, in.CanModerate); err != nil {
			return Result{}, err
		}
	}
	replaced, err := repo.Insert(ctx, s)
	if err != nil {
		return Result{}, err
	}
	return Result{Signal: s, ReplacedStance: replaced, SupersededByID: c.SupersededByID,
		NotCounted: s.Stance == StanceFavorable && s.ActorID == c.ActorID}, nil
}

// replyRoot is the signal a reply to target is stored under: target itself,
// or its root when target is a reply (one level only). The same message
// covers an unknown, foreign, withdrawn and hidden target, so it leaks nothing.
func replyRoot(ctx context.Context, repo Repository, target, contributionID string, canModerate bool) (string, error) {
	t, err := repo.Get(ctx, target)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return "", err
	}
	if err != nil || t.ContributionID != contributionID || t.WithdrawnAt != nil || (t.HiddenAt != nil && !canModerate) {
		return "", fmt.Errorf("%w: reply_to must name an active signal on this claim", ErrInvalidArgument)
	}
	if t.ReplyTo != "" {
		return t.ReplyTo, nil
	}
	return t.ID, nil
}

func requireActiveActor(ctx context.Context, actors actor.Repository, actorID string) error {
	if actorID == "" {
		return ErrActorNotFound
	}
	a, err := actors.Get(ctx, actorID)
	if errors.Is(err, actor.ErrNotFound) || (err == nil && a.Disabled()) {
		return ErrActorNotFound
	}
	if err != nil {
		return fmt.Errorf("signal: check actor: %w", err)
	}
	return nil
}

// WithdrawInput names the signal to take back. ContributionID may be empty
// (any contribution of the project).
type WithdrawInput struct {
	ProjectID, ContributionID, SignalID, ActorID string
	CanModerate                                  bool
}

// find loads the signal and the claim it is on, ErrNotFound unless both are
// in the project and visible to the caller.
func find(ctx context.Context, contributions contribution.Repository, repo Repository, projectID, contributionID, signalID string, canModerate bool) (Signal, error) {
	if err := project.ValidateID(projectID); err != nil {
		return Signal{}, err
	}
	s, err := repo.Get(ctx, signalID)
	if err != nil {
		return Signal{}, err
	}
	if contributionID != "" && s.ContributionID != contributionID {
		return Signal{}, ErrNotFound
	}
	c, err := contributions.Get(ctx, projectID, s.ContributionID)
	if err != nil {
		return Signal{}, err
	}
	if !c.VisibleTo(canModerate) || (s.HiddenAt != nil && !canModerate) {
		return Signal{}, ErrNotFound
	}
	return s, nil
}

// Withdraw takes back the caller's own signal (spec 2026-11-15-retract-supersede-moderate §3).
// It is not restored; withdrawing twice does nothing.
func Withdraw(ctx context.Context, contributions contribution.Repository, actors actor.Repository, repo Repository, in WithdrawInput) error {
	s, err := find(ctx, contributions, repo, in.ProjectID, in.ContributionID, in.SignalID, in.CanModerate)
	if err != nil {
		return err
	}
	if err := requireActiveActor(ctx, actors, in.ActorID); err != nil {
		return err
	}
	if s.ActorID != in.ActorID {
		return ErrNotAuthor
	}
	if s.WithdrawnAt != nil {
		return nil
	}
	return repo.Withdraw(ctx, s.ID)
}

// SetHidden hides (with an optional reason) or unhides a signal (the caller
// has content:Moderate).
func SetHidden(ctx context.Context, contributions contribution.Repository, repo Repository, projectID, contributionID, signalID, by, reason string, hidden bool) error {
	s, err := find(ctx, contributions, repo, projectID, contributionID, signalID, true)
	if err != nil {
		return err
	}
	return repo.SetHidden(ctx, s.ID, by, reason, hidden)
}
