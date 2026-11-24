"use client";

import { useTranslations } from "next-intl";
import type { ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import { linkify } from "@/lib/linkify";
import { cn } from "@/lib/utils";

type Node = { type: string; value?: string; url?: string; children?: Node[] };

/** Bare http(s) URLs become links and single line breaks stay, as in plain text. Skips links and code (those are not `text` nodes). */
function autolink(node: Node) {
  if (node.type === "link" || !node.children) return;
  node.children = node.children.flatMap((c): Node[] => {
    if (c.type !== "text") {
      autolink(c);
      return [c];
    }
    return (c.value ?? "").split("\n").flatMap((line, i): Node[] => [
      ...(i ? [{ type: "break" }] : []),
      ...linkify(line).filter((p) => p.text).map((p): Node =>
        p.href ? { type: "link", url: p.href, children: [{ type: "text", value: p.text }] } : { type: "text", value: p.text },
      ),
    ]);
  });
}
const remarkAutolink = () => (tree: Node) => autolink(tree);

// Only http(s) becomes a link; anything else (javascript:, data:, relative) is dropped and its text stays.
const urlTransform = (url: string) => (/^https?:\/\/./i.test(url) ? url : "");

// Classes only; react-markdown's own props (node, …) are not passed on to the DOM.
const el = (Tag: "h3" | "h4" | "h5" | "h6" | "ul" | "ol" | "blockquote" | "code" | "pre", className: string) => {
  const C = ({ children }: { children?: ReactNode }) => <Tag className={className}>{children}</Tag>;
  return C;
};
const h = "mt-3 font-semibold";

const components: Components = {
  // User text never competes with the page's own headings: h1–h3 all render as h3.
  h1: el("h3", h), h2: el("h3", h), h3: el("h3", h), h4: el("h4", h), h5: el("h5", h), h6: el("h6", h),
  a: ({ href, children }) =>
    href ? (
      <a href={href} rel="noopener noreferrer nofollow" target="_blank" className="text-primary underline underline-offset-2">{children}</a>
    ) : (
      <>{children}</>
    ),
  // No images: show what the author wrote instead.
  img: ({ alt, src }) => <span>{alt || (typeof src === "string" ? src : "")}</span>,
  ul: el("ul", "list-disc pl-5"),
  ol: el("ol", "list-decimal pl-5"),
  blockquote: el("blockquote", "border-l-2 pl-3 text-muted-foreground"),
  code: el("code", "rounded bg-muted px-1 py-0.5 font-mono text-[0.9em]"),
  pre: el("pre", "overflow-x-auto rounded-md bg-muted p-3 text-sm [&>code]:bg-transparent [&>code]:p-0"),
};

/** A small Markdown subset (paragraphs, emphasis, lists, quotes, headings, code, links). Raw HTML shows as text; images show as text. */
export function Markdown({ text, className }: { text: string; className?: string }) {
  return (
    <div className={cn("break-words [&>*+*]:mt-2", className)}>
      <ReactMarkdown remarkPlugins={[remarkAutolink]} urlTransform={urlTransform} components={components}>
        {text}
      </ReactMarkdown>
    </div>
  );
}

/** The line under a body textarea; point the textarea's aria-describedby at `id`. */
export function MarkdownHint({ id }: { id: string }) {
  return <p id={id} className="text-xs text-muted-foreground">{useTranslations("common")("markdown_hint")}</p>;
}
