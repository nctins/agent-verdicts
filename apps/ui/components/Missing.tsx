"use client";

import Link from "next/link";
import { useTranslations } from "next-intl";
import { EmptyState } from "@/components/EmptyState";
import { Button } from "@/components/ui/button";

/** Not-found state with a way home. */
export function Missing() {
  const t = useTranslations("errors");
  return <EmptyState title={t("not_found")} action={<Button asChild><Link href="/">{t("back_home")}</Link></Button>} />;
}
