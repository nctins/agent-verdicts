"use client";

import { ArrowDownIcon, ArrowLeftIcon, ArrowRightIcon, ChevronRightIcon, SearchIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useLocale, useTranslations } from "next-intl";
import { useEffect, useState, type ReactNode } from "react";
import { PageHeader } from "@/components/admin/kit";
import { EmptyState } from "@/components/EmptyState";
import { Input } from "@/components/ui/input";
import { AppShell } from "@/components/AppShell";
import { SessionGate } from "@/components/SessionGate";
import { StateBadge } from "@/components/StateBadge";
import { HELP_MD, type HelpPage } from "@/content/help";
import { HelpArticle, HelpToc } from "@/components/HelpArticle";
import { AccessLayers, AgentSequence, ClaimLifecycle, CountingExample, HowItWorks } from "@/components/HelpDiagrams";
import type { Locale } from "@/lib/i18n";
import { HELP_GROUPS, HELP_SECTIONS, helpGroupOf, type HelpSection } from "@/lib/sections";
import { fold } from "@/lib/table";
import type { TrustState } from "@/lib/trust";

export const helpHref = (k: HelpSection) => `/help/${k}/`;

type Step = { title: string; desc: string; mark?: ReactNode };

/** A left-to-right diagram of steps (top-to-bottom on a phone); an ordered list, so it reads in order without the arrows. */
function Flow({ label, steps }: { label: string; steps: Step[] }) {
  return (
    <ol aria-label={label} className="flex flex-col items-stretch gap-1 sm:flex-row sm:items-center">
      {steps.map((s, i) => (
        <li key={i} className="flex flex-col items-center gap-1 sm:flex-1 sm:flex-row">
          {i > 0 && (
            <>
              <ArrowDownIcon className="size-4 text-muted-foreground sm:hidden" aria-hidden />
              <ArrowRightIcon className="hidden size-4 shrink-0 text-muted-foreground sm:block" aria-hidden />
            </>
          )}
          <div className="flex w-full flex-col gap-1 rounded-lg border bg-card p-3 sm:h-full">
            <div className="flex flex-wrap items-center gap-2 font-medium">{s.title}{s.mark}</div>
            <p className="text-sm text-muted-foreground">{s.desc}</p>
          </div>
        </li>
      ))}
    </ol>
  );
}

/** The four states as a 2×2 grid: is there support? is there opposition? */
function StateMatrix() {
  const t = useTranslations("help");
  const cell = (s: TrustState) => (
    <td className="border p-3 align-top">
      <div className="flex flex-col gap-1">
        <StateBadge state={s} />
        <span className="text-sm text-muted-foreground">{t(`states.${s}`)}</span>
      </div>
    </td>
  );
  return (
    <table className="w-full table-fixed border-collapse text-sm">
      <caption className="mb-2 text-left text-muted-foreground">{t("matrix.label")}</caption>
      <thead>
        <tr>
          <td className="w-1/4" />
          <th scope="col" className="p-2 text-left font-medium">{t("matrix.has_fav")}</th>
          <th scope="col" className="p-2 text-left font-medium">{t("matrix.no_fav")}</th>
        </tr>
      </thead>
      <tbody>
        <tr><th scope="row" className="p-2 text-left font-medium">{t("matrix.has_unfav")}</th>{cell("Conflict")}{cell("Unfavorable")}</tr>
        <tr><th scope="row" className="p-2 text-left font-medium">{t("matrix.no_unfav")}</th>{cell("Favorable")}{cell("InsufficientEvidence")}</tr>
      </tbody>
    </table>
  );
}


