import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, describe, expect, it, vi } from "vitest";
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
import { keys } from "@/lib/api/keys";
import en from "@/messages/en.json";
import { ProjectView } from "./ProjectView";

const push = vi.fn();
const replace = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, replace }),
  useSearchParams: () => new URLSearchParams(window.location.search),
}));

const requested: string[] = [];
const puts: string[] = [];
// The results page asks for 20; the box's dropdown asks for fewer.
const resultsSearch = () => new URL(requested.find((u) => new URL(u).pathname.endsWith("/search") && new URL(u).searchParams.get("limit") === "20")!).searchParams;
let searchBody: unknown;
let authorsBody: unknown = { items: [] };
afterEach(() => {
  vi.unstubAllGlobals();
  requested.length = 0;
  puts.length = 0;
  searchBody = undefined;
  authorsBody = { items: [] };
});

const project = (id: string, name = id, description = "", last_activity_at: string | null = "2026-11-20T00:00:00Z") => ({
  id, name, description, min_favorable: 1, contribution_count: 0, open_question_count: 0, created_at: "2026-11-14T00:00:00Z",
  last_activity_at, states: { Favorable: 0, Unfavorable: 0, Conflict: 0, InsufficientEvidence: 0 }, archived: false,
});

function renderAnon(path: string, body: unknown = { items: [], next_cursor: null }, id = "demo", meta: ReturnType<typeof project> | null = project("demo"), actor: unknown = null, access?: unknown) {
  window.history.replaceState(null, "", path);
  const json = (b: unknown) => new Response(JSON.stringify(b), { headers: { "Content-Type": "application/json" } });
  vi.stubGlobal("fetch", async (req: Request) => {
    requested.push(req.url);
    const path = new URL(req.url).pathname;
    if (path.endsWith("/projects")) return json({ items: meta ? [meta] : [] });
    if (path.endsWith("/authors")) return json(authorsBody);
    if (path.endsWith("/search") && searchBody) return json(searchBody);
    if (req.method !== "GET") {
      puts.push(path);
      return path.includes("/bad/") ? new Response(JSON.stringify({ code: "internal", message: "x" }), { status: 500, headers: { "Content-Type": "application/json" } }) : new Response(null, { status: 204 });
    }
    return json(body);
  });
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData(keys.session(), {
    authenticated: actor !== null, actor, csrf_token: null, must_change_password: false, setup_required: false, access,
  });
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}>
        <ProjectView id={id} />
      </NextIntlClientProvider>
    </QueryClientProvider>,
  );
}

const ANN = { id: "a1", name: "Ann", type: "Human" };
const WRITER = { all: [], projects: { demo: ["contribution:Read", "contribution:Create"] } };
const listed = () => requested.filter((u) => new URL(u).pathname.endsWith("/contributions")).map((u) => new URL(u).searchParams);
const menu = async (name: string) => userEvent.click(await screen.findByRole("button", { name }));
const row = (id: string, o: object = {}) => ({ id, kind: "claim", title: `claim ${id}`, state: "Conflict", created_at: "2026-11-14T00:00:00Z", updated_at: "2026-11-15T00:00:00Z", actor_name: "a", actor_id: "a", ...o });

