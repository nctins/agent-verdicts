import { describe, expect, it } from "vitest";
import type { components } from "./api/schema";
import { canSave, fingerprint, fromSettings, pipelineLabel, toInput, warningKey, type SettingsForm } from "./settings-form";

const settings: components["schemas"]["Settings"] = {
  embedding: { url: "http://e", model: "m", api_key_last4: "abcd", dim: 3 },
  rerank: { url: "", model: "", api_key_last4: "" },
  status: { pipeline: "bm25+vector", embedded_count: 1, total_count: 2, reindexing: false, warnings: [] },
};
const saved = fromSettings(settings);
const off: SettingsForm = { embedding: { ...saved.embedding, url: "" }, rerank: saved.rerank };

describe("settings-form", () => {
  it("fromSettings blanks the key", () => {
    expect(saved.embedding).toEqual({ url: "http://e", model: "m", apiKey: "", clearKey: false });
  });
  it("toInput keeps the key when empty, sets it, or deletes it", () => {
    expect(toInput(saved).embedding).toEqual({ url: "http://e", model: "m" });
    expect("api_key" in toInput(saved).embedding!).toBe(false);
    const set = { ...saved, embedding: { ...saved.embedding, apiKey: "k" } };
    expect(toInput(set).embedding!.api_key).toBe("k");
    const del = { ...saved, embedding: { ...saved.embedding, apiKey: "k", clearKey: true } };
    expect(toInput(del).embedding!.api_key).toBeNull();
  });
  it("fingerprint is the JSON of the input", () => {
    expect(fingerprint(saved)).toBe(JSON.stringify(toInput(saved)));
  });
  it("canSave needs a change and a passing test of exactly this form", () => {
    expect(canSave(saved, saved, null)).toBe(false);
    const edited = { ...saved, embedding: { ...saved.embedding, url: "http://x" } };
    expect(canSave(edited, saved, null)).toBe(false);
    expect(canSave(edited, saved, fingerprint(edited))).toBe(true);
    const again = { ...edited, embedding: { ...edited.embedding, url: "http://y" } };
    expect(canSave(again, saved, fingerprint(edited))).toBe(false);
  });
  it("turning everything off needs no test", () => {
    expect(canSave(off, saved, null)).toBe(true);
    expect(canSave(off, off, null)).toBe(false);
  });
  it("pipelineLabel", () => {
    expect(pipelineLabel({ ...settings.status, pipeline: "bm25+vector+rerank" })).toBe("BM25 + vector + rerank");
    expect(pipelineLabel({ ...settings.status, pipeline: "bm25" })).toBe("BM25");
  });
  it("warningKey", () => {
    expect(warningKey("api_key_unreadable:embedding")).toEqual({ key: "settings.warning.api_key_unreadable", values: { provider: "embedding" } });
    expect(warningKey("zzz")).toEqual({ key: "settings.warning.unknown", values: { code: "zzz" } });
  });
});
