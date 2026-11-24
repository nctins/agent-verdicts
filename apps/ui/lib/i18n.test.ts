import { describe, expect, it } from "vitest";
import { resolveLocale } from "./i18n";

describe("resolveLocale", () => {
  it("stored wins", () => expect(resolveLocale("en", "vi-VN")).toBe("en"));
  it("garbage stored ignored", () => expect(resolveLocale("fr", "en-US")).toBe("en"));
  it("navigator en*", () => expect(resolveLocale(null, "en-GB")).toBe("en"));
  it("default vi", () => expect(resolveLocale(null, "ja")).toBe("vi"));
  it("no navigator", () => expect(resolveLocale(null, undefined)).toBe("vi"));
});
