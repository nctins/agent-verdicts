"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { ActorDialog } from "@/components/ActorDialog";
import { AppShell } from "@/components/AppShell";
import { Avatar } from "@/components/Avatar";
import { SessionGate } from "@/components/SessionGate";
import { AssignSheet } from "@/components/admin/AssignSheet";
import { DataTable, type Column, type Filter } from "@/components/admin/DataTable";
import { PageHeader, RelTime, RowMenu, useActorType } from "@/components/admin/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { can, managesAny } from "@/lib/access";
import { accessByActor, type RoleScope } from "@/lib/access-forms";
import { expiryState } from "@/lib/admin-forms";
import { ApiError } from "@/lib/api/errors";
import { useActors, useAssignments, useBulkSetDisabled, useProjects, useRoles, useSession } from "@/lib/api/queries";
import type { components } from "@/lib/api/schema";
import { useDebounced } from "@/lib/use-debounced-search";

type Actor = components["schemas"]["AdminActor"];

/** "contributor · p1, p2" or "admin · every project". */
export function scopeText(r: RoleScope, all: string) {
  return `${r.role} · ${r.all ? all : r.projects.join(", ")}`;
}

const actorHref = (a: { id: string; name: string }) => "/manage/actor/?" + new URLSearchParams({ id: a.id, name: a.name });

/** Token expiry as a badge: only when it needs attention (expired, or within 14 days). */
function TokenBadge({ at, now }: { at: string | null | undefined; now: number }) {
  const t = useTranslations("manage.credential");
  const s = expiryState(at ?? null, now);
  if (s === "expired") return <Badge variant="destructive">{t("expired")}</Badge>;
  if (s === "soon") return <Badge variant="outline" className="border-amber-600 text-amber-700 dark:text-amber-400">{t("expires_soon")}</Badge>;
  return null;
}

