"use client";

import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { useState } from "react";
import { toast } from "sonner";
import { ContributionForm } from "@/components/CreateContributionDialog";
import { Composer } from "@/components/SignalList";
import { Button } from "@/components/ui/button";
import { useSession } from "@/lib/api/queries";
import { loginHref } from "@/lib/safe-next";

export function AnswerForm({ project, questionId }: { project: string; questionId: string }) {
  const t = useTranslations("contribution");
  const router = useRouter();
  const session = useSession().data;
  const [round, setRound] = useState(0); // a new key clears the form after each answer
  const [open, setOpen] = useState(false); // the form stays out of the way until asked for

  return (
    <section className="flex flex-col gap-3" aria-label={t("answer.heading")}>
      {session?.authenticated && !open ? (
        <Composer label={t("answer.open")} onOpen={() => setOpen(true)} />
      ) : session?.authenticated ? (
        <ContributionForm
          key={round}
          project={project}
          kind="claim"
          answersId={questionId}
          onCancel={() => setOpen(false)}
          onDone={() => {
            toast.success(t("answer_created"));
            setRound((r) => r + 1);
            setOpen(false);
          }}
        />
      ) : (
        <Button className="self-start" onClick={() => router.push(loginHref())}>{t("answer_login")}</Button>
      )}
    </section>
  );
}
