import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { describe, expect, it, vi } from "vitest";
import { HELP_MD, HELP_PAGES } from "@/content/help";
import { mcpSetup } from "@/lib/mcp";
import { HELP_SECTIONS } from "@/lib/sections";
import en from "@/messages/en.json";
import vi_ from "@/messages/vi.json";
import { HelpIndex, HelpSectionView, useDiagrams } from "./Help";
import { HelpArticle, HelpToc, headingsOf } from "./HelpArticle";

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn(), replace: vi.fn() }) }));

// Registered in api/internal/adapter/mcp/server.go.
const TOOLS = [
  "search", "create_contribution", "reference_contribution", "create_challenge",
  "create_assessment", "list_contributions", "observe_trust_signal", "withdraw_signal",
];

describe("help content", () => {
  it("has a page in both languages for every menu entry, plus quick start and FAQ", () => {
    for (const k of [...HELP_SECTIONS, "quickstart", "faq"] as const) {
      expect(HELP_PAGES).toContain(k);
      expect(HELP_MD.vi[k].trim(), `vi/${k}`).not.toBe("");
      expect(HELP_MD.en[k].trim(), `en/${k}`).not.toBe("");
    }
  });
  it.each(HELP_PAGES)("%s: both languages have the same headings and diagrams", (k) => {
    const count = (md: string, re: RegExp) => (md.match(re) ?? []).length;
    for (const re of [/^## /gm, /^### /gm, /^```diagram$/gm, /^> \[!/gm])
      expect(count(HELP_MD.en[k], re), `${k} ${re}`).toBe(count(HELP_MD.vi[k], re));
  });
  it("the MCP page carries the exact client setup the app hands out", () => {
    const s = mcpSetup("http(s)://<host>", "<project>");
    for (const md of [HELP_MD.vi.mcp, HELP_MD.en.mcp]) {
      expect(md).toContain(s.config);
      expect(md).toContain(s.cli);
      for (const t of TOOLS) expect(md).toContain("`" + t + "`");
    }
  });
});

describe.each([["en", en], ["vi", vi_]] as const)("help (%s)", (locale, messages) => {
  const view = (el: React.ReactNode) =>
    render(<NextIntlClientProvider locale={locale} messages={messages}>{el}</NextIntlClientProvider>);
  const p = messages.help.pages as Record<string, { title: string }>;

  it("the index links to one page per section, with quick start and frequent questions", () => {
    view(<HelpIndex />);
    // Quick start and the FAQ link to pages too: look in the section groups.
    const groups = screen.getAllByRole("region").filter((r) => r.getAttribute("aria-labelledby")?.startsWith("help-group"));
    for (const k of HELP_SECTIONS)
      expect(groups.flatMap((g) => within(g).queryAllByRole("link", { name: p[k].title })).map((a) => a.getAttribute("href"))).toEqual([expect.stringMatching(new RegExp(`^/help/${k}/?$`))]);
    expect(screen.getByRole("region", { name: messages.help.quickstart })).toBeTruthy();
    expect(screen.getByRole("region", { name: messages.help.faq }).querySelectorAll("details").length).toBeGreaterThan(2);
  });

  it("the filter searches the pages' text too, without accents", async () => {
    view(<HelpIndex />);
    await userEvent.type(screen.getByLabelText(messages.help.filter), "Retry-After");
    expect(screen.getByRole("link", { name: p.mcp.title })).toBeTruthy();
    expect(screen.queryByRole("link", { name: p.concepts.title })).toBeNull();
    await userEvent.clear(screen.getByLabelText(messages.help.filter));
    await userEvent.type(screen.getByLabelText(messages.help.filter), "zzzz");
    expect(screen.getByText(messages.help.no_match)).toBeTruthy();
  });

  it.each(HELP_SECTIONS)("section %s is its own page, titled, with previous/next", (k) => {
    view(<HelpSectionView id={k} />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(p[k].title);
    const pager = screen.getByRole("navigation", { name: messages.help.pager });
    const i = HELP_SECTIONS.indexOf(k);
    expect(pager.querySelectorAll("a")).toHaveLength((i > 0 ? 1 : 0) + (i < HELP_SECTIONS.length - 1 ? 1 : 0));
  });
});

describe("HelpArticle", () => {
  const view = (md: string, diagrams = {}) =>
    render(<NextIntlClientProvider locale="en" messages={en}><HelpArticle md={md} diagrams={diagrams} /><HelpToc md={md} /></NextIntlClientProvider>);

  it("turns callouts, keys, state badges, code and diagrams into their pieces", () => {
    const { container } = view([
      "## First part", "Press `kbd:j`, see `state:Conflict` and `qstatus:open`.", "",
      "> [!WARNING]", "> Cannot be undone.", "",
      "## Second part", "```sh", "echo hi", "```", "", "```diagram", "box", "```",
    ].join("\n"), { box: <p>the diagram</p> });
    const callout = screen.getByRole("note", { name: en.help.callout.warning });
    expect(callout.textContent).toContain("Cannot be undone.");
    expect(callout.textContent).not.toContain("[!WARNING]");
    expect(container.querySelector("kbd")?.textContent).toBe("j");
    expect(screen.getByText(en.state.Conflict)).toBeTruthy();
    expect(screen.getByText(en.question_status.open)).toBeTruthy();
    expect(screen.getByRole("button", { name: en.kit.copy })).toBeTruthy();
    expect(screen.getByText("the diagram")).toBeTruthy();
    const toc = screen.getByRole("navigation", { name: en.help.on_this_page });
    expect(within(toc).getByRole("link", { name: "Second part" }).getAttribute("href")).toBe("#second-part");
    expect(screen.getByRole("heading", { name: /Second part/ }).id).toBe("second-part");
  });

  it("anchors fold accents", () => {
    expect(headingsOf("## Làm sao để đánh giá\n")).toEqual([{ id: "lam-sao-de-danh-gia", text: "Làm sao để đánh giá" }]);
  });
});

describe.each([["en", en], ["vi", vi_]] as const)("help diagrams (%s)", (locale, messages) => {
  const names = () => {
    let got: Record<string, React.ReactNode> = {};
    const Grab = () => { got = useDiagrams(); return null; };
    render(<NextIntlClientProvider locale={locale} messages={messages}><Grab /></NextIntlClientProvider>);
    return got;
  };
  it("every ```diagram block in the content names a diagram that exists, and each one draws", () => {
    const known = names();
    const used = new Set(Object.values(HELP_MD[locale]).flatMap((md) => [...md.matchAll(/^```diagram\n(.+)\n```$/gm)].map((m) => m[1])));
    expect([...used].sort()).toEqual(expect.arrayContaining(["access-layers", "agent-sequence", "claim-lifecycle", "counting-example"]));
    for (const n of used) {
      expect(known[n], n).toBeTruthy();
      const { container, unmount } = render(<NextIntlClientProvider locale={locale} messages={messages}>{known[n]}</NextIntlClientProvider>);
      expect(container.textContent?.length, n).toBeGreaterThan(20);
      unmount();
    }
  });
  it("the counting example ends Trusted after the opposition is withdrawn", () => {
    render(<NextIntlClientProvider locale={locale} messages={messages}>{names()["counting-example"]}</NextIntlClientProvider>);
    const rows = screen.getAllByRole("listitem");
    expect(rows).toHaveLength(6);
    expect(rows[4].textContent).toContain(messages.state.Conflict);
    expect(rows[5].textContent).toContain(messages.state.Favorable);
  });
});
