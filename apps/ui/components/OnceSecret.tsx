"use client";

import { useTranslations } from "next-intl";
import { useId, useRef, useState } from "react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { copyText } from "@/lib/clipboard";

/** Shows a password/token once. The value lives only in the caller's state; it never enters the query cache. */
export function OnceSecret({ label, value, onDone }: { label: string; value: string; onDone: () => void }) {
  const t = useTranslations("admin.once");
  const id = useId();
  const ref = useRef<HTMLInputElement>(null);
  const [copy, setCopy] = useState<"idle" | "ok" | "failed">("idle");

  const doCopy = async () => {
    try {
      await copyText(value);
      setCopy("ok");
    } catch {
      ref.current?.select();
      setCopy("failed");
    }
  };

  return (
    <Alert role="status" className="flex flex-col gap-3">
      <AlertDescription>{t("warning")}</AlertDescription>
      <Label htmlFor={id}>{label}</Label>
      <div className="flex gap-2">
        <Input id={id} ref={ref} readOnly value={value} className="font-mono" onFocus={(e) => e.currentTarget.select()} />
        <Button type="button" variant="outline" onClick={doCopy}>{t("copy")}</Button>
      </div>
      {copy === "ok" && <p className="text-sm">{t("copied")}</p>}
      {copy === "failed" && <p className="text-sm" role="alert">{t("copy_failed")}</p>}
      <Button type="button" onClick={onDone} className="self-start">{t("done")}</Button>
    </Alert>
  );
}
