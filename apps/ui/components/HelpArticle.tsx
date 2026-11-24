"use client";

import { CircleHelpIcon, InfoIcon, LightbulbIcon, LinkIcon, TriangleAlertIcon } from "lucide-react";
import Link from "next/link";
import { useTranslations } from "next-intl";
import type { ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import { CopyButton } from "@/components/admin/kit";
import { StateBadge } from "@/components/StateBadge";
import { fold } from "@/lib/table";
import type { TrustState } from "@/lib/trust";
import { cn } from "@/lib/utils";

// The guide's own Markdown (spec 2026-11-23-help-rewrite H3). Written by us, so links may be internal;
// user content keeps using components/Markdown.tsx.

/** A heading's anchor id: accents folded, words joined by "-". */
export const slug = (text: string) =>
  fold(text).replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");

const plain = (s: string) => s.replace(/`([^`]*)`/g, "$1").replace(/[*_]/g, "").trim();

/** The page's `##` headings, for "On this page". */
export function headingsOf(md: string): { id: string; text: string }[] {
  return [...md.matchAll(/^## (.+)$/gm)].map((m) => ({ id: slug(plain(m[1])), text: plain(m[1]) }));
}

type MdNode = { type: string; value?: string; children?: MdNode[]; data?: { hProperties?: Record<string, string> } };
const CALLOUT = /^\[!(NOTE|TIP|WARNING)\]\s*/;

/** `> [!NOTE]` (TIP, WARNING) marks a blockquote as a callout; the marker itself is dropped. */
function callouts(node: MdNode) {
  if (node.type === "blockquote") {
    const text = node.children?.[0]?.children?.[0];
    const m = text?.type === "text" ? CALLOUT.exec(text.value ?? "") : null;
    if (m && text) {
      text.value = (text.value ?? "").slice(m[0].length);
      node.data = { hProperties: { "data-callout": m[1].toLowerCase() } };
    }
  }
  node.children?.forEach(callouts);
}
const remarkCallouts = () => (tree: MdNode) => callouts(tree);

const textOf = (n: ReactNode): string =>
  typeof n === "string" || typeof n === "number" ? String(n) : Array.isArray(n) ? n.map(textOf).join("") : "";

type HNode = { children?: { tagName?: string; properties?: { className?: string[] }; children?: { value?: string }[] }[] };

function Callout({ kind, children }: { kind: string; children?: ReactNode }) {
  const t = useTranslations("help.callout");
  const look = {
    note: { Icon: InfoIcon, cls: "border-sky-500/40 bg-sky-500/5", icon: "text-sky-600 dark:text-sky-400" },
    tip: { Icon: LightbulbIcon, cls: "border-favorable/40 bg-favorable/5", icon: "text-favorable" },
    warning: { Icon: TriangleAlertIcon, cls: "border-conflict/50 bg-conflict/10", icon: "text-conflict" },
  }[kind] ?? { Icon: InfoIcon, cls: "", icon: "" };
  return (
    <aside role="note" aria-label={t(kind)} className={cn("my-4 flex gap-3 rounded-lg border p-3 text-sm", look.cls)}>
      <look.Icon className={cn("mt-0.5 size-4 shrink-0", look.icon)} aria-hidden />
      <div className="flex min-w-0 flex-col gap-1 [&_p]:m-0">
        <p className="font-medium">{t(kind)}</p>
        {children}
      </div>
    </aside>
  );
}

function Heading({ level, children }: { level: 2 | 3; children?: ReactNode }) {
  const t = useTranslations("help");
  const text = textOf(children);
  const id = slug(text);
  const Tag = level === 2 ? "h2" : "h3";
  return (
    <Tag id={id} className={cn("group scroll-mt-20 font-semibold tracking-tight", level === 2 ? "mt-8 mb-3 text-lg" : "mt-5 mb-2")}>
      {children}
      <a href={`#${id}`} className="ml-2 inline-flex align-middle text-muted-foreground opacity-0 group-hover:opacity-100 focus-visible:opacity-100" aria-label={t("anchor", { label: text })}>
        <LinkIcon className="size-3.5" aria-hidden />
      </a>
    </Tag>
  );
}

const STATES = ["Favorable", "Unfavorable", "Conflict", "InsufficientEvidence"];

/** One guide page's Markdown; `diagrams` fill ```diagram blocks by name. */
export function HelpArticle({ md, diagrams = {} }: { md: string; diagrams?: Record<string, ReactNode> }) {
  const components: Components = {
    h1: ({ children }) => <Heading level={2}>{children}</Heading>,
    h2: ({ children }) => <Heading level={2}>{children}</Heading>,
    h3: ({ children }) => <Heading level={3}>{children}</Heading>,
    p: ({ children }) => <p className="my-3 leading-relaxed">{children}</p>,
    ul: ({ children }) => <ul className="my-3 flex list-disc flex-col gap-1.5 pl-5 marker:text-muted-foreground">{children}</ul>,
    ol: ({ children }) => <ol className="my-3 flex list-decimal flex-col gap-2 pl-5 marker:font-medium marker:text-muted-foreground">{children}</ol>,
    li: ({ children }) => <li className="pl-1 leading-relaxed [&>p]:my-1">{children}</li>,
    strong: ({ children }) => <strong className="font-semibold">{children}</strong>,
    a: ({ href, children }) =>
      href?.startsWith("/") || href?.startsWith("#") ? (
        <Link href={href} className="font-medium text-primary underline underline-offset-2">{children}</Link>
      ) : (
        <a href={href} rel="noopener noreferrer" target="_blank" className="font-medium text-primary underline underline-offset-2">{children}</a>
      ),
    blockquote: ({ node, children }) => {
      const kind = (node?.properties as Record<string, unknown> | undefined)?.["data-callout"];
      return typeof kind === "string" ? <Callout kind={kind}>{children}</Callout> : <blockquote className="my-3 border-l-2 pl-3 text-muted-foreground">{children}</blockquote>;
    },
    // Inline code; a few prefixes stand for UI pieces: kbd:j, state:Favorable, qstatus:open.
    code: ({ children }) => {
      const s = textOf(children);
      if (s.startsWith("kbd:")) return <kbd className="rounded border border-b-2 bg-muted px-1.5 py-0.5 font-mono text-xs">{s.slice(4)}</kbd>;
      if (s.startsWith("state:") && STATES.includes(s.slice(6))) return <StateBadge state={s.slice(6) as TrustState} />;
      if (s.startsWith("qstatus:")) return <StateBadge state={null} questionStatus={s.slice(8) as "open"} />;
      return <code className="rounded bg-muted px-1 py-0.5 font-mono text-[0.9em]">{children}</code>;
    },
    // Code blocks: copyable; ```diagram <name> draws one of the guide's diagrams.
    pre: ({ node }) => {
      const code = (node as HNode | undefined)?.children?.find((c) => c.tagName === "code");
      const text = (code?.children ?? []).map((c) => c.value ?? "").join("").replace(/\n$/, "");
      if (code?.properties?.className?.includes("language-diagram")) return <div className="my-4">{diagrams[text.trim()] ?? null}</div>;
      return (
        <div className="relative my-4">
          <pre className="overflow-x-auto rounded-lg bg-muted p-3 pr-10 text-sm"><code>{text}</code></pre>
          <div className="absolute top-1.5 right-1.5"><CopyButton value={text} /></div>
        </div>
      );
    },
  };
  return (
    <div className="text-[0.95rem]">
      <ReactMarkdown remarkPlugins={[remarkCallouts]} components={components}>{md}</ReactMarkdown>
    </div>
  );
}

/** "On this page": the `##` headings as links. */
export function HelpToc({ md }: { md: string }) {
  const t = useTranslations("help");
  const items = headingsOf(md);
  if (items.length < 2) return null;
  return (
    <nav aria-label={t("on_this_page")} className="flex flex-col gap-1 text-sm">
      <p className="mb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">{t("on_this_page")}</p>
      {items.map((h) => (
        <a key={h.id} href={`#${h.id}`} className="border-l py-0.5 pl-3 text-muted-foreground hover:border-foreground hover:text-foreground">{h.text}</a>
      ))}
    </nav>
  );
}

/** A small "?" next to a control that opens the matching guide page (spec 2026-11-23-help-rewrite H7). */
export function HelpLink({ page, anchor, label }: { page: string; anchor?: string; label: string }) {
  const t = useTranslations("help");
  const text = t("open_help", { label });
  return (
    <Link
      href={`/help/${page}/` + (anchor ? `#${anchor}` : "")}
      aria-label={text}
      title={text}
      className="inline-flex rounded-full align-middle text-muted-foreground hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
    >
      <CircleHelpIcon className="size-4" aria-hidden />
    </Link>
  );
}
