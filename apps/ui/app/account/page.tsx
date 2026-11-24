"use client";

import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { useState, type FormEvent } from "react";
import { toast } from "sonner";
import { AppShell } from "@/components/AppShell";
import { SessionGate } from "@/components/SessionGate";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ApiError } from "@/lib/api/errors";
import { isAdmin } from "@/lib/access";
import { useChangePassword, useSession } from "@/lib/api/queries";

function AccountForm() {
  const t = useTranslations("auth.account");
  const te = useTranslations("errors");
  const router = useRouter();
  const { data: s } = useSession();
  const change = useChangePassword();
  const [cur, setCur] = useState("");
  const [pw, setPw] = useState("");
  const [again, setAgain] = useState("");
  const [local, setLocal] = useState<string | null>(null);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (pw.length < 10) return setLocal(t("error.too_short"));
    if (pw.length > 128) return setLocal(t("error.too_long"));
    if (pw !== again) return setLocal(t("error.mismatch"));
    setLocal(null);
    change.mutate(
      { current_password: cur, new_password: pw },
      {
        onSuccess: () => {
          toast.success(t("saved"));
          router.replace("/");
        },
      },
    );
  };
  const err = change.error;
  const msg =
    local ??
    (!err
      ? null
      : err instanceof ApiError && err.status === 400
        ? t("error.current_wrong")
        : te(err instanceof ApiError ? err.code : "internal"));

  return (
    <div className="mx-auto flex max-w-sm flex-col gap-4">
      <h1 className="text-xl font-semibold">{t("heading")}</h1>
      <p className="flex items-center gap-2">
        <span>{s?.actor?.name}</span>
        {isAdmin(s) && <Badge variant="secondary">{t("admin")}</Badge>}
      </p>
      {s?.must_change_password && (
        <Alert role="status">
          <AlertTitle>{t("must_change.title")}</AlertTitle>
          <AlertDescription>{t("must_change.hint")}</AlertDescription>
        </Alert>
      )}
      <form onSubmit={submit} className="flex flex-col gap-4">
        <h2 className="font-medium">{t("change_password")}</h2>
        {msg && (
          <Alert variant="destructive" role="alert"><AlertDescription>{msg}</AlertDescription></Alert>
        )}
        <div className="flex flex-col gap-2">
          <Label htmlFor="cur">{t("current_password")}</Label>
          <Input id="cur" type="password" autoComplete="current-password" required value={cur} onChange={(e) => setCur(e.target.value)} />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="new">{t("new_password")}</Label>
          <Input id="new" type="password" autoComplete="new-password" required value={pw} onChange={(e) => setPw(e.target.value)} aria-describedby="new-hint" />
          <p id="new-hint" className="text-sm text-muted-foreground">{t("password_hint")}</p>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="again">{t("confirm_password")}</Label>
          <Input id="again" type="password" autoComplete="new-password" required value={again} onChange={(e) => setAgain(e.target.value)} />
        </div>
        <Button type="submit" disabled={change.isPending}>{t("save_password")}</Button>
      </form>
    </div>
  );
}

export default function AccountPage() {
  return (
    <AppShell>
      <SessionGate require="login">
        <AccountForm />
      </SessionGate>
    </AppShell>
  );
}
