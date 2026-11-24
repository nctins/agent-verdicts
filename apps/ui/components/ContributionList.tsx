"use client";

import { useTranslations } from "next-intl";
import type { ReactNode } from "react";
import { ContributionCard, TOPIC_COLS } from "@/components/ContributionCard";
import type { components } from "@/lib/api/schema";
import { saveListNav } from "@/lib/list-nav";

type Item = components["schemas"]["ContributionItem"] | components["schemas"]["SearchItem"];

/**
 * A forum topic list: header row, then one row per contribution. Opening a row remembers this
 * list's order and URL for previous/next on the contribution page (spec P18).
 */
export function ContributionList({ items, project, empty, selected, onToggle }: {
  items: Item[];
  project: string;
  empty: ReactNode;
  selected?: Set<string>;
  onToggle?: (id: string) => void;
}) {
  const t = useTranslations("topics");
  if (items.length === 0) return <>{empty}</>;
  const remember = () => saveListNav({ url: window.location.pathname + window.location.search, ids: items.map((x) => x.id) });
  return (
    <div className="overflow-hidden rounded-lg border">
      <div aria-hidden className={`hidden gap-4 border-b bg-muted/50 px-4 py-2 text-xs font-medium text-muted-foreground sm:grid ${TOPIC_COLS}`}>
        <span>{t("topic")}</span>
        <span>{t("state")}</span>
        <span>{t("activity")}</span>
        <span className="text-right">{t("votes")}</span>
      </div>
      <ul className="divide-y">
        {items.map((it) => (
          <ContributionCard
            key={it.id}
            item={it}
            project={project}
            onOpen={remember}
            select={selected && onToggle ? { checked: selected.has(it.id), toggle: () => onToggle(it.id), active: selected.size > 0 } : undefined}
          />
        ))}
      </ul>
    </div>
  );
}
