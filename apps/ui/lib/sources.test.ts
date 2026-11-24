import { describe, expect, it } from "vitest";
import { parseSources, sourcesProblem } from "./sources";

describe("sources", () => {
  it("parses one source per line and drops blanks", () => {
    expect(parseSources(" https://a.test \n\n  abc123\r\n")).toEqual(["https://a.test", "abc123"]);
    expect(parseSources("")).toEqual([]);
  });
  it("mirrors the API limits", () => {
    expect(sourcesProblem(Array(10).fill("s"))).toBeNull();
    expect(sourcesProblem(Array(11).fill("s"))).toBe("sources.error_too_many");
    expect(sourcesProblem(["é".repeat(500)])).toBeNull();
    expect(sourcesProblem(["x".repeat(501)])).toBe("sources.error_too_long");
  });
});
