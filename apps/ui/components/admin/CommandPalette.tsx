"use client";

import { SearchIcon } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { Dialog as DialogPrimitive } from "radix-ui";
import { useEffect, useState, type KeyboardEvent } from "react";
import { Button } from "@/components/ui/button";
import { can, canManage, managesAny, type Action } from "@/lib/access";
import { useActors, useProjects, useSession } from "@/lib/api/queries";
import { contributionHref, parseRoute, projectHref } from "@/lib/routes";
import { useDebouncedSearch } from "@/lib/use-debounced-search";
import { ADMIN_LINKS } from "@/lib/sections";
import { fold } from "@/lib/table";
import { cn } from "@/lib/utils";

type Item = { id: string; group: string; label: string; hint?: string; href: string };

const PER_GROUP = 6;

/** Pick at most PER_GROUP matches per group, in order; an empty query keeps the first ones. */
export function matchItems(items: Item[], q: string): Item[] {
  const want = fold(q.trim());
  const count: Record<string, number> = {};
  return items.filter((x) => {
    if (want && !fold(`${x.label} ${x.hint ?? ""}`).includes(want)) return false;
    count[x.group] = (count[x.group] ?? 0) + 1;
    return count[x.group] <= PER_GROUP;
  });
}

/**
 * ⌘K / Ctrl+K: jump to a project, and for managers an admin page or an actor (spec 2026-11-22-admin-ui-redesign U11).
 * Inside a project also: contribute, ask, and its contributions found by search (spec 2026-11-22-project-ui-redesign P19).
 */
export function CommandPalette() {
  const t = useTranslations("nav");
  const tp = useTranslations("palette");
  const router = useRouter();
  const current = parseRoute(usePathname())?.project ?? "";
  const session = useSession().data;
  const people = can(session, "actor:Manage") || managesAny(session);
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [at, setAt] = useState(0);
  const actors = useActors(undefined, open && people);
  const projects = useProjects();
  const found = useDebouncedSearch(current, open && current && q.trim().length >= 2 ? q : "", { limit: 6 });

  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const pages: Item[] = !canManage(session) ? [] : ADMIN_LINKS.filter(([, , , need]) =>
    need === "manage" ? true : need === "people" ? people : can(session, need as Action),
  ).map(([href, key]) => ({ id: href, group: tp("pages"), label: t(key), href }));
  const all: Item[] = [
    ...pages,
    ...(actors.data?.items ?? []).map((a) => ({
      id: "a" + a.id, group: tp("actors"), label: a.name, hint: a.username ? "@" + a.username : undefined,
      href: "/manage/actor/?" + new URLSearchParams({ id: a.id, name: a.name }),
    })),
    ...(projects.data?.items ?? []).map((p) => ({ id: "p" + p.id, group: tp("projects"), label: p.id, hint: p.name !== p.id ? p.name : undefined, href: projectHref(p.id) })),
  ];
  const here: Item[] = current && can(session, "contribution:Create", current)
    ? (["claim", "question"] as const).map((k) => ({
        id: "new-" + k, group: tp("here"), label: tp(k === "claim" ? "new_claim" : "new_question"),
        href: projectHref(current, new URLSearchParams({ compose: k })),
      }))
    : [];
  // Search hits are already matched by the server: listed as they come, after the rest.
  const hits: Item[] = found.settledText === q.trim() ? found.results.map((r) => ({ id: "c" + r.id, group: tp("contributions"), label: r.title, href: contributionHref(current, r.id) })) : [];
  const shown = [...matchItems([...here, ...all], q), ...hits];
  const go = (x: Item | undefined) => {
    if (!x) return;
    setOpen(false);
    router.push(x.href);
  };
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === "ArrowDown") { e.preventDefault(); setAt((i) => Math.min(i + 1, shown.length - 1)); }
    else if (e.key === "ArrowUp") { e.preventDefault(); setAt((i) => Math.max(i - 1, 0)); }
    else if (e.key === "Enter") { e.preventDefault(); go(shown[at]); }
  };

  return (
    <DialogPrimitive.Root open={open} onOpenChange={(o) => { setOpen(o); setQ(""); setAt(0); }}>
      <DialogPrimitive.Trigger asChild>
        <Button variant="outline" size="sm" className="hidden gap-2 text-muted-foreground sm:inline-flex">
          <SearchIcon aria-hidden />
          {t("command")}
          <kbd className="rounded border bg-muted px-1 font-mono text-[0.7rem]">⌘K</kbd>
        </Button>
      </DialogPrimitive.Trigger>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/20" />
        <DialogPrimitive.Content
          className="fixed top-[15vh] left-1/2 z-50 flex w-[calc(100%-2rem)] max-w-lg -translate-x-1/2 flex-col overflow-hidden rounded-xl border bg-popover text-sm shadow-2xl outline-none"
          onKeyDown={onKeyDown}
        >
          <DialogPrimitive.Title className="sr-only">{t("command")}</DialogPrimitive.Title>
          <DialogPrimitive.Description className="sr-only">{t("command_hint")}</DialogPrimitive.Description>
          <div className="flex items-center gap-2 border-b px-3">
            <SearchIcon className="size-4 text-muted-foreground" aria-hidden />
            <input
              autoFocus
              role="combobox"
              aria-expanded="true"
              aria-controls="palette-list"
              aria-activedescendant={shown[at] ? `palette-${shown[at].id}` : undefined}
              aria-label={t("command")}
              placeholder={t("command_hint")}
              value={q}
              onChange={(e) => { setQ(e.target.value); setAt(0); }}
              className="h-11 flex-1 bg-transparent outline-none"
            />
          </div>
          <ul id="palette-list" role="listbox" aria-label={t("command")} className="max-h-80 overflow-y-auto p-1">
            {shown.length === 0 && <li className="p-3 text-muted-foreground">{tp("none")}</li>}
            {shown.map((x, i) => (
              <li key={x.id} className="contents">
                {(i === 0 || shown[i - 1].group !== x.group) && (
                  <p className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground" aria-hidden>{x.group}</p>
                )}
                <div
                  id={`palette-${x.id}`}
                  role="option"
                  aria-selected={i === at}
                  onMouseEnter={() => setAt(i)}
                  onClick={() => go(x)}
                  className={cn("flex cursor-pointer items-center justify-between gap-2 rounded-md px-2 py-1.5", i === at && "bg-accent")}
                >
                  <span className="truncate">{x.label}</span>
                  {x.hint && <span className="truncate text-xs text-muted-foreground">{x.hint}</span>}
                </div>
              </li>
            ))}
          </ul>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
