package mcp

import (
	"context"
	"errors"
	"fmt"
	"log"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/project"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// addTool registers h so that only caller-facing errors (bad input, not
// found, unauthenticated) reach the agent with their text; anything else is
// logged and returned as "internal error", like httpapi.fail.
func addTool[In, Out any](s *sdkmcp.Server, t *sdkmcp.Tool, h sdkmcp.ToolHandlerFor[In, Out]) {
	sdkmcp.AddTool(s, t, func(ctx context.Context, req *sdkmcp.CallToolRequest, in In) (*sdkmcp.CallToolResult, Out, error) {
		res, out, err := h(ctx, req, in)
		if err != nil && !isCallerErr(err) {
			log.Printf("agentverdicts: mcp: %s: %v", t.Name, err)
			err = errors.New("internal error")
		}
		return res, out, err
	})
}

func isCallerErr(err error) bool {
	for _, target := range []error{contribution.ErrInvalidArgument, contribution.ErrNotFound, contribution.ErrAlreadySuperseded, project.ErrArchived,
		project.ErrInvalidID, project.ErrNotFound, authentication.ErrUnauthenticated, access.ErrForbidden} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// actorID is the authenticated caller; missing only if the middleware was bypassed.
func actorID(ctx context.Context) (string, error) {
	id, ok := authentication.ActorID(ctx)
	if !ok {
		return "", authentication.ErrUnauthenticated
	}
	return id, nil
}

// authorize returns the caller's id if it may do a in the URL's project: a
// project it cannot read is "not found" (as over HTTP), a missing action is
// forbidden. Grants are reloaded on every call. moderator reports whether
// the caller also holds content:Moderate, i.e. may see hidden content.
func (d Deps) authorize(ctx context.Context, a access.Action) (me string, moderator bool, err error) {
	me, err = actorID(ctx)
	if err != nil {
		return "", false, err
	}
	p := projectID(ctx)
	g, err := d.Access.Grants(ctx, me)
	if err != nil {
		return "", false, err
	}
	if !g.Can(access.ContributionRead, p) {
		return "", false, fmt.Errorf("%w: project %q not found", project.ErrNotFound, p)
	}
	if !g.Can(a, p) {
		return "", false, fmt.Errorf("%w: %s on project %q", access.ErrForbidden, a, p)
	}
	return me, g.Can(access.ContentModerate, p), nil
}

// authorizeWrite is authorize for a tool that writes: an archived project is read-only.
func (d Deps) authorizeWrite(ctx context.Context, a access.Action) (string, bool, error) {
	me, mod, err := d.authorize(ctx, a)
	if err == nil && ctx.Value(projectKey{}).(project.Project).Archived() {
		err = project.ErrArchived
	}
	return me, mod, err
}

// toolErr maps domain errors for the agent. Not-found points at search;
// an unknown/disabled actor reads as unauthenticated, with no hint.
func toolErr(err error, contributionID string) error {
	switch {
	case errors.Is(err, contribution.ErrActorNotFound):
		return authentication.ErrUnauthenticated
	case errors.Is(err, contribution.ErrNotFound):
		return fmt.Errorf("%w: contribution %q not found; use search to find contributions in this project", err, contributionID)
	}
	return err
}
