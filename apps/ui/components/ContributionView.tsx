"use client";

import { ChevronLeftIcon, ChevronRightIcon, EyeOffIcon, FolderArchiveIcon, LinkIcon, ListIcon, MessageSquareIcon, MoreHorizontalIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { useState } from "react";
import { toast } from "sonner";
import { ChallengeForm } from "@/components/ChallengeForm";
import { CreateContributionDialog } from "@/components/CreateContributionDialog";
import { HelpLink } from "@/components/HelpArticle";
import { KindBadge } from "@/components/KindBadge";
import { Loading } from "@/components/Loading";
import { Missing } from "@/components/Missing";
import { Markdown } from "@/components/Markdown";
import { QuestionThread } from "@/components/QuestionThread";
import { Composer, HideDialog, Post, SignalList } from "@/components/SignalList";
import { RelTime } from "@/components/admin/kit";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Progress } from "@/components/ui/progress";
import { loadListNav, neighbours } from "@/lib/list-nav";
import { useShortcuts } from "@/lib/shortcuts";
import { SourceList } from "@/components/Sources";
import { StanceButtons } from "@/components/StanceButtons";
import { can, hiddenLocked } from "@/lib/access";
import { StateBadge } from "@/components/StateBadge";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ApiError, errorMessageKey } from "@/lib/api/errors";
import { useContribution, useProjectInfo, useReads30, useSession, useSetHidden, useSignalBusy, useUndoSupersession } from "@/lib/api/queries";
import { contributionHref, parseRoute, projectHref } from "@/lib/routes";
import { loginHref } from "@/lib/safe-next";
import type { ContributionDetail } from "@/lib/stance";
import { copyText } from "@/lib/clipboard";

function Breadcrumb({ project, kind, question }: { project: string; kind: "claim" | "question"; question?: { id: string; title: string } | null }) {
  const t = useTranslations();
  const { meta } = useProjectInfo(project);
  const list = kind === "question" && !question ? "questions" : "claims";
  const link = "break-all hover:text-foreground hover:underline";
  return (
    <nav aria-label={t("contribution.breadcrumb")} className="flex min-w-0 flex-wrap items-center gap-1 text-sm text-muted-foreground">
      <Link href={projectHref(project)} className={link} aria-label={`${t("project.prefix")}: ${meta?.name || project}`}>
        {meta?.name || project}
      </Link>
      <ChevronRightIcon className="size-3.5 shrink-0" aria-hidden />
      <Link href={projectHref(project, new URLSearchParams({ tab: question ? "questions" : list }))} className={link}>
        {t(`project.tab.${question ? "questions" : list}`)}
      </Link>
      {question && (
        <>
          <ChevronRightIcon className="size-3.5 shrink-0" aria-hidden />
          <span className="sr-only">{t("contribution.answer_to")}</span>
          <Link href={contributionHref(project, question.id)} className="min-w-0 truncate text-foreground hover:underline">
            {question.title}
          </Link>
        </>
      )}
    </nav>
  );
}

/** Previous/next through the list this was opened from (spec P18); `[` and `]` too. */
function ListNavBar({ project, id }: { project: string; id: string }) {
  const t = useTranslations("contribution.nav");
  const router = useRouter();
  // The page mounts per contribution and only in the browser (after the session and the detail load).
  const [nav] = useState(() => {
    const n = neighbours(loadListNav(), id);
    // Only a list of this project: another project's ids would lead nowhere.
    return n && parseRoute(new URL(n.url, window.location.origin).pathname)?.project === project ? n : null;
  });
  useShortcuts({
    "[": () => nav?.prev && router.push(contributionHref(project, nav.prev)),
    "]": () => nav?.next && router.push(contributionHref(project, nav.next)),
  });
  if (!nav) return null;
  const btn = (to: string | undefined, label: string, Icon: typeof ChevronLeftIcon) =>
    to ? (
      <Button asChild variant="ghost" size="sm"><Link href={contributionHref(project, to)}>{Icon === ChevronLeftIcon && <Icon aria-hidden />}{label}{Icon === ChevronRightIcon && <Icon aria-hidden />}</Link></Button>
    ) : (
      <Button variant="ghost" size="sm" disabled>{label}</Button>
    );
  return (
    <nav aria-label={t("label")} className="flex items-center gap-1">
      <Button asChild variant="ghost" size="sm"><Link href={nav.url}><ListIcon aria-hidden />{t("back")}</Link></Button>
      {btn(nav.prev, t("prev"), ChevronLeftIcon)}
      {btn(nav.next, t("next"), ChevronRightIcon)}
    </nav>
  );
}

