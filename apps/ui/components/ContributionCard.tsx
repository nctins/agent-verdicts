"use client";

import { CircleHelpIcon, MessageSquareIcon, ThumbsDownIcon, ThumbsUpIcon } from "lucide-react";
import Link from "next/link";
import { useTranslations } from "next-intl";
import { Avatar } from "@/components/Avatar";
import { RelTime } from "@/components/admin/kit";
import { StateBadge } from "@/components/StateBadge";
import { Badge } from "@/components/ui/badge";
import type { components } from "@/lib/api/schema";
import { contributionHref } from "@/lib/routes";
import { cn } from "@/lib/utils";

type Item = Omit<components["schemas"]["ContributionItem"], "actor_id"> | components["schemas"]["SearchItem"];
type Counts = components["schemas"]["Counts"];

/** Topic-list columns, shared by the header in ContributionList and each row. */
export const TOPIC_COLS = "sm:grid-cols-[minmax(0,1fr)_11rem_7rem_6rem]";

function CountsCell({ item }: { item: Item }) {
  const t = useTranslations("project");
  const c: Counts | undefined = "counts" in item ? item.counts : undefined;
  if (item.kind === "question" && item.answer_count !== undefined)
    return (
      <span className="flex items-center gap-1 text-xs text-muted-foreground tabular-nums sm:justify-end" role="img" aria-label={t("answers", { count: item.answer_count })}>
        <MessageSquareIcon className="size-3.5" aria-hidden />{item.answer_count}
      </span>
    );
  if (!c) return <span className="hidden sm:block" />;
  return (
    <span className="flex items-center gap-3 text-xs text-muted-foreground tabular-nums sm:justify-end" role="img" aria-label={t("counts", { ...c })}>
      <span className="flex items-center gap-1"><ThumbsUpIcon className="size-3.5" aria-hidden />{c.favorable}</span>
      <span className="flex items-center gap-1"><ThumbsDownIcon className="size-3.5" aria-hidden />{c.unfavorable}</span>
    </span>
  );
}

/**
 * One topic row; the whole row is the link (the title's ::after covers it). `select` adds a checkbox
 * above that cover (moderators' bulk actions); `onOpen` runs as the row is opened.
 */
export function ContributionCard({ item, project, select, onOpen }: {
  item: Item;
  project: string;
  select?: { checked: boolean; toggle: () => void; active: boolean };
  onOpen?: () => void;
}) {
  const t = useTranslations();
  const author = "actor_name" in item ? item.actor_name : null;
  const updated = "updated_at" in item ? item.updated_at : null;
  return (
    <li className={cn(`group/row relative grid gap-2 px-4 py-3 hover:bg-accent/50 has-[a:focus-visible]:bg-accent/50 sm:items-center sm:gap-4 ${TOPIC_COLS}`, select?.checked && "bg-accent/40")}>
      <div className="flex min-w-0 items-start gap-3">
        {/* A moderator's checkbox takes the avatar's place on hover/focus, or once a selection has begun (spec P2). */}
        <span className="relative size-8 shrink-0">
          {author && (
            <Avatar
              name={author}
              type={"actor_type" in item ? item.actor_type : undefined}
              seed={"actor_id" in item ? String(item.actor_id) : undefined}
              className={cn(select && (select.active || select.checked ? "opacity-0" : "group-hover/row:opacity-0 group-has-[input:focus-visible]/row:opacity-0 [@media(hover:none)]:opacity-0"))}
            />
          )}
          {select && (
            <input
              type="checkbox"
              className={cn(
                "absolute inset-0 z-10 m-auto size-4 accent-primary",
                select.active || select.checked ? "opacity-100" : "opacity-0 group-hover/row:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100",
              )}
              checked={select.checked}
              onChange={select.toggle}
              aria-label={t("project.bulk.select", { label: item.title })}
            />
          )}
        </span>
        <div className="flex min-w-0 flex-col gap-0.5">
          <Link
            href={contributionHref(project, item.id)}
            data-row-link
            onClick={onOpen}
            className="font-medium break-words outline-none after:absolute after:inset-0"
          >
            {item.kind === "question" && <CircleHelpIcon className="mr-1 inline size-4 align-[-2px] text-muted-foreground" aria-label={t("kind.question")} />}
            {item.title}
          </Link>
          {"snippet" in item && item.snippet && <p className="truncate text-sm text-muted-foreground">{item.snippet}</p>}
          {author && "created_at" in item && (
            <p className="text-xs text-muted-foreground">
              {author}
              {"actor_type" in item && item.actor_type === "Agent" && <> · {t("kit.actor_type.Agent")}</>}
              {updated && <span className="sm:hidden"> · {t("project.updated")} <RelTime at={updated} /></span>}
            </p>
          )}
        </div>
      </div>
      <div className="flex items-center gap-3 pl-11 sm:contents">
        <span className="flex flex-wrap gap-1 sm:justify-start">
          <StateBadge state={item.state} questionStatus={item.question_status} />
          {item.superseded && <Badge variant="outline">{t("contribution.superseded")}</Badge>}
          {item.hidden && <Badge variant="outline">{t("contribution.hidden")}</Badge>}
        </span>
        <span className="hidden text-xs text-muted-foreground sm:block">{updated ? <RelTime at={updated} /> : null}</span>
        <CountsCell item={item} />
      </div>
    </li>
  );
}
