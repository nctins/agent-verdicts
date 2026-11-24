// Previous/next through the list a contribution was opened from (spec 2026-11-22-project-ui-redesign P18).
// Per tab, in sessionStorage; storage may be missing or throw, and then there is simply no previous/next.
const KEY = "av.list-nav";

export type ListNav = { url: string; ids: string[] };

export function saveListNav(nav: ListNav) {
  try {
    sessionStorage.setItem(KEY, JSON.stringify(nav));
  } catch {
    /* no storage: no previous/next */
  }
}

export function loadListNav(): ListNav | null {
  try {
    const v = JSON.parse(sessionStorage.getItem(KEY) ?? "null");
    return v && typeof v.url === "string" && Array.isArray(v.ids) ? v : null;
  } catch {
    return null;
  }
}

/** The neighbours of id in nav, or null when id is not in it. */
export function neighbours(nav: ListNav | null, id: string): { prev?: string; next?: string; url: string } | null {
  const at = nav?.ids.indexOf(id) ?? -1;
  if (!nav || at < 0) return null;
  return { prev: nav.ids[at - 1], next: nav.ids[at + 1], url: nav.url };
}
