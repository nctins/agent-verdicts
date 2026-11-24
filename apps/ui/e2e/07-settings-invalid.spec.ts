import { expect, test } from "@playwright/test";
import vi from "../messages/vi.json";
import { login } from "./stack";

test("a failing connection test shows the error and keeps Save disabled", async ({ page }) => {
  await login(page, "admin", "admin-pass-123", "/manage/settings/search/");
  await page.locator("#embedding-url").fill("http://127.0.0.1:1");
  await page.locator("#embedding-model").fill("x");
  const embedding = page.getByRole("heading", { name: vi.settings.emb.heading }).locator("..");
  await embedding.getByRole("button", { name: vi.settings.button.test }).click();
  await expect(embedding.getByRole("alert")).toBeVisible({ timeout: 15_000 });
  await expect(page.getByRole("button", { name: vi.settings.button.save, exact: true })).toBeDisabled();
});
