import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { beforeEach, expect, it, vi } from "vitest";
import { keys } from "@/lib/api/keys";
import en from "@/messages/en.json";
import RolesPage from "./page";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@/components/AppShell", () => ({ AppShell: ({ children }: { children: React.ReactNode }) => children }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: vi.fn() }), usePathname: () => "/manage/roles/", useSearchParams: () => new URLSearchParams() }));

const session = { authenticated: true, actor: { id: "me", name: "Me", type: "Human" }, access: { all: ["*"], projects: {} }, csrf_token: "t", must_change_password: false, setup_required: false };
const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json" } });
const role = (id: string, actions: string[], policy_ids: string[], managed = true) => ({ id, name: id, managed, policy_ids, actions, created_at: "" });
let posts: unknown[];
beforeEach(() => {
  posts = [];
  vi.stubGlobal("fetch", async (r: Request) => {
    const u = new URL(r.url).pathname;
    if (u.endsWith("/session")) return json(session);
    if (r.method === "POST") { posts.push(await r.json()); return json(role("x", [], []), 201); }
    if (u.endsWith("/admin/roles")) return json({ items: [role("admin", ["*"], ["pa"]), role("viewer", ["contribution:Read"], ["pr"])] });
    if (u.endsWith("/admin/policies"))
      return json({ items: [
        { id: "pa", name: "AdministratorAccess", managed: true, actions: ["*"], created_at: "" },
        { id: "pr", name: "ReadOnlyAccess", managed: true, actions: ["contribution:Read"], created_at: "" },
      ] });
    if (u.endsWith("/admin/assignments"))
      return json({ items: [{ id: "1", actor_id: "a", actor_name: "A", role_id: "viewer", role_name: "viewer", project_id: "p", created_at: "" },
        { id: "2", actor_id: "a", actor_name: "A", role_id: "viewer", role_name: "viewer", project_id: "q", created_at: "" }] });
    return json({ items: [] });
  });
});

function setup() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(keys.session(), session);
  render(<QueryClientProvider client={qc}><NextIntlClientProvider locale="en" messages={en}><RolesPage /></NextIntlClientProvider></QueryClientProvider>);
}

it("shows roles × actions with checks, counts distinct holders, and creates a role from policies in a sheet", async () => {
  setup();
  const read = await screen.findByRole("rowheader", { name: new RegExp(en.manage.access.action["contribution:Read"]) });
  const row = read.closest("tr")!;
  expect(within(row).getByLabelText(`admin: ${en.admin_ui.yes}`)).toBeTruthy();
  expect(within(row).getByLabelText(`viewer: ${en.admin_ui.yes}`)).toBeTruthy();
  const create = screen.getByRole("rowheader", { name: new RegExp(en.manage.access.action["contribution:Create"]) }).closest("tr")!;
  expect(within(create).getByLabelText(`viewer: ${en.admin_ui.no}`)).toBeTruthy();
  expect((await screen.findByRole("link", { name: en.admin_ui.matrix.used_by.replace("{count}", "1") })).getAttribute("href")).toMatch(/^\/manage\/actors\/?\?role=viewer$/);

  await userEvent.click(screen.getByRole("button", { name: en.manage.access.new_role }));
  const sheet = await screen.findByRole("dialog");
  expect(within(sheet).queryByLabelText(/AdministratorAccess/)).toBeNull(); // admin's policy stays with the managed role
  await userEvent.type(within(sheet).getByLabelText(en.manage.access.name), "reader");
  await userEvent.click(within(sheet).getByLabelText(/ReadOnlyAccess/));
  await userEvent.click(within(sheet).getByRole("button", { name: en.manage.access.save }));
  await waitFor(() => expect(posts).toEqual([{ name: "reader", policy_ids: ["pr"] }]));
});
