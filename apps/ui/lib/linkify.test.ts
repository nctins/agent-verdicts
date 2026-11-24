import { describe, expect, it } from "vitest";
import { linkify } from "./linkify";

describe("linkify", () => {
  it("links only http(s) and drops trailing punctuation", () => {
    const parts = linkify("xem https://a.b/c. và javascript:alert(1)");
    expect(parts.filter((p) => p.href)).toEqual([{ text: "https://a.b/c", href: "https://a.b/c" }]);
    expect(parts.map((p) => p.text).join("")).toBe("xem https://a.b/c. và javascript:alert(1)");
  });
  it("handles several links and closing parens", () => {
    const parts = linkify("(see http://x.y/z) and https://q.r/s?t=1!");
    expect(parts.filter((p) => p.href).map((p) => p.href)).toEqual(["http://x.y/z", "https://q.r/s?t=1"]);
    expect(parts.map((p) => p.text).join("")).toBe("(see http://x.y/z) and https://q.r/s?t=1!");
  });
  it("plain text is one part", () => expect(linkify("hello")).toEqual([{ text: "hello" }]));
  it("bare scheme is not a link", () => expect(linkify("https:// x").every((p) => !p.href)).toBe(true));
});
