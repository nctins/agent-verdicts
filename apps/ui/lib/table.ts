// Pure parts of the admin DataTable: what the URL says, and which rows that leaves.

/** Lower case without Vietnamese accents (đ = d), like the server's textfold: "Bình" matches "binh". */
export const fold = (s: string) => s.normalize("NFD").replace(/\p{M}/gu, "").replace(/đ/g, "d").replace(/Đ/g, "d").toLowerCase();

export type SortDir = "asc" | "desc";
export type TableState = { q: string; sort: string | null; dir: SortDir; page: number; filters: Record<string, string> };

/** Read a table's state from the URL; `prefix` keeps two tables on one page apart. */
export function readTableState(sp: URLSearchParams, filterIds: string[], prefix = ""): TableState {
  const page = Number.parseInt(sp.get(prefix + "page") ?? "1", 10);
  const filters: Record<string, string> = {};
  for (const id of filterIds) {
    const v = sp.get(prefix + id);
    if (v) filters[id] = v;
  }
  return {
    q: sp.get(prefix + "q") ?? "",
    sort: sp.get(prefix + "sort"),
    dir: sp.get(prefix + "dir") === "desc" ? "desc" : "asc",
    page: Number.isFinite(page) && page > 0 ? page : 1,
    filters,
  };
}

/** The URL after `changes` (null removes a key); any change but paging goes back to page 1. */
export function writeTableState(sp: URLSearchParams, changes: Record<string, string | null>, prefix = ""): URLSearchParams {
  const next = new URLSearchParams(sp);
  for (const [k, v] of Object.entries(changes)) {
    if (v === null || v === "") next.delete(prefix + k);
    else next.set(prefix + k, v);
  }
  if (!("page" in changes)) next.delete(prefix + "page");
  return next;
}

export type Matchers<T> = {
  text?: (row: T) => string;
  filters?: Record<string, (row: T, value: string) => boolean>;
  sorts?: Record<string, (row: T) => string | number | null>;
};

/** Rows left by the search and filters, sorted (nulls last, either direction), and the current page of them. */
export function applyTable<T>(rows: T[], s: TableState, m: Matchers<T>, pageSize: number) {
  const q = fold(s.q.trim());
  let out = rows.filter(
    (r) => (!q || !m.text || fold(m.text(r)).includes(q)) && Object.entries(s.filters).every(([id, v]) => !m.filters?.[id] || m.filters[id](r, v)),
  );
  const key = s.sort ? m.sorts?.[s.sort] : undefined;
  if (key) {
    const sign = s.dir === "desc" ? -1 : 1;
    out = [...out].sort((a, b) => {
      const x = key(a), y = key(b);
      if (x === y) return 0;
      if (x === null) return 1;
      if (y === null) return -1;
      return (typeof x === "number" && typeof y === "number" ? x - y : String(x).localeCompare(String(y))) * sign;
    });
  }
  const pages = Math.max(1, Math.ceil(out.length / pageSize));
  const page = Math.min(s.page, pages);
  return { matched: out, total: out.length, page, pages, rows: out.slice((page - 1) * pageSize, page * pageSize) };
}
