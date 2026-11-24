"use client";

import { CircleCheckIcon, CircleDashedIcon, CircleHelpIcon, CircleXIcon, TriangleAlertIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { Badge } from "@/components/ui/badge";
import type { components } from "@/lib/api/schema";
import type { TrustState } from "@/lib/trust";
import { cn } from "@/lib/utils";

type QuestionStatus = components["schemas"]["QuestionStatus"];

const look = {
  Favorable: { Icon: CircleCheckIcon, cls: "border-favorable/40 bg-favorable/10 text-favorable" },
  Unfavorable: { Icon: CircleXIcon, cls: "border-unfavorable/40 bg-unfavorable/10 text-unfavorable" },
  Conflict: { Icon: TriangleAlertIcon, cls: "border-conflict/50 bg-conflict/15 text-conflict" },
  InsufficientEvidence: { Icon: CircleDashedIcon, cls: "border-border text-muted-foreground" },
} as const;

const questionLook = {
  open: { Icon: CircleHelpIcon, cls: "border-border text-foreground" },
  answered: { Icon: CircleHelpIcon, cls: "border-border text-muted-foreground" },
  resolved: { Icon: CircleCheckIcon, cls: "border-favorable/40 bg-favorable/10 text-favorable" },
} as const;

/** Always carries words (spec §5.8); colour and icon are only a hint. */
export function StateBadge({ state, questionStatus }: { state: TrustState | null; questionStatus?: QuestionStatus }) {
  const t = useTranslations();
  const [l, label] = questionStatus
    ? [questionLook[questionStatus], t(`question_status.${questionStatus}`)]
    : state
      ? [look[state], t(`state.${state}`)]
      : [null, ""];
  if (!l) return null;
  return (
    <Badge variant="outline" className={cn("font-medium", l.cls)}>
      <l.Icon aria-hidden />
      {label}
    </Badge>
  );
}
