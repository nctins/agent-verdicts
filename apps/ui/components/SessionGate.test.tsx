import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { keys } from "@/lib/api/keys";
import en from "@/messages/en.json";
import { SessionGate, type Require } from "./SessionGate";

const replace = vi.fn();
let path = "/";
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace }),
  usePathname: () => path,
  useSearchParams: () => new URLSearchParams(""),
}));

const session = (o: object) => ({
  authenticated: false,
  actor: null,
  csrf_token: null,
  must_change_password: false,
  setup_required: false,
  ...o,
});
const actor = { id: "a", name: "A", type: "Human" };
const access = (all: string[], projects: Record<string, string[]> = {}) => ({ all, projects });
const admin = { authenticated: true, actor, access: access(["*"]) };

function renderGate(s: object, at: string, require: Require) {
  path = at;
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  qc.setQueryData(keys.session(), session(s));
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}>
        <SessionGate require={require}>
          <p>child</p>
        </SessionGate>
      </NextIntlClientProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => replace.mockClear());
afterEach(() => vi.unstubAllGlobals());

function renderFailing(require: "none" | "login") {
  vi.stubGlobal("fetch", async () => new Response("<html>", { status: 502 }));
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}>
        <SessionGate require={require}>
          <p>child</p>
        </SessionGate>
      </NextIntlClientProvider>
    </QueryClientProvider>,
  );
}

describe("SessionGate", () => {
  it("sends to /setup/ when setup is required", () => {
    renderGate({ setup_required: true }, "/project/", "none");
    expect(replace).toHaveBeenCalledWith("/setup/");
  });
  it("leaves /setup/ once setup is done", () => {
    renderGate({}, "/setup/", "none");
    expect(replace).toHaveBeenCalledWith("/");
  });
  it("takes the admin just created by setup to the overview", () => {
    // The setup form cannot navigate itself: this redirect unmounts it as soon as the session refreshes.
    renderGate(admin, "/setup/", "none");
    expect(replace).toHaveBeenCalledWith("/manage/");
  });
  it("sends anonymous users to login with next", () => {
    renderGate({}, "/account/", "login");
    expect(replace).toHaveBeenCalledWith("/login/?next=%2Faccount%2F");
  });
  it("forces a password change", () => {
    renderGate({ ...admin, must_change_password: true }, "/manage/", "manage");
    expect(replace).toHaveBeenCalledWith("/account/");
  });
  it("shows forbidden without redirecting when the page's action is missing", () => {
    renderGate({ authenticated: true, actor, access: access(["contribution:Read"]) }, "/manage/settings/search/", "settings:Manage");
    expect(screen.getByText(en.errors.forbidden)).toBeTruthy();
    expect(replace).not.toHaveBeenCalled();
  });
  it("opens a page to the action it needs", () => {
    renderGate({ authenticated: true, actor, access: access(["settings:Manage"]) }, "/manage/settings/search/", "settings:Manage");
    expect(screen.getByText("child")).toBeTruthy();
  });
  it("opens the admin area to a project manager, not to a plain member", () => {
    renderGate({ authenticated: true, actor, access: access([], { p1: ["member:Manage"] }) }, "/manage/", "manage");
    expect(screen.getByText("child")).toBeTruthy();
  });
  it("keeps a plain member out of the admin area", () => {
    renderGate({ authenticated: true, actor, access: access([], { p1: ["contribution:Read"] }) }, "/manage/", "manage");
    expect(screen.getByText(en.errors.forbidden)).toBeTruthy();
  });
  it("sends a setup user without admin rights home", () => {
    renderGate({ authenticated: true, actor, access: access([]) }, "/setup/", "none");
    expect(replace).toHaveBeenCalledWith("/");
  });
  it("renders children for anonymous when nothing is required", () => {
    renderGate({}, "/", "none");
    expect(screen.getByText("child")).toBeTruthy();
    expect(replace).not.toHaveBeenCalled();
  });
  // Public pages (e.g. /help/) need no session; the network banner reports the outage.
  it("renders public children when the session request fails", async () => {
    renderFailing("none");
    await waitFor(() => expect(screen.getByText("child")).toBeTruthy());
  });
  it("shows the error instead of guarded children when the session request fails", async () => {
    renderFailing("login");
    await waitFor(() => expect(screen.getByText(en.errors.network)).toBeTruthy());
    expect(screen.queryByText("child")).toBeNull();
  });
});
