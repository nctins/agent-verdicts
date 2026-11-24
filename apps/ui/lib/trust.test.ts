import { describe, expect, it } from "vitest";
import cases from "../../api/internal/trust/testdata/state_cases.json";
import { rank, stateOf, type TrustState } from "./trust";

describe("trust parity with Go", () => {
  it.each(cases.state)("stateOf(%j)", ({ counts, min_favorable, want }) => {
    expect(stateOf(counts, min_favorable)).toBe(want);
  });
  it("rank order", () => {
    (cases.rank as TrustState[]).forEach((s, i) => expect(rank(s)).toBe(i));
  });
});
