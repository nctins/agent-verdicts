import { describe, expect, it } from "vitest";
import { afterLogin, safeNext } from "./safe-next";

describe("safeNext", () => {
  it.each([
    ["/project/?id=demo", "/project/?id=demo"],
    ["/manage/actors/", "/manage/actors/"],
    [null, "/"],
    ["", "/"],
    ["//evil.com", "/"],
    ["/\\evil.com", "/"],
    ["https://evil.com", "/"],
    ["javascript:alert(1)", "/"],
    ["%2F%2Fevil.com", "/"],
    ["/login/?next=/x", "/"],
    [" /x", "/"],
  ])("%s → %s", (raw, want) => expect(safeNext(raw)).toBe(want));
});

describe("afterLogin", () => {
  it("goes to next normally", () => expect(afterLogin({ must_change_password: false }, "/project/?id=a")).toBe("/project/?id=a"));
  it("goes to Account when the password must change", () =>
    expect(afterLogin({ must_change_password: true }, "/project/?id=a")).toBe("/account/"));
  it("still refuses a foreign next", () => expect(afterLogin({ must_change_password: false }, "//evil.com")).toBe("/"));
});
