"use client";

import { useTranslations } from "next-intl";
import { AppShell } from "@/components/AppShell";
import { SessionGate } from "@/components/SessionGate";
import { MatrixEditor } from "@/components/admin/MatrixEditor";
import { ACTIONS, type Action } from "@/lib/access";
import { useDeletePolicy, usePolicies, useRoles, useSavePolicy } from "@/lib/api/queries";

function Policies() {
  const t = useTranslations();
  const list = usePolicies();
  const roles = useRoles();
  const save = useSavePolicy();
  const del = useDeletePolicy();
  // How many roles include each policy.
  const inRoles = (id: string) => roles.data?.items.filter((r) => r.policy_ids.includes(id)).length;
  return (
    <MatrixEditor
      title={t("manage.access.policies_heading")}
      description={t("manage.access.policies_sub")}
      items={list.data?.items.map((p) => ({ id: p.id, name: p.name, managed: p.managed, members: p.actions, actions: p.actions, usedBy: inRoles(p.id), usedHref: "/manage/roles/" }))}
      options={ACTIONS.map((x) => ({ value: x, label: t(`manage.access.action.${x}`), detail: t(`admin_ui.matrix.desc.${x}`) }))}
      membersLabel={t("manage.access.actions")}
      newLabel={t("manage.access.new_policy")}
      error={list.error}
      saving={save.isPending}
      onSave={({ id, name, members }) => save.mutateAsync({ id, name, actions: members as Action[] })}
      onDelete={(id) => del.mutateAsync(id)}
    />
  );
}

export default function PoliciesPage() {
  return (
    <AppShell>
      <SessionGate require="policy:Manage">
        <Policies />
      </SessionGate>
    </AppShell>
  );
}
