import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { keys } from "@/lib/api/keys";
import type { components } from "@/lib/api/schema";
import type { ContributionDetail } from "@/lib/stance";
import en from "@/messages/en.json";
import { ContributionView } from "./ContributionView";

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

type Signal = components["schemas"]["Signal"];
const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const sig = (o: Partial<Signal>): Signal => ({ id: "s", kind: "challenge", stance: "unfavorable", actor_id: "x", actor_name: "X", body: "b", sources: [], created_at: "2026-11-14T00:00:00Z", ...o });
const claim = (o: Partial<ContributionDetail> = {}): ContributionDetail => ({
  id: "c1", project_id: "demo", kind: "claim", title: "Old claim", body: "", sources: [], actor_id: "author", actor_name: "Author",
  created_at: "2026-11-14T00:00:00Z", state: "Conflict", counts: { favorable: 0, unfavorable: 1, neutral: 0, favorable_actors: 0 },
  signals: {
    favorable: [], neutral: [],
    unfavorable: [sig({ id: "mine", actor_id: "me", actor_name: "Me", body: "my challenge" }), sig({ id: "theirs", body: "their challenge" })],
    history: [],
  },
  ...o,
});
const actor = { id: "me", name: "Me", type: "Human" };
const READ = ["contribution:Read", "contribution:Create", "signal:Create"];

let calls: { method: string; path: string; body: unknown }[];
let detail: ContributionDetail;
beforeEach(() => {
  calls = [];
  vi.stubGlobal("fetch", async (r: Request) => {
    const path = new URL(r.url).pathname;
    if (path.endsWith("/search")) return json(200, { results: [], degraded: false });
    if (r.method === "GET") return path.endsWith("/projects") ? json(200, { items: [{ id: "demo", archived: false }] }) : json(200, detail);
    calls.push({ method: r.method, path, body: r.method === "PUT" || r.method === "POST" ? await r.json() : null });
    if (r.method === "POST") return json(201, { id: "c2" });
    return new Response(null, { status: 204 });
  });
});
afterEach(() => vi.unstubAllGlobals());

function setup(d: ContributionDetail, { as = actor as object | null, actions = READ, archived = false } = {}) {
  detail = d;
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData(keys.session(), {
    authenticated: !!as, actor: as, csrf_token: "t", must_change_password: false, setup_required: false,
    access: { all: [], projects: { demo: actions } },
  });
  qc.setQueryData(keys.projects(), { items: [{ id: "demo", name: "Demo Lab", archived }] });
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en} timeZone="UTC" now={new Date()}><ContributionView project="demo" id="c1" /></NextIntlClientProvider>
    </QueryClientProvider>,
  );
}
const c = en.contribution;
/** Opens the contribution's "⋯" menu once the page is there. */
/** Opens the collapsed challenge box. */
const compose = async () => userEvent.click(await screen.findByRole("button", { name: c.challenge.compose }));
const openMenu = async () => userEvent.click(await screen.findByRole("button", { name: c.actions }));
const item = (name: string) => screen.queryByRole("menuitem", { name });
const reply = (text: string) => screen.getByText(text).closest("article")!;

describe("breadcrumb", () => {
  it("links to the project by its name", async () => {
    setup(claim());
    expect((await screen.findByRole("link", { name: `${en.project.prefix}: Demo Lab` })).getAttribute("href")).toContain("demo");
  });
});

describe("supporter progress", () => {
  const some = (favorable_actors: number, min_favorable: number, state: ContributionDetail["state"] = "InsufficientEvidence") =>
    claim({ state, min_favorable, counts: { favorable: favorable_actors, unfavorable: 0, neutral: 0, favorable_actors } });
  const progress = (done: number, total: number) => c.supporters.replace("{done}", String(done)).replace("{total}", String(total));
  it("shows n/N next to Insufficient evidence when the threshold is above 1 and someone supports", async () => {
    setup(some(1, 2));
    expect(await screen.findByText(progress(1, 2))).toBeTruthy();
  });
  it("is a bar: 0/N before anyone supports, full once Favorable, absent at threshold 1 or in conflict", async () => {
    setup(some(0, 2));
    expect(await screen.findByText(progress(0, 2))).toBeTruthy();
    expect(screen.getByRole("progressbar", { name: c.supporters_label })).toBeTruthy();
    cleanup();
    setup(some(2, 2, "Favorable"));
    expect(await screen.findByText(progress(2, 2))).toBeTruthy();
    cleanup();
    setup(some(1, 1));
    await screen.findByText("my challenge");
    expect(screen.queryByRole("progressbar")).toBeNull();
    cleanup();
    setup(some(1, 2, "Conflict"));
    await screen.findByText("my challenge");
    expect(screen.queryByRole("progressbar")).toBeNull();
  });
});

