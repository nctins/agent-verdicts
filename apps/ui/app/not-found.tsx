"use client";

import Link from "next/link";
import { useTranslations } from "next-intl";
import { AppShell } from "@/components/AppShell";
import { EmptyState } from "@/components/EmptyState";
import { Button } from "@/components/ui/button";

export default function NotFound() {
  const t = useTranslations("errors");
  return (
    <AppShell>
      <EmptyState
        title={t("not_found")}
        action={<Button asChild><Link href="/">{t("back_home")}</Link></Button>}
      />
    </AppShell>
  );
}
