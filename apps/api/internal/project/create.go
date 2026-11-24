package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrAlreadyExists: Create was called with an id that already exists. An
// admin creating a project must see a clear error for a duplicate id rather
// than a silent no-op.
var ErrAlreadyExists = errors.New("project: already exists")

// Create creates a Project with an id chosen by the caller (unlike New,
// which generates one). It returns ErrAlreadyExists if the id is taken and
// never silently does nothing.
//
// With ErrAlreadyExists the returned Project is NOT empty: it is the
// existing project (read by the pre-check or the re-check below). This is a
// deliberate exception to the usual Go convention that other return values
// are meaningless when err != nil.
func Create(ctx context.Context, projects Repository, id, name, description string) (Project, error) {
	if err := ValidateID(id); err != nil {
		return Project{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = id
	}
	name, err := NormalizeName(name)
	if err != nil {
		return Project{}, err
	}
	description, err = NormalizeDescription(description)
	if err != nil {
		return Project{}, err
	}

	if existing, err := projects.Get(ctx, id); err == nil {
		return existing, ErrAlreadyExists
	} else if !errors.Is(err, ErrNotFound) {
		return Project{}, fmt.Errorf("project: create: check existing: %w", err)
	}

	p := Project{ID: id, Name: name, Description: description, MinFavorable: 1, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err := projects.Create(ctx, p); err != nil {
		// Get-then-Create is not transactional: two concurrent Creates of
		// the same new id can both see ErrNotFound and both insert, and the
		// loser hits the UNIQUE constraint although the project now exists.
		// To the caller that is still ErrAlreadyExists, not an operational
		// failure, so re-check before reporting a real error.
		if existing, getErr := projects.Get(ctx, id); getErr == nil {
			return existing, ErrAlreadyExists
		}
		return Project{}, fmt.Errorf("project: create: %w", err)
	}
	return p, nil
}

// Update changes name, description, threshold and/or archived state; a nil field is left unchanged.
// ErrNotFound if the project does not exist.
func Update(ctx context.Context, projects Repository, id string, name, description *string, minFavorable *int, archived *bool) (Project, error) {
	if err := ValidateID(id); err != nil {
		return Project{}, err
	}
	p, err := projects.Get(ctx, id)
	if err != nil {
		return Project{}, err
	}
	if name != nil {
		if p.Name, err = NormalizeName(*name); err != nil {
			return Project{}, err
		}
	}
	if description != nil {
		if p.Description, err = NormalizeDescription(*description); err != nil {
			return Project{}, err
		}
	}
	if minFavorable != nil {
		if *minFavorable < MinFavorableMin || *minFavorable > MinFavorableMax {
			return Project{}, fmt.Errorf("%w: min_favorable must be %d-%d", ErrInvalidField, MinFavorableMin, MinFavorableMax)
		}
		p.MinFavorable = *minFavorable
	}
	if err := projects.Update(ctx, id, p.Name, p.Description, p.MinFavorable); err != nil {
		return Project{}, err
	}
	if archived != nil && *archived != p.Archived() {
		var at *time.Time
		if *archived {
			now := time.Now().UTC().Truncate(time.Microsecond)
			at = &now
		}
		if err := projects.SetArchived(ctx, id, at); err != nil {
			return Project{}, err
		}
		p.ArchivedAt = at
	}
	return p, nil
}
