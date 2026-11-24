"use client";

import {
  AlertTriangleIcon, CheckCircle2Icon, CircleHelpIcon, ClockIcon, FolderKanbanIcon, KeyRoundIcon, MessagesSquareIcon, UserXIcon, type LucideIcon,
} from "lucide-react";
import Link from "next/link";
import { useTranslations } from "next-intl";
import { useMemo, useState } from "react";
import { AppShell } from "@/components/AppShell";
import { AuditTable } from "@/components/AuditTable";
import { SessionGate } from "@/components/SessionGate";
import { STATE_FILL, StateBar } from "@/components/StateBar";
import { PageHeader, Section, SkeletonRows, StatCard } from "@/components/admin/kit";
import { can, managesAny } from "@/lib/access";
import { accessByActor, type RoleScope } from "@/lib/access-forms";
import { expiryState } from "@/lib/admin-forms";
import { ApiError } from "@/lib/api/errors";
import { useActors, useAdminOverview, useAssignments, useAudit, useProjects, useSession } from "@/lib/api/queries";
import { projectHref } from "@/lib/routes";
import type { TrustState } from "@/lib/trust";

const TRUST_STATES: TrustState[] = ["Favorable", "Unfavorable", "Conflict", "InsufficientEvidence"];

type Attention = { key: string; n: number; href: string; icon: LucideIcon; tone: string };

