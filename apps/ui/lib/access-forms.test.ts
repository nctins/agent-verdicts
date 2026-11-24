import { describe, expect, it } from "vitest";
import { accessByActor, assignmentBodies, coveredBy, effectiveAccess, nameAndList } from "./access-forms";

describe("assignmentBodies", () => {
  it("every project is one assignment with project_id null", () => {
    expect(assignmentBodies({ actorId: "a", roleId: "viewer", scope: "all" })).toEqual([
      { actor_id: "a", role_id: "viewer", project_id: null },
    ]);
  });
  it("chosen projects are one assignment each, without duplicates", () => {
    expect(assignmentBodies({ actorId: "a", roleId: "viewer", scope: ["p1", "p2", "p1"] })).toEqual([
      { actor_id: "a", role_id: "viewer", project_id: "p1" },
      { actor_id: "a", role_id: "viewer", project_id: "p2" },
    ]);
  });
  it("no project chosen is nothing to send", () => {
    expect(assignmentBodies({ actorId: "a", roleId: "viewer", scope: [] })).toEqual([]);
  });
});

describe("nameAndList", () => {
  it("trims the name and reports what is missing", () => {
    expect(nameAndList(" Reviewer ", ["contribution:Read"])).toEqual({ name: "Reviewer", error: null });
    expect(nameAndList("  ", ["x"]).error).toBe("name_required");
    expect(nameAndList("R", []).error).toBe("pick_one");
  });
});

describe("accessByActor", () => {
  const as = (actor: string, role: string, project: string | null) =>
    ({ id: actor + role + project, actor_id: actor, actor_name: actor, role_id: role, role_name: role, project_id: project, created_at: "" });
  it("groups each actor's roles, every-project first, projects sorted", () => {
    const m = accessByActor([as("a", "viewer", "p2"), as("a", "viewer", "p1"), as("a", "admin", null), as("b", "contributor", "p1")]);
    expect(m.get("a")).toEqual([{ role: "admin", all: true, projects: [] }, { role: "viewer", all: false, projects: ["p1", "p2"] }]);
    expect(m.get("b")).toEqual([{ role: "contributor", all: false, projects: ["p1"] }]);
    expect(m.get("nobody")).toBeUndefined();
  });
  it("a role on every project hides the same role's single projects", () => {
    const m = accessByActor([as("a", "viewer", "p1"), as("a", "viewer", null)]);
    expect(m.get("a")).toEqual([{ role: "viewer", all: true, projects: [] }]);
  });
});

describe("coveredBy", () => {
  const roles = [
    { id: "admin", name: "admin", actions: ["*"] },
    { id: "contributor", name: "contributor", actions: ["contribution:Read", "contribution:Create", "signal:Create"] },
    { id: "viewer", name: "viewer", actions: ["contribution:Read"] },
  ];
  const has = (role_id: string, project_id: string | null) => ({ role_id, role_name: role_id, project_id });

  it("finds an existing role on the same project that holds every action of the new one", () => {
    expect(coveredBy([has("contributor", "p")], roles, "viewer", ["p", "q"])).toEqual([{ project: "p", by: has("contributor", "p") }]);
  });
  it("an every-project role covers any project, and * covers every action", () => {
    expect(coveredBy([has("admin", null)], roles, "contributor", "all")).toEqual([{ project: null, by: has("admin", null) }]);
    expect(coveredBy([has("viewer", null)], roles, "viewer", ["p"])).toEqual([{ project: "p", by: has("viewer", null) }]);
  });
  it("a project role does not cover every project, and a narrower role covers nothing", () => {
    expect(coveredBy([has("contributor", "p")], roles, "viewer", "all")).toEqual([]);
    expect(coveredBy([has("viewer", "p")], roles, "contributor", ["p"])).toEqual([]);
    expect(coveredBy([has("contributor", "p")], roles, "unknown", ["p"])).toEqual([]);
  });
});

describe("effectiveAccess", () => {
  const roles = [
    { id: "admin", actions: ["*"] },
    { id: "viewer", actions: ["contribution:Read"] },
    { id: "maker", actions: ["contribution:Read", "contribution:Create", "project:Create"] },
  ];
  const has = (role_id: string, project_id: string | null) => ({ role_id, role_name: role_id, project_id });

  it("joins the roles reaching each project, and keeps instance actions apart", () => {
    const got = effectiveAccess([has("viewer", "p"), has("maker", null)], roles, ["p", "q"]);
    expect(got.system).toEqual(["project:Create"]);
    expect(got.projects).toEqual([
      { project: "p", roles: ["maker", "viewer"], actions: ["contribution:Create", "contribution:Read"] },
      { project: "q", roles: ["maker"], actions: ["contribution:Create", "contribution:Read"] },
    ]);
  });
  it("a project role grants no instance action; * stays *; unreached projects are left out", () => {
    expect(effectiveAccess([has("maker", "p")], roles, ["p", "q"])).toEqual({
      system: [], projects: [{ project: "p", roles: ["maker"], actions: ["contribution:Create", "contribution:Read"] }],
    });
    expect(effectiveAccess([has("admin", null)], roles, ["p"]).system).toEqual(["*"]);
  });
});
