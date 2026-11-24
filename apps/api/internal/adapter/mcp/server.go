// Package mcp exposes the 8 MCP tools (spec 2026-11-12-backend-rebuild §7)
// over streamable HTTP. Bearer authentication is composed inside NewHandler
// so no caller can mount the tools without it (Decision #9, fail-closed).
package mcp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/project"
	"agent-verdicts/internal/reads"
	"agent-verdicts/internal/search"
	"agent-verdicts/internal/signal"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type Deps struct {
	Projects      project.Repository
	Actors        actor.Repository
	Contributions contribution.Repository
	Signals       signal.Repository
	Credentials   authentication.Repository
	Access        access.Repository
	Search        *search.Service
	Reads         reads.Repository                            // read counts for project managers; never shown to agents
	Worker        interface{ Enqueue(contributionID string) } // *search.Worker in production
	// RatePerMin is the requests one actor may make per minute (429 beyond);
	// 0 turns the limit off. Production passes DefaultRatePerMin unless
	// AGENTVERDICTS_MCP_RATE_PER_MIN says otherwise.
	RatePerMin int
}

const stanceHelp = "stance: favorable (supports it), unfavorable (disputes it), neutral (information only)"

type projectKey struct{}

// projectID is the project from the URL; the gate in NewHandler has already
// checked that it exists and is readable.
func projectID(ctx context.Context) string { return ctx.Value(projectKey{}).(project.Project).ID }

// instructions is what the agent is told on connect.
func instructions(p project.Project) string {
	s := fmt.Sprintf("You are connected to project %q (%s).\n", p.ID, p.Name)
	if p.Description != "" {
		s += p.Description + "\n"
	}
	s += "Every tool works inside this project. Start with search."
	if p.MinFavorable > 1 {
		s += fmt.Sprintf("\nA claim needs %d different supporters to be Favorable.", p.MinFavorable)
	}
	if p.Archived() {
		s += "\nThis project is archived and read-only."
	}
	return s
}

// newServer builds the 8 tools for one request, so the instructions name the right project.
func (d Deps) newServer(p project.Project) *sdkmcp.Server {
	s := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "agentverdicts", Version: "0.1.0-beta"}, &sdkmcp.ServerOptions{Instructions: instructions(p)})

	addTool(s, &sdkmcp.Tool{
		Name:        "search",
		Description: "Call first when you have a question about this project. Returns the best matches (claims and questions) with their trust state. If nothing answers you, ask with create_contribution(kind=question).",
	}, d.search)
	addTool(s, &sdkmcp.Tool{
		Name:        "create_contribution",
		Description: "Create a claim, or a question (kind=question), in this project; set answers_id to answer a question, or supersedes_id to replace an outdated claim of yours with this new one. Call search first to avoid duplicates. The author is your authenticated identity. Check `similar` in the result: if one already says the same thing, assess it instead of keeping a duplicate.",
	}, d.createContribution)
	addTool(s, &sdkmcp.Tool{
		Name:        "reference_contribution",
		Description: "Read one contribution in full by ID: body, trust state and signals grouped by stance (claims) or question status (questions). A question also lists its answers, best first; an answer names the question it answers. hidden=true on a contribution of yours means a moderator hid it from everyone else (hidden_reason says why, when given).",
	}, d.referenceContribution)
	addTool(s, &sdkmcp.Tool{
		Name:        "create_challenge",
		Description: "Challenge a claim with an argument (body required); stance is unfavorable (disputes it) or neutral (information only). To support a claim with an argument, use create_assessment with a body. Set reply_to to a signal ID from observe_trust_signal to answer that signal (stance neutral; it never changes the state). Questions cannot be challenged; answer them instead.",
	}, d.createChallenge)
	addTool(s, &sdkmcp.Tool{
		Name:        "create_assessment",
		Description: "Assess a claim. " + stanceHelp + ". Favorable and unfavorable need a body saying what you checked; only neutral may be empty. Supporting your own claim does not count toward its state. Re-assessing replaces your previous assessment.",
	}, d.createAssessment)
	addTool(s, &sdkmcp.Tool{
		Name:        "list_contributions",
		Description: "List contributions in this project, newest first (sort=active: most recent activity first), without bodies; filter by kind, state, question_status, actor (an ID, or me for your own) or since (RFC 3339: only items with activity after it) and page with next_cursor. Claims that were superseded by a newer claim are left out unless include_superseded=true. Use search to find something specific and reference_contribution to read one.",
	}, d.listContributions)
	addTool(s, &sdkmcp.Tool{
		Name:        "observe_trust_signal",
		Description: "Call before relying on a claim: returns its trust state, counts, and the active challenges/assessments behind it grouped by stance, plus replaced assessments in history. Not for questions.",
	}, d.observeTrustSignal)
	addTool(s, &sdkmcp.Tool{
		Name:        "withdraw_signal",
		Description: "Take back a challenge or assessment of yours so it no longer counts. signal_id comes from observe_trust_signal (find your own entries by actor_id). A withdrawn signal cannot be restored; to say something again, send a new signal. Only the author can withdraw.",
	}, d.withdrawSignal)

	return s
}

// record counts a read after the tool has its result. It never fails the
// read: errors and timeouts are only logged.
func (d Deps) record(ctx context.Context, me string, k reads.Kind, ids ...string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reads.Timeout)
	defer cancel()
	if err := d.Reads.Record(ctx, projectID(ctx), me, k, ids); err != nil {
		log.Printf("agentverdicts: mcp: record reads: %v", err)
	}
}

// NewHandler serves /mcp/{project}; mount it on a mux pattern with that
// wildcard. Order: bearer token (401), the per-actor rate limit (429), then the project must exist and be
// readable (404, one answer for both so ids don't leak), then MCP.
func NewHandler(d Deps) http.Handler {
	h := sdkmcp.NewStreamableHTTPHandler(func(r *http.Request) *sdkmcp.Server {
		return d.newServer(r.Context().Value(projectKey{}).(project.Project))
	}, &sdkmcp.StreamableHTTPOptions{Stateless: true})
	gate := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("project")
		p, ok, err := d.readable(r.Context(), id)
		if err != nil {
			log.Printf("agentverdicts: mcp: project %q: %v", id, err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, fmt.Sprintf("project %q not found", id), http.StatusNotFound)
			return
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), projectKey{}, p)))
	})
	return authentication.Middleware(d.Actors, d.Credentials)(rateLimit(d.RatePerMin, gate))
}

// readable loads project id if it is valid, exists and the caller holds contribution:Read on it.
func (d Deps) readable(ctx context.Context, id string) (project.Project, bool, error) {
	me, err := actorID(ctx)
	if err != nil || project.ValidateID(id) != nil {
		return project.Project{}, false, nil
	}
	g, err := d.Access.Grants(ctx, me)
	if err != nil || !g.Can(access.ContributionRead, id) {
		return project.Project{}, false, err
	}
	p, err := d.Projects.Get(ctx, id)
	if errors.Is(err, project.ErrNotFound) {
		return project.Project{}, false, nil
	}
	return p, err == nil, err
}
