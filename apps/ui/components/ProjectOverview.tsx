"use client";

import {
  ArrowRightIcon, CircleDashedIcon, CircleHelpIcon, EyeIcon, EyeOffIcon, MessagesSquareIcon, PlugIcon, PlusIcon, TriangleAlertIcon, type LucideIcon,
} from "lucide-react";
import Link from "next/link";
import { useTranslations } from "next-intl";
import type { ReactNode } from "react";
import { RelTime, SkeletonRows, StatCard } from "@/components/admin/kit";
import { StateBadge } from "@/components/StateBadge";
import { STATE_FILL, StateBar } from "@/components/StateBar";
import { Button } from "@/components/ui/button";
import { can } from "@/lib/access";
import { useContributions, useProjectInfo, useReads30, useSession } from "@/lib/api/queries";
import type { components } from "@/lib/api/schema";
import { saveListNav } from "@/lib/list-nav";
import { contributionHref } from "@/lib/routes";
import type { TrustState } from "@/lib/trust";

type Item = components["schemas"]["ContributionItem"];
type Changes = Record<string, string | null>;

function Card({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section aria-label={title} className="flex flex-col gap-3 rounded-xl border bg-card p-4">
      <div className="flex items-center justify-between gap-2">
        <h2 className="font-semibold">{title}</h2>
        {action}
      </div>
      {children}
    </section>
  );
}

/** A short list of rows: title, state, when it last moved. */
function MiniList({ project, items, empty }: { project: string; items: Item[] | undefined; empty: string }) {
  if (!items) return <SkeletonRows rows={3} />;
  if (items.length === 0) return <p className="text-sm text-muted-foreground">{empty}</p>;
  const remember = () => saveListNav({ url: window.location.pathname + window.location.search, ids: items.map((x) => x.id) });
  return (
    <ul className="-mx-2 flex flex-col">
      {items.map((x) => (
        <li key={x.id} className="relative flex items-center gap-3 rounded-md px-2 py-1.5 hover:bg-accent/50 has-[a:focus-visible]:bg-accent/50">
          <div className="flex min-w-0 flex-1 flex-col">
            <Link href={contributionHref(project, x.id)} data-row-link onClick={remember} className="truncate text-sm font-medium outline-none after:absolute after:inset-0">
              {x.kind === "question" && <CircleHelpIcon className="mr-1 inline size-3.5 align-[-2px] text-muted-foreground" aria-hidden />}
              {x.title}
            </Link>
            <span className="text-xs text-muted-foreground">{x.actor_name} · <RelTime at={x.updated_at} /></span>
          </div>
          <StateBadge state={x.state} questionStatus={x.question_status} />
        </li>
      ))}
    </ul>
  );
}

const SeeAll = ({ onClick }: { onClick: () => void }) => {
  const t = useTranslations("project.overview");
  return <Button variant="ghost" size="sm" onClick={onClick}>{t("see_all")}<ArrowRightIcon aria-hidden /></Button>;
};

/** Agents' reads over the last 30 days (member:Manage). */
function ReadsCard({ id, onPick }: { id: string; onPick: (c: Changes) => void }) {
  const t = useTranslations("project.overview");
  const r = useReads30(id, true).data;
  const total = r ? [...r.daily, ...r.monthly].reduce((n, d) => n + d.observe + d.reference + d.search_hit, 0) : null;
  // Two stat cards under a heading: no card inside a card.
  return (
    <section aria-label={t("reads_title")} className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <h2 className="font-semibold">{t("reads_title")}</h2>
        <Button variant="ghost" size="sm" onClick={() => onPick({ tab: "settings" })}>{t("reads_settings")}</Button>
      </div>
      {r ? (
        <div className="grid grid-cols-2 gap-2">
          <StatCard label={t("reads_total")} value={total} icon={EyeIcon} />
          <StatCard label={t("never_read")} value={r.never_read} icon={EyeOffIcon} />
        </div>
      ) : <SkeletonRows rows={1} />}
    </section>
  );
}

