import { beforeEach, describe, expect, it, vi } from "vitest";
import { CSRF_MESSAGE, createApi, onUnauthenticated, setCsrfToken } from "./client";

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const csrf403 = () => json(403, { code: "forbidden", message: CSRF_MESSAGE });

describe("api client", () => {
  beforeEach(() => setCsrfToken(null));

  it("adds X-CSRF-Token to writes, not to reads", async () => {
    setCsrfToken("tok1");
    const f = vi.fn(async (_: Request) => json(200, { items: [] }));
    const api = createApi({ fetch: f });
    await api.GET("/projects");
    await api.POST("/admin/projects", { body: { id: "p" } });
    const [get, post] = f.mock.calls.map((c) => c[0]);
    expect(get.headers.get("X-CSRF-Token")).toBeNull();
    expect(post.headers.get("X-CSRF-Token")).toBe("tok1");
  });

  it("on CSRF 403 refreshes the session and retries once", async () => {
    setCsrfToken("stale");
    const f = vi
      .fn()
      .mockResolvedValueOnce(csrf403())
      .mockResolvedValueOnce(json(200, { authenticated: true, actor: null, csrf_token: "fresh", must_change_password: false, setup_required: false }))
      .mockResolvedValueOnce(json(201, { id: "p", created_at: "2026-11-14T00:00:00Z" }))
      .mockResolvedValue(csrf403());
    const api = createApi({ fetch: f });
    const r = await api.POST("/admin/projects", { body: { id: "p" } });
    expect(r.response.status).toBe(201);
    expect((f.mock.calls[2][0] as Request).headers.get("X-CSRF-Token")).toBe("fresh");
    expect(f).toHaveBeenCalledTimes(3);
  });

  it("does not retry a 403 that is not about CSRF", async () => {
    setCsrfToken("t");
    const f = vi.fn(async (_: Request) => json(403, { code: "forbidden", message: "admin required" }));
    const api = createApi({ fetch: f });
    const r = await api.POST("/admin/projects", { body: { id: "p" } });
    expect(r.response.status).toBe(403);
    expect(f).toHaveBeenCalledTimes(1);
  });

  it("401 on a write calls the unauthenticated handler; 401 on POST /session does not", async () => {
    const cb = vi.fn();
    onUnauthenticated(cb);
    const f = vi.fn(async (_: Request) => json(401, { code: "unauthenticated", message: "x" }));
    const api = createApi({ fetch: f });
    await api.POST("/session", { body: { username: "a", password: "b" } });
    expect(cb).not.toHaveBeenCalled();
    await api.POST("/projects/{p}/contributions", { params: { path: { p: "demo" } }, body: { kind: "claim", title: "t" } });
    expect(cb).toHaveBeenCalledTimes(1);
  });
});
