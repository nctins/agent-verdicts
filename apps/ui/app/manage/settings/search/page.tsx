"use client";

import Link from "next/link";
import { useFormatter, useTranslations } from "next-intl";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { AppShell } from "@/components/AppShell";
import { SessionGate } from "@/components/SessionGate";
import { PageHeader } from "@/components/admin/kit";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { ApiError, errorMessageKey } from "@/lib/api/errors";
import { usePutSettings, useReindex, useSettings, useTestSettings } from "@/lib/api/queries";
import type { components } from "@/lib/api/schema";
import {
  canSave, fingerprint, fromSettings, pipelineLabel, toInput, warningKey,
  type ProviderForm, type SettingsForm,
} from "@/lib/settings-form";
import { Loading } from "@/components/Loading";

type Settings = components["schemas"]["Settings"];
type Which = "embedding" | "rerank";

function Block({ which, form, last4, error, testing, onChange, onTest }: {
  which: Which; form: ProviderForm; last4: string; error: string | null; testing: boolean;
  onChange: (p: ProviderForm) => void; onTest: () => void;
}) {
  const t = useTranslations("settings");
  const set = (patch: Partial<ProviderForm>) => onChange({ ...form, ...patch });
  return (
    <section className="flex flex-col gap-2 rounded-xl border bg-card p-5">
      <h2 className="font-semibold">{t(which === "embedding" ? "emb.heading" : "rer.heading")}</h2>
      <Label htmlFor={`${which}-url`}>URL</Label>
      <Input id={`${which}-url`} type="url" value={form.url} onChange={(e) => set({ url: e.target.value })} autoComplete="off" spellCheck={false} aria-describedby={`${which}-url-hint`} />
      <p id={`${which}-url-hint`} className="text-sm text-muted-foreground">{t("hint.url")}</p>
      <Label htmlFor={`${which}-model`}>{t("label.model")}</Label>
      <Input id={`${which}-model`} value={form.model} onChange={(e) => set({ model: e.target.value })} autoComplete="off" spellCheck={false} />
      <Label htmlFor={`${which}-key`}>{t("label.api_key")}</Label>
      <Input
        id={`${which}-key`} type="password" value={form.apiKey} autoComplete="new-password"
        placeholder={last4 ? `••••${last4}` : ""} disabled={form.clearKey}
        onChange={(e) => set({ apiKey: e.target.value })} aria-describedby={`${which}-key-hint`}
      />
      <p id={`${which}-key-hint`} className="text-sm text-muted-foreground">{t("hint.api_key")}</p>
      <div className="flex items-center gap-2">
        <Checkbox id={`${which}-clear`} checked={form.clearKey} onCheckedChange={(c) => set({ clearKey: c === true, apiKey: "" })} />
        <Label htmlFor={`${which}-clear`}>{t("label.delete_key")}</Label>
      </div>
      <Button type="button" variant="outline" className="self-start" disabled={testing} onClick={onTest}>{t("button.test")}</Button>
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    </section>
  );
}

