"use client";

import { DownloadIcon } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { Suspense, useState } from "react";
import { selectCls } from "@/components/AccessPicker";
import { AppShell } from "@/components/AppShell";
import { AuditTable } from "@/components/AuditTable";
import { EmptyState } from "@/components/EmptyState";
import { SessionGate } from "@/components/SessionGate";
import { PageHeader, SkeletonRows } from "@/components/admin/kit";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api, unwrap } from "@/lib/api/client";
import { ApiError } from "@/lib/api/errors";
import type { AuditFilters } from "@/lib/api/keys";
import { useActors, useAudit, useProjects } from "@/lib/api/queries";
import { download, toCsv } from "@/lib/csv";

/** Every action the API records (internal/adapter/httpapi), for the action filter. */
const ACTIONS = [
  "actor.create", "actor.rename", "actor.disable", "actor.enable", "actor.password_reset", "credential.issue", "credential.revoke",
  "assignment.create", "assignment.delete", "role.create", "role.update", "role.delete", "policy.create", "policy.update", "policy.delete",
  "project.create", "project.update", "project.reads_retention", "settings.search.update", "settings.search.reindex", "setup.complete",
  "content.hide", "content.unhide",
] as const;
const FILTERS = ["actor", "project", "action", "from", "to"] as const;
/** CSV export stops here: an audit log bigger than this belongs in the database, not a spreadsheet. */
const EXPORT_MAX = 10_000;

function Audit() {
  const t = useTranslations("manage.audit");
  const ta = useTranslations("admin_ui.audit");
  const te = useTranslations();
  const router = useRouter();
  const path = usePathname();
  const sp = useSearchParams();
  const filters = Object.fromEntries(FILTERS.map((k) => [k, sp.get(k) ?? ""])) as Record<(typeof FILTERS)[number], string>;
  const query: AuditFilters = Object.fromEntries(Object.entries(filters).filter(([, v]) => v));
  const actors = useActors();
  const projects = useProjects();
  const q = useAudit(query);
  const [exporting, setExporting] = useState(false);
  const set = (k: string, v: string) => {
    const next = new URLSearchParams(sp.toString());
    if (v) next.set(k, v);
    else next.delete(k);
    next.delete("event");
    router.replace(path + (next.size ? "?" + next : ""), { scroll: false });
  };
  const names = new Map(actors.data?.items.map((a) => [a.id, a.name]));
  const items = q.data?.pages.flatMap((p) => p.items) ?? [];
  const label = (action: string) => (t.has(`action.${action}`) ? t(`action.${action}`) : action);
  const any = FILTERS.some((k) => filters[k]);

  // Fetches every page that matches the filters (newest first) and saves them as one CSV file.
  const exportCsv = async () => {
    setExporting(true);
    try {
      const rows: (string | null)[][] = [["at", "actor_id", "actor_name", "action", "target_type", "target_id", "project_id", "detail"]];
      let cursor: string | undefined;
      do {
        const page = await unwrap(api.GET("/admin/audit", { params: { query: { ...query, limit: 100, cursor } } }));
        for (const e of page.items) rows.push([e.at, e.actor_id, e.actor_name, e.action, e.target_type, e.target_id, e.project_id, JSON.stringify(e.detail)]);
        cursor = page.next_cursor ?? undefined;
      } while (cursor && rows.length <= EXPORT_MAX);
      download(`audit-${new Date().toISOString().slice(0, 10)}.csv`, toCsv(rows));
    } finally {
      setExporting(false);
    }
  };
  // A shared link narrows to the event's own UTC day, so the event is on the first page.
  const linkTo = (e: { id: string; at: string }) => {
    const d = e.at.slice(0, 10);
    return path + "?" + new URLSearchParams({ event: e.id, from: d, to: d });
  };

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title={t("heading")}
        description={t("sub")}
        actions={<Button variant="outline" disabled={exporting || items.length === 0} onClick={exportCsv}><DownloadIcon aria-hidden />{ta(exporting ? "exporting" : "export")}</Button>}
      />
      <div className="flex flex-wrap items-end gap-3 rounded-xl border bg-card p-3">
        <div className="flex flex-col gap-1">
          <Label htmlFor="audit-actor">{t("filter_actor")}</Label>
          <select id="audit-actor" className={selectCls} value={filters.actor} onChange={(e) => set("actor", e.target.value)}>
            <option value="">{t("any")}</option>
            {actors.data?.items.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
          </select>
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="audit-project">{t("filter_project")}</Label>
          <select id="audit-project" className={selectCls} value={filters.project} onChange={(e) => set("project", e.target.value)}>
            <option value="">{t("any")}</option>
            {projects.data?.items.map((p) => <option key={p.id} value={p.id}>{p.id}</option>)}
          </select>
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="audit-action">{ta("filter_action")}</Label>
          <select id="audit-action" className={selectCls} value={filters.action} onChange={(e) => set("action", e.target.value)}>
            <option value="">{t("any")}</option>
            {ACTIONS.map((a) => <option key={a} value={a}>{label(a)}</option>)}
          </select>
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="audit-from">{ta("from")}</Label>
          <Input id="audit-from" type="date" className="h-9 w-40" value={filters.from} max={filters.to || undefined} onChange={(e) => set("from", e.target.value)} />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="audit-to">{ta("to")}</Label>
          <Input id="audit-to" type="date" className="h-9 w-40" value={filters.to} min={filters.from || undefined} onChange={(e) => set("to", e.target.value)} />
        </div>
        {any && <Button variant="ghost" onClick={() => router.replace(path, { scroll: false })}>{te("kit.table.clear")}</Button>}
      </div>
      <p className="text-xs text-muted-foreground">{ta("utc_note")}</p>
      {q.error ? (
        <p role="alert">{te(`errors.${q.error instanceof ApiError ? q.error.code : "network"}`)}</p>
      ) : !q.data ? (
        <SkeletonRows />
      ) : items.length === 0 ? (
        <EmptyState title={t("empty")} />
      ) : (
        <AuditTable items={items} names={names} byDay initialOpen={sp.get("event")} linkTo={linkTo} />
      )}
      {q.hasNextPage && (
        <Button variant="outline" className="self-center" disabled={q.isFetchingNextPage} onClick={() => q.fetchNextPage()}>
          {t("more")}
        </Button>
      )}
    </div>
  );
}

export default function AuditPage() {
  return (
    <AppShell>
      <SessionGate require="people">
        <Suspense>
          <Audit />
        </Suspense>
      </SessionGate>
    </AppShell>
  );
}
