package mcp

import (
	"cmp"
	"context"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/adapter/view"
	"agent-verdicts/internal/contribution"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreateContributionInput struct {
	Title        string   `json:"title" jsonschema:"1-200 characters"`
	Body         string   `json:"body,omitempty" jsonschema:"optional details, up to 64 KiB"`
	Kind         string   `json:"kind,omitempty" jsonschema:"claim (default) or question"`
	Sources      []string `json:"sources,omitempty" jsonschema:"optional evidence for the claim: up to 10 items of 1-500 characters (a URL, commit id or file path)"`
	AnswersID    string   `json:"answers_id,omitempty" jsonschema:"ID of the question this claim answers"`
	SupersedesID string   `json:"supersedes_id,omitempty" jsonschema:"ID of a claim of yours (or any claim, if you moderate) that this new claim replaces; the old one is then left out of search and lists"`
}

type Created = view.Created

func (d Deps) createContribution(ctx context.Context, _ *sdkmcp.CallToolRequest, in CreateContributionInput) (*sdkmcp.CallToolResult, view.CreatedContribution, error) {
	me, mod, err := d.authorizeWrite(ctx, access.ContributionCreate)
	if err != nil {
		return nil, view.CreatedContribution{}, err
	}
	similar := view.Similar(ctx, d.Search, projectID(ctx), in.Title, in.AnswersID, in.SupersedesID)
	c, err := contribution.Create(ctx, d.Projects, d.Actors, d.Contributions, contribution.NewInput{
		ProjectID: projectID(ctx), ActorID: me, Kind: contribution.Kind(in.Kind),
		Title: in.Title, Body: in.Body, AnswersID: in.AnswersID, Sources: in.Sources, SupersedesID: in.SupersedesID, CanModerate: mod,
	})
	if err != nil {
		return nil, view.CreatedContribution{}, toolErr(err, cmp.Or(in.SupersedesID, in.AnswersID))
	}
	d.Worker.Enqueue(c.ID)
	return nil, view.CreatedContribution{
		Created: Created{ID: c.ID, ActorID: me, ActorName: actor.NewNameResolver(d.Actors).Name(ctx, me)}, Similar: similar,
	}, nil
}
