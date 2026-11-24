import type { components } from "@/lib/api/schema";

export type Session = components["schemas"]["Session"];
export type Action = Exclude<components["schemas"]["Action"], "*">;

const SYSTEM: Action[] = ["project:Create", "actor:Manage", "settings:Manage", "policy:Manage"];
/** Every action, in the server's display order. */
export const ACTIONS: Action[] = ["contribution:Read", "contribution:Create", "signal:Create", "content:Moderate", "member:Manage", ...SYSTEM];

/** Mirrors the server's Grants.Can, to show or hide controls; the server still checks. */
export function can(s: Session | undefined, a: Action, project?: string): boolean {
  const acc = s?.access;
  if (!acc) return false;
  if (acc.all.includes("*") || acc.all.includes(a)) return true;
  if (!project || SYSTEM.includes(a)) return false;
  return acc.projects[project]?.includes(a) ?? false;
}

/** A hidden contribution takes writes (signals, answers, a replacement) only from moderators; its author can just read it. */
export const hiddenLocked = (s: Session | undefined, project: string, d: { hidden?: boolean } | undefined) =>
  !!d?.hidden && !can(s, "content:Moderate", project);

export const isAdmin = (s: Session | undefined) => !!s?.access?.all.includes("*");

/** Whether member:Manage is held anywhere. */
export const managesAny = (s: Session | undefined) =>
  can(s, "member:Manage") || Object.values(s?.access?.projects ?? {}).some((l) => l.includes("member:Manage"));

/** Whether any admin page is open to s. */
export const canManage = (s: Session | undefined) => SYSTEM.some((a) => can(s, a)) || managesAny(s);
