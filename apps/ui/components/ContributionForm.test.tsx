import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, describe, expect, it, vi } from "vitest";
import en from "@/messages/en.json";
import { ContributionForm } from "./CreateContributionDialog";

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn(), replace: vi.fn() }) }));
afterEach(() => {
  vi.unstubAllGlobals();
  posted = [];
});

let posted: unknown[] = [];
const hit = { id: "c9", kind: "claim", title: "Webhook retry uses backoff", snippet: "", state: "Favorable" };

function renderForm(answersId?: string) {
  const urls: string[] = [];
  vi.stubGlobal("fetch", async (req: Request) => {
    urls.push(req.url);
    if (req.method === "POST") {
      posted.push(await req.json());
      return new Response(JSON.stringify({ id: "c1", similar: [] }), { status: 201, headers: { "Content-Type": "application/json" } });
    }
    return new Response(JSON.stringify({ results: [hit], degraded: false }), { headers: { "Content-Type": "application/json" } });
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}>
        <ContributionForm project="demo" kind="claim" answersId={answersId} onDone={() => {}} />
      </NextIntlClientProvider>
    </QueryClientProvider>,
  );
  return urls;
}

describe("ContributionForm", () => {
  it("shows similar contributions while the title is typed, to avoid duplicates", async () => {
    renderForm();
    await userEvent.type(screen.getByLabelText(en.contribution.create.title), "webhook retry");
    expect(await screen.findByText(en.contribution.create.similar)).toBeTruthy();
    expect((await screen.findByRole("link", { name: /Webhook retry uses backoff/ })).getAttribute("href")).toMatch(/^\/p\/demo\/c\/c9\/?$/); // trailingSlash is added by the Next config
  });
  it("sends the sources, one per line, with the contribution", async () => {
    renderForm("q1");
    await userEvent.type(screen.getByLabelText(en.contribution.create.title), "A claim");
    await userEvent.type(screen.getByLabelText(en.sources.label), "https://example.test/a{enter}{enter}commit abc  ");
    await userEvent.click(screen.getByRole("button", { name: en.contribution.answer.submit }));
    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0]).toMatchObject({ title: "A claim", answers_id: "q1", sources: ["https://example.test/a", "commit abc"] });
  });
  it("leaves sources out when none are entered, and refuses more than 10", async () => {
    renderForm("q1");
    await userEvent.type(screen.getByLabelText(en.contribution.create.title), "A claim");
    await userEvent.click(screen.getByRole("button", { name: en.contribution.answer.submit }));
    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0]).not.toHaveProperty("sources");
    await userEvent.type(screen.getByLabelText(en.sources.label), Array(11).fill("s").join("{enter}"));
    await userEvent.click(screen.getByRole("button", { name: en.contribution.answer.submit }));
    expect((await screen.findByRole("alert")).textContent).toBe(en.sources.error_too_many);
    expect(posted).toHaveLength(1);
  });
  it("does not search for an answer", async () => {
    const urls = renderForm("q1");
    await userEvent.type(screen.getByLabelText(en.contribution.create.title), "webhook retry");
    await new Promise((r) => setTimeout(r, 400));
    expect(urls.filter((u) => u.includes("/search"))).toEqual([]);
  });
});
