import { keepPreviousData, useInfiniteQuery, useIsMutating, useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { useEffect } from "react";
import { toast } from "sonner";
import { applyAssessment, applyChallenge } from "@/lib/stance";
import { api, setCsrfToken, unwrap } from "./client";
import { ApiError, errorMessageKey } from "./errors";
import { keys, type AuditFilters, type ContributionFilters } from "./keys";
import type { components, paths } from "./schema";

type Json<P extends keyof paths, M extends "post" | "put" | "patch"> = paths[P][M] extends {
  requestBody?: { content: { "application/json": infer B } };
}
  ? B
  : never;

const SECOND = 1000;
type Detail = components["schemas"]["ContributionDetail"];

export const useSession = () => {
  const q = useQuery({ queryKey: keys.session(), queryFn: () => unwrap(api.GET("/session")) });
  const token = q.data?.csrf_token;
  useEffect(() => {
    if (token !== undefined) setCsrfToken(token);
  }, [token]);
  return q;
};

export const useProjects = () =>
  useQuery({ queryKey: keys.projects(), queryFn: () => unwrap(api.GET("/projects")), refetchInterval: 30 * SECOND });

export const useContributions = (p: string, filters: ContributionFilters) =>
  useInfiniteQuery({
    queryKey: keys.contributions(p, filters),
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET("/projects/{p}/contributions", {
          params: { path: { p }, query: { ...filters, cursor: pageParam ?? undefined } },
        }),
      ),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.next_cursor,
    refetchInterval: 30 * SECOND,
  });

// Signal writes in flight per contribution (onMutate → onSettled). A detail fetch that overlaps one
// may predate the write, so it keeps the optimistic cache instead (Review Focus 1); onSettled refetches.
const signalsInFlight = new Map<string, number>();
const inFlight = (p: string, id: string) => (signalsInFlight.get(JSON.stringify(keys.contribution(p, id))) ?? 0) > 0;
const bumpInFlight = (p: string, id: string, by: number) => {
  const k = JSON.stringify(keys.contribution(p, id));
  const n = (signalsInFlight.get(k) ?? 0) + by;
  if (n > 0) signalsInFlight.set(k, n);
  else signalsInFlight.delete(k);
};

export const useContribution = (p: string, id: string) => {
  const qc = useQueryClient();
  return useQuery({
    queryKey: keys.contribution(p, id),
    queryFn: async () => {
      const before = inFlight(p, id);
      const d = await unwrap(api.GET("/projects/{p}/contributions/{id}", { params: { path: { p, id } } }));
      return before || inFlight(p, id) ? (qc.getQueryData<Detail>(keys.contribution(p, id)) ?? d) : d;
    },
    refetchInterval: 15 * SECOND,
    // A 404 will not heal by retrying; show not-found at once.
    retry: (n, e) => !(e instanceof ApiError && e.status >= 400 && e.status < 500) && n < 3,
  });
};

/** Who wrote a contribution the caller can see; the choices of the list's author filter. */
export const useAuthors = (p: string) =>
  useQuery({ queryKey: keys.authors(p), queryFn: () => unwrap(api.GET("/projects/{p}/authors", { params: { path: { p } } })) });

/** Read counts are for managers only; the page uses just today's range, for the retention setting. */
export const useReadRetention = (p: string, enabled: boolean) =>
  useQuery({
    queryKey: keys.reads(p, "today"),
    queryFn: () => {
      const today = new Date().toISOString().slice(0, 10);
      return unwrap(api.GET("/projects/{p}/reads", { params: { path: { p }, query: { from: today, to: today } } }));
    },
    enabled,
  });

/** The last 30 days of read counts (member:Manage): the overview card and the per-contribution count. */
export const useReads30 = (p: string, enabled: boolean) =>
  useQuery({
    queryKey: keys.reads(p, "30d"),
    queryFn: () => unwrap(api.GET("/projects/{p}/reads", { params: { path: { p } } })), // the default range is 30 days to today
    enabled,
  });

/** Hides or unhides several contributions one by one (spec 2026-11-22-project-ui-redesign P15); resolves to how many failed. */
export const useBulkSetHidden = (p: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ ids, hidden, reason }: { ids: string[]; hidden: boolean; reason?: string }) => {
      let failed = 0;
      for (const id of ids) {
        try {
          await unwrap(api.PUT("/projects/{p}/contributions/{id}/hidden", { params: { path: { p, id } }, body: hidden ? { hidden, ...(reason ? { reason } : {}) } : { hidden } }));
        } catch {
          failed++;
        }
      }
      return { done: ids.length - failed, failed };
    },
    onSettled: () => inv(qc, keys.contributions(p), keys.searches(p), keys.projects(), keys.adminOverview()),
  });
};

