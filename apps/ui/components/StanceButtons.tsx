"use client";

import { MinusIcon, ThumbsDownIcon, ThumbsUpIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { useId, useState, type FormEvent, type ReactNode } from "react";
import { toast } from "sonner";
import { MarkdownHint } from "@/components/Markdown";
import { SourcesField } from "@/components/Sources";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { can, hiddenLocked } from "@/lib/access";
import { useCreateSignal, useProjectInfo, useSession, useSignalBusy, useWithdrawSignal } from "@/lib/api/queries";
import { parseSources, sourcesProblem } from "@/lib/sources";
import { loginHref } from "@/lib/safe-next";
import { cn } from "@/lib/utils";
import { myActiveAssessment, STANCES, type ContributionDetail, type Stance } from "@/lib/stance";

/** The stance picked but not sent yet: say it back, take an optional reason, send only on Confirm. */
function Confirm({ stance, mine, busy, onSend, onWithdraw, onCancel }: {
  stance: Stance;
  mine: Stance | null;
  busy: boolean;
  onSend: (body: string, sources: string[]) => void;
  onWithdraw: () => void;
  onCancel: () => void;
}) {
  const t = useTranslations();
  const [body, setBody] = useState("");
  const [sourcesText, setSourcesText] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const field = useId();
  const same = stance === mine; // the server would ignore the same stance without a reason
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (same && !body.trim()) return;
    const sources = parseSources(sourcesText);
    const bad = sourcesProblem(sources);
    setProblem(bad);
    if (!bad) onSend(body.trim(), sources);
  };
  return (
    <form onSubmit={submit} className="flex flex-col gap-2 rounded-md border bg-muted/30 p-3">
      <p className="text-sm">
        {t("contribution.confirm.picked", { stance: t(`stance.${stance}`) })}
        {mine && !same && <span className="text-muted-foreground"> {t("contribution.confirm.replaces", { stance: t(`stance.${mine}`) })}</span>}
      </p>
      {same && <p className="text-xs text-muted-foreground">{t("contribution.confirm.withdraw_hint")}</p>}
      <Label htmlFor={field} className="text-xs text-muted-foreground">{t("contribution.confirm.reason")}</Label>
      <Textarea id={field} rows={3} value={body} onChange={(e) => setBody(e.target.value)} autoFocus aria-describedby={`${field}-hint`} />
      <MarkdownHint id={`${field}-hint`} />
      <SourcesField id={`${field}-sources`} value={sourcesText} onChange={setSourcesText} />
      {problem && <p role="alert" className="text-xs text-destructive">{t(problem)}</p>}
      <div className="flex flex-wrap justify-end gap-2">
        <Button type="button" variant="outline" size="sm" onClick={onCancel}>{t("common.cancel")}</Button>
        {same && <Button type="button" variant="destructive" size="sm" disabled={busy} onClick={onWithdraw}>{t("contribution.confirm.withdraw")}</Button>}
        <Button type="submit" size="sm" disabled={busy || (same && !body.trim())}>
          {t("contribution.confirm.submit", { stance: t(`stance.${stance}`) })}
        </Button>
      </div>
    </form>
  );
}

const STANCE_ICON = { favorable: ThumbsUpIcon, unfavorable: ThumbsDownIcon, neutral: MinusIcon } as const;
const MINE: Record<Stance, string> = {
  favorable: "bg-favorable text-white hover:bg-favorable/90",
  unfavorable: "bg-unfavorable text-white hover:bg-unfavorable/90",
  neutral: "",
};

/** Everyone's stances, under the buttons: "3 support · 1 oppose · 0 neutral". */
function Tally({ counts }: { counts: ContributionDetail["counts"] }) {
  const t = useTranslations();
  return (
    <p className="flex flex-wrap gap-x-3 text-xs text-muted-foreground">
      {STANCES.map((s) => (
        <span key={s} data-count={s}>
          <span className="font-medium text-foreground tabular-nums">{counts?.[s] ?? 0}</span> {t(`stance.${s}`)}
        </span>
      ))}
    </p>
  );
}

/**
 * A stance is picked, then confirmed (with an optional reason) before anything is sent:
 * picking the stance you hold offers to withdraw it (that cannot be undone). Locked while a signal write on this
 * claim runs, and until the session is known.
 */
export function StanceButtons({ project, detail, compact, children }: {
  project: string;
  detail: ContributionDetail;
  compact?: boolean;
  children?: ReactNode; // extra buttons in the same row
}) {
  const t = useTranslations();
  const router = useRouter();
  const session = useSession().data;
  const create = useCreateSignal(project, detail.id);
  const withdraw = useWithdrawSignal(project, detail.id);
  const { archived } = useProjectInfo(project);
  const busy = useSignalBusy(project, detail.id);
  const [picked, setPicked] = useState<Stance | null>(null);
  const assessment = myActiveAssessment(detail, session?.actor?.id ?? null);
  const mine = assessment?.stance ?? null;

  const click = (stance: Stance) => {
    if (!session?.authenticated) return router.push(loginHref());
    setPicked((p) => (p === stance ? null : stance));
  };
  const send = (stance: Stance, body: string, sources: string[]) => {
    setPicked(null);
    create.mutate({ kind: "assessment", stance, body, ...(sources.length ? { sources } : {}) }, { onSuccess: () => toast.success(t("contribution.confirm.saved")) });
  };

  const out = () => {
    setPicked(null);
    if (assessment) withdraw.mutate(assessment.id, { onSuccess: () => toast.success(t("contribution.withdrawn_done")) });
  };

  // An archived project is read-only, and so is a hidden claim to its author: the counts stay, the buttons go.
  if (archived || hiddenLocked(session, project, detail)) return <Tally counts={detail.counts} />;

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        {/* Three equal buttons; the viewer's own stance is filled in its colour (spec 2026-11-23-ui-polish D3). */}
        <div role="group" aria-label={t("contribution.stance_group")} className="grid min-w-0 flex-1 grid-cols-3 gap-1.5">
          {STANCES.map((s) => {
            const Icon = STANCE_ICON[s];
            return (
              <Button
                key={s}
                size={compact ? "sm" : "default"}
                variant={mine === s ? "default" : picked === s ? "secondary" : "outline"}
                // In the narrow side panel the icon sits over the word, so no label is cut.
                className={cn("min-w-0 px-2", compact && "h-auto flex-col gap-0.5 py-1.5 text-xs", mine === s && MINE[s])}
                aria-pressed={mine === s}
                aria-expanded={picked === s}
                disabled={busy || !session || (session.authenticated && !can(session, "signal:Create", project))}
                onClick={() => click(s)}
              >
                <Icon aria-hidden />
                <span className={compact ? "whitespace-normal" : "truncate"}>{t(`stance.${s}`)}</span>
              </Button>
            );
          })}
        </div>
        {children}
      </div>
      <Tally counts={detail.counts} />
      {picked && (
        <Confirm key={picked} stance={picked} mine={mine} busy={busy} onSend={(b, src) => send(picked, b, src)} onWithdraw={out} onCancel={() => setPicked(null)} />
      )}
    </div>
  );
}
