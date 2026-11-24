"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { Suspense, useEffect, type ReactNode } from "react";
import { ApiError } from "@/lib/api/errors";
import { can, canManage, isAdmin, managesAny, type Action } from "@/lib/access";
import { useSession } from "@/lib/api/queries";
import { Loading } from "@/components/Loading";

/** What a page needs: nothing, a session, an action (instance-wide), any admin page ("manage"),
 * member:Manage on some project ("members"), or either actor:Manage or that ("people"). */
export type Require = "none" | "login" | "manage" | "members" | "people" | Action;

function Gate({ require, children }: { require: Require; children: ReactNode }) {
  const t = useTranslations();
  const router = useRouter();
  const path = usePathname();
  const search = useSearchParams().toString();
  const { data: s, error } = useSession();

  // Spec §5.1, in order; first match wins.
  let target: string | null = null;
  if (s) {
    const onPage = (p: string) => path === p || path === p.slice(0, -1);
    if (s.setup_required) {
      if (!onPage("/setup/")) target = "/setup/";
    } else if (onPage("/setup/")) target = isAdmin(s) ? "/manage/" : "/"; // setup logs the new admin in
    else if (require !== "none" && !s.authenticated)
      target = "/login/?next=" + encodeURIComponent(path + (search ? "?" + search : ""));
    else if (s.authenticated && s.must_change_password && require !== "none" && !onPage("/account/"))
      target = "/account/";
  }

  useEffect(() => {
    if (target) router.replace(target);
  }, [target, router]);

  if (!s) {
    if (error && require === "none") return <>{children}</>; // public pages need no session
    const code = error instanceof ApiError ? error.code : error ? "network" : null;
    return code ? <p role="alert">{t(`errors.${code}`)}</p> : <Loading />;
  }
  if (target) return <Loading />;
  const allowed = require === "none" || require === "login" || (require === "manage" ? canManage(s) : require === "members" ? managesAny(s) : require === "people" ? can(s, "actor:Manage") || managesAny(s) : can(s, require));
  if (!allowed) return <p role="alert">{t("errors.forbidden")}</p>;
  return <>{children}</>;
}

// Suspense is here because Gate reads useSearchParams; pages need not wrap it.
export function SessionGate(props: { require: Require; children: ReactNode }) {
  return (
    <Suspense fallback={<Loading />}>
      <Gate {...props} />
    </Suspense>
  );
}