function Editor({ data }: { data: Settings }) {
  const t = useTranslations("settings");
  const tt = useTranslations();
  const format = useFormatter();
  const test = useTestSettings();
  const put = usePutSettings();
  const reindex = useReindex();
  const saved = fromSettings(data);
  const [form, setForm] = useState<SettingsForm>(saved);
  const [lastPassed, setLastPassed] = useState<string | null>(null);
  const [errors, setErrors] = useState<Record<Which, string | null>>({ embedding: null, rerank: null });
  const [testing, setTesting] = useState<Which | null>(null);
  const [lastTest, setLastTest] = useState<{ ok: boolean; at: Date } | null>(null);

  // The saved values change when a save or another admin lands; keep edits only while they still differ.
  const savedFp = fingerprint(saved);
  const seen = useRef(savedFp);
  useEffect(() => {
    if (seen.current !== savedFp) {
      seen.current = savedFp;
      setForm(fromSettings(data));
      setLastPassed(null);
    }
  }, [savedFp, data]);

  const msg = (e: unknown) => (e instanceof ApiError && e.message !== e.code ? e.message : tt(errorMessageKey(e instanceof ApiError ? e.code : "internal")));
  const change = (which: Which) => (p: ProviderForm) => setForm((f) => ({ ...f, [which]: p }));
  const runTest = (which: Which) => {
    setTesting(which);
    setErrors({ embedding: null, rerank: null });
    test.mutate(toInput(form), {
      onSuccess: () => { setLastPassed(fingerprint(form)); setLastTest({ ok: true, at: new Date() }); toast.success(t("test_ok")); },
      onError: (e) => { setLastPassed(null); setLastTest({ ok: false, at: new Date() }); setErrors((x) => ({ ...x, [which]: msg(e) })); },
      onSettled: () => setTesting(null),
    });
  };
  const save = () =>
    put.mutate(toInput(form), {
      onSuccess: (d) => { toast.success(t("saved")); setForm(fromSettings(d)); setLastPassed(null); setErrors({ embedding: null, rerank: null }); },
      onError: (e) => toast.error(msg(e)),
    });

  const s = data.status;
  const embeddingOn = data.embedding.url !== "";
  const n = (v: number) => format.number(v);
  const pct = s.total_count ? Math.round((100 * s.embedded_count) / s.total_count) : 0;
  return (
    <div className="flex flex-col gap-6">
      <section aria-labelledby="search-status" className="flex flex-col gap-3 rounded-xl border bg-card p-5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 id="search-status" className="font-semibold">{tt("admin_ui.search.status")}</h2>
          <Badge variant={embeddingOn ? "default" : "secondary"}>{pipelineLabel(s)}</Badge>
        </div>
        <p data-status="pipeline" className="text-sm">
          {t("status.line", { pipeline: pipelineLabel(s), done: n(s.embedded_count), total: n(s.total_count) })}
          {s.reindexing && <strong> · {t("status.reindexing")}</strong>}
        </p>
        {embeddingOn && (
          <div className="flex flex-col gap-1">
            <Progress aria-label={t("status.progress")} value={pct} />
            <p className="text-xs text-muted-foreground">{tt("admin_ui.search.coverage", { count: pct })}</p>
          </div>
        )}
        {lastTest && (
          <p className={lastTest.ok ? "text-sm text-emerald-700 dark:text-emerald-400" : "text-sm text-destructive"} role="status">
            {tt(lastTest.ok ? "admin_ui.search.test_ok_at" : "admin_ui.search.test_failed_at", { label: format.dateTime(lastTest.at, { timeStyle: "short" }) })}
          </p>
        )}
        {s.warnings.map((w) => {
          const { key, values } = warningKey(w);
          return <Alert key={w} variant="destructive"><AlertDescription>{t(key.slice("settings.".length), values)}</AlertDescription></Alert>;
        })}
        {!embeddingOn && (
          <Alert><AlertDescription>{t("enable_hint")}<Link href="/help/ai/" className="underline">{t("enable_link")}</Link></AlertDescription></Alert>
        )}
        {embeddingOn && (
          <Button type="button" variant="outline" className="self-start" disabled={reindex.isPending || s.reindexing}
            onClick={() => reindex.mutate(undefined, { onSuccess: () => toast.success(t("reindex_started")), onError: (e) => toast.error(msg(e)) })}>
            {t("button.reindex")}
          </Button>
        )}
      </section>
      <div className="grid gap-6 lg:grid-cols-2">
        {(["embedding", "rerank"] as const).map((w) => (
          <Block key={w} which={w} form={form[w]} last4={data[w].api_key_last4} error={errors[w]} testing={testing === w} onChange={change(w)} onTest={() => runTest(w)} />
        ))}
      </div>
      <div className="sticky bottom-0 -mx-4 flex items-center justify-between gap-3 border-t bg-background/95 px-4 py-3 backdrop-blur md:-mx-6 md:px-6">
        <p className="text-sm text-muted-foreground">{tt("admin_ui.search.save_hint")}</p>
        <Button type="button" disabled={put.isPending || !canSave(form, saved, lastPassed)} onClick={save}>{t("button.save")}</Button>
      </div>
    </div>
  );
}

function Body() {
  const t = useTranslations();
  const { data, error } = useSettings();
  if (error && !data) return <p role="alert">{t(errorMessageKey(error instanceof ApiError ? error.code : "network"))}</p>;
  if (!data) return <Loading />;
  return <Editor data={data} />;
}

export default function SearchSettingsPage() {
  const t = useTranslations("settings");
  return (
    <AppShell>
      <SessionGate require="settings:Manage">
        <PageHeader title={t("heading")} description={t("intro")} />
        <Body />
      </SessionGate>
    </AppShell>
  );
}
