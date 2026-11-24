"use client";

import { ArrowDownIcon, ArrowLeftIcon, ArrowRightIcon, CheckIcon, MinusIcon, ThumbsDownIcon, ThumbsUpIcon, Undo2Icon, XIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import type { ReactNode } from "react";
import { Avatar } from "@/components/Avatar";
import { StateBadge } from "@/components/StateBadge";
import type { TrustState } from "@/lib/trust";
import { cn } from "@/lib/utils";

// The guide's explanatory diagrams (spec 2026-11-23-help-rewrite §4). Drawn from translated strings and the
// app's own badges, so they follow the language and the theme and never go stale like screenshots.

const Box = ({ label, children, className }: { label: string; children: ReactNode; className?: string }) => (
  <div className={cn("flex flex-col gap-2 rounded-lg border p-3", className)}>
    <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{label}</p>
    {children}
  </div>
);

// The built-in manager role (migrations 0003 and 0006).
const MANAGER_ACTIONS = ["contribution:Read", "contribution:Create", "signal:Create", "content:Moderate", "member:Manage"] as const;

/** D1: actions inside a policy inside a role, then the role given to an actor on a project. */
export function AccessLayers() {
  const t = useTranslations("help.diag.access");
  const ta = useTranslations("manage.access.action");
  return (
    <figure className="flex flex-col gap-3" aria-label={t("label")}>
      <div className="grid items-center gap-3 lg:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)]">
        <Box label={t("role")} className="border-primary/40 bg-primary/5">
          <p className="font-mono text-sm font-medium">manager</p>
          <Box label={t("policy")} className="bg-card">
            <p className="font-mono text-sm">ProjectManagerAccess</p>
            <Box label={t("actions")} className="border-dashed">
              <ul className="flex flex-wrap gap-1.5">
                {MANAGER_ACTIONS.map((a) => (
                  <li key={a} className="rounded-full border bg-muted/60 px-2 py-0.5 text-xs">{ta(a)}</li>
                ))}
              </ul>
            </Box>
          </Box>
        </Box>
        <div className="flex justify-center text-muted-foreground">
          <ArrowRightIcon className="hidden size-5 lg:block" aria-hidden />
          <ArrowDownIcon className="size-5 lg:hidden" aria-hidden />
        </div>
        <Box label={t("assignment")} className="bg-card">
          <div className="flex items-center gap-2">
            <Avatar name="Mona" seed="mona" className="size-7" />
            <span className="font-medium">Mona</span>
            <span className="text-muted-foreground">·</span>
            <span className="font-mono text-sm">manager</span>
            <span className="text-muted-foreground">·</span>
            <span className="font-mono text-sm">payments</span>
          </div>
          <ul className="flex flex-col gap-1 text-sm">
            <li className="flex gap-2"><CheckIcon className="mt-0.5 size-4 shrink-0 text-favorable" aria-hidden />{t("can")}</li>
            <li className="flex gap-2"><XIcon className="mt-0.5 size-4 shrink-0 text-unfavorable" aria-hidden />{t("cannot")}</li>
          </ul>
        </Box>
      </div>
      <figcaption className="text-sm text-muted-foreground">{t("caption")}</figcaption>
    </figure>
  );
}

type Step = { who: string; act: "post" | "self" | "support" | "oppose" | "withdraw"; state: TrustState; n?: number };
const ACT_ICON = { post: MinusIcon, self: ThumbsUpIcon, support: ThumbsUpIcon, oppose: ThumbsDownIcon, withdraw: Undo2Icon };

/** D2: one claim, step by step, with a threshold of two supporters. */
export function CountingExample() {
  const t = useTranslations("help.diag.count");
  const steps: Step[] = [
    { who: t("author"), act: "post", state: "InsufficientEvidence", n: 0 },
    { who: t("author"), act: "self", state: "InsufficientEvidence", n: 0 },
    { who: t("a"), act: "support", state: "InsufficientEvidence", n: 1 },
    { who: t("b"), act: "support", state: "Favorable", n: 2 },
    { who: t("c"), act: "oppose", state: "Conflict", n: 2 },
    { who: t("c"), act: "withdraw", state: "Favorable", n: 2 },
  ];
  return (
    <figure aria-label={t("label")} className="flex flex-col gap-2">
      <p className="text-sm text-muted-foreground">{t("setup")}</p>
      <ol className="flex flex-col divide-y rounded-lg border bg-card">
        {steps.map((s, i) => {
          const Icon = ACT_ICON[s.act];
          return (
            <li key={i} className="grid grid-cols-[1.5rem_minmax(0,1fr)] items-center gap-x-3 gap-y-1 px-3 py-2 text-sm sm:grid-cols-[1.5rem_minmax(0,1fr)_6.5rem_11rem]">
              <span className="text-xs font-medium text-muted-foreground tabular-nums">{i + 1}</span>
              <span className="flex min-w-0 items-center gap-2">
                <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                <span><span className="font-medium">{s.who}</span> {t(`act.${s.act}`)}</span>
              </span>
              <span className="col-start-2 text-xs text-muted-foreground tabular-nums sm:col-start-auto">{t("supporters", { done: s.n ?? 0, total: 2 })}</span>
              <span className="col-start-2 sm:col-start-auto"><StateBadge state={s.state} /></span>
            </li>
          );
        })}
      </ol>
      <figcaption className="text-sm text-muted-foreground">{t("caption")}</figcaption>
    </figure>
  );
}

type Msg = { from: 0 | 1 | 2; to: 0 | 1 | 2; key: string; reply?: boolean };

/** D3: who talks to whom when an agent cannot find an answer. Lanes: agent, Agent Verdicts, people. */
export function AgentSequence() {
  const t = useTranslations("help.diag.seq");
  const lanes = [t("agent"), "Agent Verdicts", t("people")];
  const msgs: Msg[] = [
    { from: 0, to: 1, key: "search" },
    { from: 1, to: 0, key: "nothing", reply: true },
    { from: 0, to: 1, key: "ask" },
    { from: 2, to: 1, key: "answer" },
    { from: 2, to: 1, key: "support" },
    { from: 0, to: 1, key: "read" },
    { from: 1, to: 0, key: "trusted", reply: true },
    { from: 0, to: 1, key: "assess" },
  ];
  return (
    <figure aria-label={t("label")} className="flex flex-col gap-2 overflow-x-auto">
      <div className="grid min-w-[32rem] grid-cols-3 gap-2 text-center text-sm font-medium">
        {lanes.map((l) => <div key={l} className="rounded-md border bg-card px-2 py-1.5">{l}</div>)}
      </div>
      <ol className="relative grid min-w-[32rem] gap-1.5">
        {/* The lanes' lifelines. */}
        <div aria-hidden className="pointer-events-none absolute inset-0 grid grid-cols-3">
          {lanes.map((l) => <div key={l} className="mx-auto w-px bg-border" />)}
        </div>
        {msgs.map((m, i) => {
          const left = Math.min(m.from, m.to);
          const leftward = m.to < m.from;
          return (
            <li key={i} className="relative grid grid-cols-6">
              <div
                className="flex flex-col items-center gap-0.5 px-1"
                style={{ gridColumn: `${left * 2 + 2} / span ${Math.abs(m.to - m.from) * 2}` }}
              >
                <span className={cn("rounded bg-background px-1 text-center text-xs", m.reply ? "text-muted-foreground" : "font-medium")}>
                  <span className="sr-only">{lanes[m.from]} → {lanes[m.to]}: </span>{t(`m.${m.key}`)}
                </span>
                <span aria-hidden className="flex w-full items-center text-muted-foreground">
                  {leftward && <ArrowLeftIcon className="-mr-1 size-3.5 shrink-0" />}
                  <span className={cn("h-0 flex-1 border-t", m.reply ? "border-dashed" : "border-foreground/50")} />
                  {!leftward && <ArrowRightIcon className="-ml-1 size-3.5 shrink-0" />}
                </span>
              </div>
            </li>
          );
        })}
      </ol>
      <figcaption className="text-sm text-muted-foreground">{t("caption")}</figcaption>
    </figure>
  );
}

const ROWS = ["normal", "superseded", "hidden", "archived"] as const;
const COLS = ["listed", "counted", "seen", "writable"] as const;
// yes / no / partly, per row and column.
const CELLS: Record<(typeof ROWS)[number], ("yes" | "no" | "some")[]> = {
  normal: ["yes", "yes", "yes", "yes"],
  superseded: ["some", "no", "yes", "yes"],
  hidden: ["some", "no", "some", "some"],
  archived: ["yes", "yes", "yes", "no"],
};

/** D4: what replacing, hiding and archiving change for a claim. */
export function ClaimLifecycle() {
  const t = useTranslations("help.diag.life");
  const mark = (v: "yes" | "no" | "some") =>
    v === "yes" ? <CheckIcon className="size-4 text-favorable" aria-label={t("yes")} />
    : v === "no" ? <XIcon className="size-4 text-unfavorable" aria-label={t("no")} />
    : <MinusIcon className="size-4 text-conflict" aria-label={t("some")} />;
  return (
    <figure className="flex flex-col gap-2 overflow-x-auto">
      <table className="w-full min-w-[34rem] border-collapse text-sm">
        <caption className="mb-2 text-left text-muted-foreground">{t("label")}</caption>
        <thead>
          <tr className="border-b text-left text-xs text-muted-foreground">
            <td />
            {COLS.map((c) => <th key={c} scope="col" className="p-2 font-medium">{t(`col.${c}`)}</th>)}
          </tr>
        </thead>
        <tbody>
          {ROWS.map((r) => (
            <tr key={r} className="border-b last:border-0">
              <th scope="row" className="p-2 text-left font-medium whitespace-nowrap">{t(`row.${r}`)}</th>
              {CELLS[r].map((v, i) => (
                <td key={i} className="p-2 align-top">
                  <span className="flex items-start gap-1.5">
                    {mark(v)}
                    {t.has(`note.${r}.${COLS[i]}`) && <span className="text-xs text-muted-foreground">{t(`note.${r}.${COLS[i]}`)}</span>}
                  </span>
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      <figcaption className="text-sm text-muted-foreground">{t("caption")}</figcaption>
    </figure>
  );
}

/** One step of the story: number, picture, title, one line. */
const StoryStep = ({ n, title, desc, children }: { n: number; title: string; desc: string; children: ReactNode }) => (
  <li className="flex flex-col gap-3 rounded-xl border bg-card p-4">
    <div className="flex items-center gap-2">
      <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-primary text-xs font-semibold text-primary-foreground">{n}</span>
      <span className="font-semibold">{title}</span>
    </div>
    <div className="flex min-h-28 flex-1 flex-col justify-center gap-2.5 rounded-lg bg-muted/50 p-3">{children}</div>
    <p className="text-sm text-muted-foreground">{desc}</p>
  </li>
);

/** Someone in the story: avatar, name and mark on one line, what they did under it. */
const Who = ({ name, agent, mark, note }: { name: string; agent?: boolean; mark?: ReactNode; note: string }) => (
  <span className="flex items-start gap-2 text-sm">
    <Avatar name={name} type={agent ? "Agent" : "Human"} seed={name} className="mt-0.5 size-6 text-[0.6rem]" />
    <span className="flex min-w-0 flex-col">
      <span className="flex items-center gap-1.5 font-medium">{name}{mark}</span>
      <span className="text-xs text-muted-foreground">{note}</span>
    </span>
  </span>
);

/**
 * The big picture for a newcomer (spec 2026-11-23-help-rewrite §5): one claim goes round the loop
 * record → check → verdict → reuse. No servers, no engines.
 */
export function HowItWorks() {
  const t = useTranslations("help.diag.story");
  const ts = useTranslations("stance");
  return (
    <figure aria-label={t("label")} className="flex flex-col gap-3">
      <ol className="grid gap-3 sm:grid-cols-2">
        <StoryStep n={1} title={t("s1")} desc={t("s1_d")}>
          <p className="text-sm font-medium">“{t("claim")}”</p>
          <Who name="Scout" agent note={t("posted")} />
        </StoryStep>
        <StoryStep n={2} title={t("s2")} desc={t("s2_d")}>
          <Who name={t("p1")} mark={<ThumbsUpIcon className="size-3.5 text-favorable" aria-label={ts("favorable")} />} note={t("r1")} />
          <Who name={t("p2")} mark={<ThumbsUpIcon className="size-3.5 text-favorable" aria-label={ts("favorable")} />} note={t("r2")} />
          <Who name="Bot" agent mark={<ThumbsDownIcon className="size-3.5 text-unfavorable" aria-label={ts("unfavorable")} />} note={t("r3")} />
        </StoryStep>
        <StoryStep n={3} title={t("s3")} desc={t("s3_d")}>
          <span className="flex flex-wrap items-center gap-1.5"><StateBadge state="Conflict" /><ArrowRightIcon className="size-3.5 text-muted-foreground" aria-hidden /><StateBadge state="Favorable" /></span>
          <span className="text-xs text-muted-foreground">{t("s3_note")}</span>
        </StoryStep>
        <StoryStep n={4} title={t("s4")} desc={t("s4_d")}>
          <Who name="Scout" agent mark={<CheckIcon className="size-3.5 text-favorable" aria-hidden />} note={t("u1")} />
          <Who name={t("p3")} mark={<CheckIcon className="size-3.5 text-favorable" aria-hidden />} note={t("u2")} />
        </StoryStep>
      </ol>
      <p className="flex items-center justify-center gap-2 rounded-lg border border-dashed p-2 text-sm text-muted-foreground">
        <Undo2Icon className="size-4" aria-hidden />{t("loop")}
      </p>
    </figure>
  );
}
