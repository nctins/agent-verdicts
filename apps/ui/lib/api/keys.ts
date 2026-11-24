import type { paths } from "./schema";

export type ContributionFilters = NonNullable<
  paths["/projects/{p}/contributions"]["get"]["parameters"]["query"]
>;
export type SearchOpts = Omit<NonNullable<paths["/projects/{p}/search"]["get"]["parameters"]["query"]>, "q">;

export type AuditFilters = NonNullable<paths["/admin/audit"]["get"]["parameters"]["query"]>;

export const keys = {
  session: () => ["session"] as const,
  projects: () => ["projects"] as const,
  contributions: (p: string, filters?: ContributionFilters) =>
    filters === undefined ? (["contributions", p] as const) : (["contributions", p, filters] as const),
  contribution: (p: string, id: string) => ["contribution", p, id] as const,
  /** Mutation key of signal writes on one contribution. */
  signal: (p: string, id: string) => ["signal", p, id] as const,
  /** Prefix of every search in project p. */
  searches: (p: string) => ["search", p] as const,
  search: (p: string, q: string, opts: SearchOpts = {}) => ["search", p, q, opts] as const,
  authors: (p: string) => ["authors", p] as const,
  /** Prefix of every read-count range of project p; with a range, one of them. */
  reads: (p: string, range?: string) => (range === undefined ? (["reads", p] as const) : (["reads", p, range] as const)),
  adminOverview: () => ["admin", "overview"] as const,
  /** Prefix of every actor list; with a name search, one list. */
  actors: (q?: string) => (q === undefined ? (["admin", "actors"] as const) : (["admin", "actors", { q }] as const)),
  actor: (id: string) => ["admin", "actor", id] as const,
  /** Prefix of every audit list; with filters, one list. */
  audit: (f?: AuditFilters) => (f === undefined ? (["admin", "audit"] as const) : (["admin", "audit", f] as const)),
  adminProjects: () => ["admin", "projects"] as const,
  settings: () => ["admin", "settings"] as const,
  policies: () => ["admin", "policies"] as const,
  roles: () => ["admin", "roles"] as const,
  /** Prefix of every assignment list; with a filter, one list. */
  assignments: (f?: { actor?: string; project?: string }) =>
    f === undefined ? (["admin", "assignments"] as const) : (["admin", "assignments", f] as const),
};
