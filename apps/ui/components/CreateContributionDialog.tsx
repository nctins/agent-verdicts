"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { useState, type FormEvent } from "react";
import { toast } from "sonner";
import { contributionHref } from "@/lib/routes";
import { MarkdownHint } from "@/components/Markdown";
import { SourcesField } from "@/components/Sources";
import { StateBadge } from "@/components/StateBadge";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { ApiError, errorMessageKey } from "@/lib/api/errors";
import { useCreateContribution } from "@/lib/api/queries";
import { parseSources, sourcesProblem } from "@/lib/sources";
import { useDebouncedSearch } from "@/lib/use-debounced-search";

const TITLE_MAX = 200;
const BODY_MAX_BYTES = 65536;

type Props = {
  project: string;
  kind: "claim" | "question";
  initialTitle?: string;
  answersId?: string;
  supersedes?: { id: string; title: string }; // the claim this one replaces
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

/** Client-side check mirroring the API: title 1–200 code points after trim, body ≤ 65 536 UTF-8 bytes. Returns a message key. */
export function contributionProblem(title: string, body: string): string | null {
  const tt = title.trim();
  if (!tt) return "contribution.create.error_required";
  if ([...tt].length > TITLE_MAX) return "contribution.create.error_title_too_long";
  if (new TextEncoder().encode(body).length > BODY_MAX_BYTES) return "contribution.create.error_body_too_long";
  return null;
}

export function CreateContributionDialog(props: Props) {
  const t = useTranslations("contribution.create");
  const router = useRouter();
  const close = () => props.onOpenChange(false);
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(props.kind === "question" ? "heading_question" : "heading_claim")}</DialogTitle>
        </DialogHeader>
        {/* Content unmounts on close, so the form starts fresh (and takes a new initialTitle) each time. */}
        <ContributionForm
          {...props}
          onCancel={close}
          onDone={(id) => {
            close();
            toast.success(t("created"));
            router.push(contributionHref(props.project, id));
          }}
        />
      </DialogContent>
    </Dialog>
  );
}

/** Existing contributions that look like the title being typed, so people open them instead of posting a duplicate. */
function Similar({ project, title }: { project: string; title: string }) {
  const t = useTranslations("contribution.create");
  const text = [...title.trim()].length >= 4 ? title : "";
  const { results, settledText } = useDebouncedSearch(project, text, { delayMs: 300, limit: 3 });
  if (!text || !results.length || settledText !== text.trim()) return null;
  return (
    <div className="flex min-w-0 flex-col gap-1 overflow-hidden rounded-md border border-dashed p-2 text-sm">
      <p className="text-muted-foreground">{t("similar")}</p>
      <ul className="flex flex-col gap-1">
        {results.map((r) => (
          <li key={r.id} className="flex min-w-0 items-center gap-2">
            <StateBadge state={r.state} questionStatus={r.question_status} />
            <Link href={contributionHref(project, r.id)} className="min-w-0 truncate hover:underline">{r.title}</Link>
          </li>
        ))}
      </ul>
    </div>
  );
}

type FormProps = Pick<Props, "project" | "kind" | "initialTitle" | "answersId" | "supersedes"> & {
  onDone: (id: string) => void;
  onCancel?: () => void;
};

/** The form itself; the dialog wraps it, the question page shows it inline as the answer form. */
export function ContributionForm({ project, kind, initialTitle = "", answersId, supersedes, onDone, onCancel }: FormProps) {
  const t = useTranslations();
  const create = useCreateContribution(project);
  const [title, setTitle] = useState(initialTitle);
  const [body, setBody] = useState("");
  const [sourcesText, setSourcesText] = useState("");
  const [invalid, setInvalid] = useState<string | null>(null);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const sources = parseSources(sourcesText);
    const problem = contributionProblem(title, body) ?? sourcesProblem(sources);
    setInvalid(problem);
    if (problem) return;
    create.mutate(
      { kind, title: title.trim(), body, ...(sources.length ? { sources } : {}), ...(answersId ? { answers_id: answersId } : {}), ...(supersedes ? { supersedes_id: supersedes.id } : {}) },
      { onSuccess: (d) => onDone(d.id) },
    );
  };

  const err = create.error;
  const msg = invalid
    ? t(invalid)
    : err && !(err instanceof ApiError && err.status === 401) // 401: the client already redirects to login
      ? t(errorMessageKey(err instanceof ApiError ? err.code : "internal"))
      : null;

  const submitButton = (
    <Button type="submit" disabled={create.isPending}>
      {t(answersId ? "contribution.answer.submit" : kind === "question" ? "contribution.create.submit_question" : "contribution.create.submit_claim")}
    </Button>
  );

  return (
    <form onSubmit={submit} className="flex min-w-0 flex-col gap-4" noValidate>
      {msg && (
        <Alert variant="destructive" role="alert">
          <AlertDescription>{msg}</AlertDescription>
        </Alert>
      )}
      {supersedes && <p className="text-sm text-muted-foreground">{t("contribution.create.supersedes", { label: supersedes.title })}</p>}
      <div className="flex flex-col gap-2">
        <Label htmlFor="cc-title">{t("contribution.create.title")}</Label>
        <Input id="cc-title" required aria-describedby="cc-title-hint" value={title} onChange={(e) => setTitle(e.target.value)} />
        <p id="cc-title-hint" className="text-xs text-muted-foreground">
          {t("contribution.create.title_hint")}
        </p>
        {!answersId && <Similar project={project} title={title} />}
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="cc-body">{t("contribution.create.body")}</Label>
        <Textarea id="cc-body" rows={6} value={body} onChange={(e) => setBody(e.target.value)} aria-describedby="cc-body-hint" />
        <MarkdownHint id="cc-body-hint" />
      </div>
      <SourcesField id="cc-sources" value={sourcesText} onChange={setSourcesText} />
      {answersId ? (
        // Inline under a question: a plain row, not the dialog's footer band.
        <div className="flex justify-end gap-2">
          {onCancel && <Button type="button" variant="outline" onClick={onCancel}>{t("common.cancel")}</Button>}
          {submitButton}
        </div>
      ) : (
        <DialogFooter>
          {onCancel && (
            <Button type="button" variant="outline" onClick={onCancel}>
              {t("common.cancel")}
            </Button>
          )}
          {submitButton}
        </DialogFooter>
      )}
    </form>
  );
}