describe("sources", () => {
  it("shows the claim's and each signal's sources under its text", async () => {
    const d = claim({ sources: ["https://example.test/a", "commit 1a2b3c"] });
    d.signals.unfavorable[1] = sig({ id: "theirs", body: "their challenge", sources: ["src/x.go"] });
    setup(d);
    const post = (await screen.findByText("their challenge")).closest("article")!;
    expect(within(post).getByText("src/x.go").tagName).toBe("CODE");
    const article = document.querySelector("article")!;
    expect(within(article).getByRole("link", { name: "https://example.test/a" }).getAttribute("href")).toBe("https://example.test/a");
    expect(within(article).getByText("commit 1a2b3c").tagName).toBe("CODE");
  });
  it("a challenge is sent with the sources entered, one per line", async () => {
    setup(claim());
    await compose();
    await userEvent.type(await screen.findByLabelText(c.challenge.content), "it is wrong");
    await userEvent.click(screen.getByRole("button", { name: en.sources.add }));
    await userEvent.type(screen.getByLabelText(en.sources.label), "https://example.test/why{enter}notes.md");
    await userEvent.click(screen.getByRole("button", { name: c.challenge.submit }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toMatchObject({ method: "POST", body: { kind: "challenge", body: "it is wrong", sources: ["https://example.test/why", "notes.md"] } });
  });
});

describe("replies", () => {
  const later = (n: number) => `2026-11-14T00:0${n}:00Z`;
  const threaded = (...replies: Signal[]) => {
    const d = claim();
    d.signals = { ...d.signals, neutral: replies };
    return d;
  };

  it("the challenge form offers oppose and neutral, not support", async () => {
    setup(claim());
    await compose();
    await screen.findByLabelText(c.challenge.content);
    const values = [...document.querySelectorAll("select option")].map((o) => (o as HTMLOptionElement).value).filter(Boolean);
    expect(values).toEqual(["unfavorable", "neutral"]);
  });

  it("a top-level counting signal has Reply; the form posts a neutral challenge with reply_to", async () => {
    setup(claim());
    await screen.findByText("their challenge");
    await userEvent.click(within(reply("their challenge")).getByRole("button", { name: c.reply.open }));
    await userEvent.click(screen.getByRole("button", { name: c.reply.submit }));
    expect(await screen.findByText(c.reply.error_required)).toBeTruthy(); // a body is required
    expect(calls).toEqual([]);
    await userEvent.type(screen.getByLabelText(c.reply.label), "good point");
    await userEvent.click(screen.getByRole("button", { name: c.reply.submit }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toMatchObject({ method: "POST", path: "/api/v1/projects/demo/contributions/c1/signals", body: { kind: "challenge", stance: "neutral", body: "good point", reply_to: "theirs" } });
  });

  it("replies sit under their root in time order, even when the root is in another stance group", async () => {
    setup(threaded(
      sig({ id: "r2", stance: "neutral", reply_to: "theirs", body: "second reply", created_at: later(2) }),
      sig({ id: "r1", stance: "neutral", reply_to: "theirs", body: "first reply", created_at: later(1) }),
    ));
    await screen.findByText("first reply");
    const root = reply("their challenge");
    expect(root.contains(reply("first reply"))).toBe(true);
    expect(root.contains(reply("second reply"))).toBe(true);
    expect(root.contains(reply("my challenge"))).toBe(false);
    expect(reply("first reply").compareDocumentPosition(reply("second reply")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    // Only top-level signals can be replied to.
    expect(within(reply("first reply")).queryByRole("button", { name: c.reply.open })).toBeNull();
  });

  it("a reply carries one Reply label instead of its stance and kind", async () => {
    setup(threaded(sig({ id: "r1", stance: "neutral", reply_to: "theirs", body: "first reply", created_at: later(1) })));
    const header = (await screen.findByText("first reply")).closest("article")!.querySelector("header")!;
    expect(within(header).getByText(c.reply.open)).toBeTruthy();
    expect(within(header).queryByText(en.stance.neutral)).toBeNull();
    expect(within(header).queryByText(c.signal.challenge)).toBeNull();
    const root = reply("their challenge").querySelector("header")!;
    expect(within(root).getByText(c.signal.challenge)).toBeTruthy();
    expect(within(root).queryByText(c.reply.open)).toBeNull();
  });

  it("marks the author's own support as not counted, and no one else's", async () => {
    setup(claim({
      signals: {
        favorable: [
          sig({ id: "self", kind: "assessment", stance: "favorable", actor_id: "author", actor_name: "Author", body: "mine is right" }),
          sig({ id: "fan", kind: "assessment", stance: "favorable", actor_id: "x", body: "checked it" }),
        ],
        unfavorable: [], neutral: [], history: [],
      },
    }));
    const self = (await screen.findByText("mine is right")).closest("article")!.querySelector("header")!;
    expect(within(self).getByText(c.self_support)).toBeTruthy();
    expect(within(reply("checked it").querySelector("header")!).queryByText(c.self_support)).toBeNull();
  });

  it("a reply whose root is nowhere in the lists is shown at top level, not dropped", async () => {
    setup(threaded(sig({ id: "r1", stance: "neutral", reply_to: "gone", body: "orphan reply", created_at: later(1) })));
    await screen.findByText("orphan reply");
    expect(reply("their challenge").contains(reply("orphan reply"))).toBe(false);
    expect(reply("my challenge").contains(reply("orphan reply"))).toBe(false);
  });

  it("withdraw still works on my own reply", async () => {
    setup(threaded(sig({ id: "r1", stance: "neutral", actor_id: "me", reply_to: "theirs", body: "my reply", created_at: later(1) })));
    await screen.findByText("my reply");
    await userEvent.click(within(reply("my reply")).getByRole("button", { name: c.withdraw }));
    await userEvent.click(await screen.findByRole("button", { name: c.withdraw_confirm }));
    await waitFor(() => expect(calls).toEqual([{ method: "DELETE", path: "/api/v1/projects/demo/contributions/c1/signals/r1", body: null }]));
  });

  it("a viewer without signal:Create has no Reply button", async () => {
    setup(claim(), { actions: ["contribution:Read"] });
    await screen.findByText("their challenge");
    expect(screen.queryByRole("button", { name: c.reply.open })).toBeNull();
  });
});

describe("withdraw", () => {
  it("only my own counting signals have a Withdraw button", async () => {
    setup(claim());
    await screen.findByText("my challenge");
    expect(within(reply("my challenge")).getByRole("button", { name: c.withdraw })).toBeTruthy();
    expect(within(reply("their challenge")).queryByRole("button", { name: c.withdraw })).toBeNull();
  });

  it("asks first, and sends DELETE for that signal only after Confirm", async () => {
    setup(claim());
    await screen.findByText("my challenge");
    await userEvent.click(within(reply("my challenge")).getByRole("button", { name: c.withdraw }));
    const dialog = await screen.findByRole("dialog", { name: c.withdraw_title });
    expect(calls).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: en.common.cancel }));
    expect(calls).toEqual([]);
    await userEvent.click(within(reply("my challenge")).getByRole("button", { name: c.withdraw }));
    await userEvent.click(await screen.findByRole("button", { name: c.withdraw_confirm }));
    await waitFor(() => expect(calls).toEqual([{ method: "DELETE", path: "/api/v1/projects/demo/contributions/c1/signals/mine", body: null }]));
  });

  it("history entries say why they do not count; none of them can be withdrawn", async () => {
    const history = [
      sig({ id: "h1", actor_id: "me", body: "replaced one", superseded: true }),
      sig({ id: "h2", actor_id: "me", body: "withdrawn one", withdrawn: true }),
      sig({ id: "h3", actor_id: "me", body: "hidden one", hidden: true }),
      sig({ id: "h4", actor_id: "me", body: "disabled one", actor_disabled: true }),
    ];
    const d = claim();
    d.signals = { ...d.signals, history };
    setup(d);
    await userEvent.click(await screen.findByText(en.contribution.history.replace("{count}", "4")));
    for (const [text, why] of [["replaced one", c.why.superseded], ["withdrawn one", c.why.withdrawn], ["hidden one", c.why.hidden], ["disabled one", c.why.actor_disabled]]) {
      expect(within(reply(text)).getByText(why)).toBeTruthy();
      expect(within(reply(text)).queryByRole("button", { name: c.withdraw })).toBeNull();
    }
  });

  it("a viewer without signal:Create cannot withdraw", async () => {
    setup(claim(), { actions: ["contribution:Read"] });
    await screen.findByText("my challenge");
    expect(screen.queryByRole("button", { name: c.withdraw })).toBeNull();
  });
});

describe("supersede", () => {
  const sup = { name: c.supersede };
  it("the author and a moderator get the action; other people and anonymous visitors do not", async () => {
    setup(claim({ actor_id: "me" }));
    await openMenu();
    expect(item(sup.name)).toBeTruthy();
  });
  it("a moderator gets it on someone else's claim", async () => {
    setup(claim(), { actions: [...READ, "content:Moderate"] });
    await openMenu();
    expect(item(sup.name)).toBeTruthy();
  });
  it("another contributor does not", async () => {
    setup(claim());
    await openMenu();
    expect(item(sup.name)).toBeNull();
    expect(item(en.project.copy_link)).toBeTruthy(); // the menu is there, with the link
  });
  it("the dialog is pre-set to supersede and sends supersedes_id", async () => {
    setup(claim({ actor_id: "me" }));
    await openMenu();
    await userEvent.click(item(sup.name)!);
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(c.create.supersedes.replace("{label}", "Old claim"))).toBeTruthy();
    await userEvent.click(within(dialog).getByRole("button", { name: c.create.submit_claim }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toMatchObject({ method: "POST", body: { kind: "claim", title: "Old claim", supersedes_id: "c1" } });
  });
  it("a superseded claim has the badge, a link to the newer claim and no supersede action", async () => {
    setup(claim({ actor_id: "me", superseded_by: { id: "c2", title: "Newer claim" } }));
    const link = await screen.findByRole("link", { name: "Newer claim" });
    expect(link.getAttribute("href")).toMatch(/\/c\/c2\/?$/);
    expect(screen.getAllByText(c.superseded).length).toBeGreaterThan(0);
    await openMenu();
    expect(item(sup.name)).toBeNull();
  });
  it("its author can undo it; someone else cannot", async () => {
    setup(claim({ actor_id: "me", superseded_by: { id: "c2", title: "Newer claim" } }));
    await openMenu();
    await userEvent.click(item(c.supersede_undo)!);
    await waitFor(() => expect(calls).toEqual([{ method: "DELETE", path: "/api/v1/projects/demo/contributions/c1/supersession", body: null }]));
  });
  it("undo is absent for a non-author without content:Moderate", async () => {
    setup(claim({ superseded_by: { id: "c2", title: "Newer claim" } }));
    await screen.findByRole("link", { name: "Newer claim" });
    await openMenu();
    expect(item(c.supersede_undo)).toBeNull();
  });
});

describe("moderation", () => {
  it("without content:Moderate there are no hide controls", async () => {
    setup(claim({ actor_id: "me" }));
    await screen.findByText("my challenge");
    expect(screen.queryByRole("button", { name: c.hide })).toBeNull();
    await openMenu();
    expect(item(c.hide)).toBeNull();
    expect(item(c.unhide)).toBeNull();
  });
  it("a moderator can hide the contribution and each signal, and unhide a hidden one", async () => {
    const d = claim({ hidden: true });
    d.signals = { ...d.signals, history: [sig({ id: "h3", body: "hidden one", hidden: true })] };
    setup(d, { actions: [...READ, "content:Moderate"] });
    await screen.findByText("my challenge");
    expect(screen.getAllByText(c.hidden).length).toBeGreaterThan(0); // the claim's badge
    // Two counting signals can be hidden; the claim itself (hidden) is unhidden from its menu.
    expect(screen.getAllByRole("button", { name: c.hide })).toHaveLength(2);
    // Hide asks first; an empty reason hides without one.
    await userEvent.click(within(reply("their challenge")).getByRole("button", { name: c.hide }));
    expect(calls).toEqual([]);
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: c.hide }));
    await waitFor(() => expect(calls).toEqual([{ method: "PUT", path: "/api/v1/projects/demo/contributions/c1/signals/theirs/hidden", body: { hidden: true } }]));
    await userEvent.click(screen.getByText(en.contribution.history.replace("{count}", "1")));
    await userEvent.click(within(reply("hidden one")).getByRole("button", { name: c.unhide }));
    await waitFor(() => expect(calls[1]).toEqual({ method: "PUT", path: "/api/v1/projects/demo/contributions/c1/signals/h3/hidden", body: { hidden: false } }));
    await openMenu();
    await userEvent.click(item(c.unhide)!);
    await waitFor(() => expect(calls[2]).toEqual({ method: "PUT", path: "/api/v1/projects/demo/contributions/c1/hidden", body: { hidden: false } }));
  });
});

describe("hide reason", () => {
  const MOD = [...READ, "content:Moderate"];
  const hideWith = async (button: HTMLElement, reason: string) => {
    await userEvent.click(button);
    const dialog = await screen.findByRole("dialog");
    const field = within(dialog).getByLabelText(c.hide_reason_label);
    expect(field.getAttribute("maxlength")).toBe("500");
    await userEvent.type(field, reason);
    await userEvent.click(within(dialog).getByRole("button", { name: c.hide }));
  };
  it("hiding a signal sends the trimmed reason", async () => {
    setup(claim(), { actions: MOD });
    await screen.findByText("their challenge");
    await hideWith(within(reply("their challenge")).getByRole("button", { name: c.hide }), " spam ");
    await waitFor(() => expect(calls).toEqual([{ method: "PUT", path: "/api/v1/projects/demo/contributions/c1/signals/theirs/hidden", body: { hidden: true, reason: "spam" } }]));
  });
  it("hiding the contribution sends the reason; cancelling sends nothing", async () => {
    setup(claim(), { actions: MOD });
    await screen.findByText("their challenge");
    const own = async () => {
      await openMenu();
      return item(c.hide)!;
    };
    await userEvent.click(await own());
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: en.common.cancel }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(calls).toEqual([]);
    await hideWith(await own(), "off topic");
    await waitFor(() => expect(calls).toEqual([{ method: "PUT", path: "/api/v1/projects/demo/contributions/c1/hidden", body: { hidden: true, reason: "off topic" } }]));
  });
  it("a hidden signal shows its reason", async () => {
    const d = claim();
    d.signals = { ...d.signals, history: [sig({ id: "h3", body: "hidden one", hidden: true, hidden_reason: "rude" })] };
    setup(d, { actions: MOD });
    await screen.findByText("my challenge");
    expect(within(reply("hidden one")).getByText(c.hidden_reason.replace("{label}", "rude"))).toBeTruthy();
  });
});

