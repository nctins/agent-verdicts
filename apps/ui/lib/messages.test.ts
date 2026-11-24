import { createTranslator } from "next-intl";
import { describe, expect, it } from "vitest";
import en from "../messages/en.json";
import vi from "../messages/vi.json";
import { ACTIONS } from "./access";

type Tree = { [k: string]: string | Tree };

function leaves(t: Tree, prefix = ""): [string, string][] {
  return Object.entries(t).flatMap(([k, v]) =>
    typeof v === "string" ? [[prefix + k, v] as [string, string]] : leaves(v, `${prefix}${k}.`),
  );
}

describe("messages", () => {
  const files = { vi, en } as Record<string, Tree>;
  it("vi and en have the same keys", () => {
    const keys = (t: Tree) => leaves(t).map(([k]) => k).sort();
    expect(keys(files.en)).toEqual(keys(files.vi));
  });
  it.each(Object.keys(files))("%s: no empty value and every message parses", (locale) => {
    const t = createTranslator({
      locale,
      messages: files[locale],
      onError: (e) => {
        throw e;
      },
    });
    for (const [key, value] of leaves(files[locale])) {
      expect(value.trim(), key).not.toBe("");
      // onError rethrows, so an ICU syntax error fails the test
      expect(() => t(key as never, { count: 1, favorable: 1, unfavorable: 1, neutral: 1, done: 1, total: 1, url: "u", username: "n", password: "p", id: "i", pipeline: "p", provider: "p", code: "c", stance: "s", label: "l", from: 1, to: 2, page: 1, pages: 2, name: "n", days: 7 } as never), key).not.toThrow();
    }
  });
  it("every access action has a label", () => {
    // Policy pages build the key from the action name, so a missing label shows the raw key.
    for (const a of ACTIONS) expect((vi as unknown as { manage: { access: { action: Record<string, string> } } }).manage.access.action[a], a).toBeTruthy();
  });
  it("the core terms read as in the API and MCP, in every language (spec 2026-11-23-english-domain-terms)", () => {
    for (const m of [vi, en] as unknown as Record<string, Record<string, Record<string, string>>>[]) {
      expect(m.state).toMatchObject({ Favorable: "Favorable", Unfavorable: "Unfavorable", Conflict: "Conflict", InsufficientEvidence: "Insufficient evidence" });
      expect(m.stance).toMatchObject({ favorable: "Support", unfavorable: "Oppose", neutral: "Neutral" });
      expect(m.kind).toMatchObject({ claim: "Claim", question: "Question" });
      expect(m.question_status).toMatchObject({ open: "Open", answered: "Answered", resolved: "Resolved" });
      expect(m.contribution.signal).toMatchObject({ assessment: "Assessment", challenge: "Challenge" });
    }
  });
});
