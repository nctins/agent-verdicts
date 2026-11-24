import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useDebouncedSearch } from "./use-debounced-search";

type Call = { url: string; signal: AbortSignal; respond: (status: number, body: unknown) => void };
let calls: Call[];

beforeEach(() => {
  vi.useFakeTimers();
  calls = [];
  // Never honours the abort: a late response must still be ignored by the hook.
  vi.stubGlobal(
    "fetch",
    (r: Request) =>
      new Promise<Response>((resolve) =>
        calls.push({
          url: r.url,
          signal: r.signal,
          respond: (status, body) =>
            resolve(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })),
        }),
      ),
  );
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const hit = (id: string) => ({ id, kind: "claim", title: id, snippet: "", state: null });

function setup() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => createElement(QueryClientProvider, { client: qc }, children);
  return renderHook(({ text }) => useDebouncedSearch("demo", text), { wrapper, initialProps: { text: "" } });
}
const tick = (ms: number) => act(() => vi.advanceTimersByTimeAsync(ms));

describe("useDebouncedSearch", () => {
  it("sends one request 250 ms after the last keystroke, encoded", async () => {
    const h = setup();
    for (const t of ["n", "ng", "ngân hàng"]) {
      h.rerender({ text: t });
      await tick(100);
    }
    expect(calls).toHaveLength(0);
    await tick(150);
    expect(calls).toHaveLength(1);
    const u = new URL(calls[0].url);
    expect(calls[0].url).toMatch(/q=ng%C3%A2n(\+|%20)h%C3%A0ng/);
    expect(u.searchParams.get("rerank")).toBe("false");
    expect(u.searchParams.get("limit")).toBe("5");
  });

  it("aborts the pending request when the text changes, and ignores its late answer", async () => {
    const h = setup();
    h.rerender({ text: "old" });
    await tick(250);
    h.rerender({ text: "new" });
    await tick(250);
    expect(calls).toHaveLength(2);
    expect(calls[0].signal.aborted).toBe(true);
    calls[1].respond(200, { results: [hit("N")], degraded: false });
    await tick(0);
    calls[0].respond(200, { results: [hit("O")], degraded: true });
    await tick(0);
    expect(h.result.current.results.map((r) => r.id)).toEqual(["N"]);
    expect(h.result.current.settledText).toBe("new");
    expect(h.result.current.degraded).toBe(false);
  });

  it("passes reserved characters through intact", async () => {
    const h = setup();
    h.rerender({ text: "a&b%#" });
    await tick(250);
    expect(new URL(calls[0].url).searchParams.get("q")).toBe("a&b%#");
  });

  it("treats a 400 as no results", async () => {
    const h = setup();
    h.rerender({ text: "bad" });
    await tick(250);
    calls[0].respond(400, { code: "invalid_argument", message: "x" });
    await tick(0);
    expect(h.result.current.results).toEqual([]);
    expect(h.result.current.settledText).toBe("bad");
    expect(h.result.current.loading).toBe(false);
  });

  it("surfaces other errors instead of reporting no results", async () => {
    const h = setup();
    h.rerender({ text: "boom" });
    await tick(250);
    calls[0].respond(500, { code: "internal", message: "x" });
    await tick(0);
    expect(h.result.current.error).toMatchObject({ code: "internal" });
    expect(h.result.current.settledText).toBe("");
  });

  it("marks the previous text's results as stale while the new text is fetched", async () => {
    const h = setup();
    h.rerender({ text: "a" });
    await tick(250);
    calls[0].respond(200, { results: [hit("A")], degraded: false });
    await tick(0);
    expect(h.result.current.stale).toBe(false);
    h.rerender({ text: "b" });
    await tick(250);
    expect(h.result.current.stale).toBe(true);
    expect(h.result.current.settledText).toBe("a");
  });

  it("does not call the API for blank text", async () => {
    const h = setup();
    h.rerender({ text: "   " });
    await tick(500);
    expect(calls).toHaveLength(0);
    expect(h.result.current.results).toEqual([]);
  });
});
