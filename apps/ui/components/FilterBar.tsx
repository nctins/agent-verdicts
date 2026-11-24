"use client";

import { PlusIcon, XIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuSub, DropdownMenuSubContent,
  DropdownMenuSubTrigger, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

export type FilterDef = { id: string; label: string; options: { value: string; label: string }[]; value: string | null };

/**
 * "+ Filter" with one submenu per filter, then each active filter as a chip with ×, then "Clear filters"
 * (spec 2026-11-23-ui-polish G4: the same bar on the project page and in every admin table).
 */
export function FilterButton({ filters, onChange }: { filters: FilterDef[]; onChange: (id: string, value: string | null) => void }) {
  const t = useTranslations("search");
  if (filters.length === 0) return null;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm"><PlusIcon aria-hidden />{t("add_filter")}</Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        {filters.map((f) => (
          <DropdownMenuSub key={f.id}>
            <DropdownMenuSubTrigger>{f.label}</DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="max-h-72 overflow-y-auto">
              <DropdownMenuRadioGroup value={f.value ?? ""} onValueChange={(v) => onChange(f.id, v || null)}>
                {f.options.map((o) => <DropdownMenuRadioItem key={o.value} value={o.value}>{o.label}</DropdownMenuRadioItem>)}
              </DropdownMenuRadioGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** The active filters as removable chips, and "Clear filters"; nothing when none is set. */
export function FilterChips({ filters, onChange, onClear }: { filters: FilterDef[]; onChange: (id: string, value: string | null) => void; onClear: () => void }) {
  const t = useTranslations("search");
  const on = filters.filter((f) => f.value);
  if (on.length === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-2">
      {on.map((f) => {
        const label = `${f.label}: ${f.options.find((o) => o.value === f.value)?.label ?? f.value}`;
        return (
          <span key={f.id} className="inline-flex h-7 items-center gap-1 rounded-full border bg-accent pr-1 pl-3 text-sm">
            {label}
            <button
              type="button"
              onClick={() => onChange(f.id, null)}
              aria-label={t("remove_filter", { label })}
              className="rounded-full p-0.5 text-muted-foreground hover:bg-background hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
            >
              <XIcon className="size-3.5" aria-hidden />
            </button>
          </span>
        );
      })}
      <Button variant="ghost" size="sm" onClick={onClear}>{t("clear_filters")}</Button>
    </div>
  );
}
