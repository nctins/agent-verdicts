import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { expect, it, vi } from "vitest";
import en from "@/messages/en.json";
import { AuditTable } from "./AuditTable";

const json = (b: unknown) => new Response(JSON.stringify(b), { headers: { "Content-Type": "application/json" } });
vi.stubGlobal("fetch", async (r: Request) => {
  const u = new URL(r.url).pathname;
  if (u.endsWith("/admin/roles")) return json({ items: [{ id: "r1", name: "contributor", managed: true, policy_ids: [], actions: [], created_at: "" }] });
  if (u.endsWith("/admin/policies")) return json({ items: [{ id: "pol1", name: "ReadOnlyAccess", managed: true, actions: [], created_at: "" }] });
  return json({ items: [] });
});

const ev = (target_type: string, target_id: string, detail: Record<string, unknown>) => ({
  id: target_id, at: "2026-11-14T00:00:00Z", actor_id: null, actor_name: "", action: "x", target_type, target_id, project_id: null, detail,
});

it("shows targets by name instead of ids, and keeps the id in the title", async () => {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}>
        <AuditTable
          names={new Map([["a1", "Alice"]])}
          items={[
            ev("assignment", "as1", { actor_id: "a1", role_id: "r1" }),
            ev("role", "r2", { name: "auditor", policy_ids: ["pol1"] }),
            ev("contribution", "c1", {}),
            ev("actor", "a1", { type: "Agent", credential_id: "b3767712-6ebc-4314-db26-e11150b55094", must_change: true }),
          ]}
        />
      </NextIntlClientProvider>
    </QueryClientProvider>,
  );
  const cell = await screen.findByText("Alice · contributor");
  expect(cell.closest("td")?.getAttribute("title")).toBe("as1");
  expect(screen.getByText("auditor")).toBeTruthy();
  expect(await screen.findByText("policies: ReadOnlyAccess")).toBeTruthy();
  expect(screen.getByText("c1")).toBeTruthy();
  expect(screen.queryByText(/actor_id|role_id/)).toBeNull();
  expect(screen.getByText("type: Agent · token: b3767712… · must change password: yes")).toBeTruthy();
});

it("names the changed fields instead of showing them as JSON", async () => {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}>
        <AuditTable names={new Map()} items={[ev("project", "p1", { fields: ["description", "name", "odd"] })]} />
      </NextIntlClientProvider>
    </QueryClientProvider>,
  );
  expect(await screen.findByText(/description, name, odd/)).toBeTruthy();
  expect(screen.queryByText(/\[/)).toBeNull();
});
