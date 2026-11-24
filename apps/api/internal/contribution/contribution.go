// Package contribution defines Contribution: a claim or a question in a
// Project, written by an Actor. A claim can answer a question in the same
// project (answers_id). A claim's trust state is derived from its signal
// counts (trust.StateOf); a question has no trust state but a
// QuestionStatus derived from its answers.
package contribution

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"agent-verdicts/internal/platform/textfold"
	"agent-verdicts/internal/trust"
)

type Kind string

const (
	KindClaim    Kind = "claim"
	KindQuestion Kind = "question"
)

type Contribution struct {
	ID, ProjectID, ActorID string
	Kind                   Kind
	Title                  string
	Body                   string
	AnswersID              string       // "" when it answers no question
	Counts                 trust.Counts // always zero for a question
	Sources                []string     // evidence references, at most MaxSources
	MinFavorable           int          // the project's threshold, read with the row
	CreatedAt              time.Time
	UpdatedAt              time.Time // creation, or the newest signal
	SupersededByID         string    // "" unless a newer claim replaced this one
	SupersededAt           *time.Time
	HiddenAt               *time.Time // hidden by a moderator
	HiddenReason           string     // why, as the moderator put it; "" when not hidden or none given
}

// VisibleTo reports whether a caller may see c: a hidden contribution is
// visible to moderators (content:Moderate) only.
func (c Contribution) VisibleTo(moderator bool) bool { return c.HiddenAt == nil || moderator }

// MaxHiddenReasonRunes bounds the reason a moderator gives for hiding.
const MaxHiddenReasonRunes = 500

// NormalizeHiddenReason trims the optional reason of a hide request (shared
// by contributions and signals).
func NormalizeHiddenReason(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxHiddenReasonRunes || !textfold.Valid(s) {
		return "", fmt.Errorf("%w: reason must be at most %d characters of valid UTF-8", ErrInvalidArgument, MaxHiddenReasonRunes)
	}
	return s, nil
}

// State is the claim's trust state; nil for a question.
func (c Contribution) State() *trust.State {
	if c.Kind == KindQuestion {
		return nil
	}
	s := trust.StateOf(c.Counts, c.MinFavorable)
	return &s
}

var (
	ErrNotFound        = errors.New("contribution: not found")
	ErrInvalidArgument = errors.New("contribution: invalid argument")
	ErrBadCursor       = fmt.Errorf("%w: invalid cursor", ErrInvalidArgument)
	// ErrAlreadySuperseded: the claim already has a replacement.
	ErrAlreadySuperseded = errors.New("contribution: already superseded")
)

type QuestionStatus string

const (
	QuestionOpen     QuestionStatus = "open"     // no answers yet
	QuestionAnswered QuestionStatus = "answered" // answered, but no answer is Favorable
	QuestionResolved QuestionStatus = "resolved" // at least one answer is Favorable
)

type QuestionInfo struct {
	Status          QuestionStatus
	AnswerCount     int
	BestAnswerState *trust.State // nil when there are no answers
}

type ProjectCounts struct {
	Contributions, OpenQuestions int
	States                       map[trust.State]int // claims per trust state; every state present
	LastActivity                 *time.Time          // newest UpdatedAt of a contribution that is not hidden; nil when none
}
