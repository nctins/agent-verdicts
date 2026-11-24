"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { Loading } from "@/components/Loading";

// Roles are now given on the actors' pages.
export default function AssignmentsRedirect() {
  const router = useRouter();
  useEffect(() => router.replace("/manage/actors/"), [router]);
  return <Loading />;
}
