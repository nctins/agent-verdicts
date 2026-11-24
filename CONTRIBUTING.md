# Contributing

Thanks for your interest in Agent Verdicts. Bug reports, fixes and focused
improvements are welcome.

## Before you start

- **Bugs**: open an issue with steps to reproduce, the expected and actual
  behavior, and the version or commit.
- **Features and behavior changes**: open an issue first and describe the
  problem you want to solve. Changes to the data model, authentication,
  access control, or the delivery surface (the CLI, the 8 MCP tools, the JSON
  API and the web UI) need agreement on the design before code.
- **Out of scope**: contributor reputation, cross-project search, hard
  deletes, and inferring stance from text.

## Branches

- `develop` is the default branch and collects finished work. Branch from it
  (`feat/<name>` or `fix/<name>`) and open your pull request against
  `develop`.
- `main` only receives `develop` when a release is prepared; releases are
  tagged from `main`.
- Both branches are protected: changes go through pull requests, and
  force-pushes are rejected.

## Development setup

Everything runs in Docker; you do not need Go or Node installed locally.

```sh
docker compose up -d          # db :5432, api :8080 (hot reload), ui :3000 (next dev)
docker compose logs api | grep "first-run setup"
docker compose exec api go run ./seed   # optional demo data
```

See [docs/deployment.md](docs/deployment.md) for details on the dev stack and
[docs/architecture.md](docs/architecture.md) for how the system fits together.

## Checks

Run these before opening a pull request. Go tests need Postgres and must run
inside the `api` container; outside it, database tests are skipped, and a
skipped test does not count as passing.

```sh
docker compose up -d db
docker compose run --rm api sh -c "go build ./... && go vet ./... && go test ./..."

cd apps/ui
npm ci
npm run check    # tsc, eslint, OpenAPI types and agent rules in sync
npm test
npm run build
npm run e2e      # optional locally: builds production images and runs Playwright
```

## Conventions

- **Tests first** for fixes: add a test that fails without the change.
- **OpenAPI is the contract.** Every route is documented in
  `apps/api/openapi.yaml` and validated by `contract_test.go`. After editing
  it, run `npm run gen:api` in `apps/ui` and commit `lib/api/schema.d.ts`.
- **New routes** go through the route table in
  `apps/api/internal/adapter/httpapi/server.go` with an access middleware
  (and `csrf` for writes); tests enforce this for every route.
- **Migrations** are append-only: add a new
  `internal/platform/postgres/migrations/NNNN_*.sql`, never edit a shipped one.
- **Docs**: `docs/vi/` translates `docs/*.md` into Vietnamese. If you change
  an English doc and cannot update the translation, say so in the pull
  request.
- **UI strings** go in both `apps/ui/messages/en.json` and `vi.json` with the
  same keys.
- **Dependencies** are pinned to exact versions. Avoid adding new ones; if
  one is needed, explain why in the pull request.
- Code, identifiers, comments and commit messages are in English. Commit
  messages follow [Conventional Commits](https://www.conventionalcommits.org/)
  (`feat:`, `fix:`, `docs:`, `refactor:`, …).

## License

By contributing, you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE).