/** The few numbers that tell an admin where to look first (spec 2026-11-22-admin-ui-redesign U7). */
function NeedsAttention({ conflicts, staleQuestions }: { conflicts: number; staleQuestions: number }) {
  const t = useTranslations("admin_ui.dashboard");
  const session = useSession().data;
  const full = can(session, "actor:Manage");
  const manages = managesAny(session);
  const actors = useActors(undefined, full);
  const assignments = useAssignments({}, full && manages);
  const [now] = useState(() => Date.now());
  const byActor = useMemo(() => accessByActor(assignments.data?.items ?? []), [assignments.data]);
  const live = (actors.data?.items ?? []).filter((a) => !a.disabled);
  const items: Attention[] = [
    ...(full
      ? [{ key: "tokens", n: live.filter((a) => expiryState(a.next_token_expiry ?? null, now) === "soon").length, href: "/manage/actors/?token=attention", icon: KeyRoundIcon, tone: "text-amber-600" }]
      : []),
    ...(full && manages && assignments.data
      ? [{ key: "no_access", n: live.filter((a) => (byActor.get(a.id) ?? ([] as RoleScope[])).length === 0).length, href: "/manage/actors/?project=-", icon: UserXIcon, tone: "text-amber-600" }]
      : []),
    { key: "conflicts", n: conflicts, href: "#health", icon: AlertTriangleIcon, tone: "text-conflict" },
    { key: "stale", n: staleQuestions, href: "#health", icon: ClockIcon, tone: "text-muted-foreground" },
  ];
  const open = items.filter((x) => x.n > 0);
  return (
    <Section id="attention" title={t("attention")} description={t("attention_hint")}>
      {open.length === 0 ? (
        <p className="flex items-center gap-2 text-sm text-emerald-700 dark:text-emerald-400"><CheckCircle2Icon className="size-4" aria-hidden />{t("all_clear")}</p>
      ) : (
        <ul className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,24rem),1fr))] gap-3">
          {open.map((x) => (
            <li key={x.key}>
              <Link href={x.href} className="flex items-start gap-3 rounded-lg border p-3 hover:bg-accent/50">
                <x.icon className={`mt-0.5 size-5 shrink-0 ${x.tone}`} aria-hidden />
                <span className="flex flex-col">
                  <span className="font-medium">{t(`att.${x.key}`, { count: x.n })}</span>
                  <span className="text-xs text-muted-foreground">{t(`att.${x.key}_hint`)}</span>
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}

/** One row per project: size, open questions, conflicts and the spread of claim states. */
function ProjectHealth() {
  const t = useTranslations();
  const { data } = useProjects();
  const rows = (data?.items ?? []).filter((p) => !p.archived).sort((a, b) => b.states.Conflict - a.states.Conflict || b.contribution_count - a.contribution_count);
  return (
    <Section id="health" title={t("admin_ui.dashboard.health")} description={t("admin_ui.dashboard.health_hint")}>
      {!data ? <SkeletonRows rows={3} /> : (
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full text-left text-sm">
            <thead className="bg-muted/50 text-xs text-muted-foreground">
              <tr className="border-b">
                <th scope="col" className="p-2 font-medium">{t("admin.project.col_name")}</th>
                <th scope="col" className="hidden p-2 text-right font-medium sm:table-cell">{t("admin.project.col_contributions")}</th>
                <th scope="col" className="hidden p-2 text-right font-medium sm:table-cell">{t("admin.project.col_open")}</th>
                <th scope="col" className="p-2 text-right font-medium">{t("state.Conflict")}</th>
                <th scope="col" className="w-24 p-2 font-medium sm:w-48">{t("admin_ui.projects.col_states")}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((p) => (
                <tr key={p.id} className="border-b last:border-0">
                  <th scope="row" className="p-2 text-left font-medium">
                    <Link href={projectHref(p.id)} className="hover:underline">{p.name}</Link>
                    {p.name !== p.id && <span className="block text-xs font-normal text-muted-foreground">{p.id}</span>}
                  </th>
                  <td className="hidden p-2 text-right tabular-nums sm:table-cell">{p.contribution_count.toLocaleString()}</td>
                  <td className="hidden p-2 text-right tabular-nums sm:table-cell">{p.open_question_count.toLocaleString()}</td>
                  <td className="p-2 text-right tabular-nums">
                    {p.states.Conflict > 0
                      ? <Link href={projectHref(p.id) + "?tab=review"} className="font-medium text-conflict hover:underline">{p.states.Conflict.toLocaleString()}</Link>
                      : 0}
                  </td>
                  <td className="p-2"><StateBar states={p.states} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Section>
  );
}

/** The latest admin events, for whoever may read the audit log. */
function RecentActivity() {
  const t = useTranslations();
  const q = useAudit({ limit: 8 });
  const actors = useActors();
  const items = q.data?.pages[0]?.items ?? [];
  return (
    <Section id="recent-admin" title={t("admin_ui.dashboard.recent_admin")}>
      {!q.data ? <SkeletonRows rows={3} /> : items.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("manage.audit.empty")}</p>
      ) : (
        <AuditTable items={items} names={new Map(actors.data?.items.map((x) => [x.id, x.name]))} />
      )}
      <Link href="/manage/audit/" className="self-start text-sm hover:underline">{t("admin_ui.dashboard.see_audit")}</Link>
    </Section>
  );
}

function Overview() {
  const t = useTranslations();
  const session = useSession().data;
  const { data, error } = useAdminOverview();
  if (error) return <p role="alert">{t(`errors.${error instanceof ApiError ? error.code : "network"}`)}</p>;
  if (!data) return <SkeletonRows />;
  const kpis = [
    ["manage.dashboard.projects", data.project_count, "/manage/projects/", FolderKanbanIcon],
    ["manage.dashboard.contributions", data.contribution_count, null, MessagesSquareIcon],
    ["manage.dashboard.open_questions", data.open_question_count, null, CircleHelpIcon],
  ] as const;
  const totalClaims = TRUST_STATES.reduce((n, s) => n + data.states[s], 0);
  return (
    <div className="flex flex-col gap-6">
      <div className="grid grid-cols-3 items-start gap-2 sm:gap-3 lg:grid-cols-4">
        {kpis.map(([k, n, href, Icon]) => (
          <StatCard key={k} label={t(k)} value={n.toLocaleString()} icon={Icon} href={href && can(session, "project:Create") ? href : undefined} />
        ))}
        <div className="col-span-3 flex min-w-0 flex-col gap-2 rounded-xl border bg-card p-3 sm:p-4 lg:col-span-1">
          <p className="text-sm text-muted-foreground">{t("admin.overview.states")}</p>
          <StateBar states={data.states} />
          {/* A plain legend (dot, label, share) fits a quarter-width card; badges did not. */}
          <ul className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs lg:grid-cols-1">
            {TRUST_STATES.map((s) => (
              <li key={s} className="flex min-w-0 items-center gap-2">
                <span className={`size-2 shrink-0 rounded-full ${STATE_FILL[s]}`} aria-hidden />
                <span className="truncate">{t(`state.${s}`)}</span>
                <span className="ml-auto tabular-nums text-muted-foreground">{totalClaims ? Math.round((100 * data.states[s]) / totalClaims) : 0}%</span>
              </li>
            ))}
          </ul>
        </div>
      </div>
      <NeedsAttention conflicts={data.attention.conflicts_7d} staleQuestions={data.attention.stale_questions} />
      <ProjectHealth />
      {(can(session, "actor:Manage") || managesAny(session)) && <RecentActivity />}
    </div>
  );
}

export default function ManagePage() {
  const t = useTranslations("manage.dashboard");
  const session = useSession().data;
  return (
    <AppShell>
      <SessionGate require="manage">
        <PageHeader title={t("heading")} description={t("sub")} />
        {(can(session, "actor:Manage") || managesAny(session)) && <Overview />}
      </SessionGate>
    </AppShell>
  );
}