export const useSetReadRetention = (p: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Json<"/projects/{p}/reads/retention", "put">) =>
      unwrap(api.PUT("/projects/{p}/reads/retention", { params: { path: { p } }, body })),
    onSuccess: () => inv(qc, keys.reads(p), keys.audit()),
  });
};

export const useAdminOverview = () =>
  useQuery({ queryKey: keys.adminOverview(), queryFn: () => unwrap(api.GET("/admin/overview")) });
/** `q` (at least 2 characters) searches by name; a manager without actor:Manage sees only their projects' members without it. */
export const useActors = (q?: string, enabled = true) =>
  useQuery({
    queryKey: keys.actors(q),
    queryFn: () => unwrap(api.GET("/admin/actors", { params: { query: q ? { q } : {} } })),
    placeholderData: keepPreviousData, // typing must not unmount the search box behind a spinner
    enabled,
  });
export const useActor = (id: string) =>
  useQuery({
    queryKey: keys.actor(id),
    queryFn: () => unwrap(api.GET("/admin/actors/{id}", { params: { path: { id } } })),
  });
export const useAudit = (filters: AuditFilters, enabled = true) =>
  useInfiniteQuery({
    queryKey: keys.audit(filters),
    queryFn: ({ pageParam }) =>
      unwrap(api.GET("/admin/audit", { params: { query: { ...filters, cursor: pageParam ?? undefined } } })),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.next_cursor,
    enabled,
  });
export const useAdminProjects = () =>
  useQuery({ queryKey: keys.adminProjects(), queryFn: () => unwrap(api.GET("/admin/projects")) });

/** One project's list item (name, archived); absent while the list loads, or when the project is not readable. */
export const useProjectInfo = (p: string) => {
  const projects = useProjects();
  const meta = projects.data?.items.find((x) => x.id === p);
  // Judged only on a settled list: a cached one may predate a project an agent just created.
  return { meta, unlisted: !!projects.data && !projects.isFetching && !meta, archived: !!meta?.archived };
};
export const useSettings = () =>
  useQuery({
    queryKey: keys.settings(),
    queryFn: () => unwrap(api.GET("/admin/settings/search")),
    refetchInterval: (q) => (q.state.data?.status.reindexing ? 2 * SECOND : false),
  });

const inv = (qc: QueryClient, ...k: (readonly unknown[])[]) =>
  Promise.all(k.map((queryKey) => qc.invalidateQueries({ queryKey })));

// Session-changing mutations: the cached view depends on who is logged in.
// gcTime 0 here and on settings writes: their variables carry a password or API key.
const useSessionMutation = <V, R>(fn: (v: V) => Promise<R>, clear = false) => {
  const qc = useQueryClient();
  return useMutation({
    gcTime: 0,
    mutationFn: fn,
    onSuccess: async () => {
      if (clear) qc.clear();
      else await inv(qc, keys.session());
    },
  });
};

export const useLogin = () =>
  useSessionMutation((body: Json<"/session", "post">) => unwrap(api.POST("/session", { body })));
export const useLogout = () => useSessionMutation(() => unwrap(api.DELETE("/session")), true);
export const useSetup = () =>
  useSessionMutation((body: Json<"/setup", "post">) => unwrap(api.POST("/setup", { body })));
export const useChangePassword = () =>
  useSessionMutation((body: Json<"/account/password", "post">) => unwrap(api.POST("/account/password", { body })));

export const useCreateContribution = (p: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Json<"/projects/{p}/contributions", "post">) =>
      unwrap(api.POST("/projects/{p}/contributions", { params: { path: { p } }, body })),
    onSuccess: (_d, body) =>
      inv(
        qc,
        keys.contributions(p),
        keys.searches(p),
        keys.authors(p),
        keys.projects(),
        keys.adminOverview(),
        ...(body.answers_id ? [keys.contribution(p, body.answers_id)] : []),
        ...(body.supersedes_id ? [keys.contribution(p, body.supersedes_id)] : []),
      ),
  });
};

/** True while any signal write on this contribution runs; every signal control locks on it. */
export const useSignalBusy = (p: string, id: string) => useIsMutating({ mutationKey: keys.signal(p, id) }) > 0;

