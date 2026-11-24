"use client";

import { CornerUpLeftIcon, EyeIcon, EyeOffIcon, MessageSquareWarningIcon, ScaleIcon, Undo2Icon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useId, useState, type ComponentProps, type FormEvent, type ReactNode } from "react";
import { toast } from "sonner";
import { Avatar } from "@/components/Avatar";
import { RelTime } from "@/components/admin/kit";
import { SourceList } from "@/components/Sources";
import { Markdown, MarkdownHint } from "@/components/Markdown";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { can, hiddenLocked } from "@/lib/access";
import { useContribution, useCreateSignal, useProjectInfo, useSession, useSetSignalHidden, useSignalBusy, useWithdrawSignal } from "@/lib/api/queries";
import type { components } from "@/lib/api/schema";
import { notCounted, STANCES, type ContributionDetail, type Stance } from "@/lib/stance";
import { cn } from "@/lib/utils";

type Signal = components["schemas"]["Signal"];

const stanceTone: Record<Stance, string> = {
  favorable: "border-favorable/40 bg-favorable/10 text-favorable",
  unfavorable: "border-unfavorable/40 bg-unfavorable/10 text-unfavorable",
  neutral: "border-border text-muted-foreground",
};

/**
 * One forum post: avatar, who and when, labels, then the text. `actions` sit under the text, faint
 * until the post is hovered or focused (always clear on touch screens; spec 2026-11-23-ui-polish D4).
 */
export function Post({ author, type, seed, at, labels, children, actions, faded }: {
  author: string;
  type?: string;
  seed?: string;
  at: string;
  labels?: ReactNode;
  children?: ReactNode;
  actions?: ReactNode;
  faded?: boolean;
}) {
  const t = useTranslations("kit.actor_type");
  return (
    <article className={cn("group/post flex gap-3", faded && "opacity-60")}>
      <Avatar name={author} type={type} seed={seed} className="mt-0.5" />
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <header className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
          <span className="font-medium">{author}</span>
          {type === "Agent" && <Badge variant="secondary" className="px-1.5 text-[0.7rem]">{t("Agent")}</Badge>}
          <RelTime at={at} className="text-xs text-muted-foreground" />
          {labels}
        </header>
        {children}
        {actions && (
          <div className="opacity-70 transition-opacity group-hover/post:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100">{actions}</div>
        )}
      </div>
    </article>
  );
}

/** A collapsed reply box: one line that opens the full form (spec 2026-11-23-ui-polish D2, D6). */
export function Composer({ label, onOpen }: { label: string; onOpen: () => void }) {
  const me = useSession().data?.actor;
  return (
    <button
      type="button"
      onClick={onOpen}
      className="flex w-full items-center gap-3 rounded-lg border bg-card px-3 py-2.5 text-left text-sm text-muted-foreground transition-colors hover:border-ring/60 hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
    >
      {me && <Avatar name={me.name} type={me.type} seed={me.id} className="size-7" />}
      {label}
    </button>
  );
}

