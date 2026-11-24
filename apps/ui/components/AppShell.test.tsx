import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { describe, expect, it, vi } from "vitest";
import { keys } from "@/lib/api/keys";
import en from "@/messages/en.json";
import { AppShell } from "./AppShell";

let path = "/";
vi.mock("next/navigation", () => ({ usePathname: () => path, useRouter: () => ({ replace: vi.fn(), push: vi.fn() }) }));
vi.mock("@/components/LocaleThemeSwitch", () => ({ LocaleThemeSwitch: () => null }));

function renderAt(p: string, access: { all: string[]; projects: Record<string, string[]> } = { all: ["*"], projects: {} }) {
  path = p;
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData(keys.session(), {
    authenticated: true, actor: { id: "a", type: "human", name: "Admin" }, csrf_token: "t", must_change_password: false, setup_required: false, access,
  });
  qc.setQueryData(keys.projects(), { items: [] });
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}><AppShell>x</AppShell></NextIntlClientProvider>
    </QueryClientProvider>,
  );
  return within(screen.getAllByRole("navigation", { name: en.nav.menu })[0]);
}

describe("AppShell sidebar submenus", () => {
  it("lists only the admin pages the session may open", () => {
    const nav = renderAt("/manage/", { all: ["settings:Manage"], projects: {} });
    expect(nav.getByRole("link", { name: en.nav.search_settings })).toBeTruthy();
    expect(nav.queryByRole("link", { name: en.nav.actors })).toBeNull();
    cleanup();
    const member = renderAt("/", { all: [], projects: { p1: ["contribution:Read"] } });
    expect(member.queryByRole("link", { name: en.nav.admin })).toBeNull();
  });
  it("opens the admin submenu inside /manage and marks the current page (an actor's page counts as Actors)", () => {
    const nav = renderAt("/manage/actor/");
    expect(nav.getByRole("link", { name: en.nav.actors }).getAttribute("aria-current")).toBe("page");
    expect(nav.getByRole("link", { name: en.nav.search_settings })).toBeTruthy();
    expect(nav.queryByRole("link", { name: en.help.pages.concepts.menu })).toBeNull();
  });
  it("opens the help submenu on /help: one page per section, the current one marked", () => {
    const nav = renderAt("/help/mcp/");
    expect(nav.getByRole("link", { name: en.help.pages.mcp.menu }).getAttribute("href")).toMatch(/^\/help\/mcp\/?$/);
    expect(nav.getByRole("link", { name: en.help.pages.mcp.menu }).getAttribute("aria-current")).toBe("page");
    expect(nav.getByRole("link", { name: en.help.pages.ai.menu }).getAttribute("aria-current")).toBeNull();
    expect(nav.queryByRole("link", { name: en.nav.actors })).toBeNull();
  });
  it("keeps both closed elsewhere", () => {
    const nav = renderAt("/");
    expect(nav.queryByRole("link", { name: en.nav.actors })).toBeNull();
    expect(nav.queryByRole("link", { name: en.help.pages.mcp.menu })).toBeNull();
  });

  it("Admin and Guide are single links; only the project list folds", async () => {
    const nav = renderAt("/");
    // Both have their own menu once inside, so no sub-items or arrow here.
    for (const [name, href] of [[en.nav.admin, /^\/manage\/?$/], [en.nav.help, /^\/help\/?$/]] as const) {
      expect(nav.getByRole("link", { name }).getAttribute("href")).toMatch(href);
      expect(nav.queryByRole("button", { name: en.nav.toggle.replace("{label}", name) })).toBeNull();
    }
    expect(nav.queryByRole("link", { name: en.help.pages.mcp.menu })).toBeNull();

    const projects = nav.getByRole("button", { name: en.nav.toggle.replace("{label}", en.nav.section_projects) });
    expect(projects.getAttribute("aria-expanded")).toBe("true"); // folding it is the next test (folds are remembered per module)
  });

  it("inside /help the sidebar is the guide's menu: grouped sections and a way back", () => {
    const nav = renderAt("/help/mcp/");
    expect(nav.getByRole("link", { name: en.nav.back_to_app }).getAttribute("href")).toBe("/");
    expect(nav.getByText(en.help.group_agents)).toBeTruthy();
    expect(nav.getByRole("link", { name: en.help.pages.mcp.menu }).getAttribute("aria-current")).toBe("page");
    expect(nav.queryByRole("link", { name: en.nav.admin })).toBeNull();
  });

  it("inside /manage the sidebar is the admin console: grouped pages and a way back", () => {
    const nav = renderAt("/manage/roles/");
    expect(nav.getByRole("link", { name: en.nav.back_to_app }).getAttribute("href")).toBe("/");
    expect(nav.getByText(en.nav.group_access)).toBeTruthy();
    expect(nav.getByRole("link", { name: en.nav.roles }).getAttribute("aria-current")).toBe("page");
    expect(nav.queryByRole("link", { name: en.nav.help })).toBeNull();
  });

  it("a fold is kept when the next page mounts its own shell", async () => {
    const first = renderAt("/");
    await userEvent.click(first.getByRole("button", { name: en.nav.toggle.replace("{label}", en.nav.section_projects) }));
    cleanup();
    const next = renderAt("/account/");
    expect(next.getByRole("button", { name: en.nav.toggle.replace("{label}", en.nav.section_projects) }).getAttribute("aria-expanded")).toBe("false");
  });
});
