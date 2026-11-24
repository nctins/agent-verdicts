"use client";

import { useTranslations } from "next-intl";
import { AppShell } from "@/components/AppShell";
import { SessionGate } from "@/components/SessionGate";
import { MatrixEditor } from "@/components/admin/MatrixEditor";
import { managesAny } from "@/lib/access";
import { useAssignments, useDeleteRole, usePolicies, useRoles, useSaveRole, useSession } from "@/lib/api/queries";

function Roles() {
  const t = useTranslations();
  const list = useRoles();
  const policies = usePolicies();
  // The assignment list needs member:Manage; without it the counts are left out rather than shown as 0.
  const assignments = useAssignments({}, managesAny(useSession().data));
  const save = useSaveRole();
  const del = useDeleteRole();
  // How many actors hold each role (on any scope), linking to the actor list filtered by it.
  const holders = new Map<string, Set<string>>();
  for (const x of assignments.data?.items ?? []) holders.set(x.role_name, (holders.get(x.role_name) ?? new Set()).add(x.actor_id));
  return (
    <MatrixEditor
      title={t("manage.access.roles_heading")}
      description={t("manage.access.roles_sub")}
      items={list.data?.items.map((r) => ({
        id: r.id, name: r.name, managed: r.managed, members: r.policy_ids, actions: r.actions,
        usedBy: assignments.data ? holders.get(r.name)?.size ?? 0 : undefined, usedHref: "/manage/actors/?" + new URLSearchParams({ role: r.name }),
      }))}
      // The administrator policy belongs to the managed admin role only.
      options={(policies.data?.items ?? []).filter((p) => !p.actions.includes("*")).map((p) => ({
        value: p.id, label: p.name, detail: p.actions.map((a) => t(`manage.access.action.${a}`)).join(", "),
      }))}
      membersLabel={t("manage.access.policies")}
      newLabel={t("manage.access.new_role")}
      error={list.error ?? policies.error}
      saving={save.isPending}
      onSave={({ id, name, members }) => save.mutateAsync({ id, name, policy_ids: members })}
      onDelete={(id) => del.mutateAsync(id)}
    />
  );
}

export default function RolesPage() {
  return (
    <AppShell>
      <SessionGate require="policy:Manage">
        <Roles />
      </SessionGate>
    </AppShell>
  );
}