describe("ProjectView", () => {
  it("sends anonymous visitors to login with the current URL as next", async () => {
    renderAnon("/p/demo/?tab=open");
    const next = "/login/?next=" + encodeURIComponent("/p/demo/?tab=open");
    await userEvent.click(await screen.findByRole("button", { name: en.project.contribute }));
    expect(push).toHaveBeenLastCalledWith(next);
    await menu(en.project.compose_more);
    await userEvent.click(screen.getByRole("menuitem", { name: en.project.ask }));
    expect(push).toHaveBeenLastCalledWith(next);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("opens on Overview: tiles that lead to their list, and the latest activity", async () => {
    renderAnon("/p/demo/", { items: [row("r1")], next_cursor: null }, "demo", { ...project("demo"), contribution_count: 3, states: { Favorable: 1, Unfavorable: 0, Conflict: 2, InsufficientEvidence: 0 } });
    expect((await screen.findByRole("tab", { name: en.project.tab.overview })).getAttribute("aria-selected")).toBe("true");
    expect(await screen.findByRole("region", { name: en.project.overview.recent })).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: new RegExp(`^2\\s*${en.state.Conflict}$`) }));
    expect(replace).toHaveBeenLastCalledWith("/p/demo/?tab=review", { scroll: false });
  });

  it("an empty project's Overview offers to contribute or connect an agent", async () => {
    renderAnon("/p/demo/", undefined, "demo", project("demo", "demo", "", null), ANN, WRITER);
    expect(await screen.findByText(en.project.overview.empty_title)).toBeTruthy();
    await userEvent.click(screen.getAllByRole("button", { name: en.project.connect.button })[0]);
    expect(await screen.findByRole("dialog", { name: en.project.connect.title })).toBeTruthy();
  });

  it("Settings is only for the project's managers: no tab, and its URL is not found", async () => {
    renderAnon("/p/demo/?tab=settings", undefined, "demo", project("demo"), ANN, WRITER);
    expect(await screen.findByText(en.errors.not_found)).toBeTruthy();
    cleanup();
    renderAnon("/p/demo/", undefined, "demo", project("demo"), ANN, WRITER);
    await screen.findByRole("tab", { name: en.project.tab.overview });
    expect(screen.queryByRole("tab", { name: en.project.tab.settings })).toBeNull();
  });

  it("a manager finds the read retention setting under Settings; project:Create also edits and archives", async () => {
    renderAnon("/p/demo/?tab=settings", undefined, "demo", project("demo"), ANN, { all: [], projects: { demo: ["contribution:Read", "member:Manage"] } });
    expect(await screen.findByRole("heading", { name: en.project.reads.heading })).toBeTruthy();
    expect(screen.queryByRole("heading", { name: en.project.settings.general })).toBeNull();
    cleanup();
    renderAnon("/p/demo/?tab=settings", undefined, "demo", project("demo"), ANN, { all: ["contribution:Read", "project:Create"], projects: {} });
    expect(await screen.findByRole("heading", { name: en.project.settings.general })).toBeTruthy();
    expect(screen.getByRole("heading", { name: en.project.settings.danger })).toBeTruthy();
  });

  it("puts conflicts first under review", async () => {
    renderAnon("/p/demo/?tab=review", { items: [row("c1")], next_cursor: null });
    await waitFor(() => expect(screen.getAllByRole("heading", { level: 2 })).toHaveLength(2));
    const headings = screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent);
    expect(headings[0]).toContain(en.state.Conflict);
    expect(headings[1]).toContain(en.state.InsufficientEvidence);
  });

  it("drops an empty review section, heading included", async () => {
    renderAnon("/p/demo/?tab=review");
    expect(await screen.findByText(en.empty.review)).toBeTruthy();
    expect(screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent)).toEqual([
      expect.stringContaining(en.state.Conflict),
    ]);
  });

  it("keeps the project in the path when the tab changes", async () => {
    renderAnon("/p/demo/");
    await userEvent.click(await screen.findByRole("tab", { name: en.project.tab.questions }));
    expect(replace).toHaveBeenLastCalledWith("/p/demo/?tab=questions", { scroll: false });
  });

  it("an old list link keeps its tab when a filter is cleared", async () => {
    renderAnon("/p/demo/?state=Conflict");
    expect((await screen.findByRole("tab", { name: en.project.tab.claims })).getAttribute("aria-selected")).toBe("true");
    await userEvent.click(screen.getByRole("button", { name: en.search.remove_filter.replace("{label}", `${en.search.state_label}: ${en.state.Conflict}`) }));
    expect(replace).toHaveBeenLastCalledWith("/p/demo/?tab=claims", { scroll: false });
  });

  it("a project that does not exist is not found (no create buttons for a typo)", async () => {
    renderAnon("/p/demoo/", undefined, "demoo");
    expect(await screen.findByText(en.errors.not_found)).toBeTruthy();
    expect(screen.queryByRole("button", { name: en.project.contribute })).toBeNull();
  });

  it("an archived project is read-only, with its name: read-only notice, no write controls", async () => {
    renderAnon("/p/old/", undefined, "old", { ...project("old", "Old docs"), archived: true }, ANN, { all: [], projects: { old: ["contribution:Read", "contribution:Create"] } });
    expect(await screen.findByText(en.project.archived_notice)).toBeTruthy();
    expect(screen.getByRole("heading", { level: 1 }).textContent).toContain("Old docs");
    expect(screen.queryByRole("button", { name: en.project.contribute })).toBeNull();
    expect(screen.queryByRole("button", { name: en.project.compose_more })).toBeNull();
  });

  it("an admin sees the same notice on an archived project", async () => {
    renderAnon("/p/demo/", undefined, "demo", { ...project("demo"), archived: true }, ANN, { all: ["*"], projects: {} });
    expect(await screen.findByText(en.project.archived_notice)).toBeTruthy();
    expect(screen.queryByRole("button", { name: en.project.contribute })).toBeNull();
  });

  it("an active project shows no notice and keeps the write buttons", async () => {
    renderAnon("/p/demo/", undefined, "demo", project("demo"), ANN, WRITER);
    expect(await screen.findByRole("button", { name: en.project.contribute })).toBeTruthy();
    expect(screen.queryByText(en.project.archived_notice)).toBeNull();
  });

  it("?compose= (from the command palette) opens the dialog, and closing it drops the parameter", async () => {
    renderAnon("/p/demo/?tab=claims&compose=question", undefined, "demo", project("demo"), ANN, WRITER);
    const dialog = await screen.findByRole("dialog", { name: en.contribution.create.heading_question });
    await userEvent.click(within(dialog).getByRole("button", { name: en.common.cancel }));
    expect(replace).toHaveBeenLastCalledWith("/p/demo/?tab=claims", { scroll: false });
  });

  it("the include-superseded switch is under Display, backed by the URL, and asks the server for include_superseded", async () => {
    renderAnon("/p/demo/?tab=claims&superseded=1");
    await waitFor(() => expect(listed().some((q) => q.get("include_superseded") === "true")).toBe(true));
    await menu(en.search.display);
    const box = screen.getByRole("menuitemcheckbox", { name: en.search.include_superseded });
    expect(box.getAttribute("aria-checked")).toBe("true");
    await userEvent.click(box);
    expect(replace).toHaveBeenLastCalledWith("/p/demo/?tab=claims", { scroll: false });
  });

  it("rows carry their state, badges, activity and answer count", async () => {
    renderAnon("/p/demo/?tab=claims", { items: [row("c1", { state: "Favorable", superseded: true, hidden: true }), row("q1", { kind: "question", state: null, question_status: "open", answer_count: 3 })], next_cursor: null });
    expect(await screen.findByText(en.contribution.superseded)).toBeTruthy();
    expect(screen.getByText(en.contribution.hidden)).toBeTruthy();
    expect(screen.getByRole("img", { name: "3 answers" })).toBeTruthy();
    expect(listed().some((q) => q.get("kind") === "claim")).toBe(true);
  });

  it("lists every question, and an old Open-questions link opens Questions filtered to open", async () => {
    renderAnon("/p/demo/?tab=open");
    expect((await screen.findByRole("tab", { name: en.project.tab.questions })).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByText(`${en.search.status_label}: ${en.question_status.open}`)).toBeTruthy();
    await userEvent.click(screen.getByRole("tab", { name: en.project.tab.review }));
    expect(replace).toHaveBeenLastCalledWith("/p/demo/?tab=review", { scroll: false });
  });

  it("sorts by Recent activity, and Only mine (even from an old link with the id) asks the server for actor=me", async () => {
    renderAnon("/p/demo/?actor=a1&sort=active", undefined, "demo", project("demo"), ANN);
    await waitFor(() => expect(listed().some((q) => q.get("actor") === "me" && q.get("sort") === "active")).toBe(true));
    expect(listed().some((q) => q.get("actor") === "a1")).toBe(false);
    expect(await screen.findByRole("button", { name: `${en.search.sort_label}: ${en.search.sort_active}` })).toBeTruthy();
    expect(screen.getByText(`${en.search.actor_label}: ${en.search.actor_mine}`)).toBeTruthy();
  });

  it("a filter that matches nothing says so and offers to clear it", async () => {
    renderAnon("/p/demo/?tab=claims&state=Unfavorable");
    expect(await screen.findByText(en.empty.filtered)).toBeTruthy();
    await userEvent.click(screen.getAllByRole("button", { name: en.search.clear_filters })[0]);
    expect(replace).toHaveBeenLastCalledWith("/p/demo/?tab=claims", { scroll: false });
  });

  it("shows the name in the title, the id beside it, and the description under it", async () => {
    renderAnon("/p/demo/", undefined, "demo", project("demo", "Demo project", "What this is for."));
    const h1 = await screen.findByRole("heading", { level: 1 });
    await waitFor(() => expect(h1.textContent).toContain("Demo project"));
    expect(screen.getByText("demo", { selector: "code" })).toBeTruthy();
    expect(screen.getByText("What this is for.")).toBeTruthy();
  });

  it("shows just the id when the name equals it", async () => {
    renderAnon("/p/demo/");
    const h1 = await screen.findByRole("heading", { level: 1 });
    expect(h1.textContent).toBe(`${en.project.prefix}: demo`);
  });

  it("connect agent (in the ⋯ menu) opens a dialog with this project's URL, config and command", async () => {
    renderAnon("/p/demo/");
    await menu(en.project.more_actions);
    await userEvent.click(screen.getByRole("menuitem", { name: en.project.connect.button }));
    const dialog = await screen.findByRole("dialog");
    const url = `${window.location.origin}/mcp/demo`;
    expect(dialog.textContent).toContain(url);
    expect(dialog.textContent).toContain(`"url": "${url}"`);
    expect(dialog.textContent).toContain(`claude mcp add --transport http agentverdicts ${url}`);
    expect(dialog.textContent).toContain("Bearer <token>");
    expect(dialog.textContent).toContain(en.project.connect.token_note);
    expect(dialog.textContent).toContain("# Agent Verdicts: rules for agent clients");
    expect(screen.getByRole("button", { name: `${en.admin.once.copy}: ${en.project.connect.rules}` })).toBeTruthy();
  });

  it("search sends the tab's state and status to the server and shows every result it returns", async () => {
    const hit = (id: string, state: string) => ({ id, kind: "claim", title: `hit ${id}`, snippet: "", state, created_at: "2026-11-14T00:00:00Z", actor_name: "a" });
    searchBody = { results: [hit("a", "Conflict"), hit("b", "Conflict"), hit("c", "Favorable")], degraded: false };
    renderAnon("/p/demo/?q=hello&state=Conflict");
    expect(await screen.findByText("hit a")).toBeTruthy();
    // No client-side filtering any more: the server already applied it.
    expect(screen.getByText("hit c")).toBeTruthy();
    expect(screen.getByText("3 results for “hello”")).toBeTruthy();
    const sent = resultsSearch();
    expect(sent.getAll("state")).toEqual(["Conflict"]);
    expect(sent.get("kind")).toBe("claim");
  });

  it("search on Questions sends kind and question_status", async () => {
    searchBody = { results: [], degraded: false };
    renderAnon("/p/demo/?q=hello&tab=questions&status=answered");
    await waitFor(() => expect(requested.some((u) => u.includes("/search") && u.includes("limit=20"))).toBe(true));
    const sent = resultsSearch();
    expect(sent.get("kind")).toBe("question");
    expect(sent.getAll("question_status")).toEqual(["answered"]);
  });

  it("the author filter's options come from the authors route, not the loaded pages", async () => {
    authorsBody = { items: [{ id: "a1", name: "Ann" }, { id: "a2", name: "Zed" }] };
    renderAnon("/p/demo/?actor=a2", undefined, "demo", project("demo"), ANN);
    await waitFor(() => expect(requested.some((u) => new URL(u).pathname.endsWith("/projects/demo/authors"))).toBe(true));
    expect(await screen.findByText(`${en.search.actor_label}: Zed`)).toBeTruthy();
  });

  it("a moderator hides selected rows in bulk and is told how many failed", async () => {
    const { toast } = await import("sonner");
    renderAnon("/p/demo/?tab=claims", { items: [row("ok"), row("bad")], next_cursor: null }, "demo", project("demo"), ANN, { all: [], projects: { demo: ["contribution:Read", "content:Moderate"] } });
    await userEvent.click(await screen.findByRole("checkbox", { name: en.project.bulk.select.replace("{label}", "claim ok") }));
    await userEvent.click(screen.getByRole("checkbox", { name: en.project.bulk.select.replace("{label}", "claim bad") }));
    const bar = screen.getByRole("region", { name: en.project.bulk.label });
    expect(bar.textContent).toContain("2 selected");
    await userEvent.click(within(bar).getByRole("button", { name: en.contribution.hide }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: en.contribution.hide }));
    await waitFor(() => expect(puts).toEqual(["/api/v1/projects/demo/contributions/ok/hidden", "/api/v1/projects/demo/contributions/bad/hidden"]));
    await waitFor(() => expect(toast.error).toHaveBeenCalled());
    expect(toast.success).toHaveBeenCalled();
  });

  it("j and k walk the rows; typing in the search box does not", async () => {
    renderAnon("/p/demo/?tab=claims", { items: [row("one"), row("two")], next_cursor: null });
    const first = await screen.findByRole("link", { name: "claim one" });
    await userEvent.keyboard("j");
    expect(document.activeElement).toBe(first);
    await userEvent.keyboard("j");
    expect(document.activeElement).toBe(screen.getByRole("link", { name: "claim two" }));
    await userEvent.click(screen.getByRole("combobox", { name: en.search.label }));
    await userEvent.keyboard("k");
    expect(document.activeElement?.getAttribute("role")).toBe("combobox");
  });
});
