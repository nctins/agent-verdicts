// Package httpapi is the JSON API served under /api/v1 (spec
// 2026-11-12-backend-rebuild §8). Browser clients authenticate with the
// av_session cookie; writes carry X-CSRF-Token from GET /session.
package httpapi

import (
	"context"
	"net/http"
	"strings"

	"agent-verdicts/internal/access"
	"agent-verdicts/internal/actor"
	"agent-verdicts/internal/adapter/authflow"
	"agent-verdicts/internal/adapter/httpapi/session"
	"agent-verdicts/internal/audit"
	"agent-verdicts/internal/authentication"
	"agent-verdicts/internal/contribution"
	"agent-verdicts/internal/project"
	"agent-verdicts/internal/reads"
	"agent-verdicts/internal/search"
	"agent-verdicts/internal/setting"
	"agent-verdicts/internal/signal"
)

type Deps struct {
	Actors        actor.Repository
	Projects      project.Repository
	Contributions contribution.Repository
	Signals       signal.Repository
	Credentials   authentication.Repository
	Logins        authentication.LoginRepository
	Access        access.Repository
	Audit         audit.Repository
	Sessions      *session.Store
	// Auth is the login/setup flow behind the JSON API (one throttle,
	// one setup guard).
	Auth     *authflow.Flow
	Search   *search.Service
	Worker   worker // *search.Worker in production
	Settings *setting.Service
	Reads    reads.Repository
}

type worker interface {
	Enqueue(contributionID string)
	Reindex(context.Context) error
}

type handler struct{ d Deps }

func newHandler(d Deps) *handler { return &handler{d: d} }

// New returns the API handler; mount it with
// http.StripPrefix("/api/v1", …) at "/api/v1/".
func New(d Deps) http.Handler {
	return noStore(newHandler(d).routes())
}

type route struct {
	pattern string // "METHOD /path"
	handler http.Handler
}

