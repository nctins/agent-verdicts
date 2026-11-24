"use client";

import { ArrowUpDownIcon, FolderIcon, LayoutGridIcon, ListIcon, PlusIcon, SearchIcon } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { Suspense, useState } from "react";
import { AppShell } from "@/components/AppShell";
import { EmptyState } from "@/components/EmptyState";
import { CreateProjectDialog } from "@/components/ProjectForms";
import { SessionGate } from "@/components/SessionGate";
import { StateBar } from "@/components/StateBar";
import { PageHeader, RelTime, SkeletonRows } from "@/components/admin/kit";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { ApiError } from "@/lib/api/errors";
import { can } from "@/lib/access";
import { useProjects, useSession } from "@/lib/api/queries";
import type { components } from "@/lib/api/schema";
import { PROJECT_SORTS, sortProjects, type ProjectSort } from "@/lib/project-list";
import { projectHref } from "@/lib/routes";
import { cn } from "@/lib/utils";

type Project = components["schemas"]["Project"];
type View = "cards" | "table";
const VIEW_KEY = "av.projects-view";

// The reader's choice of layout; storage may be missing or throw, and then it is cards.
const loadView = (): View => {
  try {
    return localStorage.getItem(VIEW_KEY) === "table" ? "table" : "cards";
  } catch {
    return "cards";
  }
};
const saveView = (v: View) => {
  try {
    localStorage.setItem(VIEW_KEY, v);
  } catch {
    /* kept for this visit only */
  }
};

function Numbers({ p }: { p: Project }) {
  const t = useTranslations("home");
  return (
    <p className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
      <span><span className="font-medium text-foreground tabular-nums">{p.contribution_count}</span> {t("col_claims").toLowerCase()}</span>
      <span><span className="font-medium text-foreground tabular-nums">{p.open_question_count}</span> {t("col_open").toLowerCase()}</span>
      <span><span className={cn("font-medium tabular-nums", p.states.Conflict ? "text-conflict" : "text-foreground")}>{p.states.Conflict}</span> {t("col_conflict").toLowerCase()}</span>
    </p>
  );
}

function Activity({ p }: { p: Project }) {
  const t = useTranslations("home");
  return <span className="text-xs text-muted-foreground">{t("activity")} <RelTime at={p.last_activity_at} empty={t("no_activity")} /></span>;
}

function Cards({ items }: { items: Project[] }) {
  return (
    <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
      {items.map((p) => (
        <li key={p.id} className="relative flex flex-col gap-3 rounded-xl border bg-card p-4 hover:bg-accent/40 has-[a:focus-visible]:ring-[3px] has-[a:focus-visible]:ring-ring/50">
          <div className="flex min-w-0 flex-col gap-0.5">
            <Link href={projectHref(p.id)} data-row-link className="flex items-center gap-2 font-semibold break-all outline-none after:absolute after:inset-0">
              <FolderIcon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
              {p.name}
            </Link>
            {p.name !== p.id && <span className="text-xs break-all text-muted-foreground">{p.id}</span>}
          </div>
          {p.description && <p className="line-clamp-2 text-sm text-muted-foreground">{p.description}</p>}
          <div className="mt-auto flex flex-col gap-2">
            <StateBar states={p.states} />
            <Numbers p={p} />
            <Activity p={p} />
          </div>
        </li>
      ))}
    </ul>
  );
}

