"use client";

import Link from "next/link";
import { CheckIcon, PencilIcon, PlusIcon } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useFormatter, useTranslations } from "next-intl";
import { Suspense, useState, type FormEvent } from "react";
import { toast } from "sonner";
import { AppShell } from "@/components/AppShell";
import { Avatar } from "@/components/Avatar";
import { OnceSecret } from "@/components/OnceSecret";
import { SessionGate } from "@/components/SessionGate";
import { AssignSheet } from "@/components/admin/AssignSheet";
import { ConfirmByName, CopyButton, PageHeader, RelTime, Section, useActorType } from "@/components/admin/kit";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ACTIONS, can, managesAny } from "@/lib/access";
import { coveredBy, effectiveAccess } from "@/lib/access-forms";
import { EXPIRY_CHOICES, expiryState, issueBody, resetBody, type ExpiryChoice } from "@/lib/admin-forms";
import { ApiError, isNameTaken } from "@/lib/api/errors";
import { selectCls } from "@/components/AccessPicker";
import { AuditTable } from "@/components/AuditTable";
import {
  useActor, useActors, useAudit, useAssignments, useCreateAssignment, useDeleteAssignment, useIssueCredential, usePatchActor,
  useProjects, useResetPassword, useRevokeCredential, useRoles, useSession,
} from "@/lib/api/queries";
import type { components } from "@/lib/api/schema";
import { Loading } from "@/components/Loading";

type Actor = components["schemas"]["AdminActorDetail"];
type Assignment = components["schemas"]["Assignment"];
type Secret = { label: string; value: string };

const errText = (t: (k: string) => string, e: unknown) => t(`errors.${e instanceof ApiError ? e.code : "network"}`);

