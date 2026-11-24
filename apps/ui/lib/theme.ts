export type ThemePref = "light" | "dark" | "system";
export const THEME_KEY = "av.theme";
export const DARK_QUERY = "(prefers-color-scheme: dark)";

export function readThemePref(stored: string | null): ThemePref {
  return stored === "light" || stored === "dark" ? stored : "system";
}

export function resolveTheme(pref: ThemePref, prefersDark: boolean): "light" | "dark" {
  if (pref === "system") return prefersDark ? "dark" : "light";
  return pref;
}

/** The toggle flips what is on screen and stores that as an explicit choice. */
export function nextTheme(pref: ThemePref, prefersDark: boolean): "light" | "dark" {
  return resolveTheme(pref, prefersDark) === "dark" ? "light" : "dark";
}

// Inlined in <head> so the `dark` class is set before first paint (no flash).
// Mirrors readThemePref + resolveTheme.
export const themeBootScript = `try{var t=localStorage.getItem(${JSON.stringify(THEME_KEY)});if(t!=="light"&&t!=="dark")t=matchMedia(${JSON.stringify(DARK_QUERY)}).matches?"dark":"light";document.documentElement.classList.toggle("dark",t==="dark")}catch(e){}`;
