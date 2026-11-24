import { describe, expect, it } from "vitest";
import { sortProjects } from "./project-list";

const p = (id: string, o: { name?: string; at?: string | null; n?: number; c?: number } = {}) => ({
  id, name: o.name ?? id, description: "", min_favorable: 1, contribution_count: o.n ?? 0, open_question_count: 0,
  last_activity_at: o.at ?? null, created_at: "2026-01-01T00:00:00Z", archived: false,
  states: { Favorable: 0, Unfavorable: 0, Conflict: o.c ?? 0, InsufficientEvidence: 0 },
});

describe("sortProjects", () => {
  const all = [p("a", { at: "2026-01-01T00:00:00Z", n: 5 }), p("b", { name: "Zeta", at: null, c: 2 }), p("c", { name: "Alpha", at: "2026-03-01T00:00:00Z", n: 1 })];
  const ids = (xs: { id: string }[]) => xs.map((x) => x.id);
  it("orders by activity (none last), name, contributions, conflicts", () => {
    expect(ids(sortProjects(all, "activity"))).toEqual(["c", "a", "b"]);
    expect(ids(sortProjects(all, "name"))).toEqual(["a", "c", "b"]);
    expect(ids(sortProjects(all, "contributions"))).toEqual(["a", "c", "b"]);
    expect(ids(sortProjects(all, "conflicts"))).toEqual(["b", "a", "c"]);
  });
  it("matches name or id, ignoring case and accents", () => {
    expect(ids(sortProjects(all, "name", "ALPHA"))).toEqual(["c"]);
    expect(ids(sortProjects(all, "name", "b"))).toEqual(["b"]);
  });
});
