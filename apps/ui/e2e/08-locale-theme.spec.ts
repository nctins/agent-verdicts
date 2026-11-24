import { expect, test } from "@playwright/test";
import en from "../messages/en.json";
import vi from "../messages/vi.json";

test("language and theme survive a reload", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: vi.nav.language }).click();
  await page.getByRole("menuitemradio", { name: vi.nav.locale_en }).click();
  await expect(page.locator("html")).toHaveAttribute("lang", "en");
  // Playwright's default color scheme is light, so one click turns it dark.
  await page.getByRole("button", { name: en.nav.theme_to_dark }).click();
  await expect(page.getByRole("button", { name: en.nav.theme_to_light })).toBeVisible();
  await expect(page.locator("html")).toHaveClass(/\bdark\b/);

  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("lang", "en");
  await expect(page.locator("html")).toHaveClass(/\bdark\b/);
  // Signed out, "/" leads to the sign-in card, now in English.
  await expect(page.getByRole("heading", { name: en.auth.login.heading })).toBeVisible();
  expect(await page.evaluate(() => [localStorage.getItem("av.locale"), localStorage.getItem("av.theme")])).toEqual(["en", "dark"]);
});
