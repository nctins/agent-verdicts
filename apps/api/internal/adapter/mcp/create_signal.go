package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/adapter/view"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/signal"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The two signal tools have their own input, so each schema only says what
// applies to that tool.
type CreateChallengeInput struct {
	ContributionID string   `json:"contribution_id" jsonschema:"the claim you are responding to"`
	Stance         string   `json:"stance" jsonschema:"unfavorable (disputes it) or neutral (information only)"`
	Body           string   `json:"body,omitempty" jsonschema:"your argument, required, up to 64 KiB"`
	ReplyTo        string   `json:"reply_to,omitempty" jsonschema:"ID of a signal on this claim to answer (from observe_trust_signal); a reply must be neutral and never changes the state"`
	Sources        []string `json:"sources,omitempty" jsonschema:"optional evidence: up to 10 items of 1-500 characters (a URL, commit id or file path)"`
}

type CreateAssessmentInput struct {
	ContributionID string   `json:"contribution_id" jsonschema:"the claim you are responding to"`
	Stance         string   `json:"stance" jsonschema:"favorable (supports it), unfavorable (disputes it) or neutral (information only)"`
	Body           string   `json:"body,omitempty" jsonschema:"what you checked (a file, a command, a result); required unless stance is neutral, up to 64 KiB"`
	Sources        []string `json:"sources,omitempty" jsonschema:"optional evidence: up to 10 items of 1-500 characters (a URL, commit id or file path)"`
}

// ChallengeOutput is view.CreatedSignal without replaced_stance, which only an assessment has.
type ChallengeOutput struct {
	Created
	SupersededBy *view.QuestionRef `json:"superseded_by,omitempty" jsonschema:"set when the claim has been replaced by a newer claim: your signal was saved, but read and respond to that one instead"`
}

type AssessmentOutput = view.CreatedSignal

func (d Deps) createSignal(ctx context.Context, in signal.NewInput) (view.CreatedSignal, error) {
	me, mod, err := d.authorizeWrite(ctx, access.SignalCreate)
	if err != nil {
		return view.CreatedSignal{}, err
	}
	in.ProjectID, in.ActorID, in.CanModerate = projectID(ctx), me, mod
	// Agents only (spec 2026-11-22-assessment-reason-mcp): the web keeps one-click support.
	if in.Kind == signal.KindAssessment && (in.Stance == signal.StanceFavorable || in.Stance == signal.StanceUnfavorable) && strings.TrimSpace(in.Body) == "" {
		return view.CreatedSignal{}, fmt.Errorf("%w: a favorable or unfavorable assessment needs a body: say what you checked (a file, a command, a result) so others can judge your support", contribution.ErrInvalidArgument)
	}
	r, err := signal.Create(ctx, d.Contributions, d.Actors, d.Signals, in)
	if err != nil {
		return view.CreatedSignal{}, toolErr(err, in.ContributionID)
	}
	created := Created{ID: r.Signal.ID, ActorID: me, ActorName: actor.NewNameResolver(d.Actors).Name(ctx, me)}
	return view.NewCreatedSignal(ctx, d.Contributions, in.ProjectID, created, r, mod), nil
}

func (d Deps) createChallenge(ctx context.Context, _ *sdkmcp.CallToolRequest, in CreateChallengeInput) (*sdkmcp.CallToolResult, ChallengeOutput, error) {
	out, err := d.createSignal(ctx, signal.NewInput{
		ContributionID: in.ContributionID, Kind: signal.KindChallenge, Stance: signal.Stance(in.Stance), Body: in.Body, Sources: in.Sources, ReplyTo: in.ReplyTo,
	})
	return nil, ChallengeOutput{Created: out.Created, SupersededBy: out.SupersededBy}, err
}

func (d Deps) createAssessment(ctx context.Context, _ *sdkmcp.CallToolRequest, in CreateAssessmentInput) (*sdkmcp.CallToolResult, AssessmentOutput, error) {
	out, err := d.createSignal(ctx, signal.NewInput{
		ContributionID: in.ContributionID, Kind: signal.KindAssessment, Stance: signal.Stance(in.Stance), Body: in.Body, Sources: in.Sources,
	})
	return nil, out, err
}

type WithdrawInput struct {
	SignalID string `json:"signal_id" jsonschema:"ID of your challenge or assessment, from observe_trust_signal"`
}

type WithdrawOutput struct {
	Withdrawn bool `json:"withdrawn"`
}

func (d Deps) withdrawSignal(ctx context.Context, _ *sdkmcp.CallToolRequest, in WithdrawInput) (*sdkmcp.CallToolResult, WithdrawOutput, error) {
	me, mod, err := d.authorizeWrite(ctx, access.SignalCreate)
	if err != nil {
		return nil, WithdrawOutput{}, err
	}
	err = signal.Withdraw(ctx, d.Contributions, d.Actors, d.Signals, signal.WithdrawInput{
		ProjectID: projectID(ctx), SignalID: in.SignalID, ActorID: me, CanModerate: mod,
	})
	if errors.Is(err, signal.ErrNotFound) {
		return nil, WithdrawOutput{}, fmt.Errorf("%w: signal %q not found; ids come from observe_trust_signal", err, in.SignalID)
	}
	if err != nil {
		return nil, WithdrawOutput{}, toolErr(err, "")
	}
	return nil, WithdrawOutput{Withdrawn: true}, nil
}
