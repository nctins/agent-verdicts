"use client";

import { ChevronRightIcon, LinkIcon } from "lucide-react";
import { useFormatter, useTranslations } from "next-intl";
import { Fragment, useState } from "react";
import { usePolicies, useRoles } from "@/lib/api/queries";
import { cn } from "@/lib/utils";
import type { components } from "@/lib/api/schema";

type Event = components["schemas"]["AuditEvent"];

const show = (v: unknown) => (typeof v === "object" && v !== null ? JSON.stringify(v) : String(v));
const FIELDS = new Set(["name", "description", "min_favorable", "archived", "username", "disabled", "type"]);
// Detail keys the target column already shows by name.
const SHOWN = new Set(["actor_id", "role_id", "name"]);

/**
 * Audit events, newest first. Everything is rendered as text. Ids are shown by name where the caller can resolve
 * them: `names` maps actor ids; roles and policies come from their lists (an empty list for a project manager, who
 * then sees ids). The raw target id stays in the cell's title.
 */
export function AuditTable({ items, names, byDay, initialOpen, linkTo }: {
  items: Event[];
  names: Map<string, string>;
  /** Insert a heading row whenever the (local) day changes. */
  byDay?: boolean;
  /** The event shown expanded at first (a shared link). */
  initialOpen?: string | null;
  /** The shareable link of one event; when given, a row expands to its full detail and that link. */
  linkTo?: (e: Event) => string;
}) {
  const t = useTranslations("manage.audit");
  const ta = useTranslations("admin_ui.audit");
  const [openId, setOpenId] = useState<string | null>(initialOpen ?? null);
  const format = useFormatter();
  const day = (at: string) => format.dateTime(new Date(at), { dateStyle: "full" });
  const roles = new Map(useRoles().data?.items.map((r) => [r.id, r.name]));
  const policies = new Map(usePolicies().data?.items.map((p) => [p.id, p.name]));
  const str = (v: unknown) => (typeof v === "string" ? v : "");
  const actor = (id: string) => names.get(id) ?? id;
  const target = (e: Event) => {
    const d = e.detail;
    switch (e.target_type) {
      case "actor": return actor(e.target_id);
      case "assignment": return d.actor_id ? `${actor(str(d.actor_id))} · ${roles.get(str(d.role_id)) ?? str(d.role_id)}` : e.target_id;
      case "credential": return d.actor_id ? `${actor(str(d.actor_id))}${d.label ? ` · ${show(d.label)}` : ""}` : e.target_id;
      case "role": return str(d.name) || (roles.get(e.target_id) ?? e.target_id);
      case "policy": return str(d.name) || (policies.get(e.target_id) ?? e.target_id);
      default: return e.target_id;
    }
  };
  const value = (k: string, v: unknown) => {
    // Changed fields by their names in the reader's language, not as JSON (spec 2026-11-23-ui-polish A3).
    if (k === "fields" && Array.isArray(v)) return v.map((f) => (FIELDS.has(String(f)) ? ta(`field.${f}`) : String(f))).join(", ");
    if (k === "policy_ids" && Array.isArray(v)) return v.map((id) => policies.get(String(id)) ?? id).join(", ");
    if (typeof v === "boolean") return t(v ? "yes" : "no");
    // Other ids (token, contribution) only help to trace a row: a prefix is enough, the full id is in the title.
    if (k.endsWith("_id") && typeof v === "string" && v.length > 12) return `${v.slice(0, 8)}…`;
    if (k.endsWith("_at") && typeof v === "string") return format.dateTime(new Date(v), { dateStyle: "medium" });
    return show(v);
  };
  return (
    <div className="overflow-x-auto rounded-lg border">
      {/* Below lg each event is a card (CSS only): action and target first, then time · who · project. */}
      <table className="w-full text-left text-sm max-lg:block">
        <thead className="bg-muted/50 max-lg:hidden">
          <tr className="border-b">
            {(["time", "who", "action", "target", "project"] as const).map((c) => (
              <th key={c} scope="col" className="p-2 font-medium">{t(`col.${c}`)}</th>
            ))}
          </tr>
        </thead>
        <tbody className="max-lg:block">
          {items.map((e, i) => {
            const rest = Object.entries(e.detail).filter(([k, v]) => !SHOWN.has(k) && !(e.target_type === "credential" && k === "label") && v !== null);
            const open = openId === e.id;
            const newDay = byDay && (i === 0 || day(items[i - 1].at) !== day(e.at));
            return (
              <Fragment key={e.id}>
              {newDay && (
                <tr className="border-b bg-muted/30 max-lg:block">
                  <th colSpan={5} scope="colgroup" className="max-lg:block px-2 py-1.5 text-left text-xs font-medium text-muted-foreground">{day(e.at)}</th>
                </tr>
              )}
              <tr
                className={cn("border-b align-top last:border-0 max-lg:flex max-lg:flex-wrap max-lg:items-center max-lg:gap-x-2 max-lg:gap-y-0.5 max-lg:p-3", linkTo && "cursor-pointer hover:bg-muted/40", open && "bg-accent/40")}
                onClick={linkTo ? () => setOpenId(open ? null : e.id) : undefined}
                aria-expanded={linkTo ? open : undefined}
              >
                <td className="p-2 whitespace-nowrap max-lg:order-3 max-lg:p-0 max-lg:text-xs max-lg:text-muted-foreground">
                  {linkTo && <ChevronRightIcon className={cn("mr-1 inline size-3.5 text-muted-foreground transition-transform", open && "rotate-90")} aria-hidden />}
                  <time dateTime={e.at} title={format.dateTime(new Date(e.at), { dateStyle: "medium", timeStyle: "medium" })}>
                    {format.relativeTime(new Date(e.at))}
                  </time>
                </td>
                <td className="p-2 max-lg:order-4 max-lg:p-0 max-lg:text-xs max-lg:text-muted-foreground max-lg:before:content-['·_']">{e.actor_id === null ? t("cli") : e.actor_name}</td>
                <td className="p-2 max-lg:order-1 max-lg:basis-full max-lg:p-0 max-lg:font-medium">{t.has(`action.${e.action}`) ? t(`action.${e.action}`) : e.action}</td>
                <td className="p-2 max-lg:order-2 max-lg:basis-full max-lg:p-0" title={e.target_id}>
                  <span className="text-muted-foreground">{t.has(`target.${e.target_type}`) ? t(`target.${e.target_type}`) : e.target_type} </span>
                  <span className="break-words">{target(e)}</span>
                  {rest.length > 0 && (
                    <div className="text-xs text-muted-foreground" title={rest.map(([k, v]) => `${k}: ${show(v)}`).join("\n")}>
                      {rest.map(([k, v]) => `${t.has(`field.${k}`) ? t(`field.${k}`) : k}: ${value(k, v)}`).join(" · ")}
                    </div>
                  )}
                </td>
                <td className="p-2 max-lg:order-5 max-lg:p-0 max-lg:text-xs max-lg:text-muted-foreground max-lg:before:content-['·_'] max-lg:empty:hidden">{e.project_id ?? ""}</td>
              </tr>
              {open && linkTo && (
                <tr className="border-b bg-muted/20 max-lg:block">
                  <td colSpan={5} className="p-3 max-lg:block">
                    <div className="flex flex-col gap-2">
                      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs">
                        <dt className="text-muted-foreground">{ta("event_id")}</dt><dd className="font-mono">{e.id}</dd>
                        <dt className="text-muted-foreground">{ta("action_key")}</dt><dd className="font-mono">{e.action}</dd>
                        <dt className="text-muted-foreground">{ta("target_id")}</dt><dd className="font-mono break-all">{e.target_id}</dd>
                        <dt className="text-muted-foreground">{ta("exact_time")}</dt><dd>{format.dateTime(new Date(e.at), { dateStyle: "full", timeStyle: "long" })}</dd>
                      </dl>
                      <pre className="max-h-64 overflow-auto rounded-md bg-muted p-2 text-xs">{JSON.stringify(e.detail, null, 2)}</pre>
                      <a href={linkTo(e)} onClick={(ev) => ev.stopPropagation()} className="inline-flex items-center gap-1 self-start text-xs hover:underline">
                        <LinkIcon className="size-3" aria-hidden />{ta("permalink")}
                      </a>
                    </div>
                  </td>
                </tr>
              )}
              </Fragment>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