export const useCreateSignal = (p: string, id: string) => {
  const qc = useQueryClient();
  const t = useTranslations();
  const key = keys.contribution(p, id);
  return useMutation({
    mutationKey: keys.signal(p, id),
    mutationFn: (body: Json<"/projects/{p}/contributions/{id}/signals", "post">) =>
      unwrap(api.POST("/projects/{p}/contributions/{id}/signals", { params: { path: { p, id } }, body })),
    onMutate: async (body) => {
      bumpInFlight(p, id, 1);
      await qc.cancelQueries({ queryKey: key }); // a running poll must not land on top of the optimistic data
      const previous = qc.getQueryData<Detail>(key);
      const actor = qc.getQueryData<components["schemas"]["Session"]>(keys.session())?.actor;
      if (previous && actor) {
        const now = new Date().toISOString();
        qc.setQueryData<Detail>(
          key,
          body.kind === "assessment"
            ? applyAssessment(previous, actor, body.stance, now)
            : applyChallenge(previous, actor, body.stance, body.body ?? "", now, body.reply_to),
        );
      }
      return { previous };
    },
    onError: (err, _body, ctx) => {
      if (ctx?.previous) qc.setQueryData(key, ctx.previous);
      if (!(err instanceof ApiError && err.status === 401)) // 401: the client already redirects to login
        toast.error(t(errorMessageKey(err instanceof ApiError ? err.code : "internal")));
    },
    onSettled: () => {
      bumpInFlight(p, id, -1);
      const parent = qc.getQueryData<Detail>(key)?.answer_to?.id;
      // Not awaited: the refetch must run after this write leaves the in-flight count.
      void inv(
        qc,
        key,
        keys.contributions(p),
        keys.searches(p),
        keys.projects(),
        keys.adminOverview(),
        ...(parent ? [keys.contribution(p, parent)] : []),
      );
    },
  });
};

// The writes on one contribution that are not optimistic: withdraw, hide, undo supersession.
// They share the signal mutation key, so every signal control locks while one runs.
const useDetailWrite = <V>(p: string, id: string, fn: (v: V) => Promise<unknown>) => {
  const qc = useQueryClient();
  const t = useTranslations();
  return useMutation({
    mutationKey: keys.signal(p, id),
    mutationFn: fn,
    onError: (err) => {
      if (!(err instanceof ApiError && err.status === 401))
        toast.error(t(errorMessageKey(err instanceof ApiError ? err.code : "internal")));
    },
    onSettled: () => {
      const parent = qc.getQueryData<Detail>(keys.contribution(p, id))?.answer_to?.id;
      return inv(qc, keys.contribution(p, id), keys.contributions(p), keys.searches(p), keys.projects(), keys.adminOverview(), ...(parent ? [keys.contribution(p, parent)] : []));
    },
  });
};

export const useWithdrawSignal = (p: string, id: string) =>
  useDetailWrite(p, id, (sid: string) =>
    unwrap(api.DELETE("/projects/{p}/contributions/{id}/signals/{sid}", { params: { path: { p, id, sid } } })),
  );
export const useSetSignalHidden = (p: string, id: string) =>
  useDetailWrite(p, id, ({ sid, ...body }: { sid: string; hidden: boolean; reason?: string }) =>
    unwrap(api.PUT("/projects/{p}/contributions/{id}/signals/{sid}/hidden", { params: { path: { p, id, sid } }, body })),
  );
export const useSetHidden = (p: string, id: string) =>
  useDetailWrite(p, id, (body: { hidden: boolean; reason?: string }) =>
    unwrap(api.PUT("/projects/{p}/contributions/{id}/hidden", { params: { path: { p, id } }, body })),
  );
export const useUndoSupersession = (p: string, id: string) =>
  useDetailWrite(p, id, () =>
    unwrap(api.DELETE("/projects/{p}/contributions/{id}/supersession", { params: { path: { p, id } } })),
  );

// gcTime 0: results can carry a one-time password/token, so the cache must not keep them after reset().
const useActorMutation = <V, R>(fn: (v: V) => Promise<R>, id?: string) => {
  const qc = useQueryClient();
  return useMutation({
    gcTime: 0,
    mutationFn: fn,
    onSuccess: () => inv(qc, keys.actors(), keys.adminOverview(), keys.audit(), ...(id ? [keys.actor(id)] : [])),
  });
};

export const useCreateActor = () =>
  useActorMutation((body: Json<"/admin/actors", "post">) => unwrap(api.POST("/admin/actors", { body })));
export const usePatchActor = (id: string) =>
  useActorMutation(
    (body: Json<"/admin/actors/{id}", "patch">) =>
      unwrap(api.PATCH("/admin/actors/{id}", { params: { path: { id } }, body })),
    id,
  );