/** The diagrams a page can draw with a ```diagram block. */
export function useDiagrams(): Record<string, ReactNode> {
  const t = useTranslations("help");
  return {
    "concepts-flow": (
      <Flow label={t("flow.label")} steps={[
        { title: t("flow.post"), desc: t("flow.post_d") },
        { title: t("flow.weigh"), desc: t("flow.weigh_d") },
        { title: t("flow.state"), desc: t("flow.state_d") },
      ]} />
    ),
    "state-matrix": <StateMatrix />,
    "access-layers": <AccessLayers />,
    "how-it-works": <HowItWorks />,
    "counting-example": <CountingExample />,
    "agent-sequence": <AgentSequence />,
    "claim-lifecycle": <ClaimLifecycle />,
    "question-flow": (
      <Flow label={t("qflow.label")} steps={[
        { title: t("qflow.ask"), desc: t("qflow.ask_d"), mark: <StateBadge state={null} questionStatus="open" /> },
        { title: t("qflow.answer"), desc: t("qflow.answer_d"), mark: <StateBadge state={null} questionStatus="answered" /> },
        { title: t("qflow.resolve"), desc: t("qflow.resolve_d"), mark: <StateBadge state={null} questionStatus="resolved" /> },
      ]} />
    ),
    "mcp-flow": (
      <Flow label={t("mflow.label")} steps={[
        { title: t("mflow.agent"), desc: t("mflow.agent_d") },
        { title: t("mflow.mcp"), desc: t("mflow.mcp_d") },
        { title: t("mflow.app"), desc: t("mflow.app_d") },
        { title: t("mflow.human"), desc: t("mflow.human_d") },
      ]} />
    ),
  };
}

/** The Markdown of one page in the reader's language. */
function useMd(page: HelpPage) {
  const locale = useLocale() as Locale;
  return (HELP_MD[locale] ?? HELP_MD.vi)[page];
}

/** Splits Markdown at a heading level: [title, body] per part; text before the first heading is dropped. */
const parts = (md: string, marker: "## " | "### ") =>
  md.split(new RegExp(`^${marker}`, "m")).slice(1).map((p) => {
    const nl = p.indexOf("\n");
    return [p.slice(0, nl).trim(), p.slice(nl + 1).trim()] as const;
  });

/** "Quick start" by role: one card per `##` of quickstart.md (spec H6). */
function QuickStart() {
  const t = useTranslations("help");
  const md = useMd("quickstart");
  return (
    <section aria-labelledby="help-quickstart" className="flex flex-col gap-3">
      <h2 id="help-quickstart" className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{t("quickstart")}</h2>
      <ul className="grid gap-3 sm:grid-cols-2">
        {parts(md, "## ").map(([title, body]) => (
          <li key={title} className="flex flex-col rounded-xl border bg-card p-4">
            <h3 className="font-semibold">{title}</h3>
            <div className="text-sm [&_ol]:my-2 [&_p]:my-1"><HelpArticle md={body} /></div>
          </li>
        ))}
      </ul>
    </section>
  );
}

/** Frequent questions, each one open/close (`###` of faq.md). */
function Faq() {
  const t = useTranslations("help");
  const md = useMd("faq");
  return (
    <section aria-labelledby="help-faq" className="flex flex-col gap-3">
      <h2 id="help-faq" className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{t("faq")}</h2>
      <div className="divide-y rounded-xl border bg-card">
        {parts(md, "### ").map(([q, a]) => (
          <details key={q} className="group px-4 py-3">
            <summary className="flex cursor-pointer list-none items-center justify-between gap-2 font-medium">
              {q}
              <ChevronRightIcon className="size-4 text-muted-foreground transition-transform group-open:rotate-90" aria-hidden />
            </summary>
            <div className="text-sm text-muted-foreground"><HelpArticle md={a} /></div>
          </details>
        ))}
      </div>
    </section>
  );
}

