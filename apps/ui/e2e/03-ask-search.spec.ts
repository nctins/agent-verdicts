import { expect, test } from "@playwright/test";
import vi from "../messages/vi.json";
import { login, saveState } from "./stack";

const QUESTION = "Webhook retry tối đa mấy lần?";

test("ask a question, then find it in the search-as-you-type dropdown", async ({ page }) => {
  await login(page, "alice", "alice-pass-1234", "/p/e2e/");
  // "Ask" sits in the split button's menu, next to the main "Contribute".
  await page.getByRole("button", { name: vi.project.compose_more }).click();
  await page.getByRole("menuitem", { name: vi.project.ask, exact: true }).click();
  const dialog = page.getByRole("dialog", { name: vi.contribution.create.heading_question });
  await dialog.getByLabel(vi.contribution.create.title).fill(QUESTION);
  await dialog.getByRole("button", { name: vi.contribution.create.submit_question }).click();
  await expect(page).toHaveURL(/\/p\/e2e\/c\/[^/]+\/$/);
  await expect(page.getByRole("heading", { level: 1, name: QUESTION })).toBeVisible();
  saveState({ questionUrl: new URL(page.url()).pathname + new URL(page.url()).search });

  // The breadcrumb leads back to the project (client-side, through nginx's /p/ fallback).
  await page.getByRole("navigation", { name: vi.contribution.breadcrumb }).getByRole("link").first().click();
  await expect(page).toHaveURL(/\/p\/e2e\/$/);
  await expect(page.getByRole("heading", { level: 1, name: /e2e/ })).toBeVisible();
  // The project opens on Overview; searching is part of the list tabs.
  await page.getByRole("tab", { name: vi.project.tab.questions }).click();
  const box = page.getByRole("combobox", { name: vi.search.label });
  const started = Date.now();
  await box.fill("webhook retry");
  await expect(page.getByRole("option", { name: new RegExp(QUESTION.replace("?", "\\?")) })).toBeVisible({ timeout: 2000 });
  const ms = Date.now() - started;
  test.info().annotations.push({ type: "dropdown_ms", description: String(ms) });
  console.log(`dropdown_ms=${ms}`);
  saveState({ dropdownMs: String(ms) });

  await box.fill("xyzzy khong co");
  await page.getByRole("option", { name: vi.search.ask_this }).click();
  const ask = page.getByRole("dialog", { name: vi.contribution.create.heading_question });
  await expect(ask.getByLabel(vi.contribution.create.title)).toHaveValue("xyzzy khong co");
});
