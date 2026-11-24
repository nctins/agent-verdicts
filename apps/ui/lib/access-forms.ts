export type AssignmentBody = { actor_id: string; role_id: string; project_id: string | null };

/** One assignment for every project ("all"), or one per chosen project. */
export function assignmentBodies(f: { actorId: string; roleId: string; scope: "all" | string[] }): AssignmentBody[] {
  const base = { actor_id: f.actorId, role_id: f.roleId };
  if (f.scope === "all") return [{ ...base, project_id: null }];
  return [...new Set(f.scope)].map((p) => ({ ...base, project_id: p }));
}

/** A policy or role form: a trimmed name and at least one action / policy picked. */
export function nameAndList(name: string, picked: string[]): { name: string; error: "name_required" | "pick_one" | null } {
  const n = name.trim();
  return { name: n, error: !n ? "name_required" : picked.length === 0 ? "pick_one" : null };
}

export type RoleScope = { role: string; all: boolean; projects: string[] };
type AssignmentLike = { actor_id: string; role_name: string; project_id: string | null };

/** Each actor's roles for display: one entry per role, every-project roles first, then by name. */
export function accessByActor(list: AssignmentLike[]): Map<string, RoleScope[]> {
  const byActor = new Map<string, Map<string, RoleScope>>();
  for (const a of list) {
    const roles = byActor.get(a.actor_id) ?? new Map<string, RoleScope>();
    byActor.set(a.actor_id, roles);
    const r = roles.get(a.role_name) ?? { role: a.role_name, all: false, projects: [] };
    roles.set(a.role_name, r);
    if (a.project_id === null) r.all = true;
    else r.projects.push(a.project_id);
  }
  const out = new Map<string, RoleScope[]>();
  for (const [actor, roles] of byActor) {
    const l = [...roles.values()].map((r) => (r.all ? { ...r, projects: [] } : { ...r, projects: r.projects.sort() }));
    l.sort((x, y) => Number(y.all) - Number(x.all) || x.role.localeCompare(y.role));
    out.set(actor, l);
  }
  return out;
}

type Held = { role_id: string; role_name: string; project_id: string | null };

/**
 * Where giving roleId on scope adds nothing: an existing assignment on every project, or on that project, whose
 * role holds every action of roleId ("*" holds all). One entry per covered target (null = every project).
 */
export function coveredBy<H extends Held>(held: H[], roles: { id: string; actions: string[] }[], roleId: string, scope: "all" | string[]) {
  const actions = (id: string) => roles.find((r) => r.id === id)?.actions;
  const want = actions(roleId);
  if (!want) return [];
  const holds = (h: H) => {
    const a = actions(h.role_id) ?? [];
    return a.includes("*") || want.every((x) => a.includes(x));
  };
  const out: { project: string | null; by: H }[] = [];
  for (const project of scope === "all" ? [null] : [...new Set(scope)]) {
    const by = held.find((h) => (h.project_id === null || h.project_id === project) && holds(h));
    if (by) out.push({ project, by });
  }
  return out;
}

const SYSTEM_ACTIONS = new Set(["project:Create", "actor:Manage", "settings:Manage", "policy:Manage"]);

/**
 * What an actor may actually do (spec 2026-11-22-admin-ui-redesign U5): per project, the roles that reach it
 * and the union of their actions ("*" stays "*"); `system` holds the instance-wide actions, which only an
 * every-project assignment grants.
 */
export function effectiveAccess(held: Held[], roles: { id: string; actions: string[] }[], projects: string[]) {
  const acts = (id: string) => roles.find((r) => r.id === id)?.actions ?? [];
  const everywhere = held.filter((h) => h.project_id === null);
  const system = [...new Set(everywhere.flatMap((h) => acts(h.role_id)).filter((a) => a === "*" || SYSTEM_ACTIONS.has(a)))].sort();
  const perProject = projects.map((p) => {
    const reach = held.filter((h) => h.project_id === null || h.project_id === p);
    const actions = [...new Set(reach.flatMap((h) => acts(h.role_id)).filter((a) => !SYSTEM_ACTIONS.has(a)))].sort();
    return { project: p, roles: [...new Set(reach.map((h) => h.role_name))].sort(), actions };
  });
  return { system, projects: perProject.filter((x) => x.roles.length > 0) };
}
