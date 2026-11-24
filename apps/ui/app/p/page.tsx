"use client";

import { usePathname } from "next/navigation";
import { Suspense } from "react";
import { AppShell } from "@/components/AppShell";
import { ContributionView } from "@/components/ContributionView";
import { Missing } from "@/components/Missing";
import { ProjectView } from "@/components/ProjectView";
import { SessionGate } from "@/components/SessionGate";
import { parseRoute } from "@/lib/routes";

// One static page for /p/<project>/ and /p/<project>/c/<id>/: nginx (and the
// dev rewrite in next.config.ts) serve it for any /p/ path, and it reads the path.
function Route() {
  const r = parseRoute(usePathname());
  if (!r) return <Missing />;
  if (r.id) return <ContributionView key={r.project + "/" + r.id} project={r.project} id={r.id} />;
  return <ProjectView key={r.project} id={r.project} />;
}

export default function ProjectRoute() {
  return (
    <AppShell>
      <SessionGate require="login">
        <Suspense>
          <Route />
        </Suspense>
      </SessionGate>
    </AppShell>
  );
}
