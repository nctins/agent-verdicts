import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { describe, expect, it, vi } from "vitest";
import { keys } from "@/lib/api/keys";
import en from "@/messages/en.json";
import Home from "./page";

const replace = vi.fn();
let search = "";
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace }),
  usePathname: () => "/",
  useSearchParams: () => new URLSearchParams(search),
}));
vi.mock("@/components/AppShell", () => ({ AppShell: ({ children }: { children: React.ReactNode }) => children }));

function renderHome(all: string[], items: unknown[] = []) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData(keys.session(), {
    authenticated: true, actor: { id: "me", name: "Me", type: "Human" }, access: { all, projects: {} },
    csrf_token: "t", must_change_password: false, setup_required: false,
  });
  qc.setQueryData(keys.projects(), { items });
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}><Home /></NextIntlClientProvider>
    </QueryClientProvider>,
  );
}

describe("home with no visible project", () => {
  it("tells someone without a role to ask an admin for one", () => {
    renderHome([]);
    expect(screen.getByText(en.empty.no_access_hint)).toBeTruthy();
  });
  it("offers to create the first project to whoever may", () => {
    renderHome(["*"]);
    expect(screen.getByRole("button", { name: en.empty.create_first_project })).toBeTruthy();
  });

  it("leaves an archived project off the home page; all archived is the empty state", () => {
    const states = { Favorable: 0, Unfavorable: 0, Conflict: 0, InsufficientEvidence: 0 };
    renderHome([], [{ id: "old", name: "Old docs", description: "", contribution_count: 0, open_question_count: 0, created_at: "2026-11-14T00:00:00Z", states, archived: true }]);
    expect(screen.queryByRole("link", { name: "Old docs" })).toBeNull();
    expect(screen.getByText(en.empty.no_access_hint)).toBeTruthy();
  });

  it("lists a project by name with its id muted, and only the id when they match", () => {
    const states = { Favorable: 0, Unfavorable: 0, Conflict: 0, InsufficientEvidence: 0 };
    const p = (id: string, name: string) => ({ id, name, description: "", contribution_count: 0, open_question_count: 0, created_at: "2026-11-14T00:00:00Z", states });
    renderHome(["*"], [p("team-a", "Team A"), p("plain", "plain")]);
    expect(screen.getByRole("link", { name: "Team A" })).toBeTruthy();
    expect(screen.getByText("team-a")).toBeTruthy();
    expect(screen.getByRole("link", { name: "plain" })).toBeTruthy();
  });
});

describe("home with projects", () => {
  const states = { Favorable: 0, Unfavorable: 0, Conflict: 0, InsufficientEvidence: 0 };
  const p = (id: string, at: string | null) => ({ id, name: id, description: "", contribution_count: 0, open_question_count: 0, created_at: "2026-11-14T00:00:00Z", last_activity_at: at, states, archived: false });
  const all = [p("quiet", null), p("busy", "2026-11-20T00:00:00Z"), p("older", "2026-10-01T00:00:00Z")];
  const names = () => screen.getAllByRole("link").map((a) => a.textContent);
  it("lists the most recently active first, never-active last", () => {
    search = "";
    renderHome([], all);
    expect(names()).toEqual(["busy", "older", "quiet"]);
  });
  it("narrows to ?q= and says when nothing matches", () => {
    search = "q=zzz";
    renderHome([], all);
    expect(screen.getByText(en.home.no_match)).toBeTruthy();
    search = "";
  });
  it("writes the search and the sort to the URL, and remembers the layout", async () => {
    search = "sort=name";
    renderHome([], all);
    expect(names()).toEqual(["busy", "older", "quiet"].sort());
    await userEvent.type(screen.getByRole("searchbox", { name: en.home.search }), "b");
    expect(replace).toHaveBeenLastCalledWith("/?sort=name&q=b", { scroll: false });
    const group = screen.getByRole("group", { name: en.home.view });
    await userEvent.click(within(group).getByRole("button", { name: en.home.view_table }));
    expect(localStorage.getItem("av.projects-view")).toBe("table");
    search = "";
  });
});
