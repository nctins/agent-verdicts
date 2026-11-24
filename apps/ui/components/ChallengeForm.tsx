"use client";

import { PlusIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useState, type FormEvent } from "react";
import { toast } from "sonner";
import { MarkdownHint } from "@/components/Markdown";
import { SourcesField } from "@/components/Sources";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useCreateSignal, useSignalBusy } from "@/lib/api/queries";
import { parseSources, sourcesProblem } from "@/lib/sources";
import type { Stance } from "@/lib/stance";

// A challenge opposes or stays neutral; supporting a claim with an argument is an assessment with a body.
const CHALLENGE_STANCES: Stance[] = ["unfavorable", "neutral"];

/** A challenge needs a body (server rule) and defaults to opposing (spec §5.3). */
export function ChallengeForm({ project, id, onDone }: { project: string; id: string; onDone: () => void }) {
  const t = useTranslations();
  const create = useCreateSignal(project, id);
  const busy = useSignalBusy(project, id);
  const [body, setBody] = useState("");
  const [stance, setStance] = useState<Stance>("unfavorable");
  const [sourcesText, setSourcesText] = useState("");
  const [missing, setMissing] = useState(false);
  const [withSources, setWithSources] = useState(false);
  const [badSources, setBadSources] = useState<string | null>(null);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const empty = !body.trim();
    const sources = parseSources(sourcesText);
    const problem = sourcesProblem(sources);
    setMissing(empty);
    setBadSources(problem);
    if (empty || problem) return;
    create.mutate(
      { kind: "challenge", stance, body, ...(sources.length ? { sources } : {}) },
      {
        onSuccess: () => {
          toast.success(t("contribution.challenge_created"));
          onDone();
        },
      },
    );
  };

  return (
    <form onSubmit={submit} className="flex flex-col gap-3 rounded-lg border p-3" noValidate>
      <h2 className="font-medium">{t("contribution.challenge.heading")}</h2>
      {missing && (
        <Alert variant="destructive" role="alert">
          <AlertDescription>{t("contribution.challenge.error_required")}</AlertDescription>
        </Alert>
      )}
      <div className="flex flex-col gap-2">
        <Label htmlFor="ch-body">{t("contribution.challenge.content")}</Label>
        <Textarea id="ch-body" rows={4} autoFocus required aria-invalid={missing} aria-describedby="ch-body-hint" value={body} onChange={(e) => setBody(e.target.value)} />
        <MarkdownHint id="ch-body-hint" />
      </div>
      {badSources && (
        <Alert variant="destructive" role="alert">
          <AlertDescription>{t(badSources)}</AlertDescription>
        </Alert>
      )}
      {withSources ? (
        <SourcesField id="ch-sources" value={sourcesText} onChange={setSourcesText} />
      ) : (
        <Button type="button" variant="ghost" size="sm" className="self-start" onClick={() => setWithSources(true)}>
          <PlusIcon aria-hidden />{t("sources.add")}
        </Button>
      )}
      <div className="flex items-center gap-2">
        <Label htmlFor="ch-stance">{t("contribution.challenge.stance")}</Label>
        <Select value={stance} onValueChange={(v) => setStance(v as Stance)}>
          <SelectTrigger id="ch-stance" className="w-40"><SelectValue /></SelectTrigger>
          <SelectContent>
            {CHALLENGE_STANCES.map((s) => <SelectItem key={s} value={s}>{t(`stance.${s}`)}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onDone}>
          {t("common.cancel")}
        </Button>
        <Button type="submit" disabled={busy}>
          {t("contribution.challenge.submit")}
        </Button>
      </div>
    </form>
  );
}
