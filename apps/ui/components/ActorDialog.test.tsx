import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { keys } from "@/lib/api/keys";
import en from "@/messages/en.json";
import { ActorDialog } from "./ActorDialog";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });

const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json" } });
const session = {
  authenticated: true, actor: { id: "me", name: "Me", type: "Human" }, access: { all: ["*"], projects: {} },
  csrf_token: "t", must_change_password: false, setup_required: false,
};
const roles = [
  { id: "viewer", name: "viewer", managed: true, policy_ids: [], actions: ["contribution:Read"], created_at: "" },
  { id: "contributor", name: "contributor", managed: true, policy_ids: [], actions: ["contribution:Read"], created_at: "" },
];
let assignments: unknown[];
let failAssign: boolean;
let createConflict: string | null;
beforeEach(() => {
  assignments = [];
  failAssign = false;
  createConflict = null;
  vi.stubGlobal("fetch", async (r: Request) => {
    const u = new URL(r.url).pathname;
    if (u.endsWith("/session")) return json(session);
    if (r.method === "POST" && u.endsWith("/admin/actors") && createConflict) return json({ code: "conflict", message: createConflict }, 409);
    if (r.method === "POST" && u.endsWith("/admin/actors"))
      return json({ id: "new1", type: "Human", name: "Ann", disabled: false, created_at: "", username: "ann", has_login: true, password: "pw-once-123456" }, 201);
    if (r.method === "POST" && u.endsWith("/admin/assignments")) {
      if (failAssign) return json({ code: "forbidden", message: "no" }, 403);
      const b = await r.json();
      assignments.push(b);
      return json({ id: "x", actor_name: "Ann", role_name: b.role_id, created_at: "", ...b }, 201);
    }
    if (u.endsWith("/admin/roles")) return json({ items: roles });
    if (u.endsWith("/projects")) return json({ items: [] });
    return json({ items: [] });
  });
});

function open() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(keys.session(), session);
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}><ActorDialog open onOpenChange={() => {}} /></NextIntlClientProvider>
    </QueryClientProvider>,
  );
}
const d = en.manage.people;

async function fillHuman({ everyProject = true } = {}) {
  await userEvent.type(screen.getByLabelText(en.manage.actor.label.name), "Ann");
  await userEvent.type(screen.getByLabelText(en.admin.field.username), "ann");
  await screen.findByRole("option", { name: "contributor" });
  if (everyProject) await userEvent.click(screen.getByLabelText(en.manage.access.scope_all));
}

describe("ActorDialog", () => {
  it("creates the actor, then gives the initial role, then shows the password once", async () => {
    open();
    await fillHuman();
    await userEvent.click(screen.getByRole("button", { name: d.create }));
    expect(await screen.findByDisplayValue("pw-once-123456")).toBeTruthy();
    expect(assignments).toEqual([{ actor_id: "new1", role_id: "contributor", project_id: null }]);
  });
  it("keeps the actor when the role cannot be given, and lets you retry", async () => {
    failAssign = true;
    open();
    await fillHuman();
    await userEvent.click(screen.getByRole("button", { name: d.create }));
    expect(await screen.findByText(d.assign_failed_title)).toBeTruthy();
    expect(screen.getByDisplayValue("pw-once-123456")).toBeTruthy();
    failAssign = false;
    await userEvent.click(screen.getByRole("button", { name: d.retry }));
    await waitFor(() => expect(assignments).toHaveLength(1));
    expect(screen.queryByText(d.assign_failed_title)).toBeNull();
  });
  it("starts on chosen projects, and creates nothing until a project is picked", async () => {
    let created = 0;
    const base = globalThis.fetch;
    vi.stubGlobal("fetch", async (r: Request) => {
      if (r.method === "POST" && new URL(r.url).pathname.endsWith("/admin/actors")) created++;
      return base(r);
    });
    open();
    await fillHuman({ everyProject: false });
    expect((screen.getByLabelText(en.manage.access.scope_some) as HTMLInputElement).checked).toBe(true);
    await userEvent.click(screen.getByRole("button", { name: d.create }));
    expect(await screen.findByText(en.manage.access.pick_one)).toBeTruthy();
    expect(created).toBe(0);
    expect(assignments).toEqual([]);
  });
  it("gives no role when told to decide later", async () => {
    open();
    await fillHuman();
    await userEvent.click(screen.getByLabelText(d.later));
    await userEvent.click(screen.getByRole("button", { name: d.create }));
    expect(await screen.findByDisplayValue("pw-once-123456")).toBeTruthy();
    expect(assignments).toEqual([]);
  });
  it("shows a taken name next to the name field, and a taken username as before", async () => {
    createConflict = "actor: name already in use";
    open();
    await fillHuman();
    await userEvent.click(screen.getByRole("button", { name: d.create }));
    const msg = await screen.findByText(en.manage.actor.error.name_taken);
    expect(screen.getByLabelText(en.manage.actor.label.name).getAttribute("aria-describedby")).toBe(msg.id);
    expect(screen.queryByText(en.manage.actor.error.username_taken)).toBeNull();
    createConflict = "authentication: username already in use";
    await userEvent.click(screen.getByRole("button", { name: d.create }));
    expect(await screen.findByText(en.manage.actor.error.username_taken)).toBeTruthy();
    expect(screen.queryByText(en.manage.actor.error.name_taken)).toBeNull();
  });
});