/** The side panel: where the thread stands, and the one-click stances. */
function Summary({ project, d, onChallenge }: { project: string; d: ContributionDetail; onChallenge: () => void }) {
  const t = useTranslations();
  const { archived } = useProjectInfo(project);
  const session = useSession().data;
  const locked = hiddenLocked(session, project, d);
  const manage = can(session, "member:Manage", project);
  const r = useReads30(project, manage).data?.items.find((x) => x.id === d.id);
  const reads = !manage ? null : r ? r.observe.reads + r.reference.reads : 0;
  const need = d.min_favorable ?? 1;
  const supporters = d.counts?.favorable_actors ?? 0;
  return (
    <aside className="flex flex-col gap-4 rounded-lg border bg-card p-4 lg:sticky lg:top-20" aria-labelledby="summary-heading">
      <div className="flex items-center justify-between">
        <h2 id="summary-heading" className="text-sm font-medium text-muted-foreground">{t("contribution.summary")}</h2>
        <HelpLink page={d.kind === "question" ? "questions" : "states"} label={t("contribution.summary")} />
      </div>
      <p className="text-sm text-muted-foreground">
        {d.state ? t(`help.states.${d.state}`) : d.question_status ? t(`help.questions.${d.question_status}`) : null}
      </p>
      {d.kind === "claim" && need > 1 && (d.state === "InsufficientEvidence" || d.state === "Favorable") && (
        <div className="flex flex-col gap-1">
          <p className="flex justify-between text-xs text-muted-foreground">
            <span>{t("contribution.supporters_label")}</span>
            <span className="tabular-nums">{t("contribution.supporters", { done: Math.min(supporters, need), total: need })}</span>
          </p>
          <Progress value={(Math.min(supporters, need) / need) * 100} aria-label={t("contribution.supporters_label")} />
        </div>
      )}
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
        <dt className="text-muted-foreground">{t("contribution.meta.created")}</dt>
        <dd><RelTime at={d.created_at} /></dd>
        <dt className="text-muted-foreground">{t("sources.heading")}</dt>
        <dd className="tabular-nums">{d.sources?.length ?? 0}</dd>
        {reads !== null && (
          <>
            <dt className="text-muted-foreground">{t("contribution.reads_30d")}</dt>
            <dd className="tabular-nums">{reads}</dd>
          </>
        )}
      </dl>
      {d.kind === "claim" && (
        <>
          <p className="-mb-2 flex items-center justify-between text-sm font-medium">
            {t("contribution.stance_group")}
            <HelpLink page="stance" label={t("contribution.stance_group")} />
          </p>
          <StanceButtons project={project} detail={d} compact />
          {!archived && !locked && (
            <Button variant="outline" size="sm" onClick={onChallenge}><MessageSquareIcon aria-hidden />{t("contribution.challenge_open")}</Button>
          )}
        </>
      )}
    </aside>
  );
}

/** Reply box at the bottom of a claim's thread: one "Write a challenge…" line until opened (spec 2026-11-23-ui-polish D2). */
function Reply({ project, d, open, setOpen }: { project: string; d: ContributionDetail; open: boolean; setOpen: (o: boolean) => void }) {
  const t = useTranslations("contribution");
  const router = useRouter();
  const session = useSession().data;
  const [round, setRound] = useState(0); // a new key clears the form after each post
  if (useProjectInfo(project).archived || hiddenLocked(session, project, d)) return null;
  return (
    <section id="reply" className="scroll-mt-20">
      {session?.authenticated ? (
        can(session, "signal:Create", project) &&
        (open ? (
          <ChallengeForm key={round} project={project} id={d.id} onDone={() => { setRound((r) => r + 1); setOpen(false); }} />
        ) : (
          <Composer label={t("challenge.compose")} onOpen={() => setOpen(true)} />
        ))
      ) : (
        session && (
          <div className="flex flex-wrap items-center gap-2 rounded-lg border border-dashed p-4 text-sm">
            <Button size="sm" onClick={() => router.push(loginHref())}>{t("login")}</Button>
            <span className="text-muted-foreground">{t("login_to_respond")}</span>
          </div>
        )
      )}
    </section>
  );
}