/** Rename in a small dialog opened from the header. */
function RenameDialog({ a, open, onOpenChange }: { a: Actor; open: boolean; onOpenChange: (o: boolean) => void }) {
  const t = useTranslations();
  const patch = usePatchActor(a.id);
  const [name, setName] = useState(a.name);
  const nameTaken = isNameTaken(patch.error);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    patch.mutate({ name: name.trim() }, { onSuccess: () => { toast.success(t("manage.actor.notice.profile")); onOpenChange(false); } });
  };
  return (
    <Dialog open={open} onOpenChange={(o) => { if (o) setName(a.name); patch.reset(); onOpenChange(o); }}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>{t("admin_ui.actor.rename_title")}</DialogTitle>
            <DialogDescription>{t("manage.actor.hint.name")}</DialogDescription>
          </DialogHeader>
          {patch.error && !nameTaken && <Alert variant="destructive" role="alert"><AlertDescription>{errText(t, patch.error)}</AlertDescription></Alert>}
          <div className="flex flex-col gap-2">
            <Label htmlFor="rename">{t("admin.actor.rename")}</Label>
            <Input
              id="rename" required maxLength={64} value={name} onChange={(e) => setName(e.target.value)}
              aria-invalid={nameTaken} aria-describedby={nameTaken ? "rename-err" : undefined}
            />
            {nameTaken && <p id="rename-err" role="alert" className="text-sm text-destructive">{t("manage.actor.error.name_taken")}</p>}
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>{t("common.cancel")}</Button>
            <Button type="submit" disabled={patch.isPending}>{t("manage.actor.button.save")}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** Disabling is the one destructive control: last on the page, behind typing the actor's name. */
function DangerZone({ a }: { a: Actor }) {
  const t = useTranslations();
  const patch = usePatchActor(a.id);
  const [confirming, setConfirming] = useState(false);
  const lastAdmin = patch.error instanceof ApiError && patch.error.status === 409;
  const save = (disabled: boolean) =>
    patch.mutate({ disabled }, {
      onSuccess: () => toast.success(t(disabled ? "manage.actor.notice.disabled" : "manage.actor.notice.enabled")),
      onSettled: () => setConfirming(false),
    });
  return (
    <section aria-labelledby="danger" className="flex flex-col gap-3 rounded-xl border border-destructive/40 p-5">
      <h2 id="danger" className="font-semibold text-destructive">{t("admin_ui.actor.danger")}</h2>
      {patch.error && (
        <Alert variant="destructive" role="alert"><AlertDescription>{lastAdmin ? t("manage.actor.error.last_admin") : errText(t, patch.error)}</AlertDescription></Alert>
      )}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="max-w-xl text-sm text-muted-foreground">{t(a.disabled ? "manage.actor.hint.enable" : "manage.actor.hint.disable")}</p>
        <Button
          type="button" variant={a.disabled ? "default" : "destructive"} disabled={patch.isPending}
          onClick={() => (a.disabled ? save(false) : setConfirming(true))}
        >
          {t(a.disabled ? "manage.actor.button.enable" : "manage.actor.button.disable")}
        </Button>
      </div>
      <ConfirmByName
        open={confirming}
        onOpenChange={setConfirming}
        title={t("manage.actor.disable_title")}
        body={t("manage.actor.disable_body", { label: a.name })}
        expected={a.name}
        confirmLabel={t("manage.actor.disable_confirm")}
        busy={patch.isPending}
        onConfirm={() => save(true)}
      />
    </section>
  );
}

function PasswordForm({ a, self }: { a: Actor; self: boolean }) {
  const t = useTranslations();
  const reset = useResetPassword(a.id);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [mustChange, setMustChange] = useState<boolean | null>(null);
  const [secret, setSecret] = useState<Secret | null>(null);
  const conflict = reset.error instanceof ApiError && reset.error.status === 409;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    reset.mutate(resetBody({ username, password, mustChange: self ? false : mustChange }), {
      onSuccess: (r) => {
        setSecret({ label: t("admin.actor.password_title"), value: r.password });
        setUsername("");
        setPassword("");
        setMustChange(null);
        reset.reset(); // the secret now lives only in this component's state
      },
    });
  };
  return (
    <section className="flex flex-col gap-3 rounded-xl border bg-card p-5" aria-labelledby="login">
      <h2 id="login" className="font-semibold">{t("manage.actor.section.login")}</h2>
      <p className="text-sm text-muted-foreground">{t(a.has_login ? "manage.actor.hint.reset" : "manage.actor.hint.first_login")}</p>
      {secret && <OnceSecret {...secret} onDone={() => setSecret(null)} />}
      {reset.error && !conflict && (
        <Alert variant="destructive" role="alert"><AlertDescription>{errText(t, reset.error)}</AlertDescription></Alert>
      )}
      <form onSubmit={submit} className="flex max-w-md flex-col gap-3">
        <div className="flex flex-col gap-2">
          <Label htmlFor="r-username">{t("admin.field.new_username")}</Label>
          <Input
            id="r-username" autoComplete="off" value={username} placeholder={a.username ?? ""}
            onChange={(e) => setUsername(e.target.value)}
            aria-invalid={conflict} aria-describedby={conflict ? "r-username-err" : undefined}
          />
          {conflict && <p id="r-username-err" role="alert" className="text-sm text-destructive">{t("errors.conflict")}</p>}
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="r-password">{t("admin.field.new_password")}</Label>
          <Input
            id="r-password" type="password" autoComplete="new-password" value={password}
            onChange={(e) => setPassword(e.target.value)} aria-describedby="r-password-hint"
          />
          <p id="r-password-hint" className="text-sm text-muted-foreground">{t("manage.actor.hint.password")}</p>
        </div>
        {!self && (
          <div className="flex items-center gap-2">
            <Checkbox id="r-must" checked={mustChange ?? password === ""} onCheckedChange={(c) => setMustChange(c === true)} />
            <Label htmlFor="r-must">{t("admin.field.must_change")}</Label>
          </div>
        )}
        <Button type="submit" disabled={reset.isPending} className="self-start">
          {t(a.has_login ? "manage.actor.button.reset_password" : "manage.actor.button.create_login")}
        </Button>
      </form>
    </section>
  );
}

