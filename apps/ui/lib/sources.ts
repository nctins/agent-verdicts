export const MAX_SOURCES = 10;
export const MAX_SOURCE_LEN = 500;

/** One source per line; blank lines are dropped. */
export const parseSources = (text: string): string[] =>
  text.split("\n").map((l) => l.trim()).filter(Boolean);

/** Client-side check mirroring the API limits; returns a message key. */
export function sourcesProblem(sources: string[]): string | null {
  if (sources.length > MAX_SOURCES) return "sources.error_too_many";
  if (sources.some((s) => [...s].length > MAX_SOURCE_LEN)) return "sources.error_too_long";
  return null;
}
