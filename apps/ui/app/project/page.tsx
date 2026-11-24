"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect } from "react";
import { Loading } from "@/components/Loading";
import { legacyHref } from "@/lib/routes";

// Old /project/?id= and /contribution/?project=&id= links: move to the /p/ path.
function Redirect() {
  const router = useRouter();
  const path = usePathname();
  const sp = useSearchParams();
  useEffect(() => router.replace(legacyHref(path, sp, window.location.hash)), [router, path, sp]);
  return <Loading />;
}

export default function LegacyRedirect() {
  return (
    <Suspense>
      <Redirect />
    </Suspense>
  );
}
