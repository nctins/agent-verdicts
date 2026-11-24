import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { keys } from "@/lib/api/keys";
import en from "@/messages/en.json";
import ManagePage from "./page";
import ActorsPage from "./actors/page";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/manage/",
  useSearchParams: () => new URLSearchParams(""),
}));
vi.mock("@/components/AppShell", () => ({ AppShell: ({ children }: { children: React.ReactNode }) => children }));

const json = (b: unknown) => new Response(JSON.stringify(b), { headers: { "Content-Type": "application/json" } });
const actor = (id: string, name: string) => ({ id, name, type: "Human", disabled: false, created_at: "", has_login: false });
const states = { Favorable: 0, Unfavorable: 0, Conflict: 0, InsufficientEvidence: 0 };
const requested: URL[] = [];

beforeEach(() => {
  requested.length = 0;
  vi.stubGlobal("fetch", async (r: Request) => {
    const u = new URL(r.url);
    requested.push(u);
    if (u.pathname.endsWith("/admin/overview")) return json({ project_count: 1, contribution_count: 7, open_question_count: 2, states, recent: [], attention: { conflicts_7d: 2, stale_questions: 0 } });
    if (u.pathname.endsWith("/admin/actors"))
      return json({ items: u.searchParams.get("q") ? [actor("a3", "Bot found")] : [actor("a1", "Mona"), actor("a2", "Member One")] });
    return json({ items: [] });
  });
});
afterEach(() => vi.unstubAllGlobals());

function renderPage(page: React.ReactNode, access: unknown) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData(keys.session(), {
    authenticated: true, actor: { id: "a1", name: "Mona", type: "Human" }, access,
    csrf_token: "t", must_change_password: false, setup_required: false,
  });
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}>{page}</NextIntlClientProvider>
    </QueryClientProvider>,
  );
}
const manager = { all: [], projects: { demo: ["contribution:Read", "member:Manage"] } };
const admin = { all: ["*"], projects: {} };
const actorQueries = () => requested.filter((u) => u.pathname.endsWith("/admin/actors")).map((u) => u.searchParams.get("q"));

describe("manage overview", () => {
  it("a project manager sees the overview", async () => {
    renderPage(<ManagePage />, manager);
    expect(await screen.findByText(en.manage.dashboard.projects)).toBeTruthy();
    expect(requested.some((u) => u.pathname.endsWith("/admin/overview"))).toBe(true);
  });
});

describe("overview: needs attention", () => {
  it("lists only what is non-zero, each with a link to act on it", async () => {
    renderPage(<ManagePage />, manager);
    const card = await screen.findByText(en.admin_ui.dashboard.att.conflicts.replace("{count, number}", "2"));
    expect(card.closest("a")!.getAttribute("href")).toBe("#health");
    expect(screen.queryByText(en.admin_ui.dashboard.att.stale.replace("{count, number}", "0"))).toBeNull();
  });
});

describe("People page for a manager", () => {
  it("lists the members the server returns, without a q", async () => {
    renderPage(<ActorsPage />, manager);
    expect(await screen.findByRole("link", { name: "Member One" })).toBeTruthy();
    expect(actorQueries().every((q) => q === null)).toBe(true);
    expect(screen.queryByRole("button", { name: en.manage.people.add })).toBeNull();
  });

  it("searches by name on the server once the text has 2 characters, and never sends a shorter one", async () => {
    renderPage(<ActorsPage />, manager);
    await screen.findByRole("link", { name: "Member One" });
    const box = screen.getByLabelText(en.manage.people.search);
    await userEvent.type(box, "b");
    await new Promise((r) => setTimeout(r, 400));
    expect(actorQueries().every((q) => q === null)).toBe(true);
    await userEvent.type(box, "o");
    expect(await screen.findByRole("link", { name: "Bot found" })).toBeTruthy();
    expect(actorQueries()).toContain("bo");
    expect(actorQueries().filter((q) => q === "b")).toEqual([]);
  });
});

describe("People page for actor:Manage", () => {
  it("filters the full list locally and never sends q", async () => {
    renderPage(<ActorsPage />, admin);
    await screen.findByRole("link", { name: "Member One" });
    await userEvent.type(screen.getByLabelText(en.manage.people.filter), "mem");
    await waitFor(() => expect(screen.queryByRole("link", { name: "Mona" })).toBeNull());
    await new Promise((r) => setTimeout(r, 400));
    expect(actorQueries().every((q) => q === null)).toBe(true);
  });
});
