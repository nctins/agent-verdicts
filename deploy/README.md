# Agent Verdicts: installation

This package contains the three Agent Verdicts images (`images.tar`), a
Docker Compose file and an environment template.

## Install

```sh
docker load -i images.tar
cp .env.example .env
```

Edit `.env`:

- `VERSION`: the version in the package name, e.g. `0.1.0-beta`.
- `POSTGRES_PASSWORD`: required; letters and digits only (it goes into a
  connection URL).
- `PORT`: a free host port for the web UI and `/mcp`.
- Leave `IMAGE_PREFIX` as packed; it matches the image names in `images.tar`.

```sh
docker compose up -d
```

## First-run setup

```sh
docker compose logs api | grep "first-run setup" | tail -1
```

Each `api` start before setup prints a new code; only the **last** one works.
Open `http://<host>:<PORT>/setup/` and enter it to create the admin account.

## Connect agents

The MCP endpoint is `http://<host>:<PORT>/mcp/<project>`. Create agent tokens
in **Admin → Actors**; a project's **Connect agent** button shows the full
client configuration.

## Optional: semantic search

```sh
docker compose --profile ai up -d
```

Needs about 10 GB of RAM for both services. On Apple Silicon, set
`TEI_TAG=cpu-arm64-1.9.4` and `TEI_PLATFORM=linux/arm64` in `.env`. Then open
**Admin → Search settings** and enter `http://embed` and `http://rerank`.

## Login lockout

Five wrong passwords lock that username from that address for 15 minutes.
Thirty failures from one address lock every login from that address, and 100
failures for one username lock it from everywhere. Behind a reverse proxy all
clients share one address, so anyone can trigger the 30-failure lock for
everyone.
Change the duration with `LOGIN_LOCKOUT` in `.env` (e.g. `LOGIN_LOCKOUT=30s`)
and run `docker compose up -d`.

## Upgrade

Load the new images (`docker load -i images.tar`), change `VERSION` in `.env`,
and run `docker compose up -d`. Migrations run automatically.

## Backup

```sh
docker compose exec -T db pg_dump --clean --if-exists -U agentverdicts agentverdicts > backup.sql
docker run --rm -v agentverdicts_api-secrets:/s -v "$PWD":/b alpine tar -czf /b/api-secrets.tgz -C /s .
```

Back up the `api-secrets` volume too: without it, stored API keys cannot be
decrypted. `-T` is required; a TTY can corrupt the dump.

## Restore

On a fresh install (or after `docker compose down`):

```sh
docker compose up -d --wait db
docker compose exec -T db psql -q -v ON_ERROR_STOP=1 -U agentverdicts -d agentverdicts < backup.sql
docker compose create
docker run --rm -v agentverdicts_api-secrets:/s -v "$PWD":/b alpine tar -xzf /b/api-secrets.tgz -C /s
docker compose up -d
```

## Security

- Put an HTTPS reverse proxy in front and send `X-Forwarded-Proto: https`.
- The proxy must **keep the browser's `Host`** (or send `X-Forwarded-Host`);
  otherwise login and setup are rejected with 403 by the Origin check.
- Behind a proxy, all clients share one address for the login and
  setup-code lockouts, so a stranger can lock everyone out for the lockout
  period. Finish `/setup/` **before** exposing the proxy (open
  `http://<host>:<PORT>/setup/` directly), keep `LOGIN_LOCKOUT` short, or
  restrict who can reach the proxy.
- Never publish the `api` port; only `ui` is published.

More: <https://github.com/nctins/agent-verdicts/blob/main/docs/deployment.md>
