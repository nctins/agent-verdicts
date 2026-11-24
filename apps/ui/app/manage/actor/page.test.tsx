import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { keys } from "@/lib/api/keys";
import en from "@/messages/en.json";
import ActorDetail from "./page";

let query = "id=a1";
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/manage/actor/",
  useSearchParams: () => new URLSearchParams(query),
}));
vi.mock("@/components/AppShell", () => ({ AppShell: ({ children }: { children: React.ReactNode }) => children }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
import { toast } from "sonner";
vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });

const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json" } });
const alice = { id: "a1", type: "Human", name: "Alice", disabled: false, created_at: "2026-01-01T00:00:00Z", username: null, has_login: false };
const p1 = { id: "p1", contribution_count: 0, open_question_count: 0, states: {}, created_at: "" };
let posts: unknown[];
let detail: object;
let audits: string[];
let patchStatus = 200;
let session: object;
let held: object[];
beforeEach(() => {
  query = "id=a1";
  held = [];
  posts = [];
  audits = [];
  patchStatus = 200;
  detail = { ...alice, credentials: [], assignments: [] };
  vi.stubGlobal("fetch", async (r: Request) => {
    const u = new URL(r.url).pathname;
    if (u.endsWith("/admin/audit")) {
      audits.push(new URL(r.url).search);
      return json({ items: [{ id: "e1", at: "2026-11-14T00:00:00Z", actor_id: "a1", actor_name: "Alice", action: "credential.issue", target_type: "credential", target_id: "c9", project_id: null, detail: {} }], next_cursor: null });
    }
    if (r.method === "PATCH") {
      posts.push(await r.json());
      return patchStatus === 200 ? json({ ...detail }) : json({ code: "conflict", message: "last admin" }, patchStatus);
    }
    if (r.method === "POST" && u.endsWith("/credentials")) {
      posts.push(await r.json());
      return json({ id: "c1", token: "tok-secret", label: "", expires_at: null }, 201);
    }
    if (u.endsWith("/session")) return json(session);
    if (r.method === "POST" && u.endsWith("/admin/assignments")) {
      const b = await r.json();
      posts.push(b);
      return json({ id: "n", actor_name: "Alice", role_name: b.role_id, created_at: "", ...b }, 201);
    }
    if (u.endsWith("/admin/actors/a1")) return json(detail);
    if (u.endsWith("/admin/actors")) {
      const q = new URL(r.url).searchParams.get("q");
      return json({ items: q === "Bot" ? [{ ...alice, id: "x1", name: "Bot" }] : q ? [] : [alice] });
    }
    if (r.method === "DELETE") { posts.push({ deleted: u.split("/").pop() }); return new Response(null, { status: 204 }); }
    if (u.endsWith("/admin/assignments")) return json({ items: held });
    if (u.endsWith("/admin/roles")) return json({ items: [{ id: "viewer", name: "viewer", managed: true, policy_ids: [], actions: [], created_at: "" }] });
    if (u.endsWith("/projects")) return json({ items: [p1] });
    return json({ items: [] });
  });
});

function renderAs(access: object) {
  session = { authenticated: true, actor: { id: "me", name: "Me", type: "Human" }, access, csrf_token: "t", must_change_password: false, setup_required: false };
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(keys.session(), session);
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}><ActorDetail /></NextIntlClientProvider>
    </QueryClientProvider>,
  );
}

/** Opens the side sheet that gives a role; returns it. */
async function openSheet() {
  await userEvent.click(await screen.findByRole("button", { name: en.manage.people.add_role }));
  const sheet = await screen.findByRole("dialog");
  await within(sheet).findByRole("option", { name: "viewer" });
  return sheet;
}
const submitIn = (sheet: HTMLElement) => userEvent.click(within(sheet).getByRole("button", { name: en.manage.people.add_role }));
const tab = async (name: string) => userEvent.click(await screen.findByRole("tab", { name }));