/** Contribution-level actions in one "⋯" menu: copy link, supersede a claim, undo that, hide or unhide (moderators). */
function ActionsMenu({ project, d }: { project: string; d: ContributionDetail }) {
  const t = useTranslations();
  const session = useSession().data;
  const { archived } = useProjectInfo(project);
  const setHidden = useSetHidden(project, d.id);
  const undo = useUndoSupersession(project, d.id);
  const busy = useSignalBusy(project, d.id);
  const [replacing, setReplacing] = useState(false);
  const [hiding, setHiding] = useState(false);
  const write = !archived && !!session?.authenticated;
  const moderate = write && can(session, "content:Moderate", project);
  const mayEdit = write && (moderate || d.actor_id === session?.actor?.id) && !hiddenLocked(session, project, d); // the claim's author (unless it is hidden), or a moderator
  const supersede = d.kind === "claim" && !d.superseded_by && mayEdit && can(session, "contribution:Create", project);
  const undoable = !!d.superseded_by && mayEdit;
  const copyLink = async () => {
    try {
      await copyText(window.location.origin + contributionHref(project, d.id));
      toast.success(t("kit.copied"));
    } catch {
      toast.error(t("admin.once.copy_failed"));
    }
  };
  const hide = (hidden: boolean, reason?: string) =>
    setHidden.mutate({ hidden, reason }, { onSuccess: () => toast.success(t(hidden ? "contribution.hidden_done" : "contribution.unhidden_done")) });
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="icon" aria-label={t("contribution.actions")}><MoreHorizontalIcon aria-hidden /></Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onSelect={copyLink}><LinkIcon aria-hidden />{t("project.copy_link")}</DropdownMenuItem>
          {(supersede || undoable || moderate) && <DropdownMenuSeparator />}
          {supersede && <DropdownMenuItem onSelect={() => setReplacing(true)}>{t("contribution.supersede")}</DropdownMenuItem>}
          {undoable && (
            <DropdownMenuItem disabled={busy} onSelect={() => undo.mutate(undefined, { onSuccess: () => toast.success(t("contribution.supersede_undone")) })}>
              {t("contribution.supersede_undo")}
            </DropdownMenuItem>
          )}
          {moderate &&
            (d.hidden ? (
              <DropdownMenuItem disabled={busy} onSelect={() => hide(false)}>{t("contribution.unhide")}</DropdownMenuItem>
            ) : (
              <DropdownMenuItem variant="destructive" disabled={busy} onSelect={() => setHiding(true)}>{t("contribution.hide")}</DropdownMenuItem>
            ))}
        </DropdownMenuContent>
      </DropdownMenu>
      <HideDialog open={hiding} onOpenChange={setHiding} busy={busy} onHide={(reason) => hide(true, reason)} />
      {replacing && (
        <CreateContributionDialog project={project} kind="claim" initialTitle={d.title} supersedes={{ id: d.id, title: d.title }} open onOpenChange={(o) => !o && setReplacing(false)} />
      )}
    </>
  );
}

export function ContributionView({ project, id }: { project: string; id: string }) {
  const t = useTranslations();
  const { data: d, error } = useContribution(project, id);
  const { archived } = useProjectInfo(project);
  const [composing, setComposing] = useState(false);
  const challenge = () => {
    setComposing(true);
    requestAnimationFrame(() => document.getElementById("reply")?.scrollIntoView({ behavior: "smooth", block: "center" }));
  };

  if (!d) {
    if (error instanceof ApiError && error.code === "not_found") return <Missing />;
    if (error) return <p role="alert">{t(errorMessageKey(error instanceof ApiError ? error.code : "network"))}</p>;
    return <Loading />;
  }
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Breadcrumb project={project} kind={d.kind} question={d.answer_to} />
        <ListNavBar project={project} id={id} />
      </div>
      {archived && (
        <Alert role="status">
          <FolderArchiveIcon aria-hidden />
          <AlertDescription>{t("project.archived_notice")}</AlertDescription>
        </Alert>
      )}
      {d.hidden && (
        <Alert role="status">
          <EyeOffIcon aria-hidden />
          <AlertDescription>
            <p>{t("contribution.hidden_notice")}</p>
            {d.hidden_reason && <p>{t("contribution.hidden_reason", { label: d.hidden_reason })}</p>}
          </AlertDescription>
        </Alert>
      )}
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-2">
          <h1 className="text-2xl font-semibold tracking-tight break-words">{d.title}</h1>
          <div role="group" aria-label={t("contribution.status_label")} className="flex flex-wrap items-center gap-2">
            <KindBadge kind={d.kind} />
            <StateBadge state={d.state} questionStatus={d.question_status} />
            {d.superseded_by && <Badge variant="outline">{t("contribution.superseded")}</Badge>}
            {d.hidden && <Badge variant="outline">{t("contribution.hidden")}</Badge>}
          </div>
        </div>
        <ActionsMenu project={project} d={d} />
      </div>
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_18rem] lg:items-start">
        <div className="flex flex-col gap-6 lg:order-1">
          <article className="rounded-lg border p-4">
            <Post author={d.actor_name} type={d.actor_type} seed={d.actor_id} at={d.created_at}>
              {d.body ? <Markdown text={d.body} /> : <p className="text-sm text-muted-foreground italic">{t("contribution.no_body")}</p>}
              <SourceList sources={d.sources} />
            </Post>
          </article>
          {d.superseded_by && (
            <p className="text-sm">
              {t("contribution.superseded_by")}{" "}
              <Link href={contributionHref(project, d.superseded_by.id)} className="font-medium hover:underline">{d.superseded_by.title}</Link>
            </p>
          )}
          {d.kind === "question" ? (
            <QuestionThread project={project} question={d} />
          ) : (
            <>
              <SignalList project={project} detail={d} />
              <Reply project={project} d={d} open={composing} setOpen={setComposing} />
            </>
          )}
        </div>
        <div className="lg:order-2">
          <Summary project={project} d={d} onChallenge={challenge} />
        </div>
      </div>
    </div>
  );
}
