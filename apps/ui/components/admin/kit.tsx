"use client";

import { CheckIcon, CopyIcon, MoreHorizontalIcon, type LucideIcon } from "lucide-react";
import Link from "next/link";
import { useFormatter, useTranslations } from "next-intl";
import { useState, type FormEvent, type ReactNode } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";
import { copyText } from "@/lib/clipboard";

/** Title, one line of context, and the page's main action on the right (spec 2026-11-22-admin-ui-redesign U2). */
export function PageHeader({ title, description, actions, meta, className }: { title: ReactNode; description?: ReactNode; actions?: ReactNode; meta?: ReactNode; className?: string }) {
  return (
    <div className={cn("mb-6 flex flex-wrap items-start justify-between gap-3", className)}>
      <div className="flex min-w-0 flex-col gap-1">
        <h1 className="flex flex-wrap items-center gap-2 text-2xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="text-sm text-muted-foreground">{description}</p>}
        {meta && <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">{meta}</div>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}

/**
 * One number with its label (spec 2026-11-23-ui-polish G4): the number first, the label under it,
 * an optional icon. A link or a button when it leads somewhere.
 */
export function StatCard({ label, value, tone, icon: Icon, href, onClick }: {
  label: string;
  value: ReactNode;
  tone?: string;
  icon?: LucideIcon;
  href?: string;
  onClick?: () => void;
}) {
  const cls = "flex min-w-0 flex-col gap-1 rounded-xl border bg-card p-4 text-left";
  const live = "transition-colors hover:bg-accent/50 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none";
  const body = (
    <>
      <span className="flex items-start justify-between gap-2">
        <span className={cn("text-2xl font-semibold tracking-tight tabular-nums", tone)}>{value}</span>
        {Icon && <Icon className="size-4 text-muted-foreground" aria-hidden />}
      </span>
      <span className="truncate text-xs text-muted-foreground sm:text-sm">{label}</span>
    </>
  );
  if (href) return <Link href={href} className={cn(cls, live)}>{body}</Link>;
  if (onClick) return <button type="button" onClick={onClick} className={cn(cls, live)}>{body}</button>;
  return <div className={cls}>{body}</div>;
}

/** A titled block of a settings-style page. */
export function Section({ title, description, children, className, id }: { title: ReactNode; description?: ReactNode; children: ReactNode; className?: string; id?: string }) {
  return (
    <section aria-labelledby={id} className={cn("flex flex-col gap-3 rounded-xl border bg-card p-5", className)}>
      <div className="flex flex-col gap-1">
        <h2 id={id} className="font-semibold">{title}</h2>
        {description && <p className="text-sm text-muted-foreground">{description}</p>}
      </div>
      {children}
    </section>
  );
}

/** The "⋯" menu of one row: its actions, out of the way until asked for. */
export function RowMenu({ label, children }: { label: string; children: ReactNode }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label={label} onClick={(e) => e.stopPropagation()}>
          <MoreHorizontalIcon />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>{children}</DropdownMenuContent>
    </DropdownMenu>
  );
}

/** Copies `value`; the icon turns into a check for a moment. */
export function CopyButton({ value, label }: { value: string; label?: string }) {
  const t = useTranslations("kit");
  const [done, setDone] = useState(false);
  const copy = async (e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      await copyText(value);
      setDone(true);
      setTimeout(() => setDone(false), 1500);
    } catch {
      /* the value stays visible to copy by hand */
    }
  };
  return (
    <Button type="button" variant="ghost" size="icon-xs" onClick={copy} aria-label={`${t(done ? "copied" : "copy")}${label ? `: ${label}` : ""}`} title={t(done ? "copied" : "copy")}>
      {done ? <CheckIcon /> : <CopyIcon />}
    </Button>
  );
}

/** "3 hours ago", with the exact time on hover; `empty` when there is no time. */
export function RelTime({ at, empty = "—", className }: { at: string | null | undefined; empty?: string; className?: string }) {
  const format = useFormatter();
  const [now] = useState(() => Date.now());
  if (!at) return <span className="text-muted-foreground">{empty}</span>;
  const d = new Date(at);
  return (
    <time dateTime={at} className={className} title={format.dateTime(d, { dateStyle: "medium", timeStyle: "short" })}>
      {format.relativeTime(d, now)}
    </time>
  );
}

/** Placeholder rows while a table loads. */
export function SkeletonRows({ rows = 5 }: { rows?: number }) {
  return (
    <div className="flex flex-col gap-2" role="status" aria-busy="true">
      {Array.from({ length: rows }, (_, i) => <div key={i} className="h-10 animate-pulse rounded-md bg-muted" />)}
    </div>
  );
}

/**
 * Confirms a hard-to-undo action by typing `expected` (an id or name), as GitHub does before archiving.
 * `onConfirm` runs only when the typed text matches exactly.
 */
export function ConfirmByName({ open, onOpenChange, title, body, expected, confirmLabel, busy, onConfirm }: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  title: string;
  body: ReactNode;
  expected: string;
  confirmLabel: string;
  busy?: boolean;
  onConfirm: () => void;
}) {
  const t = useTranslations("kit");
  const [typed, setTyped] = useState("");
  const ok = typed === expected;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (ok) onConfirm();
  };
  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) setTyped(""); onOpenChange(o); }}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>{title}</DialogTitle>
            <DialogDescription>{body}</DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-2">
            <Label htmlFor="confirm-name">{t("type_to_confirm", { label: expected })}</Label>
            <Input id="confirm-name" autoComplete="off" value={typed} onChange={(e) => setTyped(e.target.value)} />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>{t("cancel")}</Button>
            <Button type="submit" variant="destructive" disabled={!ok || busy}>{confirmLabel}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** "Human" / "Agent" in the reader's language. */
export function useActorType() {
  const t = useTranslations("kit.actor_type");
  return (type: string) => (type === "Human" || type === "Agent" ? t(type) : type);
}