/** /help/: the sections as cards, by menu group, with a filter on title and summary (spec 2026-11-22-help-console H3). */
export function HelpIndex() {
  const t = useTranslations("help");
  const locale = useLocale() as Locale;
  const [q, setQ] = useState("");
  const want = fold(q.trim());
  const md = HELP_MD[locale] ?? HELP_MD.vi;
  const hit = (k: HelpSection) => !want || fold(`${t(`pages.${k}.title`)} ${t(`pages.${k}.summary`)} ${md[k]}`).includes(want);
  const groups = HELP_GROUPS.map(([g, sections]) => [g, sections.filter(hit)] as const).filter(([, list]) => list.length > 0);
  return (
    <div className="mx-auto flex max-w-4xl flex-col">
      <PageHeader
        title={t("title")}
        description={t("subtitle")}
        actions={
          <div className="relative">
            <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden />
            <Input type="search" value={q} onChange={(e) => setQ(e.target.value)} aria-label={t("filter")} placeholder={t("filter")} className="h-9 w-60 pl-8" />
          </div>
        }
      />
      {groups.length === 0 ? <EmptyState title={t("no_match")} /> : (
        <div className="flex flex-col gap-8">
          {!want && <QuickStart />}
          {groups.map(([g, list]) => (
            <section key={g} aria-labelledby={`help-${g}`} className="flex flex-col gap-3">
              <h2 id={`help-${g}`} className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{t(g)}</h2>
              <ul className="grid gap-3 sm:grid-cols-2">
                {list.map((k) => (
                  <li key={k} className="relative flex flex-col gap-1 rounded-xl border bg-card p-4 transition-colors hover:bg-accent/50">
                    <Link href={helpHref(k)} className="flex items-center justify-between gap-2 font-medium outline-none after:absolute after:inset-0 focus-visible:underline">
                      {t(`pages.${k}.title`)}
                      <ChevronRightIcon className="size-4 text-muted-foreground" aria-hidden />
                    </Link>
                    <p className="text-sm text-muted-foreground">{t(`pages.${k}.summary`)}</p>
                  </li>
                ))}
              </ul>
            </section>
          ))}
          {!want && <Faq />}
        </div>
      )}
    </div>
  );
}

/** /help/<section>/: "Guide / group" above the title, the section, then previous/next in menu order. */
export function HelpSectionView({ id }: { id: HelpSection }) {
  const t = useTranslations("help");
  const i = HELP_SECTIONS.indexOf(id);
  const prev = HELP_SECTIONS[i - 1], next = HELP_SECTIONS[i + 1];
  const md = useMd(id);
  const diagrams = useDiagrams();
  return (
    <div className="mx-auto flex max-w-5xl gap-10">
    <article className="flex min-w-0 max-w-3xl flex-1 flex-col">
      <nav aria-label={t("breadcrumb")} className="flex items-center gap-1 text-sm text-muted-foreground">
        <Link href="/help/" className="hover:text-foreground hover:underline">{t("title")}</Link>
        <ChevronRightIcon className="size-3.5" aria-hidden />
        <span>{t(helpGroupOf(id))}</span>
      </nav>
      <h1 id={id} className="mt-3 text-2xl font-semibold tracking-tight">{t(`pages.${id}.title`)}</h1>
      <p className="mt-1 text-muted-foreground">{t(`pages.${id}.summary`)}</p>
      <HelpArticle md={md} diagrams={diagrams} />
      <nav aria-label={t("pager")} className="mt-10 grid grid-cols-2 gap-3 border-t pt-4 text-sm">
        {prev ? (
          <Link href={helpHref(prev)} className="flex flex-col gap-0.5 rounded-lg border p-3 hover:bg-accent">
            <span className="flex items-center gap-1 text-xs text-muted-foreground"><ArrowLeftIcon className="size-3" aria-hidden />{t("prev")}</span>
            {t(`pages.${prev}.title`)}
          </Link>
        ) : <span />}
        {next && (
          <Link href={helpHref(next)} className="flex flex-col items-end gap-0.5 rounded-lg border p-3 text-right hover:bg-accent">
            <span className="flex items-center gap-1 text-xs text-muted-foreground">{t("next")}<ArrowRightIcon className="size-3" aria-hidden /></span>
            {t(`pages.${next}.title`)}
          </Link>
        )}
      </nav>
    </article>
    <aside className="sticky top-20 hidden w-52 shrink-0 self-start xl:block"><HelpToc md={md} /></aside>
    </div>
  );
}

export function HelpShell({ children }: { children: ReactNode }) {
  return (
    <AppShell>
      <SessionGate require="none">{children}</SessionGate>
    </AppShell>
  );
}

/** An old help link (e.g. /help/admin/): straight on to the page that replaced it. */
export function HelpMovedRedirect({ to }: { to: HelpSection }) {
  const router = useRouter();
  useEffect(() => router.replace(helpHref(to)), [router, to]);
  return null;
}
