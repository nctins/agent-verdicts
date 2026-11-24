import { expect, test } from "@playwright/test";
import vi from "../messages/vi.json";
import { login } from "./stack";

const a = vi.manage.access;

test("a project manager sees only their project and gives only roles within reach", async ({ page }) => {
  await login(page, "admin", "admin-pass-123", "/manage/projects/");
  await page.getByRole("button", { name: vi.admin_ui.projects.new }).click();
  const create = page.getByRole("dialog");
  await create.getByLabel(vi.admin.project.id).fill("e2e-private");
  await create.getByRole("button", { name: vi.manage.project.button.create }).click();
  await expect(page.getByRole("link", { name: "e2e-private", exact: true })).toBeVisible();

  await page.goto("/manage/actors/");
  await page.getByRole("button", { name: vi.manage.people.add }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel(vi.manage.actor.label.name).fill("Mona");
  await dialog.getByLabel(vi.admin.field.username).fill("mona");
  await dialog.getByLabel(vi.manage.actor.label.password, { exact: true }).fill("mona-pass-1234");
  await expect(dialog.getByRole("option", { name: "manager" })).toBeAttached();
  await dialog.getByLabel(a.role).selectOption({ label: "manager" });
  await dialog.getByLabel(a.scope_some).check();
  await dialog.getByLabel("e2e", { exact: true }).check();
  await dialog.getByRole("button", { name: vi.manage.people.create }).click();
  await dialog.getByRole("button", { name: vi.admin.once.done }).click();
  await expect(page.getByText("manager · e2e")).toBeVisible();
  await expect(page.getByRole("row", { name: /Bot/ }).getByText(vi.manage.people.no_access)).toBeVisible();
  await page.getByRole("button", { name: vi.nav.account_menu.replace("{name}", "Admin") }).click();
  await page.getByRole("menuitem", { name: vi.nav.logout }).click();

  await login(page, "mona", "mona-pass-1234");
  const nav = page.getByRole("navigation", { name: vi.nav.menu }).first();
  await expect(nav.getByRole("link", { name: /^e2e \d+$/ })).toBeVisible(); // the sidebar link carries the count
  await expect(nav.getByRole("link", { name: "e2e-private" })).toHaveCount(0);
  await page.goto("/p/e2e-private/");
  await expect(page.getByRole("link", { name: "e2e-private" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: vi.project.contribute, exact: true })).toHaveCount(0);

  // A manager sets how long agent read detail is kept on their project, in its Settings tab.
  await page.goto("/p/e2e/");
  await page.getByRole("tab", { name: vi.project.tab.settings }).click();
  const days = page.getByLabel(vi.project.reads.days_label);
  await expect(days).toHaveValue("180");
  await days.fill("365");
  await page.getByRole("button", { name: vi.project.reads.save, exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: vi.project.reads.estimate.split(":")[0] })).toBeVisible();
  await page.reload();
  await expect(page.getByLabel(vi.project.reads.days_label)).toHaveValue("365");

  // A manager gets the overview, limited to their projects.
  await page.goto("/manage/");
  await expect(page.getByRole("main").getByText(vi.manage.dashboard.projects, { exact: true }).first()).toBeVisible();

  // Mona manages roles from the actors page: viewer on e2e is within reach, admin is not.
  // The list holds only members of her projects; Bot has no role yet, so she finds Bot by name.
  await page.goto("/manage/actors/");
  await expect(nav.getByRole("link", { name: vi.nav.policies })).toHaveCount(0);
  await expect(page.getByRole("button", { name: vi.manage.people.add })).toHaveCount(0);
  await page.getByLabel(vi.manage.people.search).fill("Bot");
  await page.getByRole("link", { name: "Bot", exact: true }).click();
  await expect(page.getByText(vi.manage.people.manager_note)).toBeVisible();
  // Roles are given in a side sheet; its project choice starts empty every time it opens.
  const give = async (role: string) => {
    await page.getByRole("button", { name: vi.manage.people.add_role }).click();
    const sheet = page.getByRole("dialog");
    await expect(sheet.getByRole("option", { name: role })).toBeAttached();
    await sheet.getByLabel(a.role).selectOption({ label: role });
    await sheet.getByLabel(a.scope_some).check();
    await sheet.getByLabel("e2e", { exact: true }).check();
    await sheet.getByRole("button", { name: vi.manage.people.add_role }).click();
    return sheet;
  };
  await give("viewer");
  await expect(page.getByRole("listitem").filter({ hasText: "viewer · e2e" })).toBeVisible();

  const sheet = await give("admin");
  await expect(sheet.getByRole("alert").filter({ hasText: vi.errors.forbidden })).toBeVisible();
});