describe("hidden contribution", () => {
  const banner = () => screen.getByText(c.hidden_notice).closest("[role=status]") as HTMLElement;
  it("the author sees the banner with the reason and none of the writes the server would refuse", async () => {
    setup(claim({ actor_id: "me", hidden: true, hidden_reason: "off topic" }));
    await screen.findByText("their challenge");
    expect(within(banner()).getByText(c.hidden_reason.replace("{label}", "off topic"))).toBeTruthy();
    expect(screen.queryByRole("group", { name: c.stance_group })).toBeNull();
    expect(screen.queryByRole("link", { name: c.challenge_open })).toBeNull();
    expect(screen.queryByLabelText(c.challenge.content)).toBeNull();
    expect(screen.queryByRole("button", { name: c.reply.open })).toBeNull();
    await openMenu();
    expect(item(c.supersede)).toBeNull();
  });
  it("a moderator sees the banner (no reason given) and keeps the controls", async () => {
    setup(claim({ hidden: true }), { actions: [...READ, "content:Moderate"] });
    await screen.findByText("their challenge");
    expect(banner().textContent).toBe(c.hidden_notice);
    expect(screen.getByRole("group", { name: c.stance_group })).toBeTruthy();
    await compose();
    expect(screen.getByLabelText(c.challenge.content)).toBeTruthy();
  });
  it("the author gets no withdraw and no undo-supersession on it; a moderator keeps both", async () => {
    const d = claim({ actor_id: "me", hidden: true, superseded_by: { id: "c2", title: "Newer claim" } });
    setup(d);
    await screen.findByText("my challenge");
    expect(screen.queryByRole("button", { name: c.withdraw })).toBeNull();
    await openMenu();
    expect(item(c.supersede_undo)).toBeNull();
    cleanup();
    setup(d, { actions: [...READ, "content:Moderate"] });
    await screen.findByText("my challenge");
    expect(within(reply("my challenge")).getByRole("button", { name: c.withdraw })).toBeTruthy();
    await openMenu();
    expect(item(c.supersede_undo)).toBeTruthy();
  });
  const question = (o: Partial<ContributionDetail> = {}) =>
    claim({ kind: "question", actor_id: "me", state: undefined, question_status: "open", ...o });
  it("a hidden answer on a question shows its reason to a moderator", async () => {
    setup(question({ answers: [{ id: "a1", title: "An answer", state: "Favorable", hidden: true, hidden_reason: "duplicate" }] }), { actions: [...READ, "content:Moderate"] });
    const card = (await screen.findByRole("link", { name: "An answer" })).closest("li")!;
    expect(within(card).getByText(c.hidden_reason.replace("{label}", "duplicate"))).toBeTruthy();
  });
  it("a hidden question offers its author no answer form; a visible one does", async () => {
    setup(question({ hidden: true }));
    await screen.findByText(c.hidden_notice);
    expect(screen.queryByRole("button", { name: c.answer.open })).toBeNull();
    cleanup();
    setup(question());
    expect(await screen.findByRole("button", { name: c.answer.open })).toBeTruthy();
  });
  it("a visible contribution has no banner", async () => {
    setup(claim());
    await screen.findByText("their challenge");
    expect(screen.queryByText(c.hidden_notice)).toBeNull();
  });
});

