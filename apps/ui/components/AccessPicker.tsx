"use client";

import { useTranslations } from "next-intl";
import { useState } from "react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { coveredBy } from "@/lib/access-forms";
import { useProjects, useRoles } from "@/lib/api/queries";
import { fold } from "@/lib/table";

export type AccessChoice = { roleId: string; all: boolean; projects: string[] };

/** Start on chosen projects: every project is one click away, never the default (spec 2026-11-22-safer-role-assignment D1). */
export const NO_ACCESS: AccessChoice = { roleId: "", all: false, projects: [] };

type Held = { role_id: string; role_name: string; project_id: string | null };

export const selectCls =
  "h-9 rounded-md border bg-transparent px-3 text-sm focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none";
const choice =
  "flex cursor-pointer items-center gap-2 rounded-md border px-3 py-1.5 text-sm has-checked:border-primary has-checked:bg-accent has-focus-visible:ring-[3px] has-focus-visible:ring-ring/50";

/** Role + scope (every project, or chosen ones), controlled. `held`: the actor's roles, to warn when the pick adds nothing (D3). */
export function AccessPicker({ value, onChange, idPrefix, held = [] }: { value: AccessChoice; onChange: (v: AccessChoice) => void; idPrefix: string; held?: Held[] }) {
  const t = useTranslations("manage.access");
  const roles = useRoles();
  const projects = useProjects();
  const set = (patch: Partial<AccessChoice>) => onChange({ ...value, ...patch });
  const [find, setFind] = useState("");
  const all = projects.data?.items ?? [];
  // Picked projects stay listed whatever the search, so a choice never disappears from view.
  const listed = all.filter((p) => value.projects.includes(p.id) || fold(`${p.id} ${p.name}`).includes(fold(find.trim())));
  const covered = value.roleId ? coveredBy(held, roles.data?.items ?? [], value.roleId, value.all ? "all" : value.projects) : [];
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-1">
        <Label htmlFor={`${idPrefix}-role`}>{t("role")}</Label>
        <select id={`${idPrefix}-role`} className={selectCls} value={value.roleId} onChange={(e) => set({ roleId: e.target.value })}>
          <option value="">{t("pick_role")}</option>
          {roles.data?.items.map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}
        </select>
      </div>
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1 text-sm font-medium">{t("scope")}</legend>
        <div className="flex flex-wrap gap-2">
          <label className={choice}>
            <input type="radio" name={`${idPrefix}-scope`} checked={value.all} onChange={() => set({ all: true })} className="accent-primary" />
            {t("scope_all")}
          </label>
          <label className={choice}>
            <input type="radio" name={`${idPrefix}-scope`} checked={!value.all} onChange={() => set({ all: false })} className="accent-primary" />
            {t("scope_some")}
          </label>
        </div>
        {!value.all && (
          <div className="flex flex-col gap-2">
            {all.length > 6 && (
              <Input
                type="search" value={find} onChange={(e) => setFind(e.target.value)} className="h-8"
                aria-label={t("find_project")} placeholder={t("find_project")}
              />
            )}
            <div className="flex max-h-48 flex-wrap gap-2 overflow-y-auto">
            {listed.map((p) => (
              <label key={p.id} className={choice}>
                <input
                  type="checkbox"
                  checked={value.projects.includes(p.id)}
                  onChange={(e) => set({ projects: e.target.checked ? [...value.projects, p.id] : value.projects.filter((x) => x !== p.id) })}
                  className="accent-primary"
                />
                {p.id}
              </label>
            ))}
            </div>
            {value.projects.length > 0 && <p className="text-xs text-muted-foreground">{t("picked", { count: value.projects.length })}</p>}
          </div>
        )}
      </fieldset>
      {covered.length > 0 && (
        <Alert role="status">
          <AlertDescription>
            {[...new Set(covered.map((c) => `${c.by.role_name} · ${c.by.project_id ?? t("all_projects")}`))].map((label) => (
              <p key={label}>{t("covered", { label })}</p>
            ))}
          </AlertDescription>
        </Alert>
      )}
    </div>
  );
}
