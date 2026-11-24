import { describe, expect, it } from "vitest";
import { hue, initials } from "./Avatar";

describe("initials", () => {
  it("takes the first letters of the first two words", () => {
    expect(initials("Dave Phạm")).toBe("DP");
    expect(initials("  admin ")).toBe("A");
    expect(initials("Ánh Lê Văn")).toBe("ÁL");
    expect(initials("")).toBe("?");
  });
  it("skips punctuation: only words with a letter or digit count", () => {
    expect(initials("Scout (agent)")).toBe("SA");
    expect(initials("Bình (eval)")).toBe("BE");
    expect(initials("- 9lives")).toBe("9");
    expect(initials("(?)")).toBe("?");
  });
});

describe("hue", () => {
  it("differs for two actors who share an initial when seeded by id", () => {
    expect(hue("id-admin")).not.toBe(hue("id-alice"));
  });
});
