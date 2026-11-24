"use client";

import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { contributionHref } from "@/lib/routes";
import { KindBadge } from "@/components/KindBadge";
import { StateBadge } from "@/components/StateBadge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ApiError, errorMessageKey } from "@/lib/api/errors";
import { cn } from "@/lib/utils";
import { useDebouncedSearch } from "@/lib/use-debounced-search";

/** Search-as-you-type combobox (spec §5.2). */
export function SearchBox({
  project,
  initialText = "",
  onSubmit,
  onAsk,
}: {
  project: string;
  initialText?: string;
  onSubmit: (text: string) => void;
  onAsk: (text: string) => void;
}) {
  const t = useTranslations("search");
  const router = useRouter();
  const id = useId();
  const input = useRef<HTMLInputElement>(null);
  const [text, setText] = useState(initialText);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const tAll = useTranslations();
  const { results, degraded, loading, settledText, error } = useDebouncedSearch(project, text);

  const typed = text.trim();
  // Results belong to settledText; only offer "ask" (and pick rows by keyboard) once they match the box.
  const settled = settledText !== "" && settledText === typed;
  const ask = settled && results.length === 0;
  const count = results.length + (ask ? 1 : 0);
  const show = open && typed !== "" && (count > 0 || degraded || loading || !!error);
  const sel = active < count ? active : -1;

  // "/" jumps to the box, as the placeholder promises.
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      const el = e.target as HTMLElement;
      if (e.key !== "/" || el.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName)) return;
      e.preventDefault();
      input.current?.focus();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  const choose = (i: number) => {
    setOpen(false);
    if (i < results.length) router.push(contributionHref(project, results[i].id));
    else onAsk(typed);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setOpen(true);
      setActive(e.key === "ArrowDown" ? Math.min(sel + 1, count - 1) : Math.max(sel - 1, -1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (show && sel >= 0 && settled) choose(sel);
      else {
        setOpen(false);
        onSubmit(typed);
      }
    } else if (e.key === "Escape" && show) {
      e.preventDefault();
      setOpen(false);
    }
  };

  const optionId = (i: number) => `${id}-o${i}`;
  const option = (i: number) => ({
    id: optionId(i),
    role: "option" as const,
    "aria-selected": sel === i,
    onMouseDown: (e: { preventDefault: () => void }) => e.preventDefault(), // keep focus in the input
    onClick: () => choose(i),
    onMouseMove: () => setActive(i), // not mouseenter: a list opening under a resting cursor must not pick a row
    className: cn("flex cursor-pointer flex-col gap-1 rounded-md px-2 py-1.5", sel === i && "bg-accent text-accent-foreground"),
  });

  return (
    <div role="search" className="relative">
      <Label htmlFor={id} className="sr-only">
        {t("label")}
      </Label>
      <Input
        ref={input}
        id={id}
        type="search"
        role="combobox"
        autoComplete="off"
        aria-expanded={show}
        aria-controls={`${id}-list`}
        aria-autocomplete="list"
        aria-activedescendant={show && sel >= 0 ? optionId(sel) : undefined}
        aria-busy={loading}
        placeholder={t("placeholder")}
        value={text}
        onChange={(e) => {
          setText(e.target.value);
          setActive(-1);
          setOpen(true);
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onKeyDown={onKeyDown}
      />
      {show && (
        <div className="absolute inset-x-0 top-full z-40 mt-1 rounded-lg border bg-popover p-1 text-popover-foreground shadow-md">
          {degraded && <p className="px-2 py-1 text-xs text-muted-foreground">{t("degraded")}</p>}
          {error ? (
            <p role="alert" className="px-2 py-1 text-sm text-destructive">
              {tAll(errorMessageKey(error instanceof ApiError ? error.code : "network"))}
            </p>
          ) : (
            count === 0 && <p className="px-2 py-1 text-sm text-muted-foreground">{t("searching")}</p>
          )}
          <ul id={`${id}-list`} role="listbox" aria-label={t("results_label")}>
            {results.map((r, i) => (
              <li key={r.id} {...option(i)}>
                <span className="flex items-center gap-2">
                  <KindBadge kind={r.kind} />
                  <span className="truncate font-medium">{r.title}</span>
                  <span className="ml-auto">
                    <StateBadge state={r.state} questionStatus={r.question_status} />
                  </span>
                </span>
                {r.snippet && <span className="truncate text-xs text-muted-foreground">{r.snippet}</span>}
              </li>
            ))}
            {ask && (
              <li {...option(results.length)} aria-label={t("ask_this")}>
                <span className="text-xs text-muted-foreground">{t("no_results")}</span>
                <span className="font-medium">{t("ask_this")}</span>
              </li>
            )}
          </ul>
        </div>
      )}
    </div>
  );
}
