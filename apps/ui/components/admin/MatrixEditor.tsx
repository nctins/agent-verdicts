"use client";

import { CheckIcon, PencilIcon, PlusIcon, Trash2Icon } from "lucide-react";
import Link from "next/link";
import { useTranslations } from "next-intl";
import { useState, type FormEvent, type ReactNode } from "react";
import { toast } from "sonner";
import { HelpLink } from "@/components/HelpArticle";
import { PageHeader, SkeletonRows } from "@/components/admin/kit";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Sheet, SheetContent, SheetHeader } from "@/components/ui/sheet";
import { ACTIONS } from "@/lib/access";
import { nameAndList } from "@/lib/access-forms";
import { ApiError } from "@/lib/api/errors";

/** A policy (members = actions) or a role (members = policy ids); `actions` is what it grants either way. */
export type MatrixItem = { id: string; name: string; managed: boolean; members: string[]; actions: string[]; usedBy?: number; usedHref?: string };
type Option = { value: string; label: string; detail?: ReactNode };

const choice =
  "flex cursor-pointer items-start gap-2 rounded-md border p-3 text-sm has-checked:border-primary has-checked:bg-accent has-focus-visible:ring-[3px] has-focus-visible:ring-ring/50";

/**
 * Roles or policies as a matrix: one column per item, one row per action, a check where the item grants it
 * (spec 2026-11-22-admin-ui-redesign U9). Managed items are read-only; custom ones are created and edited in a
 * side sheet that picks `options` (actions for a policy, policies for a role).
 */
export function MatrixEditor({ title, description, items, options, membersLabel, newLabel, error, saving, onSave, onDelete }: {
  title: string;
  description: string;
  items: MatrixItem[] | undefined;
  options: Option[];
  membersLabel: string;
  newLabel: string;
  error: unknown;
  saving: boolean;
  onSave: (v: { id?: string; name: string; members: string[] }) => Promise<unknown>;
  onDelete: (id: string) => Promise<unknown>;
}) {
  const t = useTranslations();
  const a = (k: string) => t(`manage.access.${k}`);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [members, setMembers] = useState<string[]>([]);
  const [local, setLocal] = useState<string | null>(null);
  const [opErr, setOpErr] = useState<unknown>(null);

  const start = (x?: MatrixItem) => {
    setEditing(x?.id ?? null); setName(x?.name ?? ""); setMembers(x?.members ?? []); setLocal(null); setOpErr(null); setOpen(true);
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const v = nameAndList(name, members);
    setLocal(v.error && a(v.error));
    if (v.error) return;
    try {
      await onSave({ id: editing ?? undefined, name: v.name, members });
      setOpErr(null);
      setOpen(false);
      toast.success(a("saved"));
    } catch (err) {
      setOpErr(err);
    }
  };
  const remove = async (id: string) => {
    try {
      await onDelete(id);
      setOpErr(null);
      toast.success(a("deleted"));
    } catch (err) {
      setOpErr(err);
    }
  };
  const shown = opErr ?? error;
  const errMsg = shown instanceof ApiError && shown.status === 409 && shown.message.includes("in use")
    ? a("in_use")
    : shown ? t(`errors.${shown instanceof ApiError ? shown.code : "network"}`) : null;
  const grants = (x: MatrixItem, action: string) => x.actions.includes("*") || x.actions.includes(action);

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={<>{title}<HelpLink page="access" label={title} /></>} description={description} actions={<Button onClick={() => start()}><PlusIcon aria-hidden />{newLabel}</Button>} />
      {errMsg && !open && <Alert variant="destructive" role="alert"><AlertDescription>{errMsg}</AlertDescription></Alert>}
      {!items ? (
        <SkeletonRows />
      ) : (
        <div className="overflow-x-auto rounded-xl border">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr className="border-b">
                <th scope="col" className="p-3 text-left text-xs font-medium tracking-wide text-muted-foreground uppercase">{a("actions")}</th>
                {items.map((x) => (
                  <th key={x.id} scope="col" className="min-w-36 p-3 text-center align-bottom font-medium">
                    <div className="flex flex-col items-center gap-1">
                      <span>{x.name}</span>
                      {x.managed ? <Badge variant="secondary">{a("managed")}</Badge> : (
                        <span className="flex gap-1">
                          <Button size="icon-xs" variant="ghost" onClick={() => start(x)} aria-label={`${a("edit")}: ${x.name}`}><PencilIcon /></Button>
                          <Button size="icon-xs" variant="ghost" onClick={() => remove(x.id)} aria-label={`${a("delete")}: ${x.name}`}><Trash2Icon /></Button>
                        </span>
                      )}
                      {x.usedBy !== undefined && (
                        x.usedHref && x.usedBy > 0
                          ? <Link href={x.usedHref} className="text-xs font-normal text-muted-foreground hover:underline">{t("admin_ui.matrix.used_by", { count: x.usedBy })}</Link>
                          : <span className="text-xs font-normal text-muted-foreground">{t("admin_ui.matrix.used_by", { count: x.usedBy })}</span>
                      )}
                    </div>
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {ACTIONS.map((action) => (
                <tr key={action} className="border-b last:border-0">
                  <th scope="row" className="p-3 text-left font-normal">
                    <span className="font-medium">{t(`manage.access.action.${action}`)}</span>
                    <span className="block text-xs text-muted-foreground">{t(`admin_ui.matrix.desc.${action}`)}</span>
                  </th>
                  {items.map((x) => (
                    <td key={x.id} className="p-3 text-center">
                      {grants(x, action)
                        ? <CheckIcon className="mx-auto size-4 text-emerald-600" aria-label={`${x.name}: ${t("admin_ui.yes")}`} />
                        : <span className="text-muted-foreground" aria-label={`${x.name}: ${t("admin_ui.no")}`}>–</span>}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent>
          <SheetHeader title={editing ? `${a("edit")}: ${name}` : newLabel} />
          <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
            {errMsg && <Alert variant="destructive" role="alert"><AlertDescription>{errMsg}</AlertDescription></Alert>}
            {local && <p role="alert" className="text-sm text-destructive">{local}</p>}
            <div className="flex flex-col gap-1">
              <Label htmlFor="set-name">{a("name")}</Label>
              <Input id="set-name" maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <fieldset className="flex flex-col gap-2">
              <legend className="mb-1 text-sm font-medium">{membersLabel}</legend>
              {options.map((o) => (
                <label key={o.value} className={choice}>
                  <input
                    type="checkbox"
                    checked={members.includes(o.value)}
                    onChange={(e) => setMembers(e.target.checked ? [...members, o.value] : members.filter((m) => m !== o.value))}
                    className="mt-0.5 accent-primary"
                  />
                  <span className="flex flex-col">
                    <span className="font-medium">{o.label}</span>
                    {o.detail && <span className="text-xs text-muted-foreground">{o.detail}</span>}
                  </span>
                </label>
              ))}
            </fieldset>
            <div className="flex gap-2">
              <Button type="submit" disabled={saving}>{a("save")}</Button>
              <Button type="button" variant="outline" onClick={() => setOpen(false)}>{a("cancel")}</Button>
            </div>
          </form>
        </SheetContent>
      </Sheet>
    </div>
  );
}