describe("actor page", () => {
  it("gives a role right on the actor's page", async () => {
    renderAs({ all: ["*"], projects: {} });
    const sheet = await openSheet();
    await userEvent.selectOptions(within(sheet).getByLabelText(en.manage.access.role), "viewer");
    await userEvent.click(within(sheet).getByLabelText(en.manage.access.scope_all));
    await submitIn(sheet);
    await waitFor(() => expect(posts).toEqual([{ actor_id: "a1", role_id: "viewer", project_id: null }]));
  });
  it("gives nothing on chosen projects until one is picked", async () => {
    renderAs({ all: ["*"], projects: {} });
    const sheet = await openSheet();
    await userEvent.selectOptions(within(sheet).getByLabelText(en.manage.access.role), "viewer");
    await submitIn(sheet);
    expect(await within(sheet).findByText(en.manage.access.pick_one)).toBeTruthy();
    await userEvent.click(within(sheet).getByLabelText("p1"));
    await submitIn(sheet);
    await waitFor(() => expect(posts).toEqual([{ actor_id: "a1", role_id: "viewer", project_id: "p1" }]));
  });
  it("warns, without blocking, when a role already held covers the pick", async () => {
    held = [{ id: "h1", actor_id: "a1", actor_name: "Alice", role_id: "viewer", role_name: "viewer", project_id: null, created_at: "" }];
    renderAs({ all: ["*"], projects: {} });
    const sheet = await openSheet();
    await userEvent.selectOptions(within(sheet).getByLabelText(en.manage.access.role), "viewer");
    await userEvent.click(within(sheet).getByLabelText("p1"));
    const label = `viewer · ${en.manage.access.all_projects}`;
    expect(await within(sheet).findByText(en.manage.access.covered.replace("{label}", label))).toBeTruthy();
    await submitIn(sheet);
    await waitFor(() => expect(posts).toEqual([{ actor_id: "a1", role_id: "viewer", project_id: "p1" }]));
  });
  it("marks a held role that another held role already covers", async () => {
    held = [
      { id: "h1", actor_id: "a1", actor_name: "Alice", role_id: "viewer", role_name: "viewer", project_id: null, created_at: "" },
      { id: "h2", actor_id: "a1", actor_name: "Alice", role_id: "viewer", role_name: "viewer", project_id: "p1", created_at: "" },
    ];
    renderAs({ all: ["*"], projects: {} });
    const label = `viewer · ${en.manage.access.all_projects}`;
    expect(await screen.findByText(en.manage.access.redundant.replace("{label}", label))).toBeTruthy();
  });
  it("shows effective access per project, and offers Undo after removing a role", async () => {
    held = [{ id: "h1", actor_id: "a1", actor_name: "Alice", role_id: "viewer", role_name: "viewer", project_id: "p1", created_at: "" }];
    renderAs({ all: ["*"], projects: {} });
    const table = (await screen.findByRole("heading", { name: en.admin_ui.actor.effective })).closest("section")!;
    expect(within(table).getByRole("rowheader", { name: "p1" })).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: `${en.manage.access.remove}: viewer · p1` }));
    await waitFor(() => expect(posts).toEqual([{ deleted: "h1" }]));
    const undo = vi.mocked(toast.success).mock.calls.at(-1)?.[1] as unknown as { action: { onClick: () => void } };
    undo.action.onClick();
    await waitFor(() => expect(posts).toContainEqual({ actor_id: "a1", role_id: "viewer", project_id: "p1" }));
  });
  it("opens to a project manager with roles only: no profile, password or tokens", async () => {
    renderAs({ all: [], projects: { p1: ["contribution:Read", "member:Manage"] } });
    expect(await screen.findByRole("heading", { level: 1, name: /Alice/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: en.manage.people.add_role })).toBeTruthy();
    expect(screen.queryByText(en.manage.actor.section.profile)).toBeNull();
    expect(screen.getByText(en.manage.people.manager_note)).toBeTruthy();
  });
  it("lists the actor's latest audit events with a link to the filtered log", async () => {
    renderAs({ all: ["*"], projects: {} });
    await tab(en.admin_ui.actor.tab_activity);
    expect(await screen.findByRole("heading", { name: en.manage.audit.recent })).toBeTruthy();
    expect(await screen.findByText(en.manage.audit.action.credential.issue)).toBeTruthy();
    expect(audits[0]).toContain("actor=a1");
    expect(screen.getByRole("link", { name: en.manage.audit.see_all }).getAttribute("href")).toMatch(/^\/manage\/audit\/?\?actor=a1$/);
  });
  it("hides the activity section from a project manager", async () => {
    renderAs({ all: [], projects: { p1: ["contribution:Read", "member:Manage"] } });
    await screen.findByRole("heading", { level: 1, name: /Alice/ });
    expect(screen.queryByText(en.manage.audit.recent)).toBeNull();
  });
  it("asks before disabling, and enabling needs no confirmation", async () => {
    renderAs({ all: ["*"], projects: {} });
    await userEvent.click(await screen.findByRole("button", { name: en.manage.actor.button.disable }));
    expect(posts).toEqual([]);
    const dialog = await screen.findByRole("dialog", { name: en.manage.actor.disable_title });
    await userEvent.click(within(dialog).getByRole("button", { name: en.common.cancel }));
    expect(posts).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: en.manage.actor.button.disable }));
    const confirm = await screen.findByRole("dialog");
    const go = within(confirm).getByRole("button", { name: en.manage.actor.disable_confirm });
    expect((go as HTMLButtonElement).disabled).toBe(true); // until the name is typed
    await userEvent.type(within(confirm).getByRole("textbox"), "Alice");
    await userEvent.click(go);
    await waitFor(() => expect(posts).toEqual([{ disabled: true }]));
  });
  it("enables without a dialog", async () => {
    detail = { ...alice, disabled: true, credentials: [], assignments: [] };
    renderAs({ all: ["*"], projects: {} });
    await userEvent.click(await screen.findByRole("button", { name: en.manage.actor.button.enable }));
    await waitFor(() => expect(posts).toEqual([{ disabled: false }]));
    expect(screen.queryByRole("dialog")).toBeNull();
  });
  it("explains a 409 on disabling the last admin", async () => {
    patchStatus = 409;
    renderAs({ all: ["*"], projects: {} });
    await userEvent.click(await screen.findByRole("button", { name: en.manage.actor.button.disable }));
    const confirm = await screen.findByRole("dialog");
    await userEvent.type(within(confirm).getByRole("textbox"), "Alice");
    await userEvent.click(within(confirm).getByRole("button", { name: en.manage.actor.disable_confirm }));
    expect(await screen.findByText(en.manage.actor.error.last_admin)).toBeTruthy();
  });
});

