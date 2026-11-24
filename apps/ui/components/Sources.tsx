"use client";

import { useTranslations } from "next-intl";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { linkify } from "@/lib/linkify";

/** Input for a post's sources: one per line. */
export function SourcesField({ id, value, onChange }: { id: string; value: string; onChange: (v: string) => void }) {
  const t = useTranslations("sources");
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{t("label")}</Label>
      <Textarea id={id} rows={2} value={value} onChange={(e) => onChange(e.target.value)} aria-describedby={`${id}-hint`} />
      <p id={`${id}-hint`} className="text-xs text-muted-foreground">{t("hint")}</p>
    </div>
  );
}

/** The "Nguồn" block under a post: http(s) items are links, anything else (commit, path) is monospace text. */
export function SourceList({ sources }: { sources: string[] }) {
  const t = useTranslations("sources");
  if (sources.length === 0) return null;
  return (
    <section className="flex flex-col gap-1 text-sm">
      <h3 className="text-xs font-medium text-muted-foreground">{t("heading")}</h3>
      <ul className="flex flex-col gap-0.5">
        {sources.map((s, i) => {
          const [first] = linkify(s);
          return (
            <li key={i} className="min-w-0 break-all">
              {first?.href === s ? (
                <a href={s} rel="noopener noreferrer nofollow" target="_blank" className="text-primary underline underline-offset-2">{s}</a>
              ) : (
                <code className="font-mono text-[0.9em]">{s}</code>
              )}
            </li>
          );
        })}
      </ul>
    </section>
  );
}
