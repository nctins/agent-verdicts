import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { keys } from "@/lib/api/keys";
import en from "@/messages/en.json";
import ProjectsPage from "./page";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/manage/projects/",
  useSearchParams: () => new URLSearchParams(""),
}));
vi.mock("@/components/AppShell", () => ({ AppShell: ({ children }: { children: React.ReactNode }) => children }));
vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });

const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json" } });
const states = { Favorable: 0, Unfavorable: 0, Conflict: 0, InsufficientEvidence: 0 };
const row = { id: "team-a", name: "Team A", description: "old", min_favorable: 2, contribution_count: 0, open_question_count: 0, created_at: "2026-11-14T00:00:00Z", states, archived: false };
const old = { ...row, id: "legacy", name: "legacy", archived: true };

let calls: { method: string; path: string; body: unknown }[];
beforeEach(() => {
  calls = [];
  vi.stubGlobal("fetch", async (r: Request) => {
    const path = new URL(r.url).pathname;
    if (r.method === "GET") return json({ items: [row, old] });
    const body = await r.json();
    calls.push({ method: r.method, path, body });
    return json({ ...row, ...body }, r.method === "POST" ? 201 : 200);
  });
});

function setup() {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData(keys.session(), {
    authenticated: true, actor: { id: "me", name: "Me", type: "Human" }, access: { all: ["*"], projects: {} },
    csrf_token: "t", must_change_password: false, setup_required: false,
  });
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}><ProjectsPage /></NextIntlClientProvider>
    </QueryClientProvider>,
  );
}

const p = en.admin.project;
const openCreate = async () => {
  await userEvent.click(await screen.findByRole("button", { name: en.admin_ui.projects.new }));
  return screen.findByRole("dialog");
};
const openMenu = async (id: string) => userEvent.click(await screen.findByRole("button", { name: `${en.kit.table.actions}: ${id}` }));

/** Picks `option` of the filter `label` through "+ Filter" (spec 2026-11-23-ui-polish G4). */
async function pickFilter(label: string, option: string) {
  await userEvent.click(await screen.findByRole("button", { name: en.search.add_filter }));
  await userEvent.click(await screen.findByRole("menuitem", { name: label }));
  await userEvent.click(await screen.findByRole("menuitemradio", { name: option }));
}

describe("manage projects", () => {
  it("reports an invalid slug at the id field and sends nothing", async () => {
    setup();
    const dialog = await openCreate();
    const id = within(dialog).getByLabelText(p.id);
    await userEvent.type(id, "Bad.Id");
    expect((await within(dialog).findByRole("alert")).textContent).toBe(p.invalid); // while typing
    await userEvent.click(within(dialog).getByRole("button", { name: en.manage.project.button.create }));
    expect(id.getAttribute("aria-invalid")).toBe("true");
    expect(calls).toEqual([]);
  });

  it("creates with id, name and description", async () => {
    setup();
    const dialog = await openCreate();
    await userEvent.type(within(dialog).getByLabelText(p.id), "team-b");
    expect(within(dialog).getByText("/p/team-b/")).toBeTruthy();
    await userEvent.type(within(dialog).getByLabelText(p.name), " Team B ");
    await userEvent.type(within(dialog).getByLabelText(p.description), "About B");
    await userEvent.click(within(dialog).getByRole("button", { name: en.manage.project.button.create }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toMatchObject({ method: "POST", path: "/api/v1/admin/projects", body: { id: "team-b", name: "Team B", description: "About B" } });
  });

  it("edits the name and description of a row with PATCH", async () => {
    setup();
    await openMenu("team-a");
    await userEvent.click(await screen.findByRole("menuitem", { name: p.edit }));
    const dialog = await screen.findByRole("dialog");
    const name = within(dialog).getByLabelText(p.name);
    expect((name as HTMLInputElement).value).toBe("Team A");
    await userEvent.clear(name);
    await userEvent.type(name, "Renamed");
    await userEvent.clear(within(dialog).getByLabelText(p.description));
    const min = within(dialog).getByLabelText(p.min_favorable) as HTMLInputElement;
    expect(min.value).toBe("2");
    await userEvent.clear(min);
    await userEvent.type(min, "3");
    await userEvent.click(within(dialog).getByRole("button", { name: p.save }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toMatchObject({ method: "PATCH", path: "/api/v1/admin/projects/team-a", body: { name: "Renamed", description: "", min_favorable: 3 } });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("refuses a threshold outside 1-10 without calling the API", async () => {
    setup();
    await openMenu("team-a");
    await userEvent.click(await screen.findByRole("menuitem", { name: p.edit }));
    const dialog = await screen.findByRole("dialog");
    const min = within(dialog).getByLabelText(p.min_favorable);
    for (const bad of ["0", "11", ""]) {
      await userEvent.clear(min);
      if (bad) await userEvent.type(min, bad);
      await userEvent.click(within(dialog).getByRole("button", { name: p.save }));
      expect((await within(dialog).findByRole("alert")).textContent).toBe(p.min_favorable_invalid);
    }
    expect(calls).toEqual([]);
  });

  it("filters archived projects and unarchives with PATCH", async () => {
    setup();
    await pickFilter(en.admin_ui.projects.status, en.admin.project.archived_heading);
    expect(screen.getByRole("link", { name: "legacy" })).toBeTruthy();
    expect(screen.queryByRole("link", { name: "team-a" })).toBeNull();
    await openMenu("legacy");
    await userEvent.click(await screen.findByRole("menuitem", { name: p.unarchive }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toMatchObject({ method: "PATCH", path: "/api/v1/admin/projects/legacy", body: { archived: false } });
  });

  it("archives an active project with PATCH once its id is typed", async () => {
    setup();
    await openMenu("team-a");
    await userEvent.click(await screen.findByRole("menuitem", { name: p.archive }));
    const confirm = await screen.findByRole("dialog");
    await userEvent.type(within(confirm).getByRole("textbox"), "team-a");
    await userEvent.click(within(confirm).getByRole("button", { name: p.archive }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toMatchObject({ method: "PATCH", path: "/api/v1/admin/projects/team-a", body: { archived: true } });
  });
});