function ActorTable() {
  const t = useTranslations();
  const p = (k: string) => t(`manage.people.${k}`);
  const a = (k: string) => t(`admin_ui.actors.${k}`);
  const typeLabel = useActorType();
  const session = useSession().data;
  const showAccess = managesAny(session); // the assignment list needs member:Manage
  const manager = !can(session, "actor:Manage");
  // A project manager sees their projects' members, and finds anyone else by name on the server (2+ characters).
  const [q, setQ] = useState(useSearchParams().get("q") ?? "");
  const typed = useDebounced(q.trim());
  const search = manager && typed.length >= 2 ? typed : undefined;
  const actors = useActors(search);
  const assignments = useAssignments({}, showAccess);
  const projects = useProjects();
  const roles = useRoles();
  const bulkSet = useBulkSetDisabled();
  const [assignTo, setAssignTo] = useState<Actor[] | null>(null);
  const [now] = useState(() => Date.now());
  const byActor = useMemo(() => accessByActor(showAccess ? assignments.data?.items ?? [] : []), [showAccess, assignments.data]);
  const access = (x: Actor) => byActor.get(x.id) ?? [];

  const setDisabled = (rows: Actor[], disabled: boolean, clear?: () => void) =>
    bulkSet.mutate(
      { ids: rows.map((r) => r.id), disabled },
      {
        onSuccess: (failed) => {
          const ok = rows.length - failed.length;
          if (ok > 0) toast.success(t(`admin_ui.actors.${disabled ? "disabled_n" : "enabled_n"}`, { count: ok }));
          for (const f of failed) toast.error(`${rows.find((r) => r.id === f.id)?.name}: ${t(f.code === "conflict" ? "manage.actor.error.last_admin" : `errors.${f.code}`)}`);
          clear?.();
        },
      },
    );

  const columns: Column<Actor>[] = [
    {
      id: "name", header: t("manage.actor.list.col.name"), sort: (x) => x.name.toLowerCase(),
      cell: (x) => (
        <span className="flex min-w-0 items-center gap-3">
          <Avatar name={x.name} type={x.type} seed={x.id} className="size-8 shrink-0" />
          <span className="flex min-w-0 flex-col">
            <Link href={actorHref(x)} className="truncate font-medium hover:underline" onClick={(e) => e.stopPropagation()}>
              {x.name || t("manage.actor.list.unnamed")}
            </Link>
            {x.username && <span className="truncate text-xs text-muted-foreground">@{x.username}</span>}
          </span>
        </span>
      ),
    },
    { id: "type", header: t("manage.actor.list.col.type"), sort: (x) => x.type, cell: (x) => <Badge variant="secondary">{typeLabel(x.type)}</Badge> },
    ...(showAccess
      ? [{
          id: "access", header: p("col_access"), sort: (x: Actor) => access(x).length,
          cell: (x: Actor) => access(x).length === 0
            ? <span className="text-amber-700 dark:text-amber-400"><span aria-hidden>⚠ </span>{p("no_access")}</span>
            : <ul className="flex flex-col gap-0.5">{access(x).map((r) => <li key={r.role}>{scopeText(r, p("all_projects"))}</li>)}</ul>,
        }]
      : []),
    ...(!manager
      ? [
          { id: "active", header: a("col_active"), sort: (x: Actor) => x.last_active_at ?? null, cell: (x: Actor) => <RelTime at={x.last_active_at} empty={a("never")} /> },
          { id: "token", header: a("col_token"), card: false as const, cell: (x: Actor) => <TokenBadge at={x.next_token_expiry} now={now} /> },
        ]
      : []),
    {
      id: "status", header: p("col_status"), sort: (x) => (x.disabled ? 1 : 0),
      cell: (x) => (x.disabled ? <Badge variant="outline">{t("manage.actor.status.disabled")}</Badge> : <Badge variant="outline" className="border-favorable/40 bg-favorable/10 text-favorable">{p("active")}</Badge>),
    },
  ];

  const filters: Filter<Actor>[] = [
    { id: "type", label: t("manage.actor.list.col.type"), options: ["Human", "Agent"].map((v) => ({ value: v, label: typeLabel(v) })), match: (x, v) => x.type === v },
    { id: "status", label: p("col_status"), options: [{ value: "active", label: p("active") }, { value: "disabled", label: t("manage.actor.status.disabled") }], match: (x, v) => x.disabled === (v === "disabled") },
    ...(showAccess
      ? [
          {
            id: "project", label: t("manage.access.scope"),
            options: [{ value: "-", label: p("no_access") }, ...(projects.data?.items ?? []).map((x) => ({ value: x.id, label: x.id }))],
            match: (x: Actor, v: string) => (v === "-" ? access(x).length === 0 : access(x).some((r) => r.all || r.projects.includes(v))),
          },
          {
            id: "role", label: t("manage.access.role"),
            options: (roles.data?.items ?? []).map((r) => ({ value: r.name, label: r.name })),
            match: (x: Actor, v: string) => access(x).some((r) => r.role === v),
          },
        ]
      : []),
    ...(!manager
      ? [{ id: "token", label: a("col_token"), options: [{ value: "attention", label: a("token_attention") }], match: (x: Actor) => expiryState(x.next_token_expiry ?? null, now) !== "ok" && !!x.next_token_expiry }]
      : []),
  ];

  if (actors.error) return <p role="alert">{t(`errors.${actors.error instanceof ApiError ? actors.error.code : "network"}`)}</p>;
  return (
    <>
      <DataTable
        caption={p("heading")}
        rows={actors.data?.items}
        rowKey={(x) => x.id}
        rowHref={actorHref}
        columns={columns}
        filters={filters}
        text={(x) => `${x.name} ${x.username ?? ""}`}
        onSearch={setQ}
        searchLabel={p(manager ? "search" : "filter")}
        toolbar={manager ? <ManagerSearch /> : undefined}
        empty={t("manage.actor.list.empty")}
        selectable={!manager || showAccess}
        bulk={(rows, clear) => (
          <>
            {showAccess && <Button size="sm" variant="outline" onClick={() => setAssignTo(rows)}>{p("add_role")}</Button>}
            {!manager && (
              <>
                <Button size="sm" variant="outline" disabled={bulkSet.isPending} onClick={() => setDisabled(rows.filter((r) => !r.disabled), true, clear)}>{a("disable")}</Button>
                <Button size="sm" variant="outline" disabled={bulkSet.isPending} onClick={() => setDisabled(rows.filter((r) => r.disabled), false, clear)}>{a("enable")}</Button>
              </>
            )}
          </>
        )}
        rowMenu={(x) => (
          <RowMenu label={`${t("kit.table.actions")}: ${x.name}`}>
            <DropdownMenuItem asChild><Link href={actorHref(x)}>{a("open")}</Link></DropdownMenuItem>
            {showAccess && <DropdownMenuItem onSelect={() => setAssignTo([x])}>{p("add_role")}</DropdownMenuItem>}
            {!manager && session?.actor?.id !== x.id && (
              <DropdownMenuItem onSelect={() => setDisabled([x], !x.disabled)} variant={x.disabled ? "default" : "destructive"}>
                {a(x.disabled ? "enable" : "disable")}
              </DropdownMenuItem>
            )}
          </RowMenu>
        )}
      />
      <AssignSheet open={!!assignTo} onOpenChange={(o) => !o && setAssignTo(null)} actors={assignTo ?? []} />
    </>
  );
}

/** A project manager searches the server by name (2+ characters) to find people outside their projects. */
function ManagerSearch() {
  const t = useTranslations("manage.people");
  return <p className="text-xs text-muted-foreground">{t("search_hint")}</p>;
}

export default function ActorsPage() {
  const t = useTranslations("manage.people");
  const [open, setOpen] = useState(false);
  return (
    <AppShell>
      <SessionGate require="people">
        <PageHeader title={t("heading")} description={t("sub")} actions={<CreateButton onClick={() => setOpen(true)} label={t("add")} />} />
        <ActorTable />
        <ActorDialog open={open} onOpenChange={setOpen} />
      </SessionGate>
    </AppShell>
  );
}

/** Creating actors takes actor:Manage; project managers only change roles. */
function CreateButton({ onClick, label }: { onClick: () => void; label: string }) {
  return can(useSession().data, "actor:Manage") ? <Button onClick={onClick}>{label}</Button> : null;
}
