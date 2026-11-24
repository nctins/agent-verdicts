"use client";

import Link from "next/link";
import { useTranslations } from "next-intl";
import { AnswerForm } from "@/components/AnswerForm";
import { contributionHref } from "@/lib/routes";
import { EmptyState } from "@/components/EmptyState";
import { Markdown } from "@/components/Markdown";
import { Post } from "@/components/SignalList";
import { StanceButtons } from "@/components/StanceButtons";
import { StateBadge } from "@/components/StateBadge";
import { Badge } from "@/components/ui/badge";
import { can, hiddenLocked } from "@/lib/access";
import { useContribution, useProjectInfo, useSession } from "@/lib/api/queries";
import type { ContributionDetail } from "@/lib/stance";

type Answer = NonNullable<ContributionDetail["answers"]>[number];

// ponytail: one detail request per answer (counts + my stance); merge when the API returns them with the question.
function AnswerCard({ project, answer }: { project: string; answer: Answer }) {
  const { data } = useContribution(project, answer.id);
  const t = useTranslations("contribution");
  const hidden = answer.hidden && <Badge variant="outline">{t("hidden")}</Badge>;
  const reason = answer.hidden && answer.hidden_reason && <p className="text-xs text-muted-foreground">{t("hidden_reason", { label: answer.hidden_reason })}</p>;
  const title = (
    <Link href={contributionHref(project, answer.id)} className="font-medium hover:underline">
      {answer.title}
    </Link>
  );
  return (
    <li className="rounded-lg border p-3">
      {data ? (
        <Post author={data.actor_name} type={data.actor_type} seed={data.actor_id} at={data.created_at} labels={<><StateBadge state={data.state} />{hidden}</>}>
          {title}
          {data.body && <Markdown text={data.body} className="text-sm" />}
          {reason}
          <StanceButtons project={project} detail={data} compact />
        </Post>
      ) : (
        <div className="flex flex-wrap items-center gap-2"><StateBadge state={answer.state} />{hidden}{title}{reason}</div>
      )}
    </li>
  );
}

/** Answers in server order (best state first, then time), then the answer form. */
export function QuestionThread({ project, question }: { project: string; question: ContributionDetail }) {
  const session = useSession().data;
  const { archived } = useProjectInfo(project);
  const t = useTranslations();
  const answers = question.answers ?? [];
  return (
    <div className="flex flex-col gap-6">
      <section className="flex flex-col gap-3" aria-labelledby="answers-heading">
        <h2 id="answers-heading" className="border-b pb-2 font-semibold">
          {t("contribution.answers_heading")} ({answers.length})
        </h2>
        {answers.length ? (
          <ul className="flex flex-col gap-2">
            {answers.map((a) => <AnswerCard key={a.id} project={project} answer={a} />)}
          </ul>
        ) : (
          <EmptyState title={t("empty.no_answers")} />
        )}
      </section>
      {!archived && !hiddenLocked(session, project, question) && (!session?.authenticated || can(session, "contribution:Create", project)) && <AnswerForm project={project} questionId={question.id} />}
    </div>
  );
}
