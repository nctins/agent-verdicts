import { expect, test } from "@playwright/test";
import vi from "../messages/vi.json";
import { login, saveState } from "./stack";

test("a Human with must_change is sent to Account and must set a new password", async ({ page, context }) => {
  // The project the later flows use (also gives the session-expiry check below a real page).
  await login(page, "admin", "admin-pass-123", "/manage/projects/");
  await page.getByRole("button", { name: vi.admin_ui.projects.new }).click();
  const create = page.getByRole("dialog");
  await create.getByLabel(vi.admin.project.id).fill("e2e");
  await create.getByRole("button", { name: vi.manage.project.button.create }).click();
  await expect(page.getByRole("link", { name: "e2e", exact: true })).toBeVisible();

  // One modal: the person and, since projects are private, their role on e2e.
  await page.goto("/manage/actors/");
  await page.getByRole("button", { name: vi.manage.people.add }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("radio", { name: vi.manage.actor.option.human }).check();
  await dialog.getByLabel(vi.manage.actor.label.name).fill("Alice");
  await dialog.getByLabel(vi.admin.field.username).fill("alice");
  await dialog.getByLabel(vi.manage.actor.label.password, { exact: true }).fill("alice-temp-123");
  const must = dialog.getByLabel(vi.manage.actor.label.must_change);
  await must.check();
  await expect(must).toBeChecked();
  await expect(dialog.getByRole("option", { name: "contributor" })).toBeAttached();
  await dialog.getByLabel(vi.manage.access.role).selectOption({ label: "contributor" });
  await dialog.getByLabel(vi.manage.access.scope_some).check();
  await dialog.getByLabel("e2e", { exact: true }).check();
  await dialog.getByRole("button", { name: vi.manage.people.create }).click();
  await expect(dialog.getByLabel(vi.admin.actor.created_password)).toHaveValue("alice-temp-123");
  await dialog.getByRole("button", { name: vi.admin.once.done }).click();
  await expect(page.getByRole("link", { name: "Alice" })).toBeVisible();
  await expect(page.getByText("contributor · e2e")).toBeVisible();

  await page.getByRole("button", { name: vi.nav.account_menu.replace("{name}", "Admin") }).click();
  await page.getByRole("menuitem", { name: vi.nav.logout }).click();
  await expect(page).toHaveURL(/\/login\/$/);

  await login(page, "alice", "alice-temp-123");
  await expect(page).toHaveURL(/\/account\/$/);
  await expect(page.getByText(vi.auth.account.must_change.title)).toBeVisible();
  await page.getByLabel(vi.auth.account.current_password).fill("alice-temp-123");
  await page.getByLabel(vi.auth.account.new_password, { exact: true }).fill("alice-pass-1234");
  await page.getByLabel(vi.auth.account.confirm_password).fill("alice-pass-1234");
  await page.getByRole("button", { name: vi.auth.account.save_password }).click();
  await expect(page).toHaveURL(/localhost:\d+\/$/);
  saveState({ aliceUser: "alice", alicePass: "alice-pass-1234" });

  await page.goto("/manage/");
  await expect(page.getByText(vi.errors.forbidden)).toBeVisible();

  // Review Focus 5: the session cookie disappears mid-visit.
  // An old query link still lands on the project, now under its /p/ path.
  await page.goto("/project/?id=e2e");
  await expect(page).toHaveURL(/\/p\/e2e\/$/);
  await expect(page.getByRole("heading", { level: 1, name: /e2e/ })).toBeVisible();
  await context.clearCookies();

  await page.getByRole("button", { name: vi.project.compose_more }).click();
  await page.getByRole("menuitem", { name: vi.project.ask, exact: true }).click();
  // The page may still believe it is logged in (cached session): then the write itself gets the 401.
  const title = page.getByLabel(vi.contribution.create.title);
  const loginHeading = page.getByRole("heading", { name: vi.auth.login.heading });
  await expect(title.or(loginHeading)).toBeVisible();
  if (await title.isVisible()) {
    await title.fill("Câu hỏi khi phiên đã mất");
    await page.getByRole("button", { name: vi.contribution.create.submit_question }).click();
  }
  await expect(page).toHaveURL(/\/login\/\?next=/);
  expect(new URL(page.url()).searchParams.get("next")).toBe("/p/e2e/");

  await page.getByLabel(vi.auth.login.label.username).fill("alice");
  await page.getByLabel(vi.auth.login.label.password).fill("alice-pass-1234");
  await page.getByRole("button", { name: vi.auth.login.button.submit }).click();
  await expect(page).toHaveURL(/\/p\/e2e\/$/);
});
