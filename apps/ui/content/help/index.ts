// The guide's pages as Markdown, per language (spec 2026-11-23-help-rewrite H1). One import per file:
// the bundler inlines each as text (?raw: vitest natively, Turbopack via next.config.ts).

import vi_overview from "./vi/overview.md?raw";
import vi_concepts from "./vi/concepts.md?raw";
import vi_states from "./vi/states.md?raw";
import vi_stance from "./vi/stance.md?raw";
import vi_questions from "./vi/questions.md?raw";
import vi_search from "./vi/search.md?raw";
import vi_filters from "./vi/filters.md?raw";
import vi_shortcuts from "./vi/shortcuts.md?raw";
import vi_account from "./vi/account.md?raw";
import vi_projects from "./vi/projects.md?raw";
import vi_mcp from "./vi/mcp.md?raw";
import vi_ai from "./vi/ai.md?raw";
import vi_setup from "./vi/setup.md?raw";
import vi_actors from "./vi/actors.md?raw";
import vi_access from "./vi/access.md?raw";
import vi_audit from "./vi/audit.md?raw";
import vi_glossary from "./vi/glossary.md?raw";
import vi_quickstart from "./vi/quickstart.md?raw";
import vi_faq from "./vi/faq.md?raw";
import en_overview from "./en/overview.md?raw";
import en_concepts from "./en/concepts.md?raw";
import en_states from "./en/states.md?raw";
import en_stance from "./en/stance.md?raw";
import en_questions from "./en/questions.md?raw";
import en_search from "./en/search.md?raw";
import en_filters from "./en/filters.md?raw";
import en_shortcuts from "./en/shortcuts.md?raw";
import en_account from "./en/account.md?raw";
import en_projects from "./en/projects.md?raw";
import en_mcp from "./en/mcp.md?raw";
import en_ai from "./en/ai.md?raw";
import en_setup from "./en/setup.md?raw";
import en_actors from "./en/actors.md?raw";
import en_access from "./en/access.md?raw";
import en_audit from "./en/audit.md?raw";
import en_glossary from "./en/glossary.md?raw";
import en_quickstart from "./en/quickstart.md?raw";
import en_faq from "./en/faq.md?raw";

export const HELP_PAGES = ["overview", "concepts", "states", "stance", "questions", "search", "filters", "shortcuts", "account", "projects", "mcp", "ai", "setup", "actors", "access", "audit", "glossary", "quickstart", "faq"] as const;
export type HelpPage = (typeof HELP_PAGES)[number];

export const HELP_MD: Record<"vi" | "en", Record<HelpPage, string>> = {
  vi: { overview: vi_overview, concepts: vi_concepts, states: vi_states, stance: vi_stance, questions: vi_questions, search: vi_search, filters: vi_filters, shortcuts: vi_shortcuts, account: vi_account, projects: vi_projects, mcp: vi_mcp, ai: vi_ai, setup: vi_setup, actors: vi_actors, access: vi_access, audit: vi_audit, glossary: vi_glossary, quickstart: vi_quickstart, faq: vi_faq },
  en: { overview: en_overview, concepts: en_concepts, states: en_states, stance: en_stance, questions: en_questions, search: en_search, filters: en_filters, shortcuts: en_shortcuts, account: en_account, projects: en_projects, mcp: en_mcp, ai: en_ai, setup: en_setup, actors: en_actors, access: en_access, audit: en_audit, glossary: en_glossary, quickstart: en_quickstart, faq: en_faq },
};
