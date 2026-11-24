package mcp

import (
	"context"
	"fmt"
	"time"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/adapter/view"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/trust"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListInput struct {
	Kind              string   `json:"kind,omitempty" jsonschema:"claim or question; omit for both"`
	State             []string `json:"state,omitempty" jsonschema:"claims in these states: Favorable, Unfavorable, Conflict, InsufficientEvidence"`
	QuestionStatus    []string `json:"question_status,omitempty" jsonschema:"questions with these statuses: open, answered, resolved"`
	Actor             string   `json:"actor,omitempty" jsonschema:"only contributions by this actor ID, or me for yours"`
	Sort              string   `json:"sort,omitempty" jsonschema:"newest (default), active (latest signal or creation first), debated or least"`
	Since             string   `json:"since,omitempty" jsonschema:"RFC 3339 timestamp: only contributions with activity (creation or a signal) after it"`
	IncludeSuperseded bool     `json:"include_superseded,omitempty" jsonschema:"also list claims a newer claim has replaced (left out by default)"`
	Cursor            string   `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
	Limit             int      `json:"limit,omitempty" jsonschema:"default 20, max 100"`
}

type ListOutput struct {
	Items      []view.ListItem `json:"items"`
	NextCursor *string         `json:"next_cursor" jsonschema:"pass as cursor for the next page; null on the last page"`
}

func (d Deps) listContributions(ctx context.Context, _ *sdkmcp.CallToolRequest, in ListInput) (*sdkmcp.CallToolResult, ListOutput, error) {
	f := contribution.ListFilter{Kind: contribution.Kind(in.Kind), Cursor: in.Cursor, Limit: in.Limit}
	for _, s := range in.State {
		f.States = append(f.States, trust.State(s))
	}
	for _, s := range in.QuestionStatus {
		f.QuestionStatus = append(f.QuestionStatus, contribution.QuestionStatus(s))
	}
	f.ActorID, f.Sort = in.Actor, contribution.Sort(in.Sort)
	if in.Since != "" {
		t, err := time.Parse(time.RFC3339, in.Since)
		if err != nil {
			return nil, ListOutput{}, fmt.Errorf("%w: since must be an RFC 3339 timestamp", contribution.ErrInvalidArgument)
		}
		f.Since = t
	}
	me, mod, err := d.authorize(ctx, access.ContributionRead)
	if err != nil {
		return nil, ListOutput{}, err
	}
	if f.ActorID == "me" {
		f.ActorID = me
	}
	f.IncludeSuperseded, f.IncludeHidden = in.IncludeSuperseded, view.SeesHidden(f, me, mod)
	page, err := d.Contributions.List(ctx, projectID(ctx), f)
	if err != nil {
		return nil, ListOutput{}, err
	}
	items, err := view.ListItems(ctx, d.Contributions, d.Actors, page.Items)
	if err != nil {
		return nil, ListOutput{}, err
	}
	out := ListOutput{Items: items, NextCursor: view.NextCursor(page.NextCursor)}
	return nil, out, nil
}
