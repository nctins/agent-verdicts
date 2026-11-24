import { describe, expect, it } from "vitest";
import { applyAssessment, applyChallenge, myActiveStance } from "./stance";

const base = () => ({
  id: "c1", project_id: "p", kind: "claim", title: "t", body: "", sources: [], actor_id: "a0", actor_name: "A0",
  created_at: "2026-11-14T00:00:00Z", state: "Favorable",
  counts: { favorable: 1, unfavorable: 0, neutral: 0, favorable_actors: 1 },
  signals: {
    favorable: [{ id: "s1", kind: "assessment", stance: "favorable", actor_id: "me", actor_name: "Me", body: "", sources: [], created_at: "2026-11-14T00:00:00Z" }],
    unfavorable: [], neutral: [], history: [],
  },
}) as any; // eslint-disable-line @typescript-eslint/no-explicit-any
const me = { id: "me", name: "Me" };
const now = "2026-11-14T01:00:00Z";

describe("stance", () => {
  it("finds my active assessment", () => expect(myActiveStance(base(), "me")).toBe("favorable"));
  it("same stance is a no-op", () => {
    const d = base();
    expect(applyAssessment(d, me, "favorable", now)).toBe(d);
  });
  it("switching moves the old one to history and recomputes state", () => {
    const d = applyAssessment(base(), me, "unfavorable", now);
    expect(d.counts).toEqual({ favorable: 0, unfavorable: 1, neutral: 0, favorable_actors: 0 });
    expect(d.state).toBe("Unfavorable");
    expect(d.signals.history).toHaveLength(1);
    expect(d.signals.history[0].superseded).toBe(true);
    expect(myActiveStance(d, "me")).toBe("unfavorable");
  });
  it("first assessment adds to counts", () => {
    const d = applyAssessment(base(), { id: "other", name: "O" }, "unfavorable", now);
    expect(d.counts).toEqual({ favorable: 1, unfavorable: 1, neutral: 0, favorable_actors: 1 });
    expect(d.state).toBe("Conflict");
  });
  it("the author's own support is listed but not counted", () => {
    const d = applyAssessment(base(), { id: "a0", name: "A0" }, "favorable", now);
    expect(d.signals.favorable).toHaveLength(2);
    expect(d.counts).toEqual({ favorable: 1, unfavorable: 0, neutral: 0, favorable_actors: 1 });
    const back = applyAssessment(d, { id: "a0", name: "A0" }, "unfavorable", now);
    expect(back.counts).toEqual({ favorable: 1, unfavorable: 1, neutral: 0, favorable_actors: 1 });
    expect(back.state).toBe("Conflict");
  });
  it("neutral never changes state", () => {
    const d = applyAssessment(base(), { id: "o", name: "O" }, "neutral", now);
    expect(d.state).toBe("Favorable");
  });
  it("does not mutate the input", () => {
    const d = base();
    const copy = JSON.stringify(d);
    applyAssessment(d, me, "unfavorable", now);
    applyChallenge(d, me, "unfavorable", "no", now);
    expect(JSON.stringify(d)).toBe(copy);
  });
  it("a challenge adds a signal but keeps my assessment", () => {
    const d = applyChallenge(base(), me, "unfavorable", "because", now);
    expect(d.counts).toEqual({ favorable: 1, unfavorable: 1, neutral: 0, favorable_actors: 1 });
    expect(d.state).toBe("Conflict");
    expect(d.signals.unfavorable[0]).toMatchObject({ kind: "challenge", body: "because", actor_id: "me" });
    expect(myActiveStance(d, "me")).toBe("favorable");
  });
  it("appends new signals last, like the server's created_at order", () => {
    const d = applyChallenge(base(), { id: "o", name: "O" }, "favorable", "x", now);
    expect(d.signals.favorable.map((s: { id: string }) => s.id)).toEqual(["s1", "optimistic"]);
  });
  it("no actor means no stance", () => expect(myActiveStance(base(), null)).toBeNull());
});
