package mcp

import (
	"context"
	"fmt"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/adapter/view"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/reads"
	"agent-verdicts/internal/trust"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ObserveOutput struct {
	State        trust.State `json:"state"`
	Counts       view.Counts `json:"counts"`
	MinFavorable int         `json:"min_favorable" jsonschema:"different supporters (counts.favorable_actors) a claim needs in this project to be Favorable"`
	view.Signals
	Hidden       bool   `json:"hidden,omitempty" jsonschema:"moderators and the author only: the claim is hidden from everyone else"`
	HiddenReason string `json:"hidden_reason,omitempty" jsonschema:"why a moderator hid it, when a reason was given"`
}

func (d Deps) observeTrustSignal(ctx context.Context, _ *sdkmcp.CallToolRequest, in ContributionRef) (*sdkmcp.CallToolResult, ObserveOutput, error) {
	me, mod, err := d.authorize(ctx, access.ContributionRead)
	if err != nil {
		return nil, ObserveOutput{}, err
	}
	c, err := view.Load(ctx, d.Contributions, projectID(ctx), in.ContributionID, me, mod)
	if err != nil {
		return nil, ObserveOutput{}, toolErr(err, in.ContributionID)
	}
	if c.Kind == contribution.KindQuestion {
		return nil, ObserveOutput{}, fmt.Errorf("%w: questions have no trust state; use reference_contribution to see its answers", contribution.ErrInvalidArgument)
	}
	signals, err := d.Signals.ListByContribution(ctx, c.ID)
	if err != nil {
		return nil, ObserveOutput{}, err
	}
	d.record(ctx, me, reads.Observe, c.ID)
	return nil, ObserveOutput{
		State: *c.State(), Counts: view.CountsOf(c.Counts), MinFavorable: c.MinFavorable, Signals: view.GroupSignals(ctx, d.Actors, signals, mod),
		Hidden: c.HiddenAt != nil, HiddenReason: c.HiddenReason,
	}, nil
}
