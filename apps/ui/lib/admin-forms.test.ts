import { describe, expect, it } from "vitest";
import { actorBody, expiryState, issueBody, projectIdError, resetBody } from "./admin-forms";

const human = { type: "Human" as const, name: " Ann ", username: " ann ", password: "", mustChange: null };

describe("actorBody", () => {
  it("sends the case-sensitive type and omits an empty password", () => {
    expect(actorBody(human)).toEqual({ type: "Human", name: "Ann", username: "ann", must_change: true });
  });
  it("must_change defaults off when a password is chosen, but an explicit choice wins", () => {
    expect(actorBody({ ...human, password: "0123456789" })).toMatchObject({ password: "0123456789", must_change: false });
    expect(actorBody({ ...human, password: "0123456789", mustChange: true })).toMatchObject({ must_change: true });
    expect(actorBody({ ...human, mustChange: false })).toMatchObject({ must_change: false });
  });
  it("an Agent carries only type and name", () => {
    expect(actorBody({ ...human, type: "Agent" })).toEqual({ type: "Agent", name: "Ann" });
  });
});

describe("resetBody", () => {
  it("omits empty username and password", () => {
    expect(resetBody({ username: " ", password: "", mustChange: null })).toEqual({ must_change: true });
    expect(resetBody({ username: "bo", password: "x", mustChange: null })).toEqual({
      username: "bo",
      password: "x",
      must_change: false,
    });
  });
});

describe("projectIdError", () => {
  it("accepts slugs of 1-64 characters, trimmed", () => {
    for (const id of [" a ", "team-backend", "a_b-1", "0x", "a".repeat(64)]) expect(projectIdError(id)).toBeNull();
  });
  it("rejects empty and non-slugs", () => {
    expect(projectIdError("  ")).toBe("required");
    for (const id of ["A", "-a", "_a", "a.b", "a b", "é", "..", "a".repeat(65)]) expect(projectIdError(id), id).toBe("invalid");
  });
});

describe("issueBody", () => {
  it.each([["30", 30], ["90", 90], ["365", 365], ["never", null]] as const)("%s sends expires_in_days %s", (c, n) => {
    expect(issueBody("", c)).toEqual({ expires_in_days: n });
  });
  it("trims the label and omits it when empty", () => {
    expect(issueBody("  ci  ", "90")).toEqual({ label: "ci", expires_in_days: 90 });
  });
});

describe("expiryState", () => {
  const now = Date.parse("2026-11-15T00:00:00Z");
  const at = (days: number) => new Date(now + days * 86400000).toISOString();
  it("tells never, ok, soon and expired apart", () => {
    expect(expiryState(null, now)).toBe("never");
    expect(expiryState(at(30), now)).toBe("ok");
    expect(expiryState(at(6), now)).toBe("soon");
    expect(expiryState(at(-1), now)).toBe("expired");
  });
});
