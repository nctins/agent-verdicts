"use client";

import { useTranslations } from "next-intl";
import { useState } from "react";
import { toast } from "sonner";
import { HelpLink } from "@/components/HelpArticle";
import { ProjectEditForm } from "@/components/ProjectForms";
import { ReadRetention } from "@/components/ReadRetention";
import { ConfirmByName, Section } from "@/components/admin/kit";
import { Button } from "@/components/ui/button";
import { can } from "@/lib/access";
import { ApiError } from "@/lib/api/errors";
import { useProjectInfo, useSession, useUpdateProject } from "@/lib/api/queries";

/** The project's Settings tab (spec 2026-11-22-project-ui-redesign P8); each block shows only to who may change it. */
export function ProjectSettings({ id }: { id: string }) {
  const t = useTranslations();
  const session = useSession().data;
  const { meta, archived } = useProjectInfo(id);
  const update = useUpdateProject();
  const [archiving, setArchiving] = useState(false);
  const mayEdit = can(session, "project:Create");
  const manage = can(session, "member:Manage", id);
  const setArchived = (on: boolean) =>
    update.mutate({ id, archived: on }, {
      onSuccess: () => toast.success(t(`admin.project.${on ? "archived_done" : "unarchived"}`)),
      onError: (e) => toast.error(t(`errors.${e instanceof ApiError ? e.code : "internal"}`)),
      onSettled: () => setArchiving(false),
    });
  return (
    <div className="flex max-w-3xl flex-col gap-4">
      <p className="flex items-center gap-2 text-sm text-muted-foreground">
        <HelpLink page="projects" label={t("project.tab.settings")} />
        {t("pages_hint.projects")}
      </p>
      {mayEdit && meta && (
        <Section id="settings-general" title={t("project.settings.general")} description={t("project.settings.general_hint")}>
          <ProjectEditForm key={`${meta.name}|${meta.description}|${meta.min_favorable}`} project={meta} />
        </Section>
      )}
      {manage && !archived && <ReadRetention project={id} />}
      {mayEdit && meta && (
        <Section id="settings-danger" title={t("project.settings.danger")} className="border-destructive/40">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="max-w-lg text-sm text-muted-foreground">{t(archived ? "project.settings.unarchive_hint" : "project.settings.archive_hint")}</p>
            {archived ? (
              <Button variant="outline" disabled={update.isPending} onClick={() => setArchived(false)}>{t("admin.project.unarchive")}</Button>
            ) : (
              <Button variant="destructive" onClick={() => setArchiving(true)}>{t("admin.project.archive")}</Button>
            )}
          </div>
          <ConfirmByName
            open={archiving}
            onOpenChange={setArchiving}
            title={t("admin_ui.projects.archive_title", { id })}
            body={t("admin.project.archived_hint")}
            expected={id}
            confirmLabel={t("admin.project.archive")}
            busy={update.isPending}
            onConfirm={() => setArchived(true)}
          />
        </Section>
      )}
    </div>
  );
}
