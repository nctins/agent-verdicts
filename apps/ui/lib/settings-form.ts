import type { components } from "./api/schema";

type Settings = components["schemas"]["Settings"];
type Status = components["schemas"]["Status"];
type SettingsInput = components["schemas"]["SettingsInput"];
type ProviderInput = components["schemas"]["ProviderInput"];

export type ProviderForm = { url: string; model: string; apiKey: string; clearKey: boolean };
export type SettingsForm = { embedding: ProviderForm; rerank: ProviderForm };

const provider = (p: { url: string; model: string }): ProviderForm => ({ url: p.url, model: p.model, apiKey: "", clearKey: false });

export const fromSettings = (s: Settings): SettingsForm => ({ embedding: provider(s.embedding), rerank: provider(s.rerank) });

const input = (f: ProviderForm): ProviderInput => ({
  url: f.url.trim(),
  model: f.model.trim(),
  ...(f.clearKey ? { api_key: null } : f.apiKey !== "" ? { api_key: f.apiKey } : {}), // absent keeps the stored key
});

export const toInput = (f: SettingsForm): SettingsInput => ({ embedding: input(f.embedding), rerank: input(f.rerank) });

export const fingerprint = (f: SettingsForm): string => JSON.stringify(toInput(f));

export const canSave = (f: SettingsForm, saved: SettingsForm, lastPassed: string | null): boolean => {
  const fp = fingerprint(f);
  if (fp === fingerprint(saved)) return false;
  if (f.embedding.url.trim() === "" && f.rerank.url.trim() === "") return true;
  return lastPassed === fp;
};

export const pipelineLabel = (s: Status): string => s.pipeline.replace("bm25", "BM25").replaceAll("+", " + ");

export const warningKey = (code: string): { key: string; values: Record<string, string> } => {
  const [name, provider] = code.split(":");
  return name === "api_key_unreadable" && provider
    ? { key: "settings.warning.api_key_unreadable", values: { provider } }
    : { key: "settings.warning.unknown", values: { code } };
};
