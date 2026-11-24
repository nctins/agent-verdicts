import type { components } from "./schema";

export type ErrorCode = components["schemas"]["Error"]["code"] | "network" | "project_archived";

const CODES = new Set<string>([
  "invalid_argument", "unauthenticated", "forbidden", "not_found", "conflict",
  "too_many_requests", "upstream_failed", "internal", "network",
]);

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: ErrorCode,
    message?: string,
  ) {
    super(message ?? code);
    this.name = "ApiError";
  }
}

/** A 409 because another actor already has the name (the server's message tells it from a taken username). */
export const isNameTaken = (e: unknown): boolean => e instanceof ApiError && e.status === 409 && e.message.startsWith("actor: name already in use");

export const errorMessageKey = (code: ErrorCode): string => `errors.${code}`;

/** Resolves to the response data; rejects with ApiError (network failures and proxy 502/503/504 get code "network"). */
export async function unwrap<T>(p: Promise<{ data?: T; error?: unknown; response: Response }>): Promise<T> {
  let r;
  try {
    r = await p;
  } catch (e) {
    if (e instanceof TypeError) throw new ApiError(0, "network", e.message);
    throw e;
  }
  if (r.error === undefined) return r.data as T;
  const e = r.error as { code?: unknown; message?: unknown } | string;
  const status = r.response.status;
  const known = typeof e === "object" && e && typeof e.code === "string" && CODES.has(e.code);
  // A gateway status without an API error body is the proxy saying api is unreachable.
  const message = typeof e === "object" && typeof e?.message === "string" ? e.message : undefined;
  // Every write into an archived project is a 409 with this message; it gets its own text instead of "conflict".
  const code = status === 409 && message === "project is archived" ? "project_archived" : known ? (e.code as ErrorCode) : status >= 502 && status <= 504 ? "network" : "internal";
  throw new ApiError(status, code, message);
}
