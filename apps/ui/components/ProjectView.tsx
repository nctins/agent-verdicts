"use client";

import {
  ArrowUpDownIcon, ChevronDownIcon, FolderArchiveIcon, FolderIcon, KeyboardIcon, LinkIcon, MoreHorizontalIcon, PlugIcon,
  PlusIcon, SettingsIcon, SlidersHorizontalIcon,
} from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { ConnectAgentDialog } from "@/components/ConnectAgentDialog";
import { ContributionList } from "@/components/ContributionList";
import { CreateContributionDialog } from "@/components/CreateContributionDialog";
import { EmptyState } from "@/components/EmptyState";
import { Missing } from "@/components/Missing";
import { ProjectOverview } from "@/components/ProjectOverview";
import { ProjectSettings } from "@/components/ProjectSettings";
import { SearchBox } from "@/components/SearchBox";
import { FilterButton, FilterChips, type FilterDef } from "@/components/FilterBar";
import { HideDialog } from "@/components/SignalList";
import { CopyButton, PageHeader, SkeletonRows } from "@/components/admin/kit";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import {
  DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuRadioGroup,
  DropdownMenuRadioItem, DropdownMenuSeparator, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { can } from "@/lib/access";
import { ApiError, errorMessageKey } from "@/lib/api/errors";
import { useAuthors, useBulkSetHidden, useContributions, useProjectInfo, useSession } from "@/lib/api/queries";
import type { ContributionFilters } from "@/lib/api/keys";
import type { components } from "@/lib/api/schema";
import {
  listFilter, parseProjectParams, prefillTitle, QUESTION_STATUSES, SORTS, STATES, type ListTab, type Sort, type Tab,
} from "@/lib/project-filters";
import { projectHref } from "@/lib/routes";
import { loginHref } from "@/lib/safe-next";
import { moveRow, useShortcuts } from "@/lib/shortcuts";
import type { TrustState } from "@/lib/trust";
import { useDebouncedSearch } from "@/lib/use-debounced-search";
import { cn } from "@/lib/utils";
import { copyText } from "@/lib/clipboard";

type Changes = Record<string, string | null>;
type Compose = (kind: "claim" | "question", title?: string) => void;
type Params = ReturnType<typeof parseProjectParams>;

/** Parameters that only mean something on a list tab; leaving for Overview drops them. */
const LIST_PARAMS = ["q", "actor", "sort", "superseded", "state", "status"];

function SearchResults({ project, q, filter, includeSuperseded, onClear }: { project: string; q: string; filter: ContributionFilters; includeSuperseded: boolean; onClear: () => void }) {
  const t = useTranslations();
  const { results, degraded, settledText, stale, error } = useDebouncedSearch(project, q, {
    delayMs: 0,
    limit: 20,
    rerank: true,
    kind: filter.kind,
    state: filter.state,
    questionStatus: filter.question_status,
    includeSuperseded: includeSuperseded || undefined,
  });
  if (error) return <p role="alert">{t(errorMessageKey(error instanceof ApiError ? error.code : "network"))}</p>;
  // Kept data from another text or tab (kind) is not this search's answer: wait for it.
  if (stale || settledText !== q) return <SkeletonRows />;
  return (
    <div className="flex flex-col gap-2">
      <p className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground" role="status">
        {t("search.result_count", { count: results.length, label: q })}
        <Button variant="link" size="sm" className="h-auto p-0" onClick={onClear}>{t("search.clear_search")}</Button>
      </p>
      {degraded && <p className="text-xs text-muted-foreground">{t("search.degraded")}</p>}
      <ContributionList items={results} project={project} empty={<EmptyState title={t("empty.search")} />} />
    </div>
  );
}

/** Moderators' bulk hide/unhide over the selected rows (spec P15). */
function BulkBar({ project, ids, onClear }: { project: string; ids: string[]; onClear: () => void }) {
  const t = useTranslations();
  const bulk = useBulkSetHidden(project);
  const [hiding, setHiding] = useState(false);
  const run = (hidden: boolean, reason?: string) =>
    bulk.mutate({ ids, hidden, reason }, {
      onSuccess: ({ done, failed }) => {
        if (done) toast.success(t(hidden ? "project.bulk.hidden_done" : "project.bulk.unhidden_done", { count: done }));
        if (failed) toast.error(t("project.bulk.failed", { count: failed }));
        onClear();
      },
    });
  return (
    <div role="region" aria-label={t("project.bulk.label")} className="sticky bottom-4 z-20 flex flex-wrap items-center gap-2 self-center rounded-lg border bg-popover px-3 py-2 text-sm shadow-lg">
      <span className="font-medium tabular-nums">{t("project.bulk.selected", { count: ids.length })}</span>
      <Button size="sm" variant="destructive" disabled={bulk.isPending} onClick={() => setHiding(true)}>{t("contribution.hide")}</Button>
      <Button size="sm" variant="outline" disabled={bulk.isPending} onClick={() => run(false)}>{t("contribution.unhide")}</Button>
      <Button size="sm" variant="ghost" onClick={onClear}>{t("project.bulk.clear")}</Button>
      <HideDialog open={hiding} onOpenChange={setHiding} busy={bulk.isPending} title={t("project.bulk.hide_title", { count: ids.length })} onHide={(reason) => run(true, reason)} />
    </div>
  );
}

/** One paged list for a filter, under an optional heading; `empty` null renders nothing at all (heading included) when it has no items. */
function Feed({ project, filter, empty, heading, selectable }: { project: string; filter: ContributionFilters; empty: ReactNode; heading?: ReactNode; selectable?: boolean }) {
  const t = useTranslations();
  const q = useContributions(project, filter);
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const sentinel = useRef<HTMLDivElement>(null);
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = q;
  // Infinite scroll; the button below stays for keyboards and browsers without the observer.
  useEffect(() => {
    const el = sentinel.current;
    if (!el || !hasNextPage || typeof IntersectionObserver === "undefined") return;
    const io = new IntersectionObserver(([e]) => { if (e.isIntersecting && !isFetchingNextPage) fetchNextPage(); }, { rootMargin: "200px" });
    io.observe(el);
    return () => io.disconnect();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);
  const items: components["schemas"]["ContributionItem"][] = q.data?.pages.flatMap((p) => p.items) ?? [];
  if (q.error && !q.data) {
    if (q.error instanceof ApiError && q.error.code === "not_found") return <Missing />;
    return <p role="alert">{t(errorMessageKey(q.error instanceof ApiError ? q.error.code : "network"))}</p>;
  }
  if (!q.data) return <div className="flex flex-col gap-3">{heading}<SkeletonRows /></div>;
  if (!items.length && empty === null) return null;
  const toggle = (id: string) => setSelected((s) => {
    const n = new Set(s);
    if (!n.delete(id)) n.add(id);
    return n;
  });
  const chosen = items.filter((x) => selected.has(x.id)).map((x) => x.id);
  return (
    <div className="flex flex-col gap-3">
      {heading}
      <ContributionList items={items} project={project} empty={empty} selected={selectable ? selected : undefined} onToggle={selectable ? toggle : undefined} />
      <div ref={sentinel} />
      {hasNextPage && (
        <Button variant="outline" className="self-center" disabled={isFetchingNextPage} onClick={() => fetchNextPage()}>
          {t("project.more")}
        </Button>
      )}
      {chosen.length > 0 && <BulkBar project={project} ids={chosen} onClear={() => setSelected(new Set())} />}
    </div>
  );
}

/** Review = what needs a person: conflicts first (someone must settle them), then claims nobody has weighed. */
function Review({ project, selectable }: { project: string; selectable: boolean }) {
  const t = useTranslations();
  const section = (s: TrustState, sort: Sort, empty: ReactNode) => (
    <section aria-labelledby={`rv-${s}`}>
      <Feed
        project={project}
        filter={{ kind: "claim", state: [s], sort }}
        empty={empty}
        selectable={selectable}
        heading={
          <h2 id={`rv-${s}`} className="font-medium">
            {t(`state.${s}`)} <span className="text-sm font-normal text-muted-foreground">· {t(`help.states.${s}`)}</span>
          </h2>
        }
      />
    </section>
  );
  return (
    <div className="flex flex-col gap-6">
      {section("Conflict", "debated", <EmptyState title={t("empty.review")} />)}
      {section("InsufficientEvidence", "newest", null)}
    </div>
  );
}

/** Search, "+ Filter", the active filters as chips, sort and display (spec P9–P11); everything lives in the URL. */
function ListTabView({ project, tab, p, set, mayWrite, onCompose, selectable }: {
  project: string;
  tab: ListTab;
  p: Params;
  set: (c: Changes) => void;
  mayWrite: boolean;
  onCompose: Compose;
  selectable: boolean;
}) {
  const t = useTranslations();
  const me = useSession().data?.actor;
  const { q, state, status, actor, sort, superseded } = p;
  const who = me && actor === me.id ? "me" : actor; // the server resolves "me"; old links carry the id
  // Author choices are everyone who wrote something visible in the project; the selected one always stays.
  const authors = new Map(useAuthors(project).data?.items.map((a) => [a.id, a.name]));
  if (me) authors.delete(me.id);
  const defs: FilterDef[] = [
    ...(tab === "claims" ? [{ id: "state", label: t("search.state_label"), value: state, options: STATES.map((s) => ({ value: s, label: t(`state.${s}`) })) }] : []),
    ...(tab === "questions" ? [{ id: "status", label: t("search.status_label"), value: status, options: QUESTION_STATUSES.map((s) => ({ value: s, label: t(`question_status.${s}`) })) }] : []),
    {
      id: "actor", label: t("search.actor_label"), value: who || null,
      options: [...(me ? [{ value: "me", label: t("search.actor_mine") }] : []), ...[...authors].map(([id, name]) => ({ value: id, label: name || id }))],
    },
  ];
  const filtered = defs.some((d) => d.value);
  const clearAll = () => set({ state: null, status: null, actor: null });
  const filter: ContributionFilters = {
    ...listFilter(tab, state, status), ...(who ? { actor: who } : {}), ...(superseded && tab !== "questions" ? { include_superseded: true } : {}), sort,
  };
  const empty = filtered ? (
    <EmptyState title={t("empty.filtered")} action={<Button variant="outline" onClick={clearAll}>{t("search.clear_filters")}</Button>} />
  ) : (
    <EmptyState
      title={t(`empty.${tab}`)}
      action={mayWrite && tab !== "review" ? <Button onClick={() => onCompose(tab === "questions" ? "question" : "claim")}><PlusIcon aria-hidden />{t(tab === "questions" ? "project.ask" : "project.contribute")}</Button> : undefined}
    />
  );

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <div className="min-w-0 flex-1 basis-64">
            <SearchBox
              key={q}
              project={project}
              initialText={q}
              onSubmit={(text) => set({ q: text || null })}
              onAsk={(text) => mayWrite && onCompose("question", prefillTitle(text))}
            />
          </div>
          {tab !== "review" && <FilterButton filters={defs} onChange={(id, v) => set({ [id]: v })} />}
          {!q && tab !== "review" && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm" aria-label={`${t("search.sort_label")}: ${t(`search.sort_${sort}`)}`}>
                  <ArrowUpDownIcon aria-hidden />{t(`search.sort_${sort}`)}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuLabel>{t("search.sort_label")}</DropdownMenuLabel>
                <DropdownMenuRadioGroup value={sort} onValueChange={(v) => set({ sort: v === "newest" ? null : v })}>
                  {SORTS.map((s) => <DropdownMenuRadioItem key={s} value={s}>{t(`search.sort_${s}`)}</DropdownMenuRadioItem>)}
                </DropdownMenuRadioGroup>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
          {tab !== "questions" && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm"><SlidersHorizontalIcon aria-hidden />{t("search.display")}</Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuCheckboxItem checked={superseded} onCheckedChange={(on) => set({ superseded: on ? "1" : null })}>
                  {t("search.include_superseded")}
                </DropdownMenuCheckboxItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
        </div>
        {tab !== "review" && <FilterChips filters={defs} onChange={(id, v) => set({ [id]: v })} onClear={clearAll} />}
      </div>
      {q ? (
        <SearchResults project={project} q={q} filter={listFilter(tab, state, status)} includeSuperseded={superseded} onClear={() => set({ q: null })} />
      ) : tab === "review" ? (
        <Review project={project} selectable={selectable} />
      ) : (
        <Feed key={JSON.stringify(filter)} project={project} filter={filter} empty={empty} selectable={selectable} />
      )}
    </div>
  );
}

/** The description under the title: two lines, then "Show more". */
function Description({ text }: { text: string }) {
  const t = useTranslations("project");
  const [more, setMore] = useState(false);
  const long = text.length > 160 || text.split("\n").length > 2;
  return (
    <span className="flex flex-col items-start gap-0.5">
      <span className={cn("whitespace-pre-line", long && !more && "line-clamp-2")}>{text}</span>
      {long && <Button variant="link" size="sm" className="h-auto p-0" aria-expanded={more} onClick={() => setMore(!more)}>{t(more ? "show_less" : "show_more")}</Button>}
    </span>
  );
}

function ShortcutHelp({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const t = useTranslations("shortcuts");
  const rows: [string, string][] = [["j / k", t("move")], ["Enter", t("open")], ["c", t("compose")], ["/", t("search")], ["[ / ]", t("prev_next")], ["⌘K", t("palette")], ["?", t("help")]];
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader><DialogTitle>{t("title")}</DialogTitle></DialogHeader>
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          {rows.map(([k, v]) => (
            <div key={k} className="contents">
              <dt><kbd className="rounded border bg-muted px-1.5 font-mono text-xs">{k}</kbd></dt>
              <dd>{v}</dd>
            </div>
          ))}
        </dl>
      </DialogContent>
    </Dialog>
  );
}

// The active tab's line sits on the list's bottom edge (the scroll box would clip it below that).
const UNDERLINE = "group-data-horizontal/tabs:after:bottom-0 px-2.5";

const Count = ({ n }: { n: number }) => (
  <span aria-hidden className="ml-1.5 rounded-full bg-muted px-1.5 text-xs tabular-nums text-muted-foreground group-data-[state=active]:bg-primary/10 group-data-[state=active]:text-foreground">{n}</span>
);

export function ProjectView({ id }: { id: string }) {
  const t = useTranslations();
  const router = useRouter();
  const sp = useSearchParams();
  const session = useSession().data;
  const [dialog, setDialog] = useState<{ kind: "claim" | "question"; title: string } | null>(null);
  const [connect, setConnect] = useState(false);
  const [help, setHelp] = useState(false);
  const p = parseProjectParams(sp);
  const { tab } = p;
  const { meta, unlisted, archived } = useProjectInfo(id);

  const set = (changes: Changes) => {
    const next = new URLSearchParams(sp);
    // Spell the current tab out first: an old link (tab=open, tab=all, or none with list parameters)
    // must keep its tab when one of its parameters is cleared.
    if (next.get("tab") === "open" && !next.has("status")) next.set("status", "open");
    if (tab === "overview") next.delete("tab");
    else next.set("tab", tab);
    for (const [k, v] of Object.entries(changes)) {
      if (v) next.set(k, v);
      else next.delete(k);
    }
    if (!next.has("tab")) LIST_PARAMS.forEach((k) => next.delete(k));
    router.replace(projectHref(id, next), { scroll: false });
  };
  const mayCreate = !session?.authenticated || can(session, "contribution:Create", id);
  const mayWrite = !archived && mayCreate;
  const openDialog: Compose = (kind, title = "") => {
    if (!session?.authenticated) router.push(loginHref());
    else if (can(session, "contribution:Create", id)) setDialog({ kind, title });
  };
  const mayEdit = can(session, "project:Create");
  const settings = mayEdit || can(session, "member:Manage", id);
  const selectable = !archived && can(session, "content:Moderate", id);

  // ⌘K's "Contribute" / "Ask" land here with ?compose=: the dialog opens from the URL, and closing it drops the parameter.
  const compose = sp.get("compose");
  const fromUrl = (compose === "claim" || compose === "question") && !archived && can(session, "contribution:Create", id) ? { kind: compose as "claim" | "question", title: "" } : null;
  const shown = dialog ?? fromUrl;
  const closeDialog = () => {
    setDialog(null);
    if (compose) set({ compose: null });
  };

  useShortcuts({
    j: () => moveRow(1),
    k: () => moveRow(-1),
    c: () => mayWrite && openDialog("claim"),
    "?": () => setHelp(true),
  });

  // A typo in the URL is "not found", not an empty project.
  if (unlisted) return <Missing />;
  if (tab === "settings" && session && !settings) return <Missing />;
  const claims = meta ? Object.values(meta.states).reduce((a, b) => a + b, 0) : 0;
  const copyLink = async () => {
    try {
      await copyText(window.location.origin + projectHref(id));
      toast.success(t("kit.copied"));
    } catch {
      toast.error(t("admin.once.copy_failed"));
    }
  };

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        className="mb-0"
        title={
          <>
            <FolderIcon className="size-6 shrink-0 text-muted-foreground" aria-hidden />
            <span className="break-all"><span className="sr-only">{t("project.prefix")}: </span>{meta?.name ?? id}</span>
          </>
        }
        meta={
          <span className="flex items-center gap-1">
            <code className="rounded bg-muted px-1 font-mono text-xs">{id}</code>
            <CopyButton value={id} label={t("project.id")} />
          </span>
        }
        description={meta?.description ? <Description text={meta.description} /> : undefined}
        actions={
          <>
            {mayWrite && (
              <div className="flex">
                <Button className="rounded-r-none" onClick={() => openDialog("claim")}><PlusIcon aria-hidden />{t("project.contribute")}</Button>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button className="rounded-l-none border-l border-primary-foreground/25 px-2" aria-label={t("project.compose_more")}><ChevronDownIcon aria-hidden /></Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem onSelect={() => openDialog("claim")}>{t("project.compose_claim")}</DropdownMenuItem>
                    <DropdownMenuItem onSelect={() => openDialog("question")}>{t("project.ask")}</DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            )}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="icon" aria-label={t("project.more_actions")}><MoreHorizontalIcon aria-hidden /></Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => setConnect(true)}><PlugIcon aria-hidden />{t("project.connect.button")}</DropdownMenuItem>
                <DropdownMenuItem onSelect={copyLink}><LinkIcon aria-hidden />{t("project.copy_link")}</DropdownMenuItem>
                <DropdownMenuItem onSelect={() => setHelp(true)}><KeyboardIcon aria-hidden />{t("shortcuts.title")}</DropdownMenuItem>
                {settings && (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem onSelect={() => set({ tab: "settings" })}><SettingsIcon aria-hidden />{t("project.tab.settings")}</DropdownMenuItem>
                  </>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        }
      />
      {archived && (
        <Alert role="status">
          <FolderArchiveIcon aria-hidden />
          <AlertDescription>{t("project.archived_notice")}</AlertDescription>
        </Alert>
      )}
      <Tabs
        value={tab}
        onValueChange={(v) => set({ tab: v === "overview" ? null : v, state: null, status: null })}
      >
        {/* Page-level tabs: underlined, across the page; on a narrow screen they scroll, the fade says so (spec P1, P6). */}
        <div className="overflow-x-auto border-b [mask-image:linear-gradient(to_right,black_85%,transparent)] sm:[mask-image:none]">
          <TabsList variant="line" aria-label={t("project.tab.label")} className="h-10 pb-0">
            <TabsTrigger value="overview" className={UNDERLINE}>{t("project.tab.overview")}</TabsTrigger>
            <TabsTrigger value="claims" className={cn("group", UNDERLINE)}>{t("project.tab.claims")}{meta && <Count n={claims} />}</TabsTrigger>
            <TabsTrigger value="questions" className={cn("group", UNDERLINE)}>{t("project.tab.questions")}{meta && <Count n={meta.open_question_count} />}</TabsTrigger>
            <TabsTrigger value="review" className={cn("group", UNDERLINE)}>{t("project.tab.review")}{meta && <Count n={meta.states.Conflict + meta.states.InsufficientEvidence} />}</TabsTrigger>
            {settings && <TabsTrigger value="settings" className={UNDERLINE}>{t("project.tab.settings")}</TabsTrigger>}
          </TabsList>
        </div>
        <TabsContent value={tab} className="mt-2">
          {tab === "overview" ? (
            <ProjectOverview id={id} onPick={set} onCompose={openDialog} onConnect={() => setConnect(true)} mayWrite={mayWrite} />
          ) : tab === "settings" ? (
            <ProjectSettings id={id} />
          ) : (
            <ListTabView project={id} tab={tab as Exclude<Tab, "overview" | "settings">} p={p} set={set} mayWrite={mayWrite} onCompose={openDialog} selectable={selectable} />
          )}
        </TabsContent>
      </Tabs>
      <ConnectAgentDialog project={id} open={connect} onOpenChange={setConnect} />
      <ShortcutHelp open={help} onOpenChange={setHelp} />
      {shown && (
        <CreateContributionDialog
          project={id}
          kind={shown.kind}
          initialTitle={shown.title}
          open
          onOpenChange={(o) => !o && closeDialog()}
        />
      )}
    </div>
  );
}