/** Disable or enable several actors, one request each; returns the ids that failed with their error code. */
export const useBulkSetDisabled = () =>
  useActorMutation(async ({ ids, disabled }: { ids: string[]; disabled: boolean }) => {
    const failed: { id: string; code: string }[] = [];
    for (const id of ids) {
      try {
        await unwrap(api.PATCH("/admin/actors/{id}", { params: { path: { id } }, body: { disabled } }));
      } catch (e) {
        failed.push({ id, code: e instanceof ApiError ? e.code : "network" });
      }
    }
    return failed;
  });
export const useResetPassword = (id: string) =>
  useActorMutation(
    (body: Json<"/admin/actors/{id}/password", "post">) =>
      unwrap(api.POST("/admin/actors/{id}/password", { params: { path: { id } }, body })),
    id,
  );
export const useIssueCredential = (id: string) =>
  useActorMutation(
    (body: Json<"/admin/actors/{id}/credentials", "post">) =>
      unwrap(api.POST("/admin/actors/{id}/credentials", { params: { path: { id } }, body })),
    id,
  );
export const useRevokeCredential = (actorId: string) =>
  useActorMutation(
    (credentialId: string) =>
      unwrap(api.DELETE("/admin/credentials/{id}", { params: { path: { id: credentialId } } })),
    actorId,
  );

export const useCreateProject = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Json<"/admin/projects", "post">) => unwrap(api.POST("/admin/projects", { body })),
    onSuccess: () => inv(qc, keys.adminProjects(), keys.projects(), keys.audit()),
  });
};

export const useUpdateProject = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: { id: string } & Json<"/admin/projects/{id}", "patch">) =>
      unwrap(api.PATCH("/admin/projects/{id}", { params: { path: { id } }, body })),
    onSuccess: () => inv(qc, keys.adminProjects(), keys.projects(), keys.audit()),
  });
};

export const useTestSettings = () =>
  useMutation({
    gcTime: 0,
    mutationFn: (body: Json<"/admin/settings/search/test", "post">) =>
      unwrap(api.POST("/admin/settings/search/test", { body })),
  });
export const usePutSettings = () => {
  const qc = useQueryClient();
  return useMutation({
    gcTime: 0,
    mutationFn: (body: Json<"/admin/settings/search", "put">) => unwrap(api.PUT("/admin/settings/search", { body })),
    onSuccess: () => inv(qc, keys.settings(), keys.audit()),
  });
};
export const useReindex = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => unwrap(api.POST("/admin/settings/search/reindex")),
    onSuccess: () => inv(qc, keys.settings(), keys.audit()),
  });
};

// Access control: policies, roles, assignments. Any change may change what someone
// (possibly the caller) may do, so it also refreshes the session and actor pages.
export const usePolicies = () => useQuery({ queryKey: keys.policies(), queryFn: () => unwrap(api.GET("/admin/policies")) });
export const useRoles = () => useQuery({ queryKey: keys.roles(), queryFn: () => unwrap(api.GET("/admin/roles")) });
export const useAssignments = (f: { actor?: string; project?: string } = {}, enabled = true) =>
  useQuery({ queryKey: keys.assignments(f), queryFn: () => unwrap(api.GET("/admin/assignments", { params: { query: f } })), enabled });

const useAccessMutation = <V, R>(fn: (v: V) => Promise<R>) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => inv(qc, keys.policies(), keys.roles(), keys.assignments(), keys.session(), ["admin", "actor"], keys.projects(), keys.audit()),
  });
};

type PolicyBody = Json<"/admin/policies", "post">;
export const useSavePolicy = () =>
  useAccessMutation(({ id, ...body }: PolicyBody & { id?: string }) =>
    id ? unwrap(api.PUT("/admin/policies/{id}", { params: { path: { id } }, body })) : unwrap(api.POST("/admin/policies", { body })),
  );
export const useDeletePolicy = () =>
  useAccessMutation((id: string) => unwrap(api.DELETE("/admin/policies/{id}", { params: { path: { id } } })));

type RoleBody = Json<"/admin/roles", "post">;
export const useSaveRole = () =>
  useAccessMutation(({ id, ...body }: RoleBody & { id?: string }) =>
    id ? unwrap(api.PUT("/admin/roles/{id}", { params: { path: { id } }, body })) : unwrap(api.POST("/admin/roles", { body })),
  );
export const useDeleteRole = () =>
  useAccessMutation((id: string) => unwrap(api.DELETE("/admin/roles/{id}", { params: { path: { id } } })));

export const useCreateAssignment = () =>
  useAccessMutation((body: Json<"/admin/assignments", "post">) => unwrap(api.POST("/admin/assignments", { body })));
export const useDeleteAssignment = () =>
  useAccessMutation((id: string) => unwrap(api.DELETE("/admin/assignments/{id}", { params: { path: { id } } })));
