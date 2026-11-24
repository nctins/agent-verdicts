# apps/ui: web UI (Next.js, static export)

Next.js App Router built as static files (`out/`) and served by nginx in
production. All data goes through the `/api/v1` JSON API of `apps/api`.

## Commands

```sh
npm ci
npm run dev       # next dev; rewrites /api/* and /mcp → $API_URL (default http://localhost:8080)
npm run gen:api   # generates lib/api/schema.d.ts from ../api/openapi.yaml (committed)
npm run check     # tsc + eslint + fails if schema.d.ts or lib/agent-rules.md is stale
npm test          # vitest
npm run build     # static export → out/
npm run e2e       # Playwright against the production images
```

Through compose (from the repo root): `docker compose up -d ui` →
<http://localhost:3000>. The container's `node_modules` lives in the
`ui-node-modules` volume, separate from the host's `apps/ui/node_modules`.

## Pinned versions

Every dependency is pinned to an exact version (no `^` or `~`).

| Package | Version | Released |
|---|---|---|
| `next` | 16.4.6 | 2026-11-09 |
| `react` | 19.3.1 | 2026-10-14 |
| `react-dom` | 19.3.1 | 2026-10-14 |
| `@tanstack/react-query` | 5.108.2 | 2026-11-20 |
| `next-intl` | 4.15.3 | 2026-11-19 |
| `openapi-fetch` | 0.17.0 | 2026-02-11 |
| `openapi-typescript` | 7.13.0 | 2026-02-11 |
| `tailwindcss` | 4.3.5 | 2026-11-06 |
| `@tailwindcss/turbopack` | 4.3.5 | 2026-11-06 |
| `vitest` | 5.1.1 | 2026-11-18 |
| `@vitejs/plugin-react` | 5.2.0 | 2026-03-12 |
| `jsdom` | 31.0.1 | 2026-11-13 |
| `@testing-library/react` | 16.3.3 | 2026-08-27 |
| `@testing-library/user-event` | 14.6.13 | 2026-11-18 |
| `@playwright/test` | 1.65.1 | 2026-11-13 |
| `typescript` | 5.9.3 | 2025-09-30 |
| `eslint` | 9.39.5 | 2026-07-10 |
| `eslint-config-next` | 16.4.6 | 2026-11-09 |
| `shadcn` | 4.23.0 | 2026-10-27 |
| `radix-ui` | 1.8.1 | 2026-10-12 |
| `class-variance-authority` | 0.7.1 | 2024-11-26 |
| `cn` | 0.5.2 | 2026-11-02 |
| `lucide-react` | 1.77.0 | 2026-11-20 |
| `react-markdown` | 10.1.0 | 2025-03-07 |
| `sonner` | 2.0.8 | 2026-08-09 |
| `tw-animate-css` | 1.4.0 | 2025-09-24 |
| `@types/node` | 26.6.6 | 2026-10-28 |
| `@types/react` | 19.3.0 | 2026-09-09 |
| `@types/react-dom` | 19.3.0 | 2026-09-09 |
| image `node` | `26.13.1-alpine` | 2026-11-11 (amd64 + arm64) |

Notes:

- **TypeScript 5 / ESLint 9.** TypeScript 7 is blocked because
  `openapi-typescript` 7.13 requires `typescript ^5` and `typescript-eslint`
  requires `<6.1`. ESLint 10 is not yet within the peer ranges of
  `eslint-plugin-import`, `eslint-plugin-jsx-a11y` and `eslint-plugin-react`,
  which `eslint-config-next` pulls in.
- `@types/node` tracks the major version of the `node` image (26).
- `@vitejs/plugin-react` 6.x conflicts on peers (`@babel/core` 8 through
  `@rolldown/plugin-babel`, while `shadcn` needs `@babel/core` 7), so 5.2.0
  is used; it still supports the vite 8 that vitest 5 brings in.
- `shadcn`, `radix-ui`, `class-variance-authority`, `cn`, `lucide-react`,
  `sonner` and `tw-animate-css` were added by `shadcn init/add`. `cn`
  replaces `clsx` + `tailwind-merge` in this CLI version. The Toaster uses
  `useTheme()` from `app/providers.tsx` instead of `next-themes`.
- `react-markdown` renders a subset of Markdown for user content, without
  `rehype-raw` or other plugins, so raw HTML never executes. Bare http(s)
  links are linked by a small remark plugin in `components/Markdown.tsx`
  instead of `remark-gfm`.
- Fonts: system fonts; `next/font/google` (which downloads fonts at build
  time) is not used.

## Dev rewrites and the Origin check

`next.config.ts` enables `rewrites` only when `NODE_ENV !== "production"`
(Next.js does not support rewrites with `output: 'export'`).

- `trailingSlash: true` makes Next.js 308-redirect `/api/v1/session` to
  `/api/v1/session/` **before** rewriting, so dev also sets
  `skipTrailingSlashRedirect: true`.
- `next dev` rewrites send `Host` = the target host (`api:8080`),
  `X-Forwarded-Host: localhost:3000`, and the original `Origin`, with no
  `X-Forwarded-Proto`. `authflow.OriginMatches` accepts `X-Forwarded-Host`,
  so the API needs no dev-specific handling. To check:

  ```sh
  curl -s -o /dev/null -w '%{http_code}\n' -X POST http://localhost:3000/api/v1/session \
    -H 'Origin: http://localhost:3000' -H 'Content-Type: application/json' \
    -d '{"username":"nobody","password":"x"}'
  # 401 (Origin accepted, wrong password)
  # The same request with Origin: http://evil.example → 403 "cross-origin request rejected"
  ```

## i18n and theme

- URLs carry no `/vi` or `/en`. Locale: `localStorage["av.locale"]`, then
  `navigator.language` (starting with `en` → `en`), then `vi`. Theme:
  `localStorage["av.theme"]` ∈ `light|dark|system`, default `system`.
- Static HTML is prerendered with `vi` and the default theme; the real choice
  is applied after hydration. A small inline script in `<head>` sets the
  `dark` class before first paint to avoid a theme flash.
- `messages/{vi,en}.json` are nested by page with ICU placeholders
  (`{count}`). `lib/messages.test.ts` checks that both files have the same
  keys, no empty values, and that every message parses.

## E2E (Playwright)

```sh
npx playwright install chromium   # once
npm run e2e                       # builds the 3 images tagged e2e (as scripts/release.sh does), starts the stack, runs the specs, tears it down
E2E_SKIP_BUILD=1 npm run e2e      # reuse existing agentverdicts-*:e2e images
E2E_KEEP=1 npm run e2e            # keep the stack running afterwards (the next run cleans it up)
```

- The stack is the real `deploy/compose.yaml` (db + api + nginx ui, without
  the `ai` profile), compose project `ave2e`, port `18090`, with a temporary
  `.env` in `$TMPDIR/agentverdicts-e2e/.env`. `down -v` only removes this
  project's volumes.
- The first-run setup code is read from `docker compose logs api`.
- Specs `e2e/01-…` to `08-…` run sequentially on one worker and depend on
  each other (earlier specs create users, projects and questions for later
  ones, shared through `test-results/state.json`). A single spec run alone
  has no data.
- The browser uses `locale: vi-VN`; expected text comes from
  `messages/*.json`.
