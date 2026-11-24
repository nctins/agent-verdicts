import { describe, expect, it } from "vitest";
import { ACTIONS, can, canManage, isAdmin, type Session } from "./access";

const s = (all: string[], projects: Record<string, string[]> = {}): Session =>
  ({ authenticated: true, actor: { id: "a", name: "A", type: "Human" }, csrf_token: "t", must_change_password: false, setup_required: false, access: { all, projects } }) as Session;

describe("can", () => {
  it("mirrors the server: * actions, per-project actions, system actions only from *", () => {
    const g = s(["contribution:Read"], { p1: ["contribution:Create", "project:Create"] });
    expect(can(g, "contribution:Read", "p9")).toBe(true);
    expect(can(g, "contribution:Create", "p1")).toBe(true);
    expect(can(g, "contribution:Create", "p2")).toBe(false);
    expect(can(g, "project:Create")).toBe(false);
    expect(can(g, "project:Create", "p1")).toBe(false);
  });
  it("content:Moderate is a project action: held per project, never from another project", () => {
    const g = s([], { p1: ["content:Moderate"] });
    expect(can(g, "content:Moderate", "p1")).toBe(true);
    expect(can(g, "content:Moderate", "p2")).toBe(false);
    expect(ACTIONS).toContain("content:Moderate");
  });
  it("an admin can do everything; nobody without a session can do anything", () => {
    expect(can(s(["*"]), "settings:Manage")).toBe(true);
    expect(isAdmin(s(["*"]))).toBe(true);
    expect(can(undefined, "contribution:Read", "p")).toBe(false);
    expect(isAdmin(s([]))).toBe(false);
  });
});

describe("canManage", () => {
  it("is true with any admin action, or member:Manage on any project", () => {
    expect(canManage(s([]))).toBe(false);
    expect(canManage(s(["contribution:Read"]))).toBe(false);
    expect(canManage(s(["settings:Manage"]))).toBe(true);
    expect(canManage(s([], { p1: ["member:Manage"] }))).toBe(true);
  });
});
