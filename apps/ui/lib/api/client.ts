import createClient, { type Middleware } from "openapi-fetch";
import type { paths } from "./schema";

// Must equal the server's CSRF 403 message (api/internal/adapter/httpapi/middleware.go).
export const CSRF_MESSAGE = "invalid or missing CSRF token";

let csrfToken: string | null = null;
export const setCsrfToken = (t: string | null) => {
  csrfToken = t;
};

// Hard navigation on purpose: drops all in-memory state of the expired session.
const defaultUnauthenticated = () =>
  // eslint-disable-next-line @next/next/no-location-assign-relative-destination
  location.assign("/login/?next=" + encodeURIComponent(location.pathname + location.search));
let unauthenticated: () => void = defaultUnauthenticated;
export const onUnauthenticated = (cb: () => void) => {
  unauthenticated = cb;
};

export function createApi(opts?: { fetch?: (r: Request) => Promise<Response> }) {
  // Looked up per call so a replaced global fetch (tests) is honoured.
  const doFetch = (r: Request) => (opts?.fetch ?? ((x: Request) => globalThis.fetch(x)))(r);
  // The body stream is consumed by the first send; keep a clone for a retry.
  const spare = new WeakMap<Request, Request>();

  const mw: Middleware = {
    onRequest({ request }) {
      if (request.method === "GET" || request.method === "HEAD") return;
      if (csrfToken) request.headers.set("X-CSRF-Token", csrfToken);
      spare.set(request, request.clone());
    },
    async onResponse({ request, response, schemaPath }) {
      if (response.status === 403) {
        const copy = spare.get(request);
        if (!copy) return;
        const body = (await response.clone().json().catch(() => null)) as { message?: string } | null;
        if (body?.message !== CSRF_MESSAGE) return;
        // Stale token (e.g. session rotated): fetch a fresh one and retry once.
        const s = await doFetch(new Request(new URL("/api/v1/session", request.url)));
        if (!s.ok) return;
        csrfToken = ((await s.json()) as { csrf_token: string }).csrf_token;
        copy.headers.set("X-CSRF-Token", csrfToken);
        return doFetch(copy);
      }
      if (
        response.status === 401 &&
        request.method !== "GET" &&
        !(request.method === "POST" && (schemaPath === "/session" || schemaPath === "/setup"))
      )
        unauthenticated();
    },
  };

  // Request() rejects relative URLs outside browsers; absolute same-origin is equivalent.
  const baseUrl = typeof location === "undefined" ? "/api/v1" : location.origin + "/api/v1";
  const client = createClient<paths>({ baseUrl, fetch: doFetch });
  client.use(mw);
  return client;
}

export const api = createApi();

export { unwrap } from "./errors";
