import { expect, test } from "@playwright/test";
import vi from "../messages/vi.json";
import { login, readState } from "./stack";

// Builds on 04 (the answer, Favorable) and 05 (alice's opposing challenge makes it Conflict).
test("withdrawing the only challenge takes the answer out of Conflict", async ({ page }) => {
  const { answerUrl } = readState();
  await login(page, "alice", "alice-pass-1234", answerUrl);
  const summary = page.getByRole("group", { name: vi.contribution.status_label });
  await expect(summary.getByText(vi.state.Conflict, { exact: true })).toBeVisible();

  const mine = page.getByRole("listitem").filter({ hasText: "Tài liệu mới nói 10 lần" });
  await mine.getByRole("button", { name: vi.contribution.withdraw, exact: true }).click();
  // It cannot be undone, so nothing happens before the confirmation.
  const dialog = page.getByRole("dialog", { name: vi.contribution.withdraw_title });
  await expect(dialog).toBeVisible();
  // The open dialog hides the page from the accessibility tree, so look past that.
  const behind = page.getByRole("group", { name: vi.contribution.status_label, includeHidden: true });
  await expect(behind.getByText(vi.state.Conflict, { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: vi.contribution.withdraw_confirm }).click();

  await expect(summary.getByText(vi.state.Favorable, { exact: true })).toBeVisible();
  await expect(summary.getByText(vi.state.Conflict, { exact: true })).toHaveCount(0);
  await page.reload();
  await expect(summary.getByText(vi.state.Favorable, { exact: true })).toBeVisible();
  await expect(summary.getByText(vi.state.Conflict, { exact: true })).toHaveCount(0);

  // The challenge is not gone: it moved to history, labelled as withdrawn.
  await page.getByText(vi.contribution.history.replace("{count}", "1")).click();
  const old = page.getByRole("listitem").filter({ hasText: "Tài liệu mới nói 10 lần" });
  await expect(old.getByText(vi.contribution.why.withdrawn, { exact: true })).toBeVisible();
  await expect(old.getByRole("button", { name: vi.contribution.withdraw, exact: true })).toHaveCount(0);
});
