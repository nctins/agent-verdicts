# Deployment

[Tiếng Việt](vi/deployment.md)

- **Production install**: follow [deploy/README.md](../deploy/README.md)
  (install from Docker Hub, first-run setup, upgrade, backup and restore).
- This page covers the development stack, configuration, running behind a
  reverse proxy, and making a release.

## Development

```sh
docker compose up -d                                 # db :5432, api :8080, ui :3000
docker compose logs api | grep "first-run setup"     # setup code for /setup/
docker compose exec api go run ./seed                # optional demo data
```

- **api** runs under Air with `apps/api` bind-mounted, so Go changes reload
  automatically. Environment: `docker/api/dev.env`.
- **ui** runs `next dev` with `apps/ui` bind-mounted, and rewrites `/api/*`
  and `/mcp/*` to the api.
- **db** is ParadeDB with user, database and password `agentverdicts` /
  `agentverdicts` / `dev`.

Images only need rebuilding when a Dockerfile or `air.toml` changes. Tests and
checks: [CONTRIBUTING.md](../CONTRIBUTING.md#checks).

Optional local embedding and rerank providers start with
`docker compose --profile ai up -d`; see
[docker/embed/README.md](../docker/embed/README.md) for memory requirements
and Apple Silicon settings.

## Configuration

The api reads environment variables (in production, `deploy/compose.yaml`
sets them from `.env`):

| Variable | Default | Description |
|---|---|---|
| `AGENTVERDICTS_DATABASE_URL` | required | PostgreSQL connection URL |
| `AGENTVERDICTS_HTTP_ADDR` | `:8080` | HTTP listen address |
| `AGENTVERDICTS_SECRET_DIR` | `/secrets` | Holds `settings.key`, which encrypts provider API keys. Back it up. |
| `AGENTVERDICTS_MCP_RATE_PER_MIN` | `300` | MCP requests per actor per minute (`0` disables) |
| `AGENTVERDICTS_LOGIN_LOCKOUT` | `15m` | Lockout after repeated failed logins or setup codes |

Embedding and rerank providers are configured in the UI
(**Admin → Search settings**), not through environment variables.

The CLI runs inside the api container:
`docker compose exec api /agentverdicts help` in production, or
`docker compose exec api sh -c "go run ./cmd/agentverdicts help"` in
development.

## Behind a reverse proxy

Only the `ui` container publishes a port; never expose `api` or `db`. Before
exposing an instance beyond a trusted network, put an HTTPS proxy in front of
`ui`:

- Send `X-Forwarded-Proto: https`. Session cookies are only marked `Secure`
  when it says `https`.
- Keep the browser's `Host` (or send `X-Forwarded-Host`). Login and setup
  compare `Origin` with `Host` and reject a mismatch with 403.
- Caddy's `reverse_proxy localhost:<PORT>` does both. With nginx, add
  `proxy_set_header Host $host;` and
  `proxy_set_header X-Forwarded-Proto $scheme;`.
- Behind the proxy every client shares the proxy's address, and lockouts
  count per address. Anyone can lock everyone out for the lockout period: 30
  wrong passwords block all logins, and wrong setup codes block `/setup/`.
  Finish `/setup/` before exposing the proxy, keep `LOGIN_LOCKOUT` short, or
  limit who can reach the proxy (VPN, allowlist).

`/healthz` on the api returns 200 while the process is alive. The `scratch`
image has no shell, so probe it over HTTP from outside.

## Releasing

```sh
scripts/release.sh <semver>
```

Run `docker login` first. The script needs a clean git tree, an unused tag,
and the same version in the four files listed in `CLAUDE.md`. It builds
`nctins/agent-verdicts` and `nctins/agent-verdicts-web` for this machine,
smoke-tests them with `deploy/compose.yaml`, then builds them for
`linux/amd64` and `linux/arm64` and pushes them to Docker Hub. Pre-releases
(a `-` in the version) never move the `latest` tag. It never creates a git
tag; set `IMAGE_REPO` to push somewhere else.

Release notes must call out migrations that change behavior. For example,
migration `0012` stopped counting an author's support of their own claim, so
such claims moved from `Favorable` to `InsufficientEvidence`.
