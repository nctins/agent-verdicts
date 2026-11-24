package contribution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/platform/id"
	"agent-verdicts/internal/platform/textfold"
	"agent-verdicts/internal/project"
)

// ErrActorNotFound: actorID is empty or unknown. A caller error, distinct
// from ErrNotFound (which is about the contribution).
var ErrActorNotFound = errors.New("contribution: actor not found")

const (
	maxTitleRunes = 200
	maxBodyBytes  = 65536
)

type NewInput struct {
	ProjectID, ActorID string
	Kind               Kind // "" = claim
	Title, Body        string
	AnswersID          string
	Sources            []string
	SupersedesID       string // a claim of the same project this one replaces
	CanModerate        bool   // the caller holds content:Moderate on the project
}

// Limits for sources (spec 2026-11-15-trust-threshold-sources §4).
const (
	MaxSources     = 10
	maxSourceRunes = 500
)

// NormalizeSources trims and validates a list of evidence references: up to
// 10 items of 1-500 runes without control characters. Never returns nil.
func NormalizeSources(in []string) ([]string, error) {
	if len(in) > MaxSources {
		return nil, fmt.Errorf("%w: sources must have at most %d items", ErrInvalidArgument, MaxSources)
	}
	out := make([]string, len(in))
	for i, s := range in {
		s = strings.TrimSpace(s)
		switch {
		case !utf8.ValidString(s) || s == "" || utf8.RuneCountInString(s) > maxSourceRunes:
			return nil, fmt.Errorf("%w: each source must be 1-%d valid characters", ErrInvalidArgument, maxSourceRunes)
		case strings.ContainsFunc(s, unicode.IsControl):
			return nil, fmt.Errorf("%w: a source must not contain control characters", ErrInvalidArgument)
		}
		out[i] = s
	}
	return out, nil
}

// Create validates the input, confirms the project (project.ErrNotFound if
// missing) and the actor, checks answers_id (a question in the same
// project), then persists. Every check runs before any write. A
// contribution's kind never changes after creation, so checking answers_id
// outside the transaction is enough.
func Create(ctx context.Context, projects project.Repository, actors actor.Repository, repo Repository, in NewInput) (Contribution, error) {
	c := Contribution{
		ID: id.New(), ProjectID: in.ProjectID, ActorID: in.ActorID, Kind: in.Kind,
		Title: strings.TrimSpace(in.Title), Body: in.Body, AnswersID: in.AnswersID,
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond), // what timestamptz stores
	}
	c.UpdatedAt = c.CreatedAt
	if c.Kind == "" {
		c.Kind = KindClaim
	}
	switch {
	case c.Kind != KindClaim && c.Kind != KindQuestion:
		return Contribution{}, fmt.Errorf("%w: kind must be claim or question", ErrInvalidArgument)
	case c.Title == "" || utf8.RuneCountInString(c.Title) > maxTitleRunes:
		return Contribution{}, fmt.Errorf("%w: title must be 1-200 characters", ErrInvalidArgument)
	case !textfold.Valid(c.Title) || !textfold.Valid(c.Body):
		return Contribution{}, fmt.Errorf("%w: title and body must be valid UTF-8 without NUL bytes", ErrInvalidArgument)
	case len(c.Body) > maxBodyBytes:
		return Contribution{}, fmt.Errorf("%w: body must be at most 65536 bytes", ErrInvalidArgument)
	case c.Kind == KindQuestion && c.AnswersID != "":
		return Contribution{}, fmt.Errorf("%w: a question cannot answer another question", ErrInvalidArgument)
	}
	if err := project.ValidateID(c.ProjectID); err != nil {
		return Contribution{}, err
	}
	var err error
	if c.Sources, err = NormalizeSources(in.Sources); err != nil {
		return Contribution{}, err
	}
	if c.AnswersID != "" {
		q, err := repo.Get(ctx, c.ProjectID, c.AnswersID)
		if err != nil {
			return Contribution{}, err
		}
		if !q.VisibleTo(in.CanModerate) {
			return Contribution{}, ErrNotFound
		}
		if q.Kind != KindQuestion {
			return Contribution{}, fmt.Errorf("%w: answers_id must point to a question", ErrInvalidArgument)
		}
	}
	if in.SupersedesID != "" {
		if err := checkSupersedes(ctx, repo, in, c.Kind); err != nil {
			return Contribution{}, err
		}
	}
	if _, err := projects.Get(ctx, c.ProjectID); err != nil {
		return Contribution{}, fmt.Errorf("contribution: project %q: %w", c.ProjectID, err)
	}
	if err := requireActor(ctx, actors, c.ActorID); err != nil {
		return Contribution{}, err
	}
	if err := repo.Create(ctx, c, textfold.Fold(c.Title+"\n"+c.Body), in.SupersedesID); err != nil {
		return Contribution{}, err
	}
	return c, nil
}

// checkSupersedes enforces spec 2026-11-15-retract-supersede-moderate §4: both
// are claims of the same project, the old one has no replacement yet, and the
// caller wrote it or may moderate. The insert re-checks the second condition
// under a row lock.
func checkSupersedes(ctx context.Context, repo Repository, in NewInput, kind Kind) error {
	if kind != KindClaim {
		return fmt.Errorf("%w: only a claim can supersede a claim", ErrInvalidArgument)
	}
	old, err := repo.Get(ctx, in.ProjectID, in.SupersedesID)
	if err != nil {
		return err
	}
	switch {
	case !old.VisibleTo(in.CanModerate):
		return ErrNotFound
	case old.Kind != KindClaim:
		return fmt.Errorf("%w: supersedes_id must point to a claim", ErrInvalidArgument)
	case old.SupersededByID != "":
		return ErrAlreadySuperseded
	case old.ActorID != in.ActorID && !in.CanModerate:
		return fmt.Errorf("%w: only the author of a claim or a moderator can supersede it", access.ErrForbidden)
	}
	return nil
}

// Unsupersede undoes a supersession (the claim's author or a moderator); a
// claim that is not superseded is left alone.
func Unsupersede(ctx context.Context, repo Repository, projectID, id, actorID string, canModerate bool) error {
	if err := project.ValidateID(projectID); err != nil {
		return err
	}
	c, err := repo.Get(ctx, projectID, id)
	if err != nil {
		return err
	}
	switch {
	case !c.VisibleTo(canModerate):
		return ErrNotFound
	case c.ActorID != actorID && !canModerate:
		return fmt.Errorf("%w: only the author of a claim or a moderator can undo its supersession", access.ErrForbidden)
	case c.SupersededByID == "":
		return nil
	}
	return repo.ClearSuperseded(ctx, c.ID)
}

func requireActor(ctx context.Context, actors actor.Repository, actorID string) error {
	if actorID == "" {
		return ErrActorNotFound
	}
	_, err := actors.Get(ctx, actorID)
	if errors.Is(err, actor.ErrNotFound) {
		return ErrActorNotFound
	}
	if err != nil {
		return fmt.Errorf("contribution: check actor: %w", err)
	}
	return nil
}
