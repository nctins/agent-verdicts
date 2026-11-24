// Package view builds the JSON view models shared by the MCP tools and the
// JSON API, so both adapters shape contributions and signals the same way.
// The jsonschema tags feed the MCP tools' output schemas.
package view

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"time"

	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/search"
	"agent-verdicts/internal/signal"
	"agent-verdicts/internal/trust"
)

// Status is the trust part shared by every contribution shape: state for
// claims (null for questions), question fields only on questions.
type Status struct {
	State           *trust.State `json:"state" jsonschema:"Favorable, Unfavorable, Conflict or InsufficientEvidence; null for questions"`
	QuestionStatus  string       `json:"question_status,omitempty" jsonschema:"questions only: open, answered or resolved"`
	AnswerCount     *int         `json:"answer_count,omitempty" jsonschema:"questions only"`
	BestAnswerState *trust.State `json:"best_answer_state,omitempty" jsonschema:"questions only; null when unanswered"`
}

func StatusOf(kind contribution.Kind, state *trust.State, q *contribution.QuestionInfo) Status {
	if kind != contribution.KindQuestion {
		return Status{State: state}
	}
	if q == nil {
		q = &contribution.QuestionInfo{Status: contribution.QuestionOpen}
	}
	n := q.AnswerCount
	return Status{QuestionStatus: string(q.Status), AnswerCount: &n, BestAnswerState: q.BestAnswerState}
}

