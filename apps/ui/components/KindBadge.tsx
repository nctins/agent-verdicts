"use client";

import { useTranslations } from "next-intl";
import { Badge } from "@/components/ui/badge";
import type { components } from "@/lib/api/schema";

export function KindBadge({ kind }: { kind: components["schemas"]["ContributionKind"] }) {
  const t = useTranslations("kind");
  return <Badge variant="secondary">{t(kind)}</Badge>;
}
