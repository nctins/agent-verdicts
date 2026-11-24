"use client";

import { useTranslations } from "next-intl";
import type { ReactNode } from "react";
import { LocaleThemeSwitch } from "@/components/LocaleThemeSwitch";
import { Logo } from "@/components/Logo";
import { NetworkBanner } from "@/components/NetworkBanner";

/** Sign-in and first-run setup: one card in the middle, no app chrome (spec 2026-11-23-ui-polish G1). */
export function AuthShell({ children }: { children: ReactNode }) {
  const t = useTranslations("auth.shell");
  return (
    <div className="flex min-h-screen flex-col bg-muted/40">
      <div className="flex justify-end p-3"><LocaleThemeSwitch /></div>
      <NetworkBanner />
      <main id="main" className="flex flex-1 flex-col items-center justify-center gap-6 px-4 pb-16">
        <div className="flex flex-col items-center gap-2 text-center">
          <Logo className="size-10" />
          <p className="text-lg font-semibold tracking-tight">Agent Verdicts</p>
          <p className="max-w-sm text-sm text-muted-foreground">{t("tagline")}</p>
        </div>
        <div className="w-full max-w-sm rounded-xl border bg-card p-6 shadow-sm">{children}</div>
      </main>
    </div>
  );
}
