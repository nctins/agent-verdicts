"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { Suspense, useState, type FormEvent } from "react";
import { AuthShell } from "@/components/AuthShell";
import { SessionGate } from "@/components/SessionGate";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ApiError } from "@/lib/api/errors";
import { useLogin } from "@/lib/api/queries";
import { afterLogin } from "@/lib/safe-next";

function LoginForm() {
  const t = useTranslations();
  const router = useRouter();
  const next = useSearchParams().get("next");
  const login = useLogin();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  const submit = (e: FormEvent) => {
    e.preventDefault();
    login.mutate({ username, password }, { onSuccess: (s) => router.replace(afterLogin(s, next)) });
  };
  const err = login.error;
  const msg = !err
    ? null
    : err instanceof ApiError && err.status === 401
      ? t("auth.login.error.invalid_password")
      : t(`errors.${err instanceof ApiError ? err.code : "internal"}`);

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">{t("auth.login.heading")}</h1>
      {msg && (
        <Alert variant="destructive" role="alert"><AlertDescription>{msg}</AlertDescription></Alert>
      )}
      <div className="flex flex-col gap-2">
        <Label htmlFor="username">{t("auth.login.label.username")}</Label>
        <Input id="username" autoComplete="username" required value={username} onChange={(e) => setUsername(e.target.value)} />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="password">{t("auth.login.label.password")}</Label>
        <Input id="password" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
        <p className="text-sm text-muted-foreground">{t("auth.login.password_hint")}</p>
      </div>
      <Button type="submit" disabled={login.isPending}>{t("auth.login.button.submit")}</Button>
    </form>
  );
}

export default function LoginPage() {
  return (
    <AuthShell>
      <SessionGate require="none">
        <Suspense>
          <LoginForm />
        </Suspense>
      </SessionGate>
    </AuthShell>
  );
}
