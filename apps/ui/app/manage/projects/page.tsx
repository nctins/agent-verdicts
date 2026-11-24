"use client";

import Link from "next/link";
import { PlusIcon } from "lucide-react";
import { useFormatter, useTranslations } from "next-intl";
import { useState } from "react";
import { toast } from "sonner";
import { AppShell } from "@/components/AppShell";
import { SessionGate } from "@/components/SessionGate";
import { StateBar } from "@/components/StateBar";
import { DataTable, type Column } from "@/components/admin/DataTable";
import { ConfirmByName, PageHeader, RowMenu } from "@/components/admin/kit";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { DropdownMenuItem, DropdownMenuSeparator } from "@/components/ui/dropdown-menu";
import { ApiError } from "@/lib/api/errors";
import { useAdminProjects, useUpdateProject } from "@/lib/api/queries";
import { CreateProjectDialog, ProjectEditForm } from "@/components/ProjectForms";
import type { components } from "@/lib/api/schema";
import { projectHref } from "@/lib/routes";

type AdminProject = components["schemas"]["Project"];

function ProjectTable() {
  const t = useTranslations();
  const pt = (k: string) => t(`admin.project.${k}`);
  const format = useFormatter();
  const { data, error } = useAdminProjects();
  const update = useUpdateProject();
  const [editing, setEditing] = useState<AdminProject | null>(null);
  const [archiving, setArchiving] = useState<AdminProject | null>(null);
  if (error) return <p role="alert">{t(`errors.${error instanceof ApiError ? error.code : "network"}`)}</p>;
  const setArchived = (p: AdminProject, archived: boolean) =>
    update.mutate(
      { id: p.id, archived },
      {
        onSuccess: () => toast.success(pt(archived ? "archived_done" : "unarchived")),
        onError: (e) => toast.error(t(`errors.${e instanceof ApiError ? e.code : "internal"}`)),
        onSettled: () => setArchiving(null),
      },
    );
  const columns: Column<AdminProject>[] = [
    {
      id: "name", header: pt("col_name"), sort: (p) => p.id,
      cell: (p) => (
        <span className="flex min-w-0 flex-col">
          <span className="flex items-center gap-2">
            <Link href={projectHref(p.id)} className="font-medium hover:underline" onClick={(e) => e.stopPropagation()}>{p.id}</Link>
            {p.archived && <Badge variant="outline">{pt("archived_heading")}</Badge>}
          </span>
          {p.name !== p.id && <span className="truncate text-xs text-muted-foreground">{p.name}</span>}
        </span>
      ),
    },
    { id: "contributions", header: pt("col_contributions"), sort: (p) => p.contribution_count, className: "tabular-nums", cell: (p) => p.contribution_count.toLocaleString() },
    { id: "open", header: pt("col_open"), sort: (p) => p.open_question_count, className: "tabular-nums", cell: (p) => p.open_question_count.toLocaleString() },
    { id: "states", header: t("admin_ui.projects.col_states"), className: "w-48", cell: (p) => <StateBar states={p.states} /> },
    { id: "created", header: pt("col_created"), sort: (p) => p.created_at, cell: (p) => format.dateTime(new Date(p.created_at), { dateStyle: "medium" }) },
  ];
  return (
    <>
      <DataTable
        caption={pt("list")}
        rows={data?.items}
        rowKey={(p) => p.id}
        rowHref={(p) => projectHref(p.id)}
        columns={columns}
        text={(p) => `${p.id} ${p.name}`}
        filters={[{
          id: "status", label: t("admin_ui.projects.status"),
          options: [{ value: "active", label: t("admin_ui.projects.active") }, { value: "archived", label: pt("archived_heading") }],
          match: (p, v) => p.archived === (v === "archived"),
        }]}
        empty={t("empty.no_projects_title")}
        rowMenu={(p) => (
          <RowMenu label={`${t("kit.table.actions")}: ${p.id}`}>
            <DropdownMenuItem asChild><Link href={projectHref(p.id)}>{t("admin_ui.actors.open")}</Link></DropdownMenuItem>
            <DropdownMenuItem onSelect={() => setEditing(p)}>{pt("edit")}</DropdownMenuItem>
            <DropdownMenuSeparator />
            {p.archived ? (
              <DropdownMenuItem onSelect={() => setArchived(p, false)}>{pt("unarchive")}</DropdownMenuItem>
            ) : (
              <DropdownMenuItem variant="destructive" onSelect={() => setArchiving(p)}>{pt("archive")}</DropdownMenuItem>
            )}
          </RowMenu>
        )}
      />
      <Dialog open={!!editing} onOpenChange={(o) => !o && setEditing(null)}>
        <DialogContent>
          <DialogHeader><DialogTitle>{t("admin.project.edit_title", { id: editing?.id ?? "" })}</DialogTitle></DialogHeader>
          {editing && <ProjectEditForm project={editing} onDone={() => setEditing(null)} />}
        </DialogContent>
      </Dialog>
      <ConfirmByName
        open={!!archiving}
        onOpenChange={(o) => !o && setArchiving(null)}
        title={t("admin_ui.projects.archive_title", { id: archiving?.id ?? "" })}
        body={pt("archived_hint")}
        expected={archiving?.id ?? ""}
        confirmLabel={pt("archive")}
        busy={update.isPending}
        onConfirm={() => archiving && setArchived(archiving, true)}
      />
    </>
  );
}

export default function ProjectsPage() {
  const t = useTranslations();
  const [creating, setCreating] = useState(false);
  return (
    <AppShell>
      <SessionGate require="project:Create">
        <PageHeader
          title={t("admin.project.list")}
          description={t("admin.project.list_sub")}
          actions={<Button onClick={() => setCreating(true)}><PlusIcon aria-hidden />{t("admin_ui.projects.new")}</Button>}
        />
        <ProjectTable />
        <CreateProjectDialog open={creating} onOpenChange={setCreating} />
      </SessionGate>
    </AppShell>
  );
}
