"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { useSyncExternalStore } from "react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { ApiError } from "@/lib/api/errors";

export function NetworkBanner() {
  const t = useTranslations("errors");
  const cache = useQueryClient().getQueryCache();
  const down = useSyncExternalStore(
    (cb) => cache.subscribe(cb),
    () => cache.getAll().some((q) => q.getObserversCount() > 0 && q.state.error instanceof ApiError && q.state.error.code === "network"),
    () => false,
  );
  if (!down) return null;
  return (
    <Alert variant="destructive" role="status" className="mx-auto max-w-5xl">
      <AlertDescription>{t("network_retrying")}</AlertDescription>
    </Alert>
  );
}
