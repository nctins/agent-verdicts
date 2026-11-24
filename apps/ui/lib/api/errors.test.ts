import { describe, expect, it } from "vitest";
import en from "../../messages/en.json";
import vi from "../../messages/vi.json";
import { ApiError, errorMessageKey, unwrap, type ErrorCode } from "./errors";

const fail = async (p: Promise<unknown>) => (await p.then(() => null, (x: ApiError) => x))!;
const CODES: ErrorCode[] = ["invalid_argument", "unauthenticated", "forbidden", "not_found", "conflict", "too_many_requests", "upstream_failed", "internal", "network", "project_archived"];

describe("errors", () => {
  it("unwrap passes data through", async () => {
    expect(await unwrap(Promise.resolve({ data: 1, response: new Response() }))).toBe(1);
  });
  it("404 → not_found", async () => {
    const response = new Response(null, { status: 404 });
    const e = await fail(unwrap(Promise.resolve({ error: { code: "not_found", message: "x" }, response })));
    expect(e).toBeInstanceOf(ApiError);
    expect(e.status).toBe(404);
    expect(e.code).toBe("not_found");
  });
  it("409 `project is archived` → project_archived, any other 409 stays conflict", async () => {
    const response = new Response(null, { status: 409 });
    expect((await fail(unwrap(Promise.resolve({ error: { code: "conflict", message: "project is archived" }, response })))).code).toBe("project_archived");
    expect((await fail(unwrap(Promise.resolve({ error: { code: "conflict", message: "exists" }, response })))).code).toBe("conflict");
  });
  it("non-JSON 500 body → internal", async () => {
    const response = new Response("<html>", { status: 500 });
    const e = await fail(unwrap(Promise.resolve({ error: "<html>", response })));
    expect(e.code).toBe("internal");
    expect(e.status).toBe(500);
  });
  // nginx answers 502/503/504 with HTML while api is down or restarting (spec §5.7 banner).
  it.each([502, 503, 504])("gateway %i without an API error body → network", async (status) => {
    const response = new Response("<html>", { status });
    const e = await fail(unwrap(Promise.resolve({ error: "<html>", response })));
    expect(e.code).toBe("network");
    expect(e.status).toBe(status);
  });
  it("api's own 502 upstream_failed keeps its code", async () => {
    const response = new Response(null, { status: 502 });
    const e = await fail(unwrap(Promise.resolve({ error: { code: "upstream_failed", message: "x" }, response })));
    expect(e.code).toBe("upstream_failed");
  });
  it("fetch TypeError → network", async () => {
    const e = await fail(unwrap(Promise.reject(new TypeError("Failed to fetch"))));
    expect(e.code).toBe("network");
  });
  it("errorMessageKey", () => expect(errorMessageKey("conflict")).toBe("errors.conflict"));
  it.each(CODES)("message exists in both locales: %s", (c) => {
    const key = errorMessageKey(c).split(".")[1];
    expect(typeof (vi.errors as Record<string, unknown>)[key]).toBe("string");
    expect(typeof (en.errors as Record<string, unknown>)[key]).toBe("string");
  });
});
