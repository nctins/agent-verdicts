package mcp

import (
	"context"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/adapter/view"
	"agent-verdicts/internal/reads"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ContributionRef struct {
	ContributionID string `json:"contribution_id"`
}

type ReferenceOutput = view.Reference

func (d Deps) referenceContribution(ctx context.Context, _ *sdkmcp.CallToolRequest, in ContributionRef) (*sdkmcp.CallToolResult, ReferenceOutput, error) {
	me, mod, err := d.authorize(ctx, access.ContributionRead)
	if err != nil {
		return nil, ReferenceOutput{}, err
	}
	c, err := view.Load(ctx, d.Contributions, projectID(ctx), in.ContributionID, me, mod)
	if err != nil {
		return nil, ReferenceOutput{}, toolErr(err, in.ContributionID)
	}
	out, err := view.NewReference(ctx, d.Contributions, d.Signals, d.Actors, c, mod)
	if err == nil {
		d.record(ctx, me, reads.Reference, c.ID)
	}
	return nil, out, err
}
