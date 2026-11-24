import { expect, test } from "@playwright/test";
import vi from "../messages/vi.json";
import { login } from "./stack";

// Builds on 06 (admin created the agents Bot and Scout); it disables Scout (12 runs after it and does not use Scout).
test("the audit log shows earlier admin work, and disabling asks first and is logged", async ({ page }) => {
  await login(page, "admin", "admin-pass-123", "/manage/audit/");
  await expect(page.getByRole("heading", { level: 1, name: vi.manage.audit.heading })).toBeVisible();
  const created = page.getByRole("row").filter({ hasText: vi.manage.audit.action.actor.create }).filter({ hasText: "Bot" });
  await expect(created).toBeVisible();

  await page.goto("/manage/actors/");
  await page.getByRole("link", { name: "Scout", exact: true }).click();
  await page.getByRole("button", { name: vi.manage.actor.button.disable }).click();
  // Nothing is disabled before the confirmation; the open dialog hides the page, so assert on the dialog.
  const dialog = page.getByRole("dialog", { name: vi.manage.actor.disable_title });
  await expect(dialog).toBeVisible();
  // The confirm button stays off until the actor's name is typed.
  await expect(dialog.getByRole("button", { name: vi.manage.actor.disable_confirm })).toBeDisabled();
  await dialog.getByRole("textbox").fill("Scout");
  await dialog.getByRole("button", { name: vi.manage.actor.disable_confirm }).click();
  await expect(page.getByRole("button", { name: vi.manage.actor.button.enable })).toBeVisible();

  await page.goto("/manage/audit/");
  const disabled = page.getByRole("row").filter({ hasText: vi.manage.audit.action.actor.disable }).filter({ hasText: "Scout" });
  await expect(disabled).toBeVisible();
});
