import { expect, test } from "@playwright/test";
import vi from "../messages/vi.json";
import { saveState } from "./stack";

test("first-run setup with the code from the log, then the admin overview", async ({ page }) => {
  const code = process.env.AV_SETUP_CODE;
  expect(code).toMatch(/^[A-Z0-9]{6}$/);
  await page.goto("/");
  await expect(page).toHaveURL(/\/setup\/$/);
  const l = vi.auth.setup.label;
  await page.getByLabel(l.code).fill(code!);
  await page.getByLabel(l.name).fill("Admin");
  await page.getByLabel(l.username).fill("admin");
  await page.getByLabel(l.password).fill("admin-pass-123");
  await page.getByRole("button", { name: vi.auth.setup.submit }).click();
  await expect(page).toHaveURL(/\/manage\/$/);
  await expect(page.getByRole("heading", { level: 1, name: vi.manage.dashboard.heading })).toBeVisible();
  await expect(page.getByRole("main").getByText(vi.manage.dashboard.projects, { exact: true }).first()).toBeVisible();
  saveState({ adminUser: "admin", adminPass: "admin-pass-123" });
});
