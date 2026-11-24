import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { keys } from "@/lib/api/keys";
import { useContribution } from "@/lib/api/queries";
import { applyAssessment, type ContributionDetail } from "@/lib/stance";
import en from "@/messages/en.json";
import { StanceButtons } from "./StanceButtons";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push }) }));
const toastError = vi.fn();
vi.mock("sonner", () => ({ toast: { error: (m: string) => toastError(m), success: vi.fn() } }));

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const me = { id: "me", type: "human", name: "Me" };
const session = {
  authenticated: true, actor: me, csrf_token: "t", must_change_password: false, setup_required: false,
  access: { all: [] as string[], projects: { demo: ["contribution:Read", "signal:Create"] } },
};
const viewer = { ...session, access: { all: [], projects: { demo: ["contribution:Read"] } } };
const empty = (): ContributionDetail => ({
  id: "c1", project_id: "demo", kind: "claim", title: "t", body: "", sources: [], actor_id: "a0", actor_name: "A0",
  created_at: "2026-11-14T00:00:00Z", state: "InsufficientEvidence",
  counts: { favorable: 0, unfavorable: 0, neutral: 0, favorable_actors: 0 },
  signals: { favorable: [], unfavorable: [], neutral: [], history: [] },
});

// A tiny server: GET returns what POSTs have committed; POST commits after 100 ms.
let server: ContributionDetail;
let posts: number;
let fail: boolean;
let archived = false;
let lastBody: string;
let lastSources: unknown;
let deletes: string[];
beforeEach(() => {
  server = empty();
  posts = 0;
  fail = false;
  archived = false;
  lastBody = "";
  deletes = [];
  push.mockClear();
  toastError.mockClear();
  vi.stubGlobal("fetch", async (r: Request) => {
    const u = new URL(r.url).pathname;
    if (u.endsWith("/session")) return json(200, session);
    if (u.endsWith("/projects")) return json(200, { items: [{ id: "demo", archived }] });
    if (r.method === "GET") return json(200, server);
    if (r.method === "DELETE") {
      deletes.push(new URL(r.url).pathname);
      server = { ...server, counts: { favorable: 0, unfavorable: 0, neutral: 0, favorable_actors: 0 }, signals: { favorable: [], unfavorable: [], neutral: [], history: [] } };
      return new Response(null, { status: 204 });
    }
    posts++;
    const body = (await r.json()) as { stance: "favorable"; body: string; sources?: string[] };
    lastBody = body.body;
    lastSources = body.sources;
    await new Promise((ok) => setTimeout(ok, 100));
    if (fail) return json(500, { code: "internal", message: "boom" });
    server = JSON.parse(JSON.stringify(applyAssessment(server, me, body.stance, new Date().toISOString())).replaceAll('"optimistic"', `"s${posts}"`));
    return json(201, { id: "s" + posts, actor_id: "me", actor_name: "Me", replaced_stance: null });
  });
});
afterEach(() => vi.unstubAllGlobals());

function Harness() {
  const { data } = useContribution("demo", "c1");
  return data ? <StanceButtons project="demo" detail={data} /> : null;
}

const supportCount = () => document.querySelector('[data-count="favorable"]')?.textContent ?? "";

function setup({ withSession = true, as = session as object } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  if (withSession) qc.setQueryData(keys.session(), as);
  qc.setQueryData(keys.contribution("demo", "c1"), empty());
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}>
        <Harness />
      </NextIntlClientProvider>
    </QueryClientProvider>,
  );
  const btn = (name: RegExp) => screen.getByRole("button", { name });
  const confirm = () => userEvent.click(screen.getByRole("button", { name: /^Confirm/ }));
  // The counts sit under the buttons; `support()` reads the button, `supportCount()` its tally.
  return { qc, confirm, support: () => btn(/^Support/), all: () => within(screen.getByRole("group")).getAllByRole("button") };
}

