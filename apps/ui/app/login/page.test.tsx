import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { expect, it, vi } from "vitest";
import { keys } from "@/lib/api/keys";
import en from "@/messages/en.json";
import LoginPage from "./page";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/login/",
  useSearchParams: () => new URLSearchParams(""),
}));
vi.mock("@/app/providers", () => ({
  useLocale: () => ({ locale: "en", setLocale: vi.fn() }),
  useTheme: () => ({ dark: false, toggle: vi.fn() }),
}));

it("is a card of its own: no app menu, the product's name and the form", async () => {
  const qc = new QueryClient();
  qc.setQueryData(keys.session(), { authenticated: false, actor: null, csrf_token: null, must_change_password: false, setup_required: false });
  render(
    <QueryClientProvider client={qc}>
      <NextIntlClientProvider locale="en" messages={en}><LoginPage /></NextIntlClientProvider>
    </QueryClientProvider>,
  );
  expect(await screen.findByRole("heading", { name: en.auth.login.heading })).toBeTruthy();
  expect(screen.getByText("Agent Verdicts")).toBeTruthy();
  expect(screen.queryByRole("navigation")).toBeNull();
  expect(screen.getByLabelText(en.auth.login.label.username)).toBeTruthy();
});
