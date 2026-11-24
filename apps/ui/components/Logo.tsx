import { cn } from "@/lib/utils";

/** The product mark: a speech bubble holding a check, for claims that get a verdict (spec 2026-11-23-ui-polish G2). */
export function Logo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" aria-hidden className={cn("size-7 shrink-0", className)}>
      <rect width="32" height="32" rx="8" className="fill-primary" />
      <path d="M8 10.5A2.5 2.5 0 0 1 10.5 8h11A2.5 2.5 0 0 1 24 10.5v8a2.5 2.5 0 0 1-2.5 2.5H15l-4.5 3.5V21h0A2.5 2.5 0 0 1 8 18.5z" className="fill-primary-foreground/95" />
      <path d="m12.5 14.5 2.5 2.5 4.5-5" fill="none" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" className="stroke-primary" />
    </svg>
  );
}
