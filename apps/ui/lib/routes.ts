// Readable URLs: /p/<project>/ and /p/<project>/c/<id>/. Both are one static
// page (app/p) that nginx serves for any /p/ path; it reads the path back here.
const enc = encodeURIComponent;

export const projectHref = (id: string, query?: URLSearchParams) => {
  const q = query?.toString();
  return `/p/${enc(id)}/` + (q ? "?" + q : "");
};

export const contributionHref = (project: string, id: string) => `/p/${enc(project)}/c/${enc(id)}/`;

const ROUTE = /^\/p\/([^/]+)(?:\/c\/([^/]+))?\/?$/;

export function parseRoute(pathname: string): { project: string; id: string | null } | null {
  const m = ROUTE.exec(pathname);
  if (!m) return null;
  try {
    return { project: decodeURIComponent(m[1]), id: m[2] ? decodeURIComponent(m[2]) : null };
  } catch {
    return null; // malformed percent-encoding
  }
}

/** The old query URLs (/project/?id=, /contribution/?project=&id=) as new paths, so saved links keep working. */
export function legacyHref(pathname: string, sp: URLSearchParams, hash = ""): string {
  if (pathname.startsWith("/contribution")) {
    const project = sp.get("project"), id = sp.get("id");
    if (!project) return "/";
    return (id ? contributionHref(project, id) : projectHref(project)) + hash;
  }
  const id = sp.get("id");
  if (!id) return "/";
  const rest = new URLSearchParams(sp);
  rest.delete("id");
  return projectHref(id, rest) + hash;
}
