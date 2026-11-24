import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { keys } from "@/lib/api/keys";
import en from "@/messages/en.json";
import AuditPage from "./page";

let query = "";
const replace = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace }),
  usePathname: () => "/manage/audit/",
  useSearchParams: () => new URLSearchParams(query),
}));
vi.mock("@/components/AppShell", () => ({ AppShell: ({ children }: { children: React.ReactNode }) => children }));
vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });

const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json" } });
const ev = (o: object) => ({ id: "e", at: "2026-11-14T00:00:00Z", actor_id: "a1", actor_name: "Alice", action: "actor.create", target_type: "actor", target_id: "b1", project_id: null, detail: {}, ...o });
let requests: URL[];
let session: object;
let pages: { items: object[]; next_cursor: string | null }[];

beforeEach(() => {
  query = "";
  replace.mockClear();
  requests = [];
  pages = [{ items: [ev({ id: "1", detail: { type: "Agent" } })], next_cursor: null }];
  vi.stubGlobal("fetch", async (r: Request) => {
    const u = new URL(r.url);
    if (u.pathname.endsWith("/session")) return json(session);
    if (u.pathname.endsWith("/admin/audit")) {
      requests.push(u);
      return json(pages[u.searchParams.get("cursor") ? 1 : 0]);
    }
    if (u.pathname.endsWith("/admin/actors"))
      return json({ items: [{ id: "a1", type: "Human", name: "Alice", disabled: false, created_at: "", username: "alice", has_login: true }, { id: "b1", type: "Agent", name: "Bot", disabled: false, created_at: "", username: null, has_login: false }] });
    if (u.pathname.endsWith("/projects")) return json({ items: [{ id: "p1", contribution_count: 0, open_question_count: 0, states: {}, created_at: "" }] });
    return json({ items: [] });
  });
});

function renderAs(access: object) {
  session = { authenticated: true, actor: { id: "me", name: "Me", type: "Human" }, access, csrf_token: "t", must_change_password: false, setup_required: false };
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(keys.session(), session);
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}><AuditPage /></NextIntlClientProvider>
    </QueryClientProvider>,
  );
}
const admin = { all: ["*"], projects: {} };

describe("audit page", () => {
  it("shows who, the action label, the target name and the detail as text", async () => {
    renderAs(admin);
    const row = (await screen.findByRole("cell", { name: en.manage.audit.action.actor.create })).closest("tr")!;
    expect(within(row).getByText("Alice")).toBeTruthy();
    expect(within(row).getByText("Bot")).toBeTruthy(); // the target actor id, resolved to its name
    expect(within(row).getByText("type: Agent")).toBeTruthy();
  });
  it("labels CLI events and shows an unknown action by its raw name", async () => {
    pages = [{ items: [ev({ id: "1", actor_id: null, actor_name: "", action: "future.thing", detail: { note: "<b>x</b>" } })], next_cursor: null }];
    renderAs(admin);
    const row = (await screen.findByText("future.thing")).closest("tr")!;
    expect(within(row).getByText(en.manage.audit.cli)).toBeTruthy();
    expect(within(row).getByText("note: <b>x</b>")).toBeTruthy(); // text, not markup
  });
  it("reads the filters from the URL and writes a change back", async () => {
    query = "actor=a1&project=p1";
    renderAs(admin);
    await screen.findByRole("cell", { name: en.manage.audit.action.actor.create });
    await screen.findByRole("option", { name: "Bot" });
    expect(requests[0].searchParams.get("actor")).toBe("a1");
    expect(requests[0].searchParams.get("project")).toBe("p1");
    await userEvent.selectOptions(screen.getByLabelText(en.manage.audit.filter_actor), "b1");
    expect(replace).toHaveBeenCalledWith("/manage/audit/?actor=b1&project=p1", { scroll: false });
  });
  it("loads the next page with the cursor", async () => {
    pages = [{ items: [ev({ id: "1" })], next_cursor: "c2" }, { items: [ev({ id: "2", action: "actor.disable" })], next_cursor: null }];
    renderAs(admin);
    await userEvent.click(await screen.findByRole("button", { name: en.manage.audit.more }));
    expect(await screen.findByRole("cell", { name: en.manage.audit.action.actor.disable })).toBeTruthy();
    expect(requests[1].searchParams.get("cursor")).toBe("c2");
    await waitFor(() => expect(screen.queryByRole("button", { name: en.manage.audit.more })).toBeNull());
  });
  it("opens to a project manager", async () => {
    pages = [{ items: [ev({ id: "1", action: "assignment.create", target_type: "assignment", target_id: "x", project_id: "p1" })], next_cursor: null }];
    renderAs({ all: [], projects: { p1: ["contribution:Read", "member:Manage"] } });
    expect(await screen.findByRole("cell", { name: en.manage.audit.action.assignment.create })).toBeTruthy();
  });
  it("filters by action and day range, and expands an event shared by link", async () => {
    query = "event=1&from=2026-11-14&to=2026-11-14";
    renderAs(admin);
    const row = (await screen.findByRole("cell", { name: en.manage.audit.action.actor.create })).closest("tr")!;
    expect(row.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByText(en.admin_ui.audit.permalink).closest("a")!.getAttribute("href")).toBe("/manage/audit/?event=1&from=2026-11-14&to=2026-11-14");
    expect(requests[0].searchParams.get("from")).toBe("2026-11-14");
    expect(requests[0].searchParams.has("event")).toBe(false); // the link opens a row; it is not a server filter
    await userEvent.selectOptions(screen.getByLabelText(en.admin_ui.audit.filter_action), "credential.issue");
    expect(replace).toHaveBeenLastCalledWith("/manage/audit/?from=2026-11-14&to=2026-11-14&action=credential.issue", { scroll: false });
  });
  it("says so when there is nothing", async () => {
    pages = [{ items: [], next_cursor: null }];
    renderAs(admin);
    expect(await screen.findByText(en.manage.audit.empty)).toBeTruthy();
  });
});
