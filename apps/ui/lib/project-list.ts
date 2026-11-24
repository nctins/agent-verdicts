import type { components } from "@/lib/api/schema";
import { fold } from "@/lib/table";

type Project = components["schemas"]["Project"];
export type ProjectSort = "activity" | "name" | "contributions" | "conflicts";
export const PROJECT_SORTS: readonly ProjectSort[] = ["activity", "name", "contributions", "conflicts"];

/** The home page's projects: matching q (name or id), in the chosen order; ties and unknowns by id. */
export function sortProjects(items: Project[], sort: ProjectSort, q = ""): Project[] {
  const want = fold(q.trim());
  const byId = (a: Project, b: Project) => a.id.localeCompare(b.id);
  const order: Record<ProjectSort, (a: Project, b: Project) => number> = {
    // Newest activity first; a project with none goes last.
    activity: (a, b) => (b.last_activity_at ?? "").localeCompare(a.last_activity_at ?? ""),
    name: (a, b) => a.name.localeCompare(b.name),
    contributions: (a, b) => b.contribution_count - a.contribution_count,
    conflicts: (a, b) => b.states.Conflict - a.states.Conflict,
  };
  return items
    .filter((p) => !want || fold(`${p.name} ${p.id}`).includes(want))
    .sort((a, b) => order[sort](a, b) || byId(a, b));
}
