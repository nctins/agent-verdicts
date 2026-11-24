import { describe, expect, it } from "vitest";
import { nextTheme, readThemePref, resolveTheme } from "./theme";

describe("theme", () => {
  it("system follows media", () => {
    expect(resolveTheme("system", true)).toBe("dark");
    expect(resolveTheme("system", false)).toBe("light");
  });
  it("explicit wins", () => expect(resolveTheme("light", true)).toBe("light"));
  it("bad stored → system", () => expect(readThemePref("purple")).toBe("system"));
  it("toggle flips what is shown", () => {
    expect(nextTheme("system", true)).toBe("light");
    expect(nextTheme("system", false)).toBe("dark");
    expect(nextTheme("dark", false)).toBe("light");
    expect(nextTheme("light", true)).toBe("dark");
  });
});