// questionInfo loads QuestionInfo for the questions among cs.
func questionInfo(ctx context.Context, repo contribution.Repository, cs ...contribution.Contribution) (map[string]contribution.QuestionInfo, error) {
	var ids []string
	for _, c := range cs {
		if c.Kind == contribution.KindQuestion {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return repo.QuestionInfo(ctx, ids)
}

func infoPtr(m map[string]contribution.QuestionInfo, id string) *contribution.QuestionInfo {
	if q, ok := m[id]; ok {
		return &q
	}
	return nil
}

// TS formats a timestamp the way every response does.
func TS(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

type Counts struct {
	Favorable       int `json:"favorable" jsonschema:"active favorable signals, not counting the claim's author"`
	Unfavorable     int `json:"unfavorable"`
	Neutral         int `json:"neutral"`
	FavorableActors int `json:"favorable_actors" jsonschema:"distinct actors other than the author with an active favorable signal; this, not favorable, is compared with min_favorable"`
}

func CountsOf(c trust.Counts) Counts {
	return Counts{Favorable: c.Favorable, Unfavorable: c.Unfavorable, Neutral: c.Neutral, FavorableActors: c.FavorableActors}
}

type AnswerItem struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	State        *trust.State `json:"state"`
	Hidden       bool         `json:"hidden,omitempty" jsonschema:"moderators only: the answer is hidden from everyone else"`
	HiddenReason string       `json:"hidden_reason,omitempty" jsonschema:"why a moderator hid it, when a reason was given"`
}

// Load reads a contribution of the project for caller me; a hidden one is
// not found unless the caller moderates (content:Moderate) or wrote it.
func Load(ctx context.Context, repo contribution.Repository, projectID, id, me string, moderator bool) (contribution.Contribution, error) {
	c, err := repo.Get(ctx, projectID, id)
	if err == nil && !c.VisibleTo(moderator) && (me == "" || c.ActorID != me) {
		return contribution.Contribution{}, contribution.ErrNotFound
	}
	return c, err
}

// SeesHidden is ListFilter.IncludeHidden for a caller: moderators see every
// hidden contribution, anyone sees their own when listing just themselves.
func SeesHidden(f contribution.ListFilter, me string, moderator bool) bool {
	return moderator || (me != "" && f.ActorID == me)
}

// SupersededRef names the claim that replaced a superseded one; nil when id
// is "" or the newer claim is hidden from a caller who does not moderate.
func SupersededRef(ctx context.Context, repo contribution.Repository, projectID, id string, moderator bool) (*QuestionRef, error) {
	if id == "" {
		return nil, nil
	}
	n, err := Load(ctx, repo, projectID, id, "", moderator)
	if errors.Is(err, contribution.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &QuestionRef{ID: n.ID, Title: n.Title}, nil
}

// CreatedSignal is the result of a new signal.
type CreatedSignal struct {
	Created
	ReplacedStance *signal.Stance `json:"replaced_stance" jsonschema:"stance of your previous assessment that this one replaced; null if none"`
	SupersededBy   *QuestionRef   `json:"superseded_by,omitempty" jsonschema:"set when the claim has been replaced by a newer claim: your signal was saved, but read and respond to that one instead"`
	NotCounted     bool           `json:"not_counted,omitempty" jsonschema:"you wrote this claim: your own support is kept but does not count toward its trust state"`
}

// NewCreatedSignal shapes r. A failure to load the newer claim only drops
// the hint: the signal is already saved.
func NewCreatedSignal(ctx context.Context, repo contribution.Repository, projectID string, created Created, r signal.Result, moderator bool) CreatedSignal {
	ref, err := SupersededRef(ctx, repo, projectID, r.SupersededByID, moderator)
	if err != nil {
		log.Printf("agentverdicts: superseded_by: %v", err)
	}
	return CreatedSignal{Created: created, ReplacedStance: r.ReplacedStance, SupersededBy: ref, NotCounted: r.NotCounted}
}

type QuestionRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// orEmpty keeps a list field a JSON array, never null.
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Reference is one contribution in full (reference_contribution).
type Reference struct {
	ID        string   `json:"id"`
	ProjectID string   `json:"project_id"`
	Kind      string   `json:"kind"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Sources   []string `json:"sources" jsonschema:"evidence references given by the author: URLs, commit ids, file paths"`
	ActorID   string   `json:"actor_id"`
	ActorName string   `json:"actor_name"`
	ActorType string   `json:"actor_type,omitempty" jsonschema:"Human or Agent"`
	CreatedAt string   `json:"created_at"`
	Status
	Counts       *Counts      `json:"counts,omitempty" jsonschema:"claims only: signal counts by stance"`
	MinFavorable *int         `json:"min_favorable,omitempty" jsonschema:"claims only: different supporters (counts.favorable_actors) needed for Favorable"`
	Answers      []AnswerItem `json:"answers,omitempty" jsonschema:"questions only: answers, best state first"`
	AnswerTo     *QuestionRef `json:"answer_to,omitempty" jsonschema:"answers only: the question answered"`
	Signals      Signals      `json:"signals" jsonschema:"the signals on a claim, grouped by stance; empty groups for questions"`
	// SupersededBy is set on a claim a newer claim replaced; read it as outdated.
	SupersededBy *QuestionRef `json:"superseded_by,omitempty" jsonschema:"claims only: the newer claim that replaced this one"`
	Hidden       bool         `json:"hidden,omitempty" jsonschema:"moderators and the author only: the contribution is hidden from everyone else"`
	HiddenReason string       `json:"hidden_reason,omitempty" jsonschema:"why a moderator hid it, when a reason was given"`
}

// NewReference shapes c in full. moderator (content:Moderate) also shows
// hidden answers and hidden signals, flagged.
func NewReference(ctx context.Context, contributions contribution.Repository, signals signal.Repository, actors actor.Repository, c contribution.Contribution, moderator bool) (Reference, error) {
	info, err := questionInfo(ctx, contributions, c)
	if err != nil {
		return Reference{}, err
	}
	author := actor.NewNameResolver(actors)
	out := Reference{
		ID: c.ID, ProjectID: c.ProjectID, Kind: string(c.Kind), Title: c.Title, Body: c.Body, Sources: orEmpty(c.Sources),
		ActorID: c.ActorID, ActorName: author.Name(ctx, c.ActorID), ActorType: author.Type(ctx, c.ActorID), CreatedAt: TS(c.CreatedAt),
		Status:  StatusOf(c.Kind, c.State(), infoPtr(info, c.ID)),
		Signals: GroupSignals(ctx, actors, nil, moderator), Hidden: c.HiddenAt != nil, HiddenReason: c.HiddenReason,
	}
	if out.SupersededBy, err = SupersededRef(ctx, contributions, c.ProjectID, c.SupersededByID, moderator); err != nil {
		return Reference{}, err
	}
	if c.Kind == contribution.KindQuestion {
		answers, err := contributions.Answers(ctx, c.ID, moderator)
		if err != nil {
			return Reference{}, err
		}
		out.Answers = []AnswerItem{}
		for _, a := range answers {
			out.Answers = append(out.Answers, AnswerItem{ID: a.ID, Title: a.Title, State: a.State(), Hidden: a.HiddenAt != nil, HiddenReason: a.HiddenReason})
		}
		return out, nil
	}
	n := CountsOf(c.Counts)
	out.Counts, out.MinFavorable = &n, &c.MinFavorable
	sigs, err := signals.ListByContribution(ctx, c.ID)
	if err != nil {
		return Reference{}, err
	}
	out.Signals = GroupSignals(ctx, actors, sigs, moderator)
	if c.AnswersID != "" {
		q, err := Load(ctx, contributions, c.ProjectID, c.AnswersID, "", moderator)
		if err != nil && !errors.Is(err, contribution.ErrNotFound) {
			return Reference{}, err
		}
		if err == nil {
			out.AnswerTo = &QuestionRef{ID: q.ID, Title: q.Title}
		}
	}
	return out, nil
}

type SignalItem struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind" jsonschema:"challenge or assessment"`
	Stance     string   `json:"stance"`
	ActorID    string   `json:"actor_id"`
	ActorName  string   `json:"actor_name"`
	ActorType  string   `json:"actor_type,omitempty" jsonschema:"Human or Agent"`
	Body       string   `json:"body"`
	Sources    []string `json:"sources" jsonschema:"evidence references given by the sender"`
	CreatedAt  string   `json:"created_at"`
	ReplyTo    string   `json:"reply_to,omitempty" jsonschema:"set on a reply: the ID of the signal it answers (always a top-level signal of the same claim)"`
	Superseded bool     `json:"superseded,omitempty" jsonschema:"true for an assessment replaced by a newer one from the same actor"`
	Withdrawn  bool     `json:"withdrawn,omitempty" jsonschema:"true when its author withdrew it; it cannot be restored"`
	// Hidden signals only reach moderators.
	Hidden        bool   `json:"hidden,omitempty" jsonschema:"moderators only: hidden from everyone else"`
	HiddenReason  string `json:"hidden_reason,omitempty" jsonschema:"why a moderator hid it, when a reason was given"`
	ActorDisabled bool   `json:"actor_disabled,omitempty" jsonschema:"true when its sender is disabled; it does not count"`
}

// Signals groups a claim's signals: active ones by stance; replaced,
// withdrawn, hidden and disabled-sender ones in History.
type Signals struct {
	Favorable   []SignalItem `json:"favorable"`
	Unfavorable []SignalItem `json:"unfavorable"`
	Neutral     []SignalItem `json:"neutral"`
	History     []SignalItem `json:"history" jsonschema:"signals that do not count toward state: replaced, withdrawn, hidden or from a disabled actor"`
}

// GroupSignals drops hidden signals unless moderator.
func GroupSignals(ctx context.Context, actors actor.Repository, signals []signal.Signal, moderator bool) Signals {
	out := Signals{Favorable: []SignalItem{}, Unfavorable: []SignalItem{}, Neutral: []SignalItem{}, History: []SignalItem{}}
	names := actor.NewNameResolver(actors)
	for _, s := range signals {
		if s.HiddenAt != nil && !moderator {
			continue
		}
		it := SignalItem{
			ID: s.ID, Kind: string(s.Kind), Stance: string(s.Stance), ActorID: s.ActorID, ActorName: names.Name(ctx, s.ActorID), ActorType: names.Type(ctx, s.ActorID),
			Body: s.Body, Sources: orEmpty(s.Sources), CreatedAt: TS(s.CreatedAt), ReplyTo: s.ReplyTo, Superseded: s.SupersededAt != nil,
			Withdrawn: s.WithdrawnAt != nil, Hidden: s.HiddenAt != nil, HiddenReason: s.HiddenReason, ActorDisabled: s.ActorDisabled,
		}
		switch {
		case !s.Active():
			out.History = append(out.History, it)
		case s.Stance == signal.StanceFavorable:
			out.Favorable = append(out.Favorable, it)
		case s.Stance == signal.StanceUnfavorable:
			out.Unfavorable = append(out.Unfavorable, it)
		default:
			out.Neutral = append(out.Neutral, it)
		}
	}
	return out
}

type ListItem struct {
	ID             string       `json:"id"`
	Kind           string       `json:"kind"`
	Title          string       `json:"title"`
	State          *trust.State `json:"state" jsonschema:"null for questions"`
	QuestionStatus string       `json:"question_status,omitempty" jsonschema:"questions only"`
	AnswerCount    *int         `json:"answer_count,omitempty" jsonschema:"questions only: answers that are not hidden"`
	Counts         *Counts      `json:"counts,omitempty" jsonschema:"claims only: signal counts by stance"`
	CreatedAt      string       `json:"created_at"`
	UpdatedAt      string       `json:"updated_at" jsonschema:"creation, or the newest signal on it"`
	ActorName      string       `json:"actor_name"`
	ActorType      string       `json:"actor_type,omitempty" jsonschema:"Human or Agent"`
	Superseded     bool         `json:"superseded,omitempty" jsonschema:"true when a newer claim replaced it (only listed with include_superseded)"`
	Hidden         bool         `json:"hidden,omitempty" jsonschema:"moderators, or the author listing their own: hidden from everyone else"`
	HiddenReason   string       `json:"hidden_reason,omitempty" jsonschema:"why a moderator hid it, when a reason was given"`
}

// ListItems shapes a page of contributions, one item per contribution in order.
func ListItems(ctx context.Context, contributions contribution.Repository, actors actor.Repository, cs []contribution.Contribution) ([]ListItem, error) {
	info, err := questionInfo(ctx, contributions, cs...)
	if err != nil {
		return nil, err
	}
	names := actor.NewNameResolver(actors)
	out := []ListItem{}
	for _, c := range cs {
		st := StatusOf(c.Kind, c.State(), infoPtr(info, c.ID))
		it := ListItem{
			ID: c.ID, Kind: string(c.Kind), Title: c.Title, State: st.State, QuestionStatus: st.QuestionStatus, AnswerCount: st.AnswerCount,
			CreatedAt: TS(c.CreatedAt), UpdatedAt: TS(c.UpdatedAt), ActorName: names.Name(ctx, c.ActorID), ActorType: names.Type(ctx, c.ActorID),
			Superseded: c.SupersededByID != "", Hidden: c.HiddenAt != nil, HiddenReason: c.HiddenReason,
		}
		if c.Kind != contribution.KindQuestion {
			n := CountsOf(c.Counts)
			it.Counts = &n
		}
		out = append(out, it)
	}
	return out, nil
}

// NextCursor is a page's cursor as JSON: null on the last page.
func NextCursor(c string) *string {
	if c == "" {
		return nil
	}
	return &c
}

type SearchResult struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Status
	Superseded   bool   `json:"superseded,omitempty" jsonschema:"true when a newer claim replaced it (only found with include_superseded)"`
	Hidden       bool   `json:"hidden,omitempty" jsonschema:"moderators only: hidden from everyone else"`
	HiddenReason string `json:"hidden_reason,omitempty" jsonschema:"why a moderator hid it, when a reason was given"`
}

// Search validates kind, state and question_status, runs q and shapes the results.
func Search(ctx context.Context, svc *search.Service, q search.Query) (results []SearchResult, degraded bool, err error) {
	if k := q.Filters.Kind; k != "" && k != contribution.KindClaim && k != contribution.KindQuestion {
		return nil, false, fmt.Errorf("%w: kind must be claim or question", contribution.ErrInvalidArgument)
	}
	if err := contribution.ValidateStates(q.Filters.States, q.Filters.QuestionStatus); err != nil {
		return nil, false, err
	}
	resp, err := svc.Search(ctx, q)
	if err != nil {
		return nil, false, err
	}
	results = []SearchResult{}
	for _, r := range resp.Results {
		results = append(results, SearchResult{
			ID: r.ID, Kind: string(r.Kind), Title: r.Title, Snippet: r.Snippet,
			Status:     StatusOf(r.Kind, r.State, r.Question),
			Superseded: r.Superseded, Hidden: r.Hidden, HiddenReason: r.HiddenReason,
		})
	}
	return results, resp.Degraded, nil
}

type Created struct {
	ID        string `json:"id"`
	ActorID   string `json:"actor_id"`
	ActorName string `json:"actor_name"`
}

// CreatedContribution is Created plus the duplicate hint of a new contribution.
type CreatedContribution struct {
	Created
	Similar []SearchResult `json:"similar" jsonschema:"up to 3 existing contributions with a similar title, found before this one was saved"`
}

// similarMax is how many duplicate hints a creation returns.
const similarMax = 3

// Similar is the duplicate hint for a title about to be created: a lexical
// search, at most 3 results, never an error (creation must not depend on it).
// exclude lists ids that are related by design, not duplicates: the question
// being answered and the claim being replaced ("" entries are ignored).
func Similar(ctx context.Context, svc *search.Service, projectID, title string, exclude ...string) []SearchResult {
	results, _, err := Search(ctx, svc, search.Query{ProjectID: projectID, Text: title, Limit: similarMax + len(exclude), LexicalOnly: true, DropAccentMismatch: true})
	if err != nil {
		if !errors.Is(err, contribution.ErrInvalidArgument) { // Create reports bad input itself
			log.Printf("agentverdicts: similar: %v", err)
		}
		return []SearchResult{}
	}
	results = slices.DeleteFunc(results, func(r SearchResult) bool { return slices.Contains(exclude, r.ID) })
	return results[:min(similarMax, len(results))]
}