describe("archived project", () => {
  it("shows the read-only notice and none of the write controls", async () => {
    setup(claim({ actor_id: "me" }), { archived: true, actions: [...READ, "content:Moderate"] });
    expect(await screen.findByText(en.project.archived_notice)).toBeTruthy();
    for (const name of [c.withdraw, c.supersede, c.hide, c.challenge_open, c.challenge.submit])
      expect(screen.queryByRole("button", { name })).toBeNull();
    expect(screen.queryByRole("link", { name: c.challenge_open })).toBeNull();
    expect(screen.queryByRole("group", { name: c.stance_group })).toBeNull();
    await openMenu();
    for (const name of [c.supersede, c.hide, c.unhide]) expect(item(name)).toBeNull();
  });
});

describe("previous / next", () => {
  afterEach(() => sessionStorage.clear());
  it("walks the list the contribution was opened from, and only a list of this project", async () => {
    sessionStorage.setItem("av.list-nav", JSON.stringify({ url: "/p/demo/?tab=review", ids: ["c0", "c1", "c2"] }));
    setup(claim());
    const nav = await screen.findByRole("navigation", { name: c.nav.label });
    expect(within(nav).getByRole("link", { name: c.nav.back }).getAttribute("href")).toMatch(/^\/p\/demo\/?\?tab=review$/);
    expect(within(nav).getByRole("link", { name: c.nav.prev }).getAttribute("href")).toMatch(/\/c\/c0\/?$/);
    expect(within(nav).getByRole("link", { name: c.nav.next }).getAttribute("href")).toMatch(/\/c\/c2\/?$/);
    cleanup();
    sessionStorage.setItem("av.list-nav", JSON.stringify({ url: "/p/other/", ids: ["c0", "c1"] }));
    setup(claim());
    await screen.findByText("my challenge");
    expect(screen.queryByRole("navigation", { name: c.nav.label })).toBeNull();
  });
});