describe("StanceButtons", () => {
  it("shows the counts but cannot be pressed without signal:Create on the project", () => {
    const { all } = setup({ as: viewer });
    for (const b of all()) expect((b as HTMLButtonElement).disabled).toBe(true);
  });

  it("a click only opens a confirmation with an optional reason; nothing is sent yet", async () => {
    const { support } = setup();
    await userEvent.click(support());
    expect(posts).toBe(0);
    expect(supportCount()).toContain("0");
    expect(screen.getByLabelText(/reason/i)).toBeTruthy();
  });

  it("cancel sends nothing", async () => {
    const { support } = setup();
    await userEvent.click(support());
    await userEvent.click(screen.getByRole("button", { name: en.common.cancel }));
    expect(posts).toBe(0);
    expect(screen.queryByLabelText(/reason/i)).toBeNull();
  });

  it("confirming counts at once, marks the choice and locks all three until done", async () => {
    const { support, all, confirm } = setup();
    await userEvent.click(support());
    await confirm();
    expect(supportCount()).toContain("1");
    expect(support().getAttribute("aria-pressed")).toBe("true");
    expect(all().every((b) => b.hasAttribute("disabled"))).toBe(true);
    await waitFor(() => expect(all().every((b) => !b.hasAttribute("disabled"))).toBe(true));
    expect(supportCount()).toContain("1");
    expect(posts).toBe(1);
    expect(screen.queryByLabelText(/reason/i)).toBeNull();
  });

  it("the reason goes with the stance", async () => {
    const { support, confirm } = setup();
    await userEvent.click(support());
    await userEvent.type(screen.getByLabelText(/reason/i), "read the code");
    await confirm();
    await waitFor(() => expect(posts).toBe(1));
    expect(lastBody).toBe("read the code");
  });

  it("sources typed with the stance are sent with it", async () => {
    const { support, confirm } = setup();
    await userEvent.click(support());
    await userEvent.type(screen.getByLabelText(en.sources.label), "src/pool.go{enter}https://example.test/x");
    await confirm();
    await waitFor(() => expect(posts).toBe(1));
    expect(lastSources).toEqual(["src/pool.go", "https://example.test/x"]);
  });

  it("my current stance again: only to add a reason, so an empty one cannot be sent", async () => {
    const { support, all, confirm } = setup();
    await userEvent.click(support());
    await confirm();
    await waitFor(() => expect(all().every((b) => !b.hasAttribute("disabled"))).toBe(true));
    await userEvent.click(support());
    expect(screen.getByRole("button", { name: /^Confirm/ }).hasAttribute("disabled")).toBe(true);
    await userEvent.type(screen.getByLabelText(/reason/i), "why");
    await confirm();
    await waitFor(() => expect(posts).toBe(2));
  });

  it("rolls back and toasts when the server fails", async () => {
    fail = true;
    const { support, confirm } = setup();
    await userEvent.click(support());
    await confirm();
    expect(supportCount()).toContain("1");
    await waitFor(() => expect(toastError).toHaveBeenCalled());
    await waitFor(() => expect(supportCount()).toContain("0"));
    expect(support().getAttribute("aria-pressed")).toBe("false");
  });

  it("a refetch while pending does not undo the optimistic count", async () => {
    const { qc, support, confirm } = setup();
    await userEvent.click(support());
    await confirm();
    await act(() => qc.refetchQueries({ queryKey: keys.contribution("demo", "c1") }));
    expect(qc.getQueryData<ContributionDetail>(keys.contribution("demo", "c1"))?.counts).toEqual({ favorable: 1, unfavorable: 0, neutral: 0, favorable_actors: 1 });
    await new Promise((ok) => setTimeout(ok, 10)); // let React render whatever the refetch left
    expect(supportCount()).toContain("1");
    await waitFor(() => expect(support().hasAttribute("disabled")).toBe(false));
    expect(supportCount()).toContain("1");
  });

  it("stays disabled while the session is still loading", async () => {
    const pending = () => new Promise<Response>(() => {}); // session never resolves
    vi.stubGlobal("fetch", pending);
    const { all } = setup({ withSession: false });
    expect(all().every((b) => b.hasAttribute("disabled"))).toBe(true);
    await userEvent.click(all()[0]);
    expect(push).not.toHaveBeenCalled();
  });

  it("sends a logged-out visitor to login instead of posting", async () => {
    const { qc, support } = setup();
    act(() => qc.setQueryData(keys.session(), { ...session, authenticated: false, actor: null, csrf_token: null }));
    await userEvent.click(support());
    expect(push).toHaveBeenCalledWith(expect.stringMatching(/^\/login\/\?next=/));
    expect(posts).toBe(0);
  });

  it("picking my current stance offers to withdraw it; withdrawing sends DELETE for my signal", async () => {
    const { support, all, confirm } = setup();
    await userEvent.click(support());
    expect(screen.queryByRole("button", { name: en.contribution.confirm.withdraw })).toBeNull(); // nothing to withdraw yet
    await confirm();
    await waitFor(() => expect(all().every((b) => !b.hasAttribute("disabled"))).toBe(true));
    await userEvent.click(support());
    expect(screen.getByText(en.contribution.confirm.withdraw_hint)).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: en.contribution.confirm.withdraw }));
    await waitFor(() => expect(deletes).toEqual(["/api/v1/projects/demo/contributions/c1/signals/s1"]));
    await waitFor(() => expect(support().getAttribute("aria-pressed")).toBe("false"));
  });

  it("another stance does not offer to withdraw", async () => {
    const { support, all, confirm } = setup();
    await userEvent.click(support());
    await confirm();
    await waitFor(() => expect(all().every((b) => !b.hasAttribute("disabled"))).toBe(true));
    await userEvent.click(all()[1]);
    expect(screen.queryByRole("button", { name: en.contribution.confirm.withdraw })).toBeNull();
  });

  it("in an archived project the buttons are gone and only the counts remain", async () => {
    archived = true;
    setup();
    await waitFor(() => expect(screen.queryByRole("group")).toBeNull());
    expect(supportCount()).toBe(`0 ${en.stance.favorable}`);
  });
});
