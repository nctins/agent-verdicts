export type Locale = "vi" | "en";
export const LOCALES: Locale[] = ["vi", "en"];
export const LOCALE_KEY = "av.locale";

// Stored choice → browser language (en* → en) → vi.
export function resolveLocale(stored: string | null, navigatorLanguage: string | undefined): Locale {
  if (stored === "vi" || stored === "en") return stored;
  return navigatorLanguage?.toLowerCase().startsWith("en") ? "en" : "vi";
}
