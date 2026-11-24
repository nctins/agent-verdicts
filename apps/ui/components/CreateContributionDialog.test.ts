import { describe, expect, it } from "vitest";
import { contributionProblem } from "./CreateContributionDialog";

describe("contributionProblem", () => {
  it("requires a title after trimming", () => {
    expect(contributionProblem("   ", "")).toBe("contribution.create.error_required");
    expect(contributionProblem(" a ", "")).toBe(null);
  });
  it("counts the title in code points, at most 200", () => {
    expect(contributionProblem("😀".repeat(200), "")).toBe(null); // 400 UTF-16 units
    expect(contributionProblem("a".repeat(201), "")).toBe("contribution.create.error_title_too_long");
    expect(contributionProblem(`  ${"a".repeat(200)}  `, "")).toBe(null);
  });
  it("limits the body to 65536 UTF-8 bytes", () => {
    expect(contributionProblem("t", "a".repeat(65536))).toBe(null);
    expect(contributionProblem("t", "a".repeat(65537))).toBe("contribution.create.error_body_too_long");
    expect(contributionProblem("t", "ă".repeat(32769))).toBe("contribution.create.error_body_too_long"); // 2 bytes each
  });
});
