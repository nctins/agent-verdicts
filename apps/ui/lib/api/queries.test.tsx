import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import en from "@/messages/en.json";
import { keys } from "./keys";
import {
  useChangePassword, useContributions, useCreateContribution, useCreateSignal, useLogin, usePutSettings, useSettings,
  useSetup, useTestSettings,
} from "./queries";

const json = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
const wrap = (qc: QueryClient) =>
  function W({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <NextIntlClientProvider locale="en" messages={en}>
          {children}
        </NextIntlClientProvider>
      </QueryClientProvider>
    );
  };
const item = (id: string) => ({ id });

afterEach(() => vi.unstubAllGlobals());

describe("queries", () => {
  it("useContributions follows next_cursor and stops at null", async () => {
    const urls: string[] = [];
    vi.stubGlobal("fetch", async (r: Request) => {
      urls.push(r.url);
      return json(r.url.includes("cursor=c1") ? { items: [item("b")], next_cursor: null } : { items: [item("a")], next_cursor: "c1" });
    });
    const qc = new QueryClient();
    const { result } = renderHook(() => useContributions("demo", {}), { wrapper: wrap(qc) });
    await waitFor(() => expect(result.current.data?.pages.length).toBe(1));
    expect(result.current.hasNextPage).toBe(true);
    await result.current.fetchNextPage();
    await waitFor(() => expect(result.current.data?.pages.length).toBe(2));
    expect(result.current.hasNextPage).toBe(false);
    expect(urls[1]).toContain("cursor=c1");
  });

  it("useSettings polls every 2s while reindexing", async () => {
    vi.stubGlobal("fetch", async () => json({ status: { reindexing: true } }));
    const qc = new QueryClient();
    const { result } = renderHook(() => useSettings(), { wrapper: wrap(qc) });
    await waitFor(() => expect(result.current.data).toBeTruthy());
    const q = qc.getQueryCache().findAll()[0];
    const ri = (q.options as { refetchInterval?: unknown }).refetchInterval as (q: unknown) => unknown;
    expect(ri(q)).toBe(2000);
    expect(ri({ state: { data: { status: { reindexing: false } } } })).toBe(false);
  });

  // A cached "no results" search must not survive the write that answers it (dedup loop).
  const seeded = () => {
    const qc = new QueryClient();
    qc.setQueryData(keys.search("demo", "reset token", { limit: 5 }), { results: [], text: "reset token" });
    qc.setQueryData(keys.projects(), []);
    return qc;
  };
  const stale = (qc: QueryClient, k: readonly unknown[]) => qc.getQueryState(k)?.isInvalidated;

  it("creating a contribution invalidates the project's searches", async () => {
    vi.stubGlobal("fetch", async () => json({ id: "c9" }));
    const qc = seeded();
    const { result } = renderHook(() => useCreateContribution("demo"), { wrapper: wrap(qc) });
    await act(() => result.current.mutateAsync({ kind: "question", title: "reset token", body: "" }));
    expect(stale(qc, keys.search("demo", "reset token", { limit: 5 }))).toBe(true);
  });

  it("a signal invalidates the project's searches and the home project counts", async () => {
    vi.stubGlobal("fetch", async () => json({}));
    const qc = seeded();
    const { result } = renderHook(() => useCreateSignal("demo", "c1"), { wrapper: wrap(qc) });
    await act(() => result.current.mutateAsync({ kind: "assessment", stance: "favorable" }));
    await waitFor(() => expect(stale(qc, keys.search("demo", "reset token", { limit: 5 }))).toBe(true));
    expect(stale(qc, keys.projects())).toBe(true);
  });

  // Passwords and API keys must not linger in the MutationCache once the form is gone.
  it.each([
    ["useLogin", useLogin],
    ["useSetup", useSetup],
    ["useChangePassword", useChangePassword],
    ["useTestSettings", useTestSettings],
    ["usePutSettings", usePutSettings],
  ] as const)("%s keeps no secret after unmount", async (_n, hook) => {
    vi.stubGlobal("fetch", async () => json({}));
    const qc = new QueryClient();
    const { result, unmount } = renderHook(() => hook(), { wrapper: wrap(qc) });
    await act(() => (result.current.mutateAsync as (v: unknown) => Promise<unknown>)({ password: "s3cret", api_key: "k" }));
    unmount();
    await waitFor(() => expect(qc.getMutationCache().getAll()).toHaveLength(0));
  });
});
