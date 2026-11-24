import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, describe, expect, it, vi } from "vitest";
import { keys } from "@/lib/api/keys";
import en from "@/messages/en.json";
import Actors from "./page";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/manage/actors/",
  useSearchParams: () => new URLSearchParams(""),
}));
vi.mock("@/components/AppShell", () => ({ AppShell: ({ children }: { children: React.ReactNode }) => children }));

vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
afterEach(() => vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} }));

const json = (b: unknown, status = 200) =>
  new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json" } });

/** Picks `option` of the filter `label` through "+ Filter" (spec 2026-11-23-ui-polish G4). */
async function pickFilter(label: string, option: string) {
  await userEvent.click(await screen.findByRole("button", { name: en.search.add_filter }));
  await userEvent.click(await screen.findByRole("menuitem", { name: label }));
  await userEvent.click(await screen.findByRole("menuitemradio", { name: option }));
}

describe("actors list", () => {
  it("shows each actor's access, and flags who has none", async () => {
    vi.stubGlobal("fetch", async (r: Request) => {
      const u = new URL(r.url).pathname;
      if (u.endsWith("/admin/actors"))
        return json({ items: [
          { id: "a1", type: "Human", name: "Alice", disabled: false, created_at: "2026-01-01T00:00:00Z", username: "alice", has_login: true },
          { id: "b1", type: "Agent", name: "Bot", disabled: false, created_at: "2026-01-01T00:00:00Z", username: null, has_login: false },
        ] });
      if (u.endsWith("/admin/assignments"))
        return json({ items: [
          { id: "x", actor_id: "a1", actor_name: "Alice", role_id: "contributor", role_name: "contributor", project_id: "p1", created_at: "" },
          { id: "y", actor_id: "a1", actor_name: "Alice", role_id: "contributor", role_name: "contributor", project_id: "p2", created_at: "" },
        ] });
      return json({ items: [] });
    });
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    qc.setQueryData(keys.session(), {
      authenticated: true, actor: { id: "me", name: "Me", type: "Human" }, access: { all: ["*"], projects: {} },
      csrf_token: "t", must_change_password: false, setup_required: false,
    });
    render(
      <QueryClientProvider client={qc}>
        <NextIntlClientProvider locale="en" messages={en}><Actors /></NextIntlClientProvider>
      </QueryClientProvider>,
    );
    expect(await screen.findByText("contributor · p1, p2")).toBeTruthy();
    expect(screen.getByText(en.manage.people.no_access)).toBeTruthy();
    await pickFilter(en.manage.access.scope, en.manage.people.no_access);
    expect(screen.queryByRole("link", { name: "Alice" })).toBeNull();
    expect(screen.getByRole("link", { name: "Bot" })).toBeTruthy();
  });
});

describe("create actor", () => {
  it("keeps the one-time password out of the query and mutation caches", async () => {
    const SECRET = "generated-pw-123456";
    vi.stubGlobal("fetch", async (r: Request) =>
      r.method === "POST"
        ? json({ id: "x", type: "Human", name: "Ann", disabled: false, created_at: "2026-01-01T00:00:00Z", username: "ann", has_login: true, password: SECRET }, 201)
        : json({ items: [] }),
    );
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    qc.setQueryData(keys.session(), {
      authenticated: true, actor: { id: "me", name: "Me", type: "Human" }, access: { all: ["*"], projects: {} },
      csrf_token: "t", must_change_password: false, setup_required: false,
    });
    render(
      <QueryClientProvider client={qc}>
        <NextIntlClientProvider locale="en" messages={en}>
          <Actors />
        </NextIntlClientProvider>
      </QueryClientProvider>,
    );
    await userEvent.click(await screen.findByRole("button", { name: en.manage.people.add }));
    await userEvent.type(await screen.findByLabelText(en.manage.actor.label.name), "Ann");
    await userEvent.type(screen.getByLabelText(en.admin.field.username), "ann");
    await userEvent.click(screen.getByLabelText(en.manage.people.later));
    await userEvent.click(screen.getByRole("button", { name: en.manage.people.create }));
    expect((await screen.findByLabelText(en.admin.actor.created_password) as HTMLInputElement).value).toBe(SECRET);
    await userEvent.click(screen.getByRole("button", { name: en.admin.once.done }));
    expect(screen.queryByLabelText(en.admin.actor.created_password)).toBeNull();
    const dump = JSON.stringify([
      qc.getQueryCache().getAll().map((q) => q.state),
      qc.getMutationCache().getAll().map((m) => m.state),
    ]);
    expect(dump).not.toContain(SECRET);
  });
});
