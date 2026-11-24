import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api, unwrap } from "@/lib/api/client";
import { ApiError } from "@/lib/api/errors";
import { keys, type SearchOpts } from "@/lib/api/keys";
import type { components } from "@/lib/api/schema";

export type SearchItem = components["schemas"]["SearchItem"];
type Result = { results: SearchItem[]; degraded: boolean; text: string };

const NONE: Result = { results: [], degraded: false, text: "" };

/** `value`, but only after it has stayed unchanged for `delayMs`. */
export function useDebounced<T>(value: T, delayMs = 250): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(t);
  }, [value, delayMs]);
  return debounced;
}

/** Search-as-you-type (spec §5.2). A changed text cancels the in-flight request via the query's signal. */
export function useDebouncedSearch(
  project: string,
  text: string,
  {
    delayMs = 250,
    limit = 5,
    rerank = false,
    kind,
    state,
    questionStatus,
    includeSuperseded,
  }: {
    delayMs?: number;
    limit?: number;
    rerank?: boolean;
    kind?: SearchItem["kind"];
    state?: SearchOpts["state"];
    questionStatus?: SearchOpts["question_status"];
    includeSuperseded?: boolean;
  } = {},
) {
  const q = useDebounced(text, delayMs).trim();
  const query = useQuery({
    queryKey: keys.search(project, q, { limit, rerank, kind, state, question_status: questionStatus, include_superseded: includeSuperseded }),
    queryFn: async ({ signal }): Promise<Result> => {
      try {
        const r = await unwrap(
          api.GET("/projects/{p}/search", { params: { path: { p: project }, query: { q, limit, rerank, kind, state, question_status: questionStatus, include_superseded: includeSuperseded } }, signal }),
        );
        return { ...r, text: q };
      } catch (e) {
        // A query the server rejects (too long, odd syntax) simply matches nothing.
        if (e instanceof ApiError && e.status === 400) return { ...NONE, text: q };
        throw e;
      }
    },
    enabled: q !== "",
    placeholderData: keepPreviousData,
  });

  const data = q === "" ? NONE : (query.data ?? NONE);
  return {
    results: data.results,
    degraded: data.degraded,
    loading: text.trim() !== "" && (text.trim() !== q || query.isFetching),
    /** The text `results` belong to; compare with the current text before trusting them. */
    settledText: data.text,
    /** `results` are kept from a previous key (text or options) while the current one loads. */
    stale: q !== "" && query.isPlaceholderData,
    /** A failure other than 400 (which counts as no results). */
    error: q === "" ? null : query.error,
  };
}
