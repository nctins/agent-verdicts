"use client";

import { ArrowDownIcon, ArrowUpDownIcon, ArrowUpIcon, SearchIcon, XIcon } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { useMemo, useState, type ReactNode } from "react";
import { EmptyState } from "@/components/EmptyState";
import { FilterButton, FilterChips, type FilterDef } from "@/components/FilterBar";
import { SkeletonRows } from "@/components/admin/kit";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { applyTable, readTableState, writeTableState, type Matchers } from "@/lib/table";
import { cn } from "@/lib/utils";

export type Column<T> = {
  id: string;
  header: ReactNode;
  cell: (row: T) => ReactNode;
  /** A sort key makes the header a sort button. */
  sort?: (row: T) => string | number | null;
  className?: string;
  /** Left out of the card layout on narrow screens (the first column is the card's title). */
  card?: false;
};

export type Filter<T> = {
  id: string;
  label: string;
  options: { value: string; label: string }[];
  match: (row: T, value: string) => boolean;
};

/**
 * The admin list (spec 2026-11-22-admin-ui-redesign U3): search, filter chips, sortable columns, row links,
 * a "⋯" menu per row, selection with bulk actions, paging. Search, filters, sort and page live in the URL
 * (under `prefix`), so a filtered list can be shared. Rows are filtered in the browser.
 */
