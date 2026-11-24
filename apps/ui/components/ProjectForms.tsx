"use client";

import { useTranslations } from "next-intl";
import { useState, type FormEvent } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { projectIdError } from "@/lib/admin-forms";
import { ApiError } from "@/lib/api/errors";
import { useCreateProject, useUpdateProject } from "@/lib/api/queries";
import type { components } from "@/lib/api/schema";
import { projectHref } from "@/lib/routes";

type AdminProject = components["schemas"]["Project"];

// Shared by Manage → Projects and the project's own Settings tab (spec 2026-11-22-project-ui-redesign P1, P8).
/** New project, in a dialog: the id is checked as you type and the project's URL is previewed. */
export function CreateProjectDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const t = useTranslations();
  const create = useCreateProject();
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [tried, setTried] = useState(false);
  const bad = projectIdError(id);
  // An empty id is only an error once the form was sent; a malformed one shows while typing.
  const local = bad === "invalid" || (tried && bad) ? t(`admin.project.${bad}`) : null;
  const reset = () => { setId(""); setName(""); setDescription(""); setTried(false); create.reset(); };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTried(true);
    if (bad) return;
    create.mutate(
      { id: id.trim(), name: name.trim(), description: description.trim() },
      { onSuccess: () => { toast.success(t("admin.project.created")); reset(); onOpenChange(false); } },
    );
  };
  const err = create.error instanceof ApiError ? create.error : null;
  const msg = local ?? (!err ? null : err.status === 409 ? t("manage.project.error.already_exists") : err.status === 400 ? t("errors.invalid_argument") : t(`errors.${err.code}`));
  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) reset(); onOpenChange(o); }}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit} className="flex flex-col gap-3" noValidate>
          <DialogHeader>
            <DialogTitle>{t("manage.project.heading")}</DialogTitle>
            <DialogDescription>{t("admin_ui.projects.create_hint")}</DialogDescription>
          </DialogHeader>
          <Label htmlFor="project-id">{t("admin.project.id")}</Label>
          <Input
            id="project-id" value={id} onChange={(e) => setId(e.target.value)} maxLength={64} autoFocus
            aria-invalid={!!msg} aria-describedby={msg ? "project-err project-hint" : "project-hint"}
          />
          <p id="project-hint" className="text-sm text-muted-foreground">{t("admin.project.hint")}</p>
          {!bad && <p className="text-sm text-muted-foreground">{t("admin_ui.projects.url_preview")} <code className="rounded bg-muted px-1 font-mono">{projectHref(id.trim())}</code></p>}
          {msg && <p id="project-err" role="alert" className="text-sm text-destructive">{msg}</p>}
          <Label htmlFor="project-name">{t("admin.project.name")}</Label>
          <Input id="project-name" value={name} maxLength={100} onChange={(e) => setName(e.target.value)} aria-describedby="project-name-hint" />
          <p id="project-name-hint" className="text-sm text-muted-foreground">{t("admin.project.name_hint")}</p>
          <Label htmlFor="project-description">{t("admin.project.description")}</Label>
          <Textarea id="project-description" value={description} maxLength={500} onChange={(e) => setDescription(e.target.value)} aria-describedby="project-description-hint" />
          <p id="project-description-hint" className="text-sm text-muted-foreground">{t("admin.project.description_hint")}</p>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>{t("common.cancel")}</Button>
            <Button type="submit" disabled={create.isPending}>{t("manage.project.button.create")}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function ProjectEditForm({ project, onDone }: { project: AdminProject; onDone?: () => void }) {
  const t = useTranslations("admin.project");
  const root = useTranslations();
  const update = useUpdateProject();
  const [name, setName] = useState(project.name);
  const [description, setDescription] = useState(project.description);
  const [minFavorable, setMinFavorable] = useState(String(project.min_favorable));
  const [local, setLocal] = useState<string | null>(null);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return setLocal(t("name_required"));
    const min = Number(minFavorable);
    if (!Number.isInteger(min) || min < 1 || min > 10) return setLocal(t("min_favorable_invalid"));
    setLocal(null);
    update.mutate(
      { id: project.id, name: name.trim(), description: description.trim(), min_favorable: min },
      { onSuccess: () => { toast.success(t("updated")); onDone?.(); } },
    );
  };
  const err = update.error instanceof ApiError ? update.error : null;
  const msg = local ?? (!err ? null : err.status === 400 ? t("invalid_fields") : root(`errors.${err.code}`));
  return (
    <form onSubmit={submit} className="flex flex-col gap-2" noValidate>
      <Label htmlFor="edit-name">{t("name")}</Label>
      <Input id="edit-name" value={name} maxLength={100} onChange={(e) => setName(e.target.value)} aria-invalid={!!msg} aria-describedby={msg ? "edit-err" : undefined} />
      <Label htmlFor="edit-description">{t("description")}</Label>
      <Textarea id="edit-description" value={description} maxLength={500} onChange={(e) => setDescription(e.target.value)} />
      <Label htmlFor="edit-min-favorable">{t("min_favorable")}</Label>
      <Input id="edit-min-favorable" className="w-24" type="number" min={1} max={10} step={1} value={minFavorable} onChange={(e) => setMinFavorable(e.target.value)} aria-describedby="edit-min-favorable-hint" />
      <p id="edit-min-favorable-hint" className="text-sm text-muted-foreground">{t("min_favorable_hint")}</p>
      {msg && <p id="edit-err" role="alert" className="text-sm text-destructive">{msg}</p>}
      <div className="flex justify-end gap-2">
        {onDone && <Button type="button" variant="outline" onClick={onDone}>{t("cancel")}</Button>}
        <Button type="submit" disabled={update.isPending}>{t("save")}</Button>
      </div>
    </form>
  );
}