/** The project's Overview tab (spec 2026-11-22-project-ui-redesign P7). */
export function ProjectOverview({ id, onPick, onCompose, onConnect, mayWrite }: {
  id: string;
  onPick: (c: Changes) => void;
  onCompose: (kind: "claim" | "question") => void;
  onConnect: () => void;
  mayWrite: boolean;
}) {
  const t = useTranslations();
  const p = useProjectInfo(id).meta;
  const session = useSession().data;
  const conflicts = useContributions(id, { kind: "claim", state: ["Conflict"], sort: "active", limit: 5 }).data?.pages[0]?.items;
  // Open = no answer yet; the oldest of the first page waits longest.
  const open = useContributions(id, { kind: "question", question_status: ["open"] }).data?.pages[0]?.items;
  const recent = useContributions(id, { sort: "active", limit: 10 }).data?.pages[0]?.items;
  if (!p) return <SkeletonRows />;
  if (p.last_activity_at === null)
    return (
      <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed p-10 text-center">
        <h2 className="text-lg font-semibold">{t("project.overview.empty_title")}</h2>
        <p className="max-w-md text-sm text-muted-foreground">{t("project.overview.empty_body")}</p>
        <div className="flex flex-wrap justify-center gap-2">
          {mayWrite && <Button onClick={() => onCompose("claim")}><PlusIcon aria-hidden />{t("project.contribute")}</Button>}
          <Button variant="outline" onClick={onConnect}><PlugIcon aria-hidden />{t("project.connect.button")}</Button>
        </div>
      </div>
    );

  const tiles: { label: string; n: number; tone?: string; icon: LucideIcon; go: Changes }[] = [
    { label: t("home.col_claims"), n: p.contribution_count, icon: MessagesSquareIcon, go: { tab: "claims" } },
    { label: t("home.col_open"), n: p.open_question_count, icon: CircleHelpIcon, go: { tab: "questions", status: "open" } },
    { label: t("state.Conflict"), n: p.states.Conflict, tone: p.states.Conflict ? "text-conflict" : undefined, icon: TriangleAlertIcon, go: { tab: "review" } },
    { label: t("state.InsufficientEvidence"), n: p.states.InsufficientEvidence, icon: CircleDashedIcon, go: { tab: "claims", state: "InsufficientEvidence" } },
  ];
  const oldestOpen = open ? [...open].reverse().slice(0, 5) : undefined;
  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 items-start gap-2 lg:grid-cols-4">
        {tiles.map((x) => <StatCard key={x.label} label={x.label} value={x.n} tone={x.tone} icon={x.icon} onClick={() => onPick(x.go)} />)}
      </div>
      <div className="flex flex-col gap-2 rounded-xl border bg-card p-4">
        <StateBar states={p.states} />
        <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
          {(Object.keys(STATE_FILL) as TrustState[]).map((s) => (
            <li key={s}>
              <button type="button" className="flex items-center gap-1.5 hover:text-foreground" onClick={() => onPick({ tab: "claims", state: s })}>
                <span aria-hidden className={`size-2 rounded-full ${STATE_FILL[s]}`} />
                {t(`state.${s}`)} <span className="tabular-nums text-foreground">{p.states[s]}</span>
              </button>
            </li>
          ))}
        </ul>
      </div>
      <div className="grid items-start gap-4 lg:grid-cols-2">
        <Card title={t("project.overview.conflicts")} action={<SeeAll onClick={() => onPick({ tab: "review" })} />}>
          <MiniList project={id} items={conflicts} empty={t("project.overview.no_conflicts")} />
        </Card>
        <Card title={t("project.overview.waiting")} action={<SeeAll onClick={() => onPick({ tab: "questions", status: "open" })} />}>
          <MiniList project={id} items={oldestOpen} empty={t("project.overview.no_waiting")} />
        </Card>
        <Card title={t("project.overview.recent")} action={<SeeAll onClick={() => onPick({ tab: "claims", sort: "active" })} />}>
          <MiniList project={id} items={recent} empty={t("empty.claims")} />
        </Card>
        {can(session, "member:Manage", id) && <ReadsCard id={id} onPick={onPick} />}
      </div>
    </div>
  );
}
