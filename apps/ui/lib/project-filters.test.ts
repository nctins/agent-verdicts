import { describe, expect, it } from "vitest";
import { listFilter, parseProjectParams, prefillTitle } from "./project-filters";

describe("listFilter", () => {
  it("maps each tab", () => {
    expect(listFilter("claims")).toEqual({ kind: "claim" });
    expect(listFilter("questions")).toEqual({ kind: "question" });
    expect(listFilter("review")).toEqual({ kind: "claim", state: ["Conflict", "InsufficientEvidence"] });
  });
  it("adds the state on Claims and the status on Questions, nowhere else", () => {
    expect(listFilter("claims", "Conflict")).toEqual({ kind: "claim", state: ["Conflict"] });
    expect(listFilter("questions", null, "resolved")).toEqual({ kind: "question", question_status: ["resolved"] });
    expect(listFilter("review", "Favorable", "open")).toEqual(listFilter("review"));
  });
});

describe("parseProjectParams", () => {
  const p = (s: string) => parseProjectParams(new URLSearchParams(s));
  it("defaults", () => {
    expect(p("")).toEqual({ tab: "overview", actor: "", sort: "newest", q: "", state: null, status: null, superseded: false });
  });
  it("reads valid values and trims q", () => {
    expect(p("tab=questions&actor=a1&sort=least&q=%20a%26b%20&state=Conflict&status=answered")).toEqual({
      tab: "questions", actor: "a1", sort: "least", q: "a&b", state: "Conflict", status: "answered", superseded: false,
    });
  });
  it("reads the include-superseded toggle only from superseded=1", () => {
    expect(p("superseded=1")).toMatchObject({ superseded: true });
    expect(p("superseded=yes")).toMatchObject({ superseded: false });
  });
  it("reads sort=active and the me actor", () => {
    expect(p("sort=active&actor=me")).toMatchObject({ sort: "active", actor: "me" });
  });
  it("opens an old list link (tab=all, or list parameters without a tab) on Claims", () => {
    expect(p("tab=all")).toMatchObject({ tab: "claims" });
    expect(p("state=Conflict")).toMatchObject({ tab: "claims", state: "Conflict" });
    expect(p("q=x")).toMatchObject({ tab: "claims" });
    expect(p("tab=settings")).toMatchObject({ tab: "settings" });
  });
  it("reads the old Open-questions tab as Questions filtered to open", () => {
    expect(p("tab=open")).toMatchObject({ tab: "questions", status: "open" });
  });
  it("falls back on invalid tab/sort/state/status", () => {
    expect(p("tab=bogus&sort=random&state=Purple&status=nope")).toEqual({ tab: "overview", actor: "", sort: "newest", q: "", state: null, status: null, superseded: false });
  });
});

describe("prefillTitle", () => {
  it("cuts at 200 code points without splitting a surrogate pair", () => {
    const s = "a" + "😀".repeat(250);
    const out = prefillTitle(s);
    expect([...out].length).toBe(200);
    expect(out.endsWith("😀")).toBe(true);
  });
});
