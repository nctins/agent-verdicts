import type { ContributionFilters } from "@/lib/api/keys";
import type { TrustState } from "@/lib/trust";
import type { SearchItem } from "@/lib/use-debounced-search";

export type Tab = "overview" | "claims" | "questions" | "review" | "settings";
/** The tabs that list contributions. */
export type ListTab = "claims" | "questions" | "review";
export type Sort = "newest" | "debated" | "least" | "active";
export type QuestionStatus = NonNullable<SearchItem["question_status"]>;
export const TABS: readonly Tab[] = ["overview", "claims", "questions", "review", "settings"];
export const SORTS: readonly Sort[] = ["newest", "debated", "least", "active"];
export const QUESTION_STATUSES: readonly QuestionStatus[] = ["open", "answered", "resolved"];

const TAB_FILTER: Record<Tab, ContributionFilters> = {
  overview: {},
  settings: {},
  claims: { kind: "claim" },
  questions: { kind: "question" },
  review: { kind: "claim", state: ["Conflict", "InsufficientEvidence"] },
};

/** The list filter for a tab: Claims may narrow to one trust state, Questions to one status. */
export function listFilter(tab: Tab, state?: TrustState | null, status?: QuestionStatus | null): ContributionFilters {
  return {
    ...TAB_FILTER[tab],
    ...(tab === "claims" && state ? { state: [state] } : {}),
    ...(tab === "questions" && status ? { question_status: [status] } : {}),
  };
}

const pick = <T extends string>(v: string | null, all: readonly T[], def: T): T =>
  all.includes(v as T) ? (v as T) : def;

export const STATES: readonly TrustState[] = ["Favorable", "Unfavorable", "Conflict", "InsufficientEvidence"];

/** The project page's query (the project itself is in the path). */
export function parseProjectParams(sp: URLSearchParams) {
  const state = sp.get("state");
  const raw = sp.get("tab");
  const legacyOpen = raw === "open"; // the old "Open questions" tab
  // Before the Overview tab, the default (and "all") listed contributions: an old link with
  // a list parameter still opens a list (spec 2026-11-22-project-ui-redesign P6).
  const listy = ["state", "q", "actor", "sort", "superseded"].some((k) => sp.has(k));
  const tab: Tab = legacyOpen ? "questions" : raw === "all" || (raw === null && listy) ? "claims" : pick(raw, TABS, "overview");
  return {
    tab,
    actor: sp.get("actor") ?? "",
    sort: pick(sp.get("sort"), SORTS, "newest"),
    q: (sp.get("q") ?? "").trim(),
    superseded: sp.get("superseded") === "1",
    state: STATES.includes(state as TrustState) ? (state as TrustState) : null,
    status: legacyOpen ? ("open" as const) : QUESTION_STATUSES.includes(sp.get("status") as QuestionStatus) ? (sp.get("status") as QuestionStatus) : null,
  };
}

/** The typed text as a question title: at most 200 code points (the server's limit). */
export const prefillTitle = (text: string) => [...text].slice(0, 200).join("");
