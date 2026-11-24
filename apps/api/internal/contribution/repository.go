package contribution

import (
	"context"
	"fmt"
	"slices"
	"time"

	"agent-verdicts/internal/platform/textfold"
	"agent-verdicts/internal/trust"
)

type Sort string

const (
	SortNewest  Sort = "newest"  // created_at descending
	SortDebated Sort = "debated" // total signals descending
	SortLeast   Sort = "least"   // total signals ascending
	SortActive  Sort = "active"  // updated_at descending
)

const (
	DefaultLimit = 20
	MaxLimit     = 100
)

// ListFilter filters List. States applies only to claims and QuestionStatus
// only to questions: when either is set, a claim passes only if States
// contains its state, and a question only if QuestionStatus contains its
// status. So States alone drops every question, QuestionStatus alone drops
// every claim, and both give the union of the two groups. Kind, if set,
// narrows further.
type ListFilter struct {
	Kind           Kind // "" = all
	States         []trust.State
	QuestionStatus []QuestionStatus
	ActorID        string
	Since          time.Time // zero = no bound; else only updated_at strictly after
	Sort           Sort      // "" = newest
	// Superseded and hidden claims are left out unless asked for.
	IncludeSuperseded bool
	IncludeHidden     bool // moderators only
	Cursor            string
	Limit             int // 0 → DefaultLimit; > MaxLimit → MaxLimit
}

// Normalize applies defaults, clamps Limit and validates enum values. The
// repository calls it at the start of List so every adapter shares one rule.
func (f ListFilter) Normalize() (ListFilter, error) {
	if f.Sort == "" {
		f.Sort = SortNewest
	}
	if !slices.Contains([]Sort{SortNewest, SortDebated, SortLeast, SortActive}, f.Sort) {
		return f, fmt.Errorf("%w: sort must be newest, debated, least or active", ErrInvalidArgument)
	}
	if f.Kind != "" && f.Kind != KindClaim && f.Kind != KindQuestion {
		return f, fmt.Errorf("%w: kind must be claim or question", ErrInvalidArgument)
	}
	if err := ValidateStates(f.States, f.QuestionStatus); err != nil {
		return f, err
	}
	if !textfold.Valid(f.ActorID) {
		return f, fmt.Errorf("%w: actor must be valid UTF-8 without NUL bytes", ErrInvalidArgument)
	}
	switch {
	case f.Limit <= 0:
		f.Limit = DefaultLimit
	case f.Limit > MaxLimit:
		f.Limit = MaxLimit
	}
	return f, nil
}

// ValidateStates checks the enum values of a state / question_status filter
// (shared by the list and the search endpoints).
func ValidateStates(states []trust.State, statuses []QuestionStatus) error {
	for _, s := range states {
		if !slices.Contains([]trust.State{trust.StateFavorable, trust.StateUnfavorable, trust.StateConflict, trust.StateInsufficientEvidence}, s) {
			return fmt.Errorf("%w: unknown state %q", ErrInvalidArgument, s)
		}
	}
	for _, s := range statuses {
		if !slices.Contains([]QuestionStatus{QuestionOpen, QuestionAnswered, QuestionResolved}, s) {
			return fmt.Errorf("%w: unknown question status %q", ErrInvalidArgument, s)
		}
	}
	return nil
}

// Author is an actor that wrote a contribution.
type Author struct{ ID, Name string }

type Page struct {
	Items      []Contribution
	NextCursor string // "" when there is nothing more
}

type Repository interface {
	// Create writes c with its folded searchText for lexical search. A
	// non-empty supersedes marks that claim superseded by c in the same
	// transaction (ErrAlreadySuperseded if it already is).
	Create(ctx context.Context, c Contribution, searchText, supersedes string) error
	// ClearSuperseded undoes a supersession; a no-op on a claim that is not superseded.
	ClearSuperseded(ctx context.Context, id string) error
	// SetHidden hides (by = moderator, with an optional reason) or unhides a
	// contribution; unhiding clears the reason.
	SetHidden(ctx context.Context, id, by, reason string, hidden bool) error
	// Get: ErrNotFound when the id is unknown, malformed, or in another project.
	Get(ctx context.Context, projectID, id string) (Contribution, error)
	// GetByID ignores the project (background jobs); ErrNotFound like Get.
	GetByID(ctx context.Context, id string) (Contribution, error)
	// List: keyset pagination by f.Sort; a broken cursor or one from another sort → ErrBadCursor.
	List(ctx context.Context, projectID string, f ListFilter) (Page, error)
	// Answers are ordered by trust.Rank, then created_at, then id; hidden ones only with includeHidden.
	Answers(ctx context.Context, questionID string, includeHidden bool) ([]Contribution, error)
	// QuestionInfo returns one entry per requested id (open when unanswered); hidden answers are ignored.
	QuestionInfo(ctx context.Context, questionIDs []string) (map[string]QuestionInfo, error)
	// Authors is every actor with at least one contribution in projectID (hidden ones only with includeHidden), by name then id.
	Authors(ctx context.Context, projectID string, includeHidden bool) ([]Author, error)
	CountByProject(ctx context.Context) (map[string]ProjectCounts, error)
	// Recent is the newest limit contributions in projectIDs (nil = every project).
	Recent(ctx context.Context, projectIDs []string, limit int) ([]Contribution, error)
	// Attention counts what an admin should look at (nil projectIDs = every
	// project); like the totals, hidden and superseded contributions are left out.
	Attention(ctx context.Context, projectIDs []string, now time.Time) (Attention, error)
}

// Attention is spec 2026-11-22-admin-ui-redesign A4: claims in Conflict that
// changed in the last 7 days, and questions still open 14 days after they were asked.
type Attention struct {
	Conflicts7d    int `json:"conflicts_7d"`
	StaleQuestions int `json:"stale_questions"`
}
