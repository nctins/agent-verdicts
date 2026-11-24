// Only same-origin relative paths; anything that could leave the site → "/".
export function safeNext(raw: string | null): string {
  if (!raw || raw[0] !== "/" || raw[1] === "/" || raw[1] === "\\") return "/";
  try {
    const u = new URL(raw, "http://x.invalid");
    if (u.origin !== "http://x.invalid" || u.pathname.startsWith("/login")) return "/";
    return u.pathname + u.search + u.hash;
  } catch {
    return "/";
  }
}

/** Login URL that comes back to the current page. */
export const loginHref = () => "/login/?next=" + encodeURIComponent(location.pathname + location.search);

/** Where the login form goes: a password that must change comes first (the server refuses most else until then). */
export const afterLogin = (s: { must_change_password: boolean }, next: string | null) =>
  s.must_change_password ? "/account/" : safeNext(next);
