"use client";

import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { useState, type FormEvent } from "react";
import { AuthShell } from "@/components/AuthShell";
import { SessionGate } from "@/components/SessionGate";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ApiError, isNameTaken } from "@/lib/api/errors";
import { useSetup } from "@/lib/api/queries";

function SetupForm() {
  const t = useTranslations();
  const router = useRouter();
  const setup = useSetup();
  const [f, setF] = useState({ code: "", name: "", username: "", password: "" });
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    // On success the refreshed session makes SessionGate leave /setup/ (for /manage/).
    setup.mutate(f, {
      onError: (er) => {
        if (er instanceof ApiError && er.status === 404) router.replace("/");
      },
    });
  };
  const err = setup.error;
  const nameTaken = isNameTaken(err);
  const msg = !err || nameTaken
    ? null
    : err instanceof ApiError && err.status === 403
      ? t("auth.setup.error.code")
      : t(`errors.${err instanceof ApiError ? err.code : "internal"}`);

  const fields = [
    ["code", "auth.setup.label.code", "text", "off"],
    ["name", "auth.setup.label.name", "text", "name"],
    ["username", "auth.setup.label.username", "text", "username"],
    ["password", "auth.setup.label.password", "password", "new-password"],
  ] as const;

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">{t("auth.setup.heading")}</h1>
      <p className="text-muted-foreground">{t("auth.setup.intro")}</p>
      {msg && (
        <Alert variant="destructive" role="alert"><AlertDescription>{msg}</AlertDescription></Alert>
      )}
      {fields.map(([k, label, type, ac]) => (
        <div key={k} className="flex flex-col gap-2">
          <Label htmlFor={k}>{t(label)}</Label>
          <Input
            id={k}
            type={type}
            autoComplete={ac}
            required
            minLength={k === "password" ? 10 : undefined}
            value={f[k]}
            onChange={set(k)}
            aria-invalid={k === "name" && nameTaken}
            aria-describedby={k === "code" ? "code-hint" : k === "name" && nameTaken ? "name-err" : undefined}
          />
          {k === "name" && nameTaken && <p id="name-err" role="alert" className="text-sm text-destructive">{t("manage.actor.error.name_taken")}</p>}
          {k === "code" && (
            <p id="code-hint" className="text-sm text-muted-foreground">
              {t("auth.setup.hint.code")} {t("auth.setup.hint.command")}
            </p>
          )}
        </div>
      ))}
      <Button type="submit" disabled={setup.isPending}>{t("auth.setup.submit")}</Button>
    </form>
  );
}

export default function SetupPage() {
  return (
    <AuthShell>
      <SessionGate require="none">
        <SetupForm />
      </SessionGate>
    </AuthShell>
  );
}
