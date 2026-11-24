"use client";

import { useTranslations } from "next-intl";
import type { TrustState } from "@/lib/trust";

// Bar order (good → bad, unknown last); also the order of the label.
export const STATE_FILL: Record<TrustState, string> = {
  Favorable: "bg-favorable",
  Conflict: "bg-conflict",
  Unfavorable: "bg-unfavorable",
  InsufficientEvidence: "bg-muted-foreground/30",
};

/** Claims per trust state as one stacked bar; the numbers are in its label (and in the tiles/legend beside it). */
export function StateBar({ states }: { states: Record<TrustState, number> }) {
  const t = useTranslations();
  const order = Object.keys(STATE_FILL) as TrustState[];
  const total = order.reduce((n, s) => n + states[s], 0);
  const label = order.map((s) => `${t(`state.${s}`)}: ${states[s]}`).join(", ");
  return (
    <div role="img" aria-label={label} title={label} className="flex h-1.5 w-full overflow-hidden rounded-full bg-muted">
      {total > 0 &&
        order.map((s) =>
          states[s] ? <div key={s} className={STATE_FILL[s]} style={{ width: `${(states[s] / total) * 100}%` }} /> : null,
        )}
    </div>
  );
}
