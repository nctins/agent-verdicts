import { describe, expect, it } from "vitest";
import { applyTable, fold, readTableState, writeTableState } from "./table";

type R = { name: string; type: string; seen: number | null };
const rows: R[] = [
  { name: "Bình", type: "Human", seen: 3 },
  { name: "agent-a", type: "Agent", seen: null },
  { name: "Đông", type: "Human", seen: 1 },
];
const m = {
  text: (r: R) => r.name,
  filters: { type: (r: R, v: string) => r.type === v },
  sorts: { seen: (r: R) => r.seen, name: (r: R) => r.name },
};
const state = (q: string) => readTableState(new URLSearchParams(q), ["type"]);

describe("table", () => {
  it("folds case and Vietnamese accents", () => {
    expect(fold("Bình Đông")).toBe("binh dong");
  });
  it("searches without accents and filters", () => {
    expect(applyTable(rows, state("q=binh"), m, 10).rows.map((r) => r.name)).toEqual(["Bình"]);
    expect(applyTable(rows, state("type=Human"), m, 10).total).toBe(2);
  });
  it("sorts both ways with nulls last", () => {
    expect(applyTable(rows, state("sort=seen"), m, 10).rows.map((r) => r.seen)).toEqual([1, 3, null]);
    expect(applyTable(rows, state("sort=seen&dir=desc"), m, 10).rows.map((r) => r.seen)).toEqual([3, 1, null]);
  });
  it("pages, clamping a page past the end", () => {
    const r = applyTable(rows, state("page=9"), m, 2);
    expect([r.page, r.pages, r.rows.length, r.total]).toEqual([2, 2, 1, 3]);
  });
  it("writes changes to the URL, back to page 1 unless paging, with an optional prefix", () => {
    const sp = new URLSearchParams("page=3&q=a&tab=x");
    expect(writeTableState(sp, { q: "b" }).toString()).toBe("q=b&tab=x");
    expect(writeTableState(sp, { page: "4" }).toString()).toBe("page=4&q=a&tab=x");
    expect(writeTableState(new URLSearchParams(), { q: "z" }, "a_").toString()).toBe("a_q=z");
    expect(readTableState(new URLSearchParams("a_q=z&q=other"), [], "a_").q).toBe("z");
  });
});
