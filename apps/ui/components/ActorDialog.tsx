"use client";

import { useTranslations } from "next-intl";
import { useState, type FormEvent } from "react";
import { AccessPicker, NO_ACCESS, type AccessChoice } from "@/components/AccessPicker";
import { OnceSecret } from "@/components/OnceSecret";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { managesAny } from "@/lib/access";
import { assignmentBodies, type AssignmentBody } from "@/lib/access-forms";
import { actorBody } from "@/lib/admin-forms";
import { ApiError, isNameTaken } from "@/lib/api/errors";
import { useCreateActor, useCreateAssignment, useSession } from "@/lib/api/queries";

const choiceCls =
  "flex cursor-pointer items-center gap-2 rounded-md border px-3 py-1.5 text-sm has-checked:border-primary has-checked:bg-accent has-focus-visible:ring-[3px] has-focus-visible:ring-ring/50";

/** Sends bodies one by one; returns the ones that failed with their error code. */
export async function giveAccess(send: (b: AssignmentBody) => Promise<unknown>, bodies: AssignmentBody[]) {
  const failed: { body: AssignmentBody; code: string }[] = [];
  for (const b of bodies) {
    try {
      await send(b);
    } catch (e) {
      failed.push({ body: b, code: e instanceof ApiError ? e.code : "network" });
    }
  }
  return failed;
}

type Result = { secret: { label: string; value: string } | null; failed: { body: AssignmentBody; code: string }[] };