/** Takes back the viewer's own signal; it cannot be restored, so it asks first. */
function Withdraw({ project, id, sid }: { project: string; id: string; sid: string }) {
  const t = useTranslations();
  const [open, setOpen] = useState(false);
  const withdraw = useWithdrawSignal(project, id);
  const busy = useSignalBusy(project, id);
  return (
    <>
      <Button variant="ghost" size="xs" disabled={busy} onClick={() => setOpen(true)}><Undo2Icon aria-hidden />{t("contribution.withdraw")}</Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("contribution.withdraw_title")}</DialogTitle>
            <DialogDescription>{t("contribution.withdraw_body")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>{t("common.cancel")}</Button>
            <Button
              variant="destructive"
              disabled={busy}
              onClick={() => {
                setOpen(false);
                withdraw.mutate(sid, { onSuccess: () => toast.success(t("contribution.withdrawn_done")) });
              }}
            >
              {t("contribution.withdraw_confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

/** The Hide control of a contribution or a signal: asks for an optional reason first (unhiding is one click and needs none). */
export function HideButton({ busy, onHide, ...button }: { busy: boolean; onHide: (reason?: string) => void } & Pick<ComponentProps<typeof Button>, "variant" | "size">) {
  const t = useTranslations();
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button {...button} disabled={busy} onClick={() => setOpen(true)}><EyeOffIcon aria-hidden />{t("contribution.hide")}</Button>
      <HideDialog open={open} onOpenChange={setOpen} busy={busy} onHide={onHide} />
    </>
  );
}

/** Asks for an optional reason before hiding; `title` names what is hidden (one or several). */
export function HideDialog({ open, onOpenChange, busy, onHide, title }: { open: boolean; onOpenChange: (o: boolean) => void; busy: boolean; onHide: (reason?: string) => void; title?: string }) {
  const t = useTranslations();
  const [reason, setReason] = useState("");
  const field = useId();
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{title ?? t("contribution.hide_title")}</DialogTitle>
          <DialogDescription>{t("contribution.hide_body")}</DialogDescription>
        </DialogHeader>
        <Label htmlFor={field}>{t("contribution.hide_reason_label")}</Label>
        <Textarea id={field} rows={3} maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t("common.cancel")}</Button>
          <Button
            variant="destructive"
            disabled={busy}
            onClick={() => {
              onOpenChange(false);
              setReason("");
              onHide(reason.trim() || undefined);
            }}
          >
            {t("contribution.hide")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** A reply to a top-level signal: a neutral challenge, so it never changes the claim's state. */
function ReplyForm({ project, id, sid, onDone }: { project: string; id: string; sid: string; onDone: () => void }) {
  const t = useTranslations();
  const create = useCreateSignal(project, id);
  const busy = useSignalBusy(project, id);
  const [body, setBody] = useState("");
  const [missing, setMissing] = useState(false);
  const field = `reply-${sid}`;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setMissing(!body.trim());
    if (!body.trim()) return;
    create.mutate(
      { kind: "challenge", stance: "neutral", body, reply_to: sid },
      {
        onSuccess: () => {
          toast.success(t("contribution.reply.done"));
          onDone();
        },
      },
    );
  };
  return (
    <form onSubmit={submit} className="flex flex-col gap-2 rounded-md border bg-muted/30 p-3" noValidate>
      {missing && (
        <Alert variant="destructive" role="alert"><AlertDescription>{t("contribution.reply.error_required")}</AlertDescription></Alert>
      )}
      <Label htmlFor={field} className="text-xs text-muted-foreground">{t("contribution.reply.label")}</Label>
      <Textarea id={field} rows={3} required aria-invalid={missing} aria-describedby={`${field}-hint`} autoFocus value={body} onChange={(e) => setBody(e.target.value)} />
      <MarkdownHint id={`${field}-hint`} />
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" size="sm" onClick={onDone}>{t("common.cancel")}</Button>
        <Button type="submit" size="sm" disabled={busy}>{t("contribution.reply.submit")}</Button>
      </div>
    </form>
  );
}

/** Controls under one signal: reply (top-level, still counting), withdraw (own, still counting) and hide/unhide (moderators). Absent in an archived project. */
function SignalActions({ project, id, s, history }: { project: string; id: string; s: Signal; history: boolean }) {
  const t = useTranslations("contribution");
  const session = useSession().data;
  const setHidden = useSetSignalHidden(project, id);
  const busy = useSignalBusy(project, id);
  const [replying, setReplying] = useState(false);
  const { archived } = useProjectInfo(project);
  const locked = hiddenLocked(session, project, useContribution(project, id).data);
  if (archived) return null;
  const own = !history && !locked && s.actor_id === session?.actor?.id && can(session, "signal:Create", project);
  const moderate = can(session, "content:Moderate", project);
  const reply = !history && !locked && !s.reply_to && can(session, "signal:Create", project);
  if (!own && !moderate && !reply) return null;
  return (
    <>
      <div className="flex gap-1">
        {reply && !replying && <Button variant="ghost" size="xs" disabled={busy} onClick={() => setReplying(true)}><CornerUpLeftIcon aria-hidden />{t("reply.open")}</Button>}
        {own && <Withdraw project={project} id={id} sid={s.id} />}
        {moderate &&
          (s.hidden ? (
            <Button variant="ghost" size="xs" disabled={busy} onClick={() => setHidden.mutate({ sid: s.id, hidden: false })}><EyeIcon aria-hidden />{t("unhide")}</Button>
          ) : (
            <HideButton variant="ghost" size="xs" busy={busy} onHide={(reason) => setHidden.mutate({ sid: s.id, hidden: true, reason })} />
          ))}
      </div>
      {replying && <ReplyForm project={project} id={id} sid={s.id} onDone={() => setReplying(false)} />}
    </>
  );
}

/** Why a signal in history no longer counts: replaced, withdrawn, hidden or its sender disabled. */
const WHY = ["superseded", "withdrawn", "hidden", "actor_disabled"] as const;

function SignalPost({ s, project, id, author, history, replies = [] }: { s: Signal; project: string; id: string; author: string; history: boolean; replies?: { s: Signal; history: boolean }[] }) {
  const t = useTranslations();
  return (
    <Post
      author={s.actor_name}
      type={s.actor_type}
      seed={s.actor_id}
      at={s.created_at}
      faded={history}
      labels={
        <>
          {s.reply_to ? (
            <Badge variant="secondary">{t("contribution.reply.open")}</Badge>
          ) : (
            <>
              {/* One badge: the stance, with the kind as its icon. */}
              <Badge variant="outline" className={stanceTone[s.stance as Stance]} title={t(`contribution.signal.${s.kind}`)}>
                {s.kind === "assessment" ? <ScaleIcon aria-hidden /> : <MessageSquareWarningIcon aria-hidden />}
                {t(`stance.${s.stance}`)}
                <span className="sr-only">{t(`contribution.signal.${s.kind}`)}</span>
              </Badge>
              {notCounted({ actor_id: author }, s) && <Badge variant="outline">{t("contribution.self_support")}</Badge>}
            </>
          )}
          {WHY.map((w) => s[w] && <Badge key={w} variant="outline">{t(`contribution.why.${w}`)}</Badge>)}
        </>
      }
      actions={<SignalActions project={project} id={id} s={s} history={history} />}
    >
      {s.body ? <Markdown text={s.body} className="text-sm" /> : <p className="text-sm text-muted-foreground italic">{t("contribution.no_reason")}</p>}
      <SourceList sources={s.sources} />
      {s.hidden && s.hidden_reason && <p className="text-xs text-muted-foreground">{t("contribution.hidden_reason", { label: s.hidden_reason })}</p>}
      {replies.length > 0 && (
        <ol className="mt-2 flex flex-col gap-4 border-l-2 pl-4">
          {replies.map((r, i) => <li key={r.s.id + i}><SignalPost s={r.s} project={project} id={id} author={author} history={r.history} /></li>)}
        </ol>
      )}
    </Post>
  );
}

/** The replies under a claim: the assessments and challenges that count, in time order; the rest (replaced, withdrawn, hidden, sender disabled) folded away at the end. */
export function SignalList({ project, detail }: { project: string; detail: ContributionDetail }) {
  const t = useTranslations();
  const { history } = detail.signals;
  const all = STANCES.flatMap((st) => detail.signals[st]).sort((a, b) => a.created_at.localeCompare(b.created_at));
  // A reply sits under its root, whichever stance group the root is in. A counting reply whose root no longer counts,
  // or whose root is not in the lists at all, stays at top level rather than being folded away or dropped.
  const active = new Set(all.map((s) => s.id));
  const known = new Set([...active, ...history.map((s) => s.id)]);
  const nested = (s: Signal, h: boolean) => !!s.reply_to && (h ? known : active).has(s.reply_to);
  const repliesTo = new Map<string, { s: Signal; history: boolean }[]>();
  for (const [list, h] of [[all, false], [history, true]] as const)
    for (const s of list) if (nested(s, h)) repliesTo.set(s.reply_to!, [...(repliesTo.get(s.reply_to!) ?? []), { s, history: h }]);
  for (const l of repliesTo.values()) l.sort((a, b) => a.s.created_at.localeCompare(b.s.created_at));
  const post = (s: Signal, i: number, h: boolean) => (
    <li key={s.id + i}><SignalPost s={s} project={project} id={detail.id} author={detail.actor_id} history={h} replies={repliesTo.get(s.id)} /></li>
  );
  const top = all.filter((s) => !nested(s, false));
  const topHistory = history.filter((s) => !nested(s, true));
  return (
    <section className="flex flex-col gap-4" aria-labelledby="evidence-heading">
      <h2 id="evidence-heading" className="border-b pb-2 font-semibold">
        {t("contribution.replies", { count: all.length })}
      </h2>
      {all.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("contribution.no_signals")}</p>
      ) : (
        <ol className="flex flex-col gap-5">{top.map((s, i) => post(s, i, false))}</ol>
      )}
      {history.length > 0 && (
        <details className="text-sm">
          <summary className="cursor-pointer text-muted-foreground">{t("contribution.history", { count: history.length })}</summary>
          <ol className="mt-3 flex flex-col gap-4">{topHistory.map((s, i) => post(s, i, true))}</ol>
        </details>
      )}
    </section>
  );
}
