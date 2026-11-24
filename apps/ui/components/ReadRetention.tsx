"use client";

import { useFormatter, useTranslations } from "next-intl";
import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ApiError, errorMessageKey } from "@/lib/api/errors";
import { useReadRetention, useSetReadRetention } from "@/lib/api/queries";

export const MIN_DETAIL_DAYS = 30;
export const MAX_DETAIL_DAYS = 3650;

/** Bytes as the largest unit that keeps the number at least 1 (kB, MB, GB). */
function sizeUnit(bytes: number): [number, "kilobyte" | "megabyte" | "gigabyte"] {
  if (bytes >= 1e9) return [bytes / 1e9, "gigabyte"];
  if (bytes >= 1e6) return [bytes / 1e6, "megabyte"];
  return [bytes / 1e3, "kilobyte"];
}

/** A project manager's setting: how many days of per-actor read detail the server keeps (spec 2026-11-21-claim-read-counts §5.3). */
export function ReadRetention({ project }: { project: string }) {
  const t = useTranslations();
  const format = useFormatter();
  const current = useReadRetention(project, true).data?.detail_days;
  const save = useSetReadRetention(project);
  const [draft, setDraft] = useState<string | null>(null);
  const [invalid, setInvalid] = useState(false);
  const value = draft ?? (current === undefined ? "" : String(current));

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const n = Number(value);
    const ok = Number.isInteger(n) && n >= MIN_DETAIL_DAYS && n <= MAX_DETAIL_DAYS;
    setInvalid(!ok);
    if (ok) save.mutate({ detail_days: n }, { onSuccess: () => { setDraft(null); toast.success(t("project.reads.saved")); } });
  };
  const result = save.data;
  const [size, unit] = sizeUnit(result?.estimated_bytes ?? 0);

  return (
    <section aria-labelledby="read-retention" className="flex flex-col gap-3 rounded-xl border bg-card p-5">
      <h2 id="read-retention" className="font-semibold">{t("project.reads.heading")}</h2>
      <form onSubmit={submit} noValidate className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <Label htmlFor="read-detail-days">{t("project.reads.days_label")}</Label>
          <Input
            id="read-detail-days"
            type="number"
            min={MIN_DETAIL_DAYS}
            max={MAX_DETAIL_DAYS}
            step={1}
            className="w-32"
            value={value}
            onChange={(e) => setDraft(e.target.value)}
            aria-invalid={invalid || undefined}
            aria-describedby="read-detail-note"
          />
        </div>
        <p id="read-detail-note" className="text-sm text-muted-foreground">{t("project.reads.note")}</p>
        {/* Every settings block ends with its button, bottom right. */}
        <div className="flex justify-end">
          <Button type="submit" disabled={save.isPending}>{t("project.reads.save")}</Button>
        </div>
      </form>
      {invalid && <p role="alert" className="text-sm text-destructive">{t("project.reads.invalid")}</p>}
      {save.error && (
        <p role="alert" className="text-sm text-destructive">
          {t(errorMessageKey(save.error instanceof ApiError ? save.error.code : "network"))}
        </p>
      )}
      {result && !invalid && (
        <p role="status" className="text-sm">
          {t("project.reads.estimate", {
            label: format.number(size, { style: "unit", unit, maximumFractionDigits: 1 }),
            count: result.rows_per_day,
          })}
        </p>
      )}
    </section>
  );
}