export function DataTable<T>({
  rows, columns, rowKey, rowHref, rowMenu, text, filters = [], pageSize = 25, prefix = "", searchLabel,
  selectable, bulk, empty, toolbar, caption, onSearch,
}: {
  rows: T[] | undefined;
  columns: Column<T>[];
  rowKey: (row: T) => string;
  rowHref?: (row: T) => string;
  rowMenu?: (row: T) => ReactNode;
  text?: (row: T) => string;
  filters?: Filter<T>[];
  pageSize?: number;
  prefix?: string;
  searchLabel?: string;
  selectable?: boolean;
  /** The bar shown while rows are selected; `clear` empties the selection. */
  bulk?: (selected: T[], clear: () => void) => ReactNode;
  empty: string;
  toolbar?: ReactNode;
  caption: string;
  /** Told the search text as it changes (e.g. to also search on the server). */
  onSearch?: (q: string) => void;
}) {
  const t = useTranslations("kit.table");
  const router = useRouter();
  const path = usePathname();
  const sp = useSearchParams();
  // The URL seeds the state and mirrors every change, so a filtered list can be shared or reloaded.
  const [url, setUrl] = useState(() => new URLSearchParams(sp.toString()));
  const state = readTableState(url, filters.map((f) => f.id), prefix);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const set = (changes: Record<string, string | null>) => {
    const next = writeTableState(url, changes, prefix);
    setUrl(next);
    if ("q" in changes) onSearch?.(changes.q ?? "");
    router.replace(path + (next.size ? "?" + next : ""), { scroll: false });
  };
  const matchers: Matchers<T> = useMemo(
    () => ({
      text,
      filters: Object.fromEntries(filters.map((f) => [f.id, f.match])),
      sorts: Object.fromEntries(columns.filter((c) => c.sort).map((c) => [c.id, c.sort!])),
    }),
    [text, filters, columns],
  );
  if (!rows) return <SkeletonRows />;
  const r = applyTable(rows, state, matchers, pageSize);
  const active = !!state.q || Object.keys(state.filters).length > 0;
  const selected = r.matched.filter((x) => picked.has(rowKey(x)));
  const pageKeys = r.rows.map(rowKey);
  const allOnPage = pageKeys.length > 0 && pageKeys.every((k) => picked.has(k));
  const toggle = (keys: string[], on: boolean) => {
    const next = new Set(picked);
    for (const k of keys) if (on) next.add(k); else next.delete(k);
    setPicked(next);
  };
  const open = (row: T) => rowHref && router.push(rowHref(row));
  const defs: FilterDef[] = filters.map((f) => ({ id: f.id, label: f.label, options: f.options, value: state.filters[f.id] ?? null }));
  const first = (r.page - 1) * pageSize + 1;

  const sortHeader = (c: Column<T>) => {
    if (!c.sort) return c.header;
    const on = state.sort === c.id;
    const Icon = !on ? ArrowUpDownIcon : state.dir === "asc" ? ArrowUpIcon : ArrowDownIcon;
    return (
      <button
        type="button"
        className="inline-flex items-center gap-1 hover:text-foreground"
        onClick={() => set({ sort: c.id, dir: on && state.dir === "asc" ? "desc" : "asc" })}
      >
        {c.header}
        <Icon className={cn("size-3.5", !on && "opacity-40")} aria-hidden />
      </button>
    );
  };

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        {text && (
          <div className="relative">
            <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden />
            <Input
              type="search"
              aria-label={searchLabel ?? t("search")}
              placeholder={searchLabel ?? t("search")}
              value={state.q}
              onChange={(e) => set({ q: e.target.value })}
              className="h-8 w-60 pl-8"
            />
          </div>
        )}
        <FilterButton filters={defs} onChange={(id, v) => set({ [id]: v })} />
        {state.q && !Object.keys(state.filters).length && (
          <Button variant="ghost" size="sm" onClick={() => set({ q: null })}>
            <XIcon aria-hidden />
            {t("clear")}
          </Button>
        )}
        <div className="ml-auto flex items-center gap-2">{toolbar}</div>
      </div>
      <FilterChips filters={defs} onChange={(id, v) => set({ [id]: v })} onClear={() => set({ q: null, ...Object.fromEntries(filters.map((f) => [f.id, null])) })} />

      {selectable && selected.length > 0 && bulk && (
        <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-accent/50 px-3 py-2 text-sm" role="region" aria-label={t("bulk")}>
          <span className="font-medium">{t("selected", { count: selected.length })}</span>
          {bulk(selected, () => setPicked(new Set()))}
          <Button variant="ghost" size="sm" className="ml-auto" onClick={() => setPicked(new Set())}>{t("clear_selection")}</Button>
        </div>
      )}

      {r.total === 0 ? (
        <EmptyState title={active ? t("no_match") : empty} />
      ) : (
        <>
          {/* One table; below md each row becomes a card (CSS only, so the DOM and screen readers see one list). */}
          {/* overflow-clip (not hidden) rounds the corners without becoming a scroll box, so the header sticks to the page. */}
          <div className="md:overflow-clip md:rounded-xl md:border">
            <table className="w-full text-left text-sm max-md:block">
              <caption className="sr-only">{caption}</caption>
              <thead className="sticky top-14 z-10 bg-muted/60 text-muted-foreground backdrop-blur max-md:hidden">
                <tr className="border-b">
                  {selectable && (
                    <th scope="col" className="w-10 p-3">
                      <input type="checkbox" className="accent-primary" aria-label={t("select_page")} checked={allOnPage} onChange={(e) => toggle(pageKeys, e.target.checked)} />
                    </th>
                  )}
                  {columns.map((c) => (
                    <th
                      key={c.id}
                      scope="col"
                      className={cn("p-3 text-xs font-medium", c.className)}
                      aria-sort={state.sort === c.id ? (state.dir === "asc" ? "ascending" : "descending") : undefined}
                    >
                      {sortHeader(c)}
                    </th>
                  ))}
                  {rowMenu && <th scope="col" className="w-12 p-3"><span className="sr-only">{t("actions")}</span></th>}
                </tr>
              </thead>
              <tbody className="max-md:flex max-md:flex-col max-md:gap-2">
                {r.rows.map((row) => {
                  const k = rowKey(row);
                  return (
                    <tr
                      key={k}
                      onClick={() => open(row)}
                      className={cn(
                        "border-b align-middle transition-colors last:border-0 hover:bg-muted/40",
                        "max-md:relative max-md:flex max-md:flex-col max-md:gap-1 max-md:rounded-xl max-md:border max-md:bg-card max-md:p-3 max-md:last:border",
                        rowHref && "cursor-pointer",
                        picked.has(k) && "bg-accent/40",
                      )}
                    >
                      {selectable && (
                        <td className="p-3 max-md:absolute max-md:top-3 max-md:right-12 max-md:p-0" onClick={(e) => e.stopPropagation()}>
                          <input type="checkbox" className="accent-primary" aria-label={t("select_row")} checked={picked.has(k)} onChange={(e) => toggle([k], e.target.checked)} />
                        </td>
                      )}
                      {columns.map((c, i) => (
                        <td
                          key={c.id}
                          data-label={i > 0 && typeof c.header === "string" ? c.header : undefined}
                          className={cn(
                            "p-3 max-md:p-0",
                            i > 0 && "max-md:flex max-md:gap-2 max-md:before:min-w-28 max-md:before:text-muted-foreground max-md:before:content-[attr(data-label)]",
                            c.card === false && "max-md:empty:hidden",
                            c.className,
                          )}
                        >
                          {c.cell(row)}
                        </td>
                      ))}
                      {rowMenu && <td className="p-3 text-right max-md:absolute max-md:top-2 max-md:right-2 max-md:p-0" onClick={(e) => e.stopPropagation()}>{rowMenu(row)}</td>}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <div className="flex items-center justify-between gap-2 text-sm text-muted-foreground">
            <span>{t("range", { from: first, to: first + r.rows.length - 1, total: r.total })}</span>
            {r.pages > 1 && (
              <div className="flex items-center gap-2">
                <Button variant="outline" size="sm" disabled={r.page <= 1} onClick={() => set({ page: String(r.page - 1) })}>{t("prev")}</Button>
                <span>{t("page", { page: r.page, pages: r.pages })}</span>
                <Button variant="outline" size="sm" disabled={r.page >= r.pages} onClick={() => set({ page: String(r.page + 1) })}>{t("next")}</Button>
              </div>
            )}
          </div>
        </>
      )}
    </div>
  );
}
