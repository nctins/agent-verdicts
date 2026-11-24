import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, describe, expect, it, vi } from "vitest";
import en from "@/messages/en.json";
import { ReadRetention } from "./ReadRetention";

const puts: unknown[] = [];
afterEach(() => {
  vi.unstubAllGlobals();
  puts.length = 0;
});

function renderIt() {
  const json = (b: unknown) => new Response(JSON.stringify(b), { headers: { "Content-Type": "application/json" } });
  vi.stubGlobal("fetch", async (req: Request) => {
    if (req.method === "PUT") {
      const body = await req.json();
      puts.push(body);
      return json({ detail_days: body.detail_days, estimated_bytes: 230_000_000, rows_per_day: 4500 });
    }
    return json({ from: "2026-11-21", to: "2026-11-21", detail_days: 180, detail_from: "2026-05-01", daily: [], monthly: [], items: [], never_read: 0 });
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}>
        <ReadRetention project="demo" />
      </NextIntlClientProvider>
    </QueryClientProvider>,
  );
}

describe("ReadRetention", () => {
  it("rejects days outside 30-3650 without saving", async () => {
    renderIt();
    const box = await screen.findByDisplayValue("180");
    for (const v of ["29", "3651", "1.5"]) {
      await userEvent.clear(box);
      await userEvent.type(box, v);
      await userEvent.click(screen.getByRole("button", { name: en.project.reads.save }));
      expect(screen.getByRole("alert").textContent).toBe(en.project.reads.invalid);
    }
    expect(puts).toEqual([]);
  });

  it("saves and shows the size estimate", async () => {
    renderIt();
    const box = await screen.findByDisplayValue("180");
    await userEvent.clear(box);
    await userEvent.type(box, "365");
    await userEvent.click(screen.getByRole("button", { name: en.project.reads.save }));
    const status = await screen.findByRole("status");
    expect(puts).toEqual([{ detail_days: 365 }]);
    expect(status.textContent).toContain("230 MB");
    expect(status.textContent).toContain("4,500");
  });
});
