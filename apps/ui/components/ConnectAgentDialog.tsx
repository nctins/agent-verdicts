"use client";

import { PlugIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { HelpLink } from "@/components/HelpArticle";
import { mcpSetup } from "@/lib/mcp";
// Copy of docs/instructions/agent-verdicts.md (the Docker build only sees apps/ui); `npm run check` fails if they differ.
import agentRules from "@/lib/agent-rules.md?raw";

function Snippet({ label, text, scroll }: { label: string; text: string; scroll?: boolean }) {
  const t = useTranslations("admin.once");
  const [copy, setCopy] = useState<"idle" | "ok" | "failed">("idle");
  const doCopy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopy("ok");
    } catch {
      setCopy("failed");
    }
  };
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-medium">{label}</span>
        <Button type="button" size="sm" variant="outline" onClick={doCopy} aria-label={`${t("copy")}: ${label}`}>{t("copy")}</Button>
      </div>
      <pre className={`overflow-x-auto rounded-md bg-muted p-3 text-sm${scroll ? " max-h-64 overflow-y-auto" : ""}`}><code>{text}</code></pre>
      {copy === "ok" && <p className="text-sm" role="status">{t("copied")}</p>}
      {copy === "failed" && <p className="text-sm" role="alert">{t("copy_failed")}</p>}
    </div>
  );
}

/** Content mounts only once the dialog is open, so reading window is safe in the static export. */
function Body({ project }: { project: string }) {
  const t = useTranslations("project.connect");
  const [origin] = useState(() => window.location.origin);
  const s = mcpSetup(origin, project);
  return (
    <div className="flex min-w-0 flex-col gap-3">
      <Snippet label={t("url")} text={s.url} />
      <Snippet label={t("config")} text={s.config} />
      <Snippet label={t("cli")} text={s.cli} />
      <p className="text-sm text-muted-foreground">{t("token_note")}</p>
      <Snippet label={t("rules")} text={agentRules} scroll />
      <p className="text-sm text-muted-foreground">{t("rules_note")}</p>
    </div>
  );
}

/** With `open`/`onOpenChange` it is opened from elsewhere (the project's "⋯" menu) and has no button of its own. */
export function ConnectAgentDialog({ project, open, onOpenChange }: { project: string; open?: boolean; onOpenChange?: (o: boolean) => void }) {
  const t = useTranslations("project.connect");
  const controlled = open !== undefined;
  return (
    <Dialog {...(controlled ? { open, onOpenChange } : {})}>
      {!controlled && (
        <DialogTrigger asChild>
          <Button variant="outline"><PlugIcon aria-hidden />{t("button")}</Button>
        </DialogTrigger>
      )}
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          {/* Beside the title, not in it: the dialog is named by the title alone. */}
          <div className="flex items-center gap-2">
            <DialogTitle>{t("title")}</DialogTitle>
            <HelpLink page="mcp" label={t("title")} />
          </div>
          <DialogDescription>{t("intro", { id: project })}</DialogDescription>
        </DialogHeader>
        <Body project={project} />
      </DialogContent>
    </Dialog>
  );
}