describe("agent tokens", () => {
  const bot = { ...alice, id: "a1", type: "Agent", name: "Bot" };
  const cred = (o: object) => ({ id: "c", created_at: "2026-10-01T00:00:00Z", revoked_at: null, label: "", expires_at: null, last_used_at: null, ...o });
  it.each([["30 days", 30], ["90 days", 90], ["365 days", 365], ["Never expires", null]] as const)("issues with %s", async (choice, days) => {
    detail = { ...bot, credentials: [], assignments: [] };
    renderAs({ all: ["*"], projects: {} });
    await tab(en.admin_ui.actor.tab_login);
    await userEvent.selectOptions(await screen.findByLabelText(en.manage.credential.expiry_field), choice);
    await userEvent.type(screen.getByLabelText(en.manage.credential.label_field), "ci");
    await userEvent.click(screen.getByRole("button", { name: en.manage.credential.button.issue }));
    await waitFor(() => expect(posts).toEqual([{ label: "ci", expires_in_days: days }]));
    expect(await screen.findByDisplayValue("tok-secret")).toBeTruthy();
  });
  it("shows label, expiry, last use and the warning badges", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-11-15T00:00:00Z"));
    try {
      detail = { ...bot, assignments: [], credentials: [
        cred({ id: "c-never", label: "forever" }),
        cred({ id: "c-soon", expires_at: "2026-11-18T00:00:00Z", last_used_at: "2026-11-14T00:00:00Z" }),
        cred({ id: "c-old", expires_at: "2026-11-01T00:00:00Z" }),
        cred({ id: "c-ok", expires_at: "2027-02-01T00:00:00Z" }),
      ] };
      renderAs({ all: ["*"], projects: {} });
      await tab(en.admin_ui.actor.tab_login);
      const row = async (id: string) => (await screen.findByText(id)).closest("tr")!;
      expect(within(await row("c-never")).getByText("forever")).toBeTruthy();
      expect(within(await row("c-never")).getAllByText(en.manage.credential.never_expires)).toHaveLength(1);
      expect(within(await row("c-never")).getByText(en.manage.credential.never_used)).toBeTruthy();
      expect(within(await row("c-soon")).getByText(en.manage.credential.expires_soon)).toBeTruthy();
      expect(within(await row("c-soon")).getByText("1 day ago")).toBeTruthy();
      expect(within(await row("c-old")).getByText(en.manage.credential.expired)).toBeTruthy();
      const ok = within(await row("c-ok"));
      expect(ok.queryByText(en.manage.credential.expired)).toBeNull();
      expect(ok.queryByText(en.manage.credential.expires_soon)).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("actor page for a manager: the name comes from the server, not the URL", () => {
  const mgr = { all: [], projects: { p1: ["contribution:Read", "member:Manage"] } };
  it("resolves the actor through the hinted search and shows the server's name with the role controls", async () => {
    query = "id=x1&name=Bot";
    renderAs(mgr);
    expect(await screen.findByRole("heading", { level: 1, name: /Bot/ })).toBeTruthy();
    expect(await screen.findByRole("button", { name: en.manage.people.add_role })).toBeTruthy();
  });
  it("shows no name from the URL when the search does not return that id", async () => {
    query = "id=x1&name=Alice";
    renderAs(mgr);
    expect((await screen.findByRole("alert")).textContent).toBe(en.admin.actor.not_found);
    expect(screen.queryByText(/Alice/)).toBeNull();
    expect(screen.queryByRole("button", { name: en.manage.people.add_role })).toBeNull();
  });
  it("is not found without a hint when the actor is not in the default list", async () => {
    query = "id=x1";
    renderAs(mgr);
    expect((await screen.findByRole("alert")).textContent).toBe(en.admin.actor.not_found);
    expect(screen.queryByRole("button", { name: en.manage.people.add_role })).toBeNull();
  });
});
