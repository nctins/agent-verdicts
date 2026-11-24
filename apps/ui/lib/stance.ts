import type { components } from "@/lib/api/schema";
import { stateOf } from "@/lib/trust";

export type Stance = components["schemas"]["Stance"];
export type ContributionDetail = components["schemas"]["ContributionDetail"];
type Signal = components["schemas"]["Signal"];
type Actor = { id: string; name: string };

export const STANCES: Stance[] = ["favorable", "unfavorable", "neutral"];
export const OPTIMISTIC_ID = "optimistic";

const isMine = (s: Signal, actorId: string) => s.kind === "assessment" && s.actor_id === actorId;

/** The actor's assessment that currently counts, if any. */
export function myActiveAssessment(detail: ContributionDetail, actorId: string | null): Signal | null {
  if (!actorId) return null;
  for (const st of STANCES) {
    const s = detail.signals[st].find((x) => isMine(x, actorId));
    if (s) return s;
  }
  return null;
}

/** Stance of the actor's assessment that currently counts, if any. */
export const myActiveStance = (detail: ContributionDetail, actorId: string | null): Stance | null =>
  myActiveAssessment(detail, actorId)?.stance ?? null;

/** Client copy of the server rule: the author's own favorable signals are listed but not counted (spec 2026-11-21-ignore-self-support). */
export const notCounted = (detail: { actor_id: string }, s: Pick<Signal, "stance" | "actor_id">) =>
  s.stance === "favorable" && s.actor_id === detail.actor_id;

// Adds one signal to its stance group and recomputes counts + state; `drop` is an earlier assessment it replaces.
function add(detail: ContributionDetail, sig: Signal, drop?: Stance): ContributionDetail {
  const counts = { ...(detail.counts ?? { favorable: 0, unfavorable: 0, neutral: 0, favorable_actors: 0 }) };
  const signals = { ...detail.signals, [sig.stance]: [...detail.signals[sig.stance], sig] }; // server order: created_at ASC
  if (drop) {
    const old = detail.signals[drop].filter((s) => isMine(s, sig.actor_id));
    signals[drop] = detail.signals[drop].filter((s) => !isMine(s, sig.actor_id));
    signals.history = [...old.map((s) => ({ ...s, superseded: true })), ...detail.signals.history];
    counts[drop] -= old.filter((s) => !notCounted(detail, s)).length;
  }
  if (!notCounted(detail, sig)) counts[sig.stance] += 1;
  counts.favorable_actors = new Set(signals.favorable.filter((s) => !notCounted(detail, s)).map((s) => s.actor_id)).size;
  return { ...detail, counts, signals, state: stateOf(counts, detail.min_favorable ?? 1) };
}

const signal = (kind: Signal["kind"], actor: Actor, stance: Stance, body: string, nowISO: string, replyTo?: string): Signal => ({
  id: OPTIMISTIC_ID, kind, stance, actor_id: actor.id, actor_name: actor.name, body, sources: [], created_at: nowISO,
  ...(replyTo ? { reply_to: replyTo } : {}),
});

/** Client copy of the server rule: one active assessment per actor; the same stance again is a no-op. */
export function applyAssessment(detail: ContributionDetail, actor: Actor, stance: Stance, nowISO: string): ContributionDetail {
  const prev = myActiveStance(detail, actor.id);
  if (prev === stance) return detail;
  return add(detail, signal("assessment", actor, stance, "", nowISO), prev ?? undefined);
}

/** A challenge only adds; it never replaces an assessment. */
export function applyChallenge(detail: ContributionDetail, actor: Actor, stance: Stance, body: string, nowISO: string, replyTo?: string): ContributionDetail {
  return add(detail, signal("challenge", actor, stance, body, nowISO, replyTo));
}
