"use client";

import { useTranslations } from "next-intl";
import { useState, type FormEvent } from "react";
import { toast } from "sonner";
import { AccessPicker, NO_ACCESS, type AccessChoice } from "@/components/AccessPicker";
import { giveAccess } from "@/components/ActorDialog";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetHeader } from "@/components/ui/sheet";
import { assignmentBodies } from "@/lib/access-forms";
import { useCreateAssignment } from "@/lib/api/queries";
import type { components } from "@/lib/api/schema";

type Assignment = components["schemas"]["Assignment"];

/**
 * Gives one role on a scope to one or more actors, in a side sheet (spec 2026-11-22-admin-ui-redesign U5, U4).
 * `held` (one actor only) feeds the "already covered" warning. Failures stay listed in the sheet.
 */
export function AssignSheet({ open, onOpenChange, actors, held = [] }: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  actors: { id: string; name: string }[];
  held?: Assignment[];
}) {
  const t = useTranslations();
  const x = (k: string) => t(`manage.access.${k}`);
  const create = useCreateAssignment();
  const [choice, setChoice] = useState<AccessChoice>(NO_ACCESS);
  const [failed, setFailed] = useState<string[]>([]);
  const name = new Map(actors.map((a) => [a.id, a.name]));
  const close = (o: boolean) => {
    if (!o) { setChoice(NO_ACCESS); setFailed([]); }
    onOpenChange(o);
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const scope = choice.all ? "all" : choice.projects;
    const bodies = choice.roleId ? actors.flatMap((a) => assignmentBodies({ actorId: a.id, roleId: choice.roleId, scope })) : [];
    if (bodies.length === 0) return setFailed([x("pick_one")]);
    const bad = await giveAccess(create.mutateAsync, bodies);
    setFailed(bad.map((f) => `${actors.length > 1 ? name.get(f.body.actor_id) + " · " : ""}${f.body.project_id ?? x("all_projects")}: ${t(`errors.${f.code}`)}`));
    if (bad.length < bodies.length) toast.success(x("saved"));
    if (bad.length === 0) close(false);
  };
  return (
    <Sheet open={open} onOpenChange={close}>
      <SheetContent>
        <SheetHeader
          title={t("manage.people.add_role")}
          description={actors.length === 1 ? actors[0].name : t("admin_ui.assign_many", { count: actors.length })}
        />
        <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
          {failed.map((m) => <Alert key={m} variant="destructive" role="alert"><AlertDescription>{m}</AlertDescription></Alert>)}
          <AccessPicker idPrefix="sheet-access" value={choice} onChange={(v) => { setChoice(v); setFailed([]); }} held={actors.length === 1 ? held : []} />
          <div className="flex gap-2">
            <Button type="submit" disabled={create.isPending}>{t("manage.people.add_role")}</Button>
            <Button type="button" variant="outline" onClick={() => close(false)}>{t("common.cancel")}</Button>
          </div>
        </form>
      </SheetContent>
    </Sheet>
  );
}
