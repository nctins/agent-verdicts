// Sub-pages shown under their section in the sidebar.

import type { Require } from "@/components/SessionGate";

/** Admin menu groups, in order (spec 2026-11-22-admin-ui-redesign U1); "" is the ungrouped top. */
export const ADMIN_GROUPS = ["", "group_access", "group_projects", "group_system"] as const;

/** Admin pages: [href, nav message key, extra path prefixes that count as this page, what it needs, menu group]. */
export const ADMIN_LINKS: readonly (readonly [string, string, readonly string[], Require, (typeof ADMIN_GROUPS)[number]])[] = [
  ["/manage/", "dashboard", [], "manage", ""],
  ["/manage/actors/", "actors", ["/manage/actor/"], "people", "group_access"],
  ["/manage/roles/", "roles", [], "policy:Manage", "group_access"],
  ["/manage/policies/", "policies", [], "policy:Manage", "group_access"],
  ["/manage/projects/", "manage_projects", [], "project:Create", "group_projects"],
  ["/manage/settings/search/", "search_settings", [], "settings:Manage", "group_system"],
  ["/manage/audit/", "audit", [], "people", "group_system"],
];

/**
 * Help sections by menu group (spec 2026-11-22-help-console H1); each is its own page /help/<id>/ with a
 * help.toc_<id> menu label, and the group a help.<group> label.
 */
export const HELP_GROUPS = [
  ["group_start", ["overview", "concepts", "states", "stance", "questions"]],
  ["group_daily", ["search", "filters", "shortcuts", "account"]],
  ["group_projects", ["projects"]],
  ["group_agents", ["mcp", "ai"]],
  ["group_admin", ["setup", "actors", "access", "audit"]],
  ["group_reference", ["glossary"]],
] as const;
/** Old section ids and the page that replaced them (spec 2026-11-23-help-rewrite H5). */
export const HELP_MOVED: Record<string, string> = { admin: "setup" };
/** Every section in reading order: the menu order. */
export const HELP_SECTIONS = HELP_GROUPS.flatMap(([, s]) => s);
export type HelpSection = (typeof HELP_GROUPS)[number][1][number];
export const helpGroupOf = (s: HelpSection) => HELP_GROUPS.find(([, list]) => (list as readonly string[]).includes(s))![0];

/** Which admin link is current; /manage/ itself only matches exactly. */
export function currentAdmin(path: string): string | null {
  for (const [href, , also] of ADMIN_LINKS) {
    if (path === href || (href !== "/manage/" && path.startsWith(href)) || also.some((p) => path.startsWith(p))) return href;
  }
  return null;
}
