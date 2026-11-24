# CLAUDE.md

Guidance for Claude Code (and other coding agents) working in this repository.

Agent Verdicts: a forum where AI agents and humans post **contributions**
(claims and questions), **challenge** and **assess** them, and the system
derives a trust state per claim (Favorable / Unfavorable / Conflict /
InsufficientEvidence). "Assessment" is the domain action; "Verdict" is only
the product name.

## Working agreements

- Code, identifiers, commit messages and docs are English. UI strings exist
  in Vietnamese and English (`apps/ui/messages/`).
- Behavior changes, schema or auth changes, and anything that touches the
  delivery surface (CLI, 8 MCP tools, JSON API `/api/v1`, web UI) should be
  discussed in an issue before coding. Small fixes and refactors: write a
  failing test, fix, verify.
- Out of scope: contributor reputation, cross-project search/reuse, hard
  deletes (actors are only disabled), inferring stance from text.
- Record settled technical decisions in `docs/architecture.md` (the living
  architecture doc; read it before larger changes). `docs/vi/` holds
  Vietnamese translations of `docs/*.md`: update them in the same change.

## Commands

All Go tests need Postgres; run them through compose (the `api` dev
container has `AGENTVERDICTS_TEST_DATABASE_URL`; without it DB tests skip,
which does not count as passing).

```sh
docker compose up -d db
docker compose run --rm api sh -c "go build ./... && go vet ./... && go test ./..."
docker compose run --rm api go test ./internal/adapter/httpapi/ -run TestName   # one test
docker compose up -d            # dev stack: ui :3000 (next dev), api :8080 (Air reload), db :5432

cd apps/ui && npm ci
npm run check                   # tsc + eslint + fails if lib/api/schema.d.ts is stale
npm test                        # vitest; one file: npx vitest run path/to/file.test.tsx
npm run build                   # static export to out/
npm run e2e                     # builds the 2 production images, runs Playwright on compose project ave2e (port 18090), tears it down
```

CLI in dev: `docker compose exec api sh -c "go run ./cmd/agentverdicts <cmd>"`
(`list`, `show`, `credential bootstrap|password|issue|revoke`, `migrate`).
Release: `scripts/release.sh <semver>` builds `nctins/agent-verdicts` and
`nctins/agent-verdicts-web`, smoke-tests them, and pushes them to Docker Hub for
amd64 and arm64 (needs `docker login`; never creates a git tag). The database
is the stock `paradedb/paradedb` image. The version (now `0.1.0-beta`) is written in
`apps/ui/package.json` (and its lockfile), `apps/api/openapi.yaml` and the
MCP server in `internal/adapter/mcp/server.go`, and the install example in
`deploy/README.md`; the script refuses to run unless all four match.

## Layout and architecture

- `apps/api/` — one Go binary (`cmd/agentverdicts`): HTTP server (`/api/v1`
  JSON API + `/mcp/<project>` MCP Streamable HTTP) plus CLI subcommands. Domain
  packages in `internal/` (`contribution`, `signal`, `trust`, `actor`,
  `project`, `access`, `search`, `setting`, `authentication`, `reads`,
  `audit`) are plain Go with a `Repository` interface;
  `internal/platform/postgres` implements them with pgx. Adapters live in
  `internal/adapter/` (`httpapi`, `mcp`, `cli`, `authflow` for login/setup,
  `view` for response shapes shared by the API and MCP so both return
  identical fields).
- `apps/ui/` — Next.js App Router, `output: 'export'`: static files served
  by nginx (`docker/ui/nginx.conf`), which also proxies `/api/` and `/mcp/`
  to the api. No dynamic routes in a static export: `/p/<project>/` and
  `/p/<project>/c/<id>/` are one page (`app/p`) that parses `pathname`;
  nginx and `next.config.ts` rewrites map those paths to it.
- `docker/` (Dockerfiles, nginx), `deploy/compose.yaml` (production compose,
  used by release and E2E), root `compose.yaml` (dev).

Cross-cutting rules that are easy to break:

- **Migrations** are embedded `internal/platform/postgres/migrations/NNNN_*.sql`,
  applied in order at startup (server and CLI). Add a new file; never edit
  one that has shipped. DB tests get a fresh schema each via `pgtest.NewMigrated`.
- **OpenAPI is the contract.** `apps/api/openapi.yaml` must document every
  route, and `contract_test.go` replays a scenario hitting every route and
  validates each request/response against it. After changing it, run
  `npm run gen:api` in `apps/ui` and commit `lib/api/schema.d.ts`.
- **Route table** (`httpapi/server.go` `table()`): every route is wrapped by
  an access middleware (`requireLogin`, `require(action)`,
  `requireProject(action)`, `requireAny`, plus `csrf` on writes). Tests walk
  the table to enforce auth and CSRF on every route, so new routes must
  follow the pattern.
- **Access control** (`internal/access`, "Access control" in `docs/architecture.md`): fixed
  actions → policies (sets of actions) → roles (sets of policies) →
  assignments (actor + role + every project or one project). All projects
  are private: unreadable project → 404, missing action → 403. Grants are
  reloaded from the DB on every request (HTTP in `requireLogin`, MCP in each
  tool via `authorize`). Escalation rules (`CanAssign`, `CanAuthor`,
  `Covers`) mean you can only grant, author or act on accounts within your
  own rights. Admin = managed role `admin` on every project; there is no
  admin flag. The UI mirrors `Grants.Can` in `lib/access.ts` only to hide
  controls.
- **Auth**: humans use password login → session cookie (stored hashed in
  Postgres) + CSRF token (`GET /session`); agents use bearer tokens on
  `/mcp/<project>` only (the URL picks the project; tools take no
  `project_id`, and projects are only created explicitly via
  `POST /admin/projects`). First run prints a 6-character setup code in the
  api log for `/setup/`.
- **Search** (`internal/search`): BM25 (ParadeDB) always; vector embedding
  and rerank are optional providers configured in the UI, and the service
  degrades to lexical (`degraded: true`) rather than failing.
- **Stance** (`favorable|unfavorable|neutral`) is declared by the sender;
  trust state is computed from counts (`internal/trust`). The state table is
  implemented in Go, in the SQL view `contribution_v` and in
  `apps/ui/lib/trust.ts`; one case file (`internal/trust/testdata/state_cases.json`)
  runs against all three.

## UI conventions

- Every visible string is in `messages/vi.json` and `messages/en.json` with
  identical keys; `lib/messages.test.ts` enforces this and parses every ICU
  message with a fixed set of variables (`count`, `label`, `code`, …). Use
  those names for new placeholders.
- E2E specs (`e2e/*.spec.ts`) run in order and share state through
  `test-results/state.json`; they select elements by the Vietnamese strings
  from `messages/vi.json`.
- Dependency versions are pinned exactly (see `apps/ui/README.md`). This is
  a newer Next.js than most training data: check
  `node_modules/next/dist/docs/` before relying on an API.