/** Add an actor and, in the same step, its first role. */
export function ActorDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const t = useTranslations();
  const p = (k: string) => t(`manage.people.${k}`);
  const session = useSession().data;
  const canAssign = managesAny(session);
  const create = useCreateActor();
  const assign = useCreateAssignment();
  const [type, setType] = useState<"Human" | "Agent">("Human");
  const [name, setName] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [mustChange, setMustChange] = useState<boolean | null>(null);
  const firstAccess: AccessChoice = { ...NO_ACCESS, roleId: "contributor" };
  const [access, setAccess] = useState<AccessChoice>(firstAccess);
  const [pickErr, setPickErr] = useState(false);
  const [later, setLater] = useState(false);
  const [result, setResult] = useState<Result | null>(null);
  const human = type === "Human";

  const reset = () => {
    setType("Human"); setName(""); setUsername(""); setPassword(""); setMustChange(null);
    setAccess(firstAccess); setPickErr(false); setLater(false); setResult(null);
    create.reset();
  };
  const close = (o: boolean) => {
    if (!o) reset();
    onOpenChange(o);
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    // A role on no project would create an actor with no access and say nothing (spec 2026-11-22-safer-role-assignment D2).
    const noTarget = canAssign && !later && (!access.roleId || (!access.all && access.projects.length === 0));
    setPickErr(noTarget);
    if (noTarget) return;
    let r;
    try {
      r = await create.mutateAsync(actorBody({ type, name, username, password, mustChange }));
    } catch {
      return; // shown from create.error
    }
    const value = r.password ?? r.token;
    const secret = value ? { label: t(r.token ? "admin.actor.created_token" : "admin.actor.created_password"), value } : null;
    create.reset(); // the secret now lives only in this component's state
    const bodies = canAssign && !later && access.roleId ? assignmentBodies({ actorId: r.id, roleId: access.roleId, scope: access.all ? "all" : access.projects }) : [];
    setResult({ secret, failed: await giveAccess(assign.mutateAsync, bodies) });
  };
  const retry = async () => {
    if (!result) return;
    setResult({ ...result, failed: await giveAccess(assign.mutateAsync, result.failed.map((f) => f.body)) });
  };

  const err = create.error instanceof ApiError ? create.error : null;
  const nameTaken = isNameTaken(err);
  const errMsg = !err || nameTaken ? null : err.status === 409 ? t("manage.actor.error.username_taken") : t(`errors.${err.code}`);

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{p("dialog_title")}</DialogTitle>
        </DialogHeader>
        {result ? (
          <div className="flex flex-col gap-3">
            {result.failed.length > 0 && (
              <Alert variant="destructive" role="alert">
                <AlertTitle>{p("assign_failed_title")}</AlertTitle>
                <AlertDescription>
                  {result.failed.map((f) => `${f.body.project_id ?? p("all_projects")}: ${t(`errors.${f.code}`)}`).join(" · ")}
                </AlertDescription>
                <Button type="button" size="sm" variant="outline" className="mt-2 self-start" disabled={assign.isPending} onClick={retry}>
                  {p("retry")}
                </Button>
              </Alert>
            )}
            {result.secret ? (
              <OnceSecret {...result.secret} onDone={() => close(false)} />
            ) : (
              <Button type="button" className="self-start" onClick={() => close(false)}>{p("close")}</Button>
            )}
          </div>
        ) : (
          <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
            {errMsg && <Alert variant="destructive" role="alert"><AlertDescription>{errMsg}</AlertDescription></Alert>}
            <fieldset className="flex flex-col gap-3">
              <legend className="mb-1 text-sm font-medium">{p("details")}</legend>
              <div className="flex gap-2">
                {(["Human", "Agent"] as const).map((v) => (
                  <label key={v} className={choiceCls}>
                    <input type="radio" name="actor-type" value={v} checked={type === v} onChange={() => setType(v)} className="accent-primary" />
                    {t(`manage.actor.option.${v === "Human" ? "human" : "agent"}`)}
                  </label>
                ))}
              </div>
              <div className="flex flex-col gap-1">
                <Label htmlFor="actor-name">{t("manage.actor.label.name")}</Label>
                <Input
                  id="actor-name" required maxLength={64} value={name} onChange={(e) => setName(e.target.value)}
                  aria-invalid={nameTaken} aria-describedby={nameTaken ? "actor-name-err" : undefined}
                />
                {nameTaken && <p id="actor-name-err" role="alert" className="text-sm text-destructive">{t("manage.actor.error.name_taken")}</p>}
              </div>
              {human && (
                <>
                  <div className="flex flex-col gap-1">
                    <Label htmlFor="actor-username">{t("admin.field.username")}</Label>
                    <Input id="actor-username" required autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
                  </div>
                  <div className="flex flex-col gap-1">
                    <Label htmlFor="actor-password">{t("manage.actor.label.password")}</Label>
                    <Input id="actor-password" type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} aria-describedby="actor-password-hint" />
                    <p id="actor-password-hint" className="text-sm text-muted-foreground">{t("manage.actor.hint.password")}</p>
                  </div>
                  <div className="flex items-center gap-2">
                    <Checkbox id="actor-must-change" checked={mustChange ?? password === ""} onCheckedChange={(c) => setMustChange(c === true)} />
                    <Label htmlFor="actor-must-change">{t("manage.actor.label.must_change")}</Label>
                  </div>
                </>
              )}
              {!human && <p className="text-sm text-muted-foreground">{t("manage.credential.first_expiry")}</p>}
            </fieldset>
            {canAssign && (
              <fieldset className="flex flex-col gap-3">
                <legend className="mb-1 text-sm font-medium">{p("initial_access")}</legend>
                <label className="flex items-center gap-2 text-sm">
                  <input type="checkbox" checked={later} onChange={(e) => setLater(e.target.checked)} className="accent-primary" />
                  {p("later")}
                </label>
                {!later && <AccessPicker idPrefix="new-actor" value={access} onChange={(v) => { setAccess(v); setPickErr(false); }} />}
                {!later && pickErr && <p role="alert" className="text-sm text-destructive">{t("manage.access.pick_one")}</p>}
              </fieldset>
            )}
            <Button type="submit" disabled={create.isPending || assign.isPending} className="self-start">{p("create")}</Button>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