// table is every API route. Every non-GET route must be wrapped in write,
// writeAs, projectWrite or requireAny + csrf (session + CSRF); the only exceptions are the session-less
// POST /session and POST /setup, guarded by sameOrigin instead.
// TestRoutes_NonGETRequireCSRF walks this table to enforce that.
func (h *handler) table() []route {
	return []route{
		{"GET /session", http.HandlerFunc(h.getSession)},
		{"POST /session", sameOrigin(http.HandlerFunc(h.login))},
		{"DELETE /session", h.write(h.logout)},
		{"POST /setup", sameOrigin(http.HandlerFunc(h.setup))},
		{"POST /account/password", h.write(h.changePassword)},
		{"GET /projects", h.requireLogin(http.HandlerFunc(h.listProjects))},
		{"GET /projects/{p}/contributions", h.requireProject(access.ContributionRead, http.HandlerFunc(h.listContributions))},
		{"GET /projects/{p}/contributions/{id}", h.requireProject(access.ContributionRead, http.HandlerFunc(h.getContribution))},
		{"GET /projects/{p}/authors", h.requireProject(access.ContributionRead, http.HandlerFunc(h.listAuthors))},
		{"GET /projects/{p}/search", h.requireProject(access.ContributionRead, http.HandlerFunc(h.search))},
		{"POST /projects/{p}/contributions", h.projectWrite(access.ContributionCreate, h.createContribution)},
		{"POST /projects/{p}/contributions/{id}/signals", h.projectWrite(access.SignalCreate, h.createSignal)},
		{"DELETE /projects/{p}/contributions/{id}/signals/{sid}", h.projectWrite(access.SignalCreate, h.withdrawSignal)},
		{"DELETE /projects/{p}/contributions/{id}/supersession", h.projectWrite(access.ContributionRead, h.deleteSupersession)},
		{"PUT /projects/{p}/contributions/{id}/hidden", h.projectWrite(access.ContentModerate, h.hideContribution)},
		{"PUT /projects/{p}/contributions/{id}/signals/{sid}/hidden", h.projectWrite(access.ContentModerate, h.hideSignal)},
		{"GET /projects/{p}/reads", h.requireProject(access.MemberManage, http.HandlerFunc(h.getReads))},
		{"PUT /projects/{p}/reads/retention", h.projectWrite(access.MemberManage, h.putReadRetention)},
		{"GET /admin/overview", h.requireAny("actor:Manage or member:Manage", func(g access.Grants) bool { return g.Can(access.ActorManage, "") || g.ManagesAny() }, http.HandlerFunc(h.overview))},
		{"GET /admin/actors", h.requireAny("actor:Manage or member:Manage", func(g access.Grants) bool { return g.Can(access.ActorManage, "") || g.ManagesAny() }, http.HandlerFunc(h.listActors))},
		{"POST /admin/actors", h.writeAs(access.ActorManage, h.createActor)},
		{"GET /admin/actors/{id}", h.require(access.ActorManage, http.HandlerFunc(h.getActor))},
		{"PATCH /admin/actors/{id}", h.writeAs(access.ActorManage, h.patchActor)},
		{"POST /admin/actors/{id}/password", h.writeAs(access.ActorManage, h.resetPassword)},
		{"POST /admin/actors/{id}/credentials", h.writeAs(access.ActorManage, h.issueCredential)},
		{"DELETE /admin/credentials/{id}", h.writeAs(access.ActorManage, h.revokeCredential)},
		{"GET /admin/projects", h.requireLogin(http.HandlerFunc(h.listProjects))},
		{"POST /admin/projects", h.writeAs(access.ProjectCreate, h.createProject)},
		{"PATCH /admin/projects/{id}", h.writeAs(access.ProjectCreate, h.patchProject)},
		{"GET /admin/policies", h.requireAny("policy:Manage or member:Manage", authorsOrManages, http.HandlerFunc(h.listPolicies))},
		{"POST /admin/policies", h.writeAs(access.PolicyManage, h.createPolicy)},
		{"PUT /admin/policies/{id}", h.writeAs(access.PolicyManage, h.updatePolicy)},
		{"DELETE /admin/policies/{id}", h.writeAs(access.PolicyManage, h.deletePolicy)},
		{"GET /admin/roles", h.requireAny("policy:Manage or member:Manage", authorsOrManages, http.HandlerFunc(h.listRoles))},
		{"POST /admin/roles", h.writeAs(access.PolicyManage, h.createRole)},
		{"PUT /admin/roles/{id}", h.writeAs(access.PolicyManage, h.updateRole)},
		{"DELETE /admin/roles/{id}", h.writeAs(access.PolicyManage, h.deleteRole)},
		{"GET /admin/assignments", h.requireAny("member:Manage", access.Grants.ManagesAny, http.HandlerFunc(h.listAssignments))},
		{"POST /admin/assignments", h.requireAny("member:Manage", access.Grants.ManagesAny, h.csrf(http.HandlerFunc(h.createAssignment)))},
		{"DELETE /admin/assignments/{id}", h.requireAny("member:Manage", access.Grants.ManagesAny, h.csrf(http.HandlerFunc(h.deleteAssignment)))},
		{"GET /admin/audit", h.requireAny("actor:Manage or member:Manage", func(g access.Grants) bool { return g.Can(access.ActorManage, "") || g.ManagesAny() }, http.HandlerFunc(h.listAudit))},
		{"GET /admin/settings/search", h.require(access.SettingsManage, http.HandlerFunc(h.getSettings))},
		{"PUT /admin/settings/search", h.writeAs(access.SettingsManage, h.putSettings)},
		{"POST /admin/settings/search/test", h.writeAs(access.SettingsManage, h.testSettings)},
		{"POST /admin/settings/search/reindex", h.writeAs(access.SettingsManage, h.reindex)},
	}
}

// validProject rejects a malformed {p} (400) before anything reads the database with it.
func validProject(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := project.ValidateID(r.PathValue("p")); err != nil {
			fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *handler) routes() *http.ServeMux {
	mux := http.NewServeMux()
	for _, rt := range h.table() {
		next := rt.handler
		if strings.Contains(rt.pattern, "{p}") {
			next = validProject(next)
		}
		mux.Handle(rt.pattern, next)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, "not_found", http.StatusNotFound, "no such endpoint")
	})
	return mux
}
