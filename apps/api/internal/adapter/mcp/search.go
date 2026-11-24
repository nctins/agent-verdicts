package mcp

import (
	"context"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/adapter/view"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/reads"
	"agent-verdicts/internal/search"
	"agent-verdicts/internal/trust"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchInput struct {
	Query             string   `json:"query" jsonschema:"what you want to know, in natural language"`
	Kind              string   `json:"kind,omitempty" jsonschema:"claim or question; omit for both"`
	State             []string `json:"state,omitempty" jsonschema:"claims in these states only: Favorable, Unfavorable, Conflict, InsufficientEvidence"`
	QuestionStatus    []string `json:"question_status,omitempty" jsonschema:"questions with these statuses only: open, answered, resolved"`
	Limit             int      `json:"limit,omitempty" jsonschema:"default 5, max 20"`
	IncludeSuperseded bool     `json:"include_superseded,omitempty" jsonschema:"also find claims a newer claim has replaced (left out by default)"`
}

type SearchOutput struct {
	Results  []view.SearchResult `json:"results"`
	Degraded bool                `json:"degraded" jsonschema:"true when semantic search or reranking was unavailable and results are lexical only"`
}

func (d Deps) search(ctx context.Context, _ *sdkmcp.CallToolRequest, in SearchInput) (*sdkmcp.CallToolResult, SearchOutput, error) {
	me, mod, err := d.authorize(ctx, access.ContributionRead)
	if err != nil {
		return nil, SearchOutput{}, err
	}
	f := search.Filters{Kind: contribution.Kind(in.Kind), IncludeSuperseded: in.IncludeSuperseded, IncludeHidden: mod}
	for _, s := range in.State {
		f.States = append(f.States, trust.State(s))
	}
	for _, s := range in.QuestionStatus {
		f.QuestionStatus = append(f.QuestionStatus, contribution.QuestionStatus(s))
	}
	results, degraded, err := view.Search(ctx, d.Search, search.Query{
		ProjectID: projectID(ctx), Text: in.Query, Limit: in.Limit, Rerank: true, Filters: f,
	})
	if err != nil {
		return nil, SearchOutput{}, err
	}
	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.ID
	}
	d.record(ctx, me, reads.SearchHit, ids...)
	return nil, SearchOutput{Results: results, Degraded: degraded}, nil
}