function Credentials({ a }: { a: Actor }) {
  const t = useTranslations();
  const format = useFormatter();
  const issue = useIssueCredential(a.id);
  const revoke = useRevokeCredential(a.id);
  const [secret, setSecret] = useState<Secret | null>(null);
  const [target, setTarget] = useState<string | null>(null);
  const [label, setLabel] = useState("");
  const [expiry, setExpiry] = useState<ExpiryChoice>("90");
  const [now] = useState(() => Date.now()); // fixed per visit; expiry badges need no ticking
  const date = (s: string) => format.dateTime(new Date(s), { dateStyle: "medium", timeStyle: "short" });
  const isAgent = a.type === "Agent";
  const err = issue.error ?? revoke.error;
  return (
    <section className="flex flex-col gap-3 rounded-xl border bg-card p-5" aria-labelledby="creds">
      <h2 id="creds" className="font-semibold">{t("manage.actor.section.credentials")}</h2>
      <p className="text-sm text-muted-foreground">{t(isAgent ? "manage.actor.hint.credentials" : "manage.actor.hint.legacy_tokens")}</p>
      {secret && <OnceSecret {...secret} onDone={() => setSecret(null)} />}
      {err && <Alert variant="destructive" role="alert"><AlertDescription>{errText(t, err)}</AlertDescription></Alert>}
      {isAgent && (
        <form
          className="flex flex-wrap items-end gap-3"
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            issue.mutate(issueBody(label, expiry), {
              onSuccess: (r) => {
                setSecret({ label: t("admin.actor.token_title"), value: r.token });
                setLabel("");
                issue.reset();
              },
            });
          }}
        >
          <div className="flex flex-col gap-1">
            <Label htmlFor="token-label">{t("manage.credential.label_field")}</Label>
            <Input id="token-label" maxLength={64} value={label} onChange={(e) => setLabel(e.target.value)} className="w-56" />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="token-expiry">{t("manage.credential.expiry_field")}</Label>
            <select id="token-expiry" className={selectCls} value={expiry} onChange={(e) => setExpiry(e.target.value as ExpiryChoice)}>
              {EXPIRY_CHOICES.map((c) => <option key={c} value={c}>{t(`manage.credential.expiry.${c}`)}</option>)}
            </select>
          </div>
          <Button type="submit" disabled={issue.isPending}>{t("manage.credential.button.issue")}</Button>
        </form>
      )}
      {a.credentials.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("manage.credential.list.empty")}</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead>
              <tr className="border-b">
                <th scope="col" className="p-2 font-medium">{t("admin.col.token_id")}</th>
                <th scope="col" className="p-2 font-medium">{t("manage.credential.col.label")}</th>
                <th scope="col" className="p-2 font-medium">{t("manage.actor.list.col.created")}</th>
                <th scope="col" className="p-2 font-medium">{t("manage.credential.col.expires")}</th>
                <th scope="col" className="p-2 font-medium">{t("manage.credential.col.last_used")}</th>
                <th scope="col" className="p-2 font-medium">{t("admin.col.revoked")}</th>
                <th scope="col" className="p-2"><span className="sr-only">{t("manage.credential.button.revoke")}</span></th>
              </tr>
            </thead>
            <tbody>
              {a.credentials.map((c) => (
                <tr key={c.id} className="border-b">
                  <td className="p-2 font-mono">{c.id}</td>
                  <td className="p-2">{c.label}</td>
                  <td className="p-2">{date(c.created_at)}</td>
                  <td className="p-2">
                    {c.expires_at ? date(c.expires_at) : t("manage.credential.never_expires")}
                    {!c.revoked_at && expiryState(c.expires_at, now) === "expired" && <Badge variant="destructive" className="ml-2">{t("manage.credential.expired")}</Badge>}
                    {!c.revoked_at && expiryState(c.expires_at, now) === "soon" && <Badge variant="outline" className="ml-2 border-amber-600 text-amber-700 dark:text-amber-400">{t("manage.credential.expires_soon")}</Badge>}
                  </td>
                  <td className="p-2">{c.last_used_at ? format.relativeTime(new Date(c.last_used_at), now) : t("manage.credential.never_used")}</td>
                  <td className="p-2">
                    {c.revoked_at ? <Badge variant="secondary">{t("manage.credential.list.status.revoked")} {date(c.revoked_at)}</Badge> : <Badge variant="outline">{t("manage.credential.list.status.live")}</Badge>}
                  </td>
                  <td className="p-2">
                    {!c.revoked_at && (
                      <Button type="button" variant="outline" size="sm" onClick={() => setTarget(c.id)}>
                        {t("manage.credential.button.revoke")}
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <Dialog open={target !== null} onOpenChange={(o) => !o && setTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("admin.revoke.title")}</DialogTitle>
            <DialogDescription>{t("admin.revoke.body", { id: target ?? "" })}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setTarget(null)}>{t("common.cancel")}</Button>
            <Button
              type="button"
              variant="destructive"
              disabled={revoke.isPending}
              onClick={() => target && revoke.mutate(target, { onSettled: () => setTarget(null) })}
            >
              {t("admin.revoke.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

/**
 * The actor's roles: remove one (with "Undo"), give one in the side sheet, and see what they add up to per
 * project. Who may change which is checked by the server.
 */
function Roles({ actor, fallback }: { actor: { id: string; name: string }; fallback: Assignment[] }) {
  const t = useTranslations();
  const x = (k: string) => t(`manage.access.${k}`);
  const p = (k: string) => t(`manage.people.${k}`);
  const session = useSession().data;
  const manages = managesAny(session);
  const listed = useAssignments({ actor: actor.id }, manages); // needs member:Manage; else the detail's own list
  const list = manages ? listed.data?.items : fallback;
  const remove = useDeleteAssignment();
  const create = useCreateAssignment();
  const roles = useRoles().data?.items ?? [];
  const projects = useProjects().data?.items ?? [];
  const [sheet, setSheet] = useState(false);
  // A role another one of the actor's roles already holds on the same scope (spec 2026-11-22-safer-role-assignment D3).
  const coveredLabel = (r: Assignment) => {
    const c = coveredBy((list ?? []).filter((y) => y.id !== r.id), roles, r.role_id, r.project_id === null ? "all" : [r.project_id])[0];
    return c && `${c.by.role_name} · ${c.by.project_id ?? x("all_projects")}`;
  };
  const drop = (r: Assignment) =>
    remove.mutate(r.id, {
      onSuccess: () =>
        toast.success(x("removed"), {
          action: { label: t("admin_ui.undo"), onClick: () => create.mutate({ actor_id: r.actor_id, role_id: r.role_id, project_id: r.project_id }) },
        }),
    });
  const removeErr = remove.error instanceof ApiError && remove.error.status === 409 ? x("last_admin") : remove.error ? errText(t, remove.error) : null;
  const eff = list ? effectiveAccess(list, roles, projects.map((y) => y.id)) : null;
  const shown = ACTIONS.filter((a) => !["project:Create", "actor:Manage", "settings:Manage", "policy:Manage"].includes(a));
  return (
    <div className="flex flex-col gap-6">
      <Section
        id="roles"
        title={x("actor_assignments")}
        description={!can(session, "actor:Manage") ? p("manager_note") : undefined}
      >
        {removeErr && <Alert variant="destructive" role="alert"><AlertDescription>{removeErr}</AlertDescription></Alert>}
        {!list ? (
          <Loading />
        ) : list.length === 0 ? (
          <p className="text-sm text-amber-700 dark:text-amber-400">{x("actor_no_assignments")}</p>
        ) : (
          <ul className="flex flex-col divide-y rounded-lg border">
            {list.map((r) => (
              <li key={r.id} className="flex items-center justify-between gap-2 px-3 py-2 text-sm">
                <span>
                  <span className="font-medium">{r.role_name}</span> · {r.project_id ?? x("all_projects")}
                  {coveredLabel(r) && <span className="ml-2 text-xs text-muted-foreground">{t("manage.access.redundant", { label: coveredLabel(r) })}</span>}
                </span>
                {manages && (
                  <Button size="sm" variant="ghost" disabled={remove.isPending} onClick={() => drop(r)} aria-label={`${x("remove")}: ${r.role_name} · ${r.project_id ?? x("all_projects")}`}>
                    {x("remove")}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
        {manages && (
          <Button size="sm" variant="outline" className="self-start" onClick={() => setSheet(true)}>
            <PlusIcon aria-hidden />
            {p("add_role")}
          </Button>
        )}
      </Section>
      {eff && (eff.projects.length > 0 || eff.system.length > 0) && (
        <Section id="effective" title={t("admin_ui.actor.effective")} description={t("admin_ui.actor.effective_hint")}>
          {eff.system.length > 0 && (
            <p className="text-sm">
              <span className="text-muted-foreground">{t("admin_ui.actor.system")}: </span>
              {eff.system.map((a) => t(`manage.access.action.${a}`)).join(", ")}
            </p>
          )}
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full text-left text-sm">
              <thead className="bg-muted/50 text-xs text-muted-foreground">
                <tr className="border-b">
                  <th scope="col" className="p-2 font-medium">{t("admin.project.col_name")}</th>
                  <th scope="col" className="p-2 font-medium">{x("col_roles")}</th>
                  {shown.map((a) => <th key={a} scope="col" className="p-2 text-center font-medium">{t(`manage.access.action.${a}`)}</th>)}
                </tr>
              </thead>
              <tbody>
                {eff.projects.map((row) => (
                  <tr key={row.project} className="border-b last:border-0">
                    <th scope="row" className="p-2 font-medium">{row.project}</th>
                    <td className="p-2 text-muted-foreground">{row.roles.join(", ")}</td>
                    {shown.map((a) => {
                      const on = row.actions.includes(a) || eff.system.includes("*");
                      return (
                        <td key={a} className="p-2 text-center">
                          {on ? <CheckIcon className="mx-auto size-4 text-emerald-600" aria-label={t("admin_ui.yes")} /> : <span className="text-muted-foreground" aria-label={t("admin_ui.no")}>–</span>}
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}
      <AssignSheet open={sheet} onOpenChange={setSheet} actors={[actor]} held={list ?? []} />
    </div>
  );
}

/** The latest audit events done by this actor. */
function Activity({ id }: { id: string }) {
  const t = useTranslations("manage.audit");
  const q = useAudit({ actor: id, limit: 10 });
  const actors = useActors();
  const items = q.data?.pages[0]?.items ?? [];
  return (
    <section className="flex flex-col gap-3" aria-labelledby="activity">
      <h2 id="activity" className="font-semibold">{t("recent")}</h2>
      {!q.data ? <Loading /> : items.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("empty")}</p>
      ) : (
        <AuditTable items={items} names={new Map(actors.data?.items.map((x) => [x.id, x.name]))} />
      )}
      <Link href={"/manage/audit/?" + new URLSearchParams({ actor: id })} className="self-start text-sm hover:underline">{t("see_all")}</Link>
    </section>
  );
}

const TABS = ["access", "login", "activity"] as const;
type Tab = (typeof TABS)[number];

/** Everything about one actor, for actor:Manage: a header, three tabs (kept in the URL), and the danger zone. */
function FullActor({ id }: { id: string }) {
  const t = useTranslations();
  const format = useFormatter();
  const typeLabel = useActorType();
  const router = useRouter();
  const path = usePathname();
  const sp = useSearchParams();
  const { data: a, error } = useActor(id);
  const { data: s } = useSession();
  const [tab, setTab] = useState<Tab>(() => (TABS as readonly string[]).includes(sp.get("tab") ?? "") ? (sp.get("tab") as Tab) : "access");
  const [renaming, setRenaming] = useState(false);
  if (error) return <p role="alert">{error instanceof ApiError && error.status === 404 ? t("admin.actor.not_found") : errText(t, error)}</p>;
  if (!a) return <Loading />;
  const self = a.id === s?.actor?.id;
  const pick = (v: string) => {
    setTab(v as Tab);
    const next = new URLSearchParams(sp.toString());
    if (v === "access") next.delete("tab"); else next.set("tab", v);
    router.replace(path + "?" + next, { scroll: false });
  };
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={
          <>
            <Avatar name={a.name} type={a.type} seed={a.id} className="size-10 text-sm" />
            {a.name}
            <Badge variant="secondary">{typeLabel(a.type)}</Badge>
            {a.disabled && <Badge variant="outline">{t("manage.actor.status.disabled")}</Badge>}
          </>
        }
        meta={
          <>
            {a.username && <span>@{a.username}</span>}
            <span>{t("admin_ui.actor.created", { label: format.dateTime(new Date(a.created_at), { dateStyle: "medium" }) })}</span>
            <span>{t("admin_ui.actors.col_active")}: <RelTime at={a.last_active_at} empty={t("admin_ui.actors.never")} /></span>
            <span className="inline-flex items-center gap-1 font-mono text-xs">{a.id.slice(0, 8)}<CopyButton value={a.id} label="ID" /></span>
          </>
        }
        actions={<Button variant="outline" onClick={() => setRenaming(true)}><PencilIcon aria-hidden />{t("admin_ui.actor.rename")}</Button>}
      />
      <Tabs value={tab} onValueChange={pick}>
        <TabsList>
          <TabsTrigger value="access">{t("admin_ui.actor.tab_access")}</TabsTrigger>
          <TabsTrigger value="login">{t("admin_ui.actor.tab_login")}</TabsTrigger>
          <TabsTrigger value="activity">{t("admin_ui.actor.tab_activity")}</TabsTrigger>
        </TabsList>
        <TabsContent value="access" className="pt-4"><Roles actor={a} fallback={a.assignments} /></TabsContent>
        <TabsContent value="login" className="flex flex-col gap-6 pt-4">
          {a.type === "Human" && <PasswordForm a={a} self={self} />}
          {/* A person signs in with a password; their token section is only for legacy tokens still on record. */}
          {(a.type === "Agent" || a.credentials.length > 0) && <Credentials a={a} />}
          {a.type === "Human" && a.credentials.length === 0 && <p className="text-sm text-muted-foreground">{t("admin_ui.actor.no_tokens_human")}</p>}
        </TabsContent>
        <TabsContent value="activity" className="pt-4"><Activity id={a.id} /></TabsContent>
      </Tabs>
      {self ? <p className="text-sm text-muted-foreground">{t("admin.actor.self")}</p> : <DangerZone a={a} />}
      <RenameDialog a={a} open={renaming} onOpenChange={setRenaming} />
    </div>
  );
}

/** A project manager sees who the actor is and their roles, nothing about their login. */
function RolesOnly({ id }: { id: string }) {
  const t = useTranslations();
  const typeLabel = useActorType();
  const { data, error } = useActors();
  // Someone found by name search is not in the default list (their projects' members). The link's `name` is only
  // a search hint: what is shown, and who gets a role, is whatever the server returns for this id.
  const hint = (useSearchParams().get("name") ?? "").trim();
  const q = hint.length >= 2 ? hint : undefined;
  const listed = data?.items.find((x) => x.id === id);
  const hinted = useActors(q, !!data && !listed && !!q);
  if (error || hinted.error) return <p role="alert">{errText(t, error ?? hinted.error)}</p>;
  if (!data || (!listed && q && (!hinted.data || hinted.isPlaceholderData || hinted.isFetching))) return <Loading />;
  const a = listed ?? hinted.data?.items.find((x) => x.id === id);
  if (!a) return <p role="alert">{t("admin.actor.not_found")}</p>;
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={<><Avatar name={a.name} type={a.type} seed={a.id} className="size-10 text-sm" />{a.name}<Badge variant="secondary">{typeLabel(a.type)}</Badge></>} />
      <Roles actor={a} fallback={[]} />
    </div>
  );
}

function ActorPage() {
  const id = useSearchParams().get("id") ?? "";
  return can(useSession().data, "actor:Manage") ? <FullActor id={id} /> : <RolesOnly id={id} />;
}

export default function ActorDetail() {
  return (
    <AppShell>
      <SessionGate require="people">
        <Suspense>
          <ActorPage />
        </Suspense>
      </SessionGate>
    </AppShell>
  );
}