function Table({ items }: { items: Project[] }) {
  const t = useTranslations("home");
  const cols = "sm:grid-cols-[minmax(0,1fr)_6rem_6rem_6rem_8rem]";
  return (
    <div className="overflow-hidden rounded-lg border">
      <div aria-hidden className={`hidden gap-4 border-b bg-muted/50 px-4 py-2 text-xs font-medium text-muted-foreground sm:grid ${cols}`}>
        <span>{t("col_project")}</span>
        <span className="text-right">{t("col_claims")}</span>
        <span className="text-right">{t("col_open")}</span>
        <span className="text-right">{t("col_conflict")}</span>
        <span className="text-right">{t("col_activity")}</span>
      </div>
      <ul className="divide-y">
        {items.map((p) => (
          <li key={p.id} className={`relative grid gap-2 px-4 py-3 hover:bg-accent/50 has-[a:focus-visible]:bg-accent/50 sm:items-center sm:gap-4 ${cols}`}>
            <div className="flex min-w-0 flex-col gap-1.5">
              <div className="flex items-baseline gap-2">
                <Link href={projectHref(p.id)} data-row-link className="flex items-center gap-2 font-medium break-all outline-none after:absolute after:inset-0">
                  <FolderIcon className="size-4 shrink-0 self-center text-muted-foreground" aria-hidden />
                  {p.name}
                </Link>
                {p.name !== p.id && <span className="text-xs break-all text-muted-foreground">{p.id}</span>}
              </div>
              <StateBar states={p.states} />
            </div>
            <div className="flex flex-wrap gap-x-4 sm:contents">
              <span className="sm:hidden"><Numbers p={p} /></span>
              <span className="hidden text-right text-sm tabular-nums sm:block">{p.contribution_count}</span>
              <span className="hidden text-right text-sm tabular-nums sm:block">{p.open_question_count}</span>
              <span className={cn("hidden text-right text-sm tabular-nums sm:block", p.states.Conflict > 0 && "text-conflict")}>{p.states.Conflict}</span>
              <span className="text-xs text-muted-foreground sm:text-right"><RelTime at={p.last_activity_at} empty={t("no_activity")} /></span>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

function ProjectList() {
  const t = useTranslations();
  const router = useRouter();
  const path = usePathname();
  const sp = useSearchParams();
  const { data, error } = useProjects();
  const session = useSession().data;
  const mayCreate = can(session, "project:Create");
  const [creating, setCreating] = useState(false);
  const [view, setView] = useState<View>(loadView);
  const q = sp.get("q") ?? "";
  const sort: ProjectSort = PROJECT_SORTS.includes(sp.get("sort") as ProjectSort) ? (sp.get("sort") as ProjectSort) : "activity";
  const setParam = (k: string, v: string | null) => {
    const next = new URLSearchParams(sp);
    if (v) next.set(k, v);
    else next.delete(k);
    const s = next.toString();
    router.replace(path + (s ? "?" + s : ""), { scroll: false });
  };
  const pick = (v: View) => {
    setView(v);
    saveView(v);
  };

  const header = (
    <PageHeader
      title={t("project.list_heading")}
      description={t("project.intro")}
      actions={mayCreate && <Button onClick={() => setCreating(true)}><PlusIcon aria-hidden />{t("admin_ui.projects.new")}</Button>}
    />
  );
  const dialog = mayCreate && <CreateProjectDialog open={creating} onOpenChange={setCreating} />;
  if (error && !data) return <>{header}<p role="alert">{t(`errors.${error instanceof ApiError ? error.code : "network"}`)}</p></>;
  if (!data) return <>{header}<SkeletonRows /></>;
  const live = data.items.filter((p) => !p.archived); // archived projects are reachable from Manage → Projects
  if (live.length === 0)
    return (
      <>
        {header}
        <EmptyState
          title={t("empty.projects")}
          action={mayCreate ? <Button onClick={() => setCreating(true)}>{t("empty.create_first_project")}</Button> : <p className="text-sm text-muted-foreground">{t("empty.no_access_hint")}</p>}
        />
        {dialog}
      </>
    );
  const items = sortProjects(live, sort, q);
  return (
    <>
      {header}
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="relative min-w-0 flex-1 basis-56">
          <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden />
          <Input
            type="search"
            className="pl-8"
            aria-label={t("home.search")}
            placeholder={t("home.search")}
            defaultValue={q}
            onChange={(e) => setParam("q", e.target.value.trim() || null)}
          />
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm" aria-label={`${t("search.sort_label")}: ${t(`home.sort_${sort}`)}`}>
              <ArrowUpDownIcon aria-hidden />{t(`home.sort_${sort}`)}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuLabel>{t("search.sort_label")}</DropdownMenuLabel>
            <DropdownMenuRadioGroup value={sort} onValueChange={(v) => setParam("sort", v === "activity" ? null : v)}>
              {PROJECT_SORTS.map((s) => <DropdownMenuRadioItem key={s} value={s}>{t(`home.sort_${s}`)}</DropdownMenuRadioItem>)}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>
        <div role="group" aria-label={t("home.view")} className="flex rounded-md border p-0.5">
          <Button variant={view === "cards" ? "secondary" : "ghost"} size="icon-sm" aria-pressed={view === "cards"} aria-label={t("home.view_cards")} onClick={() => pick("cards")}><LayoutGridIcon /></Button>
          <Button variant={view === "table" ? "secondary" : "ghost"} size="icon-sm" aria-pressed={view === "table"} aria-label={t("home.view_table")} onClick={() => pick("table")}><ListIcon /></Button>
        </div>
      </div>
      {items.length === 0 ? (
        <EmptyState title={t("home.no_match")} action={<Button variant="outline" onClick={() => router.replace(path)}>{t("search.clear_search")}</Button>} />
      ) : view === "cards" ? <Cards items={items} /> : <Table items={items} />}
      {dialog}
    </>
  );
}

export default function Home() {
  return (
    <AppShell>
      <SessionGate require="login">
        <Suspense>
          <ProjectList />
        </Suspense>
      </SessionGate>
    </AppShell>
  );
}
