import { expect, test } from "@playwright/test";
import vi from "../messages/vi.json";
import { login, readState } from "./stack";

const ROOT = "Cần xác nhận phiên bản tài liệu";
const REPLY = "Bản 2.3 theo changelog";

// Builds on 04 (the answer, Favorable) and 10 (its only challenge was withdrawn, so it is Favorable again). Runs last.
test("a reply nests under its challenge and leaves the claim's state alone", async ({ page }) => {
  const { answerUrl } = readState();
  await login(page, "alice", "alice-pass-1234", answerUrl);
  const summary = page.getByRole("group", { name: vi.contribution.status_label });
  await expect(summary.getByText(vi.state.Favorable, { exact: true })).toBeVisible();

  // The challenge form offers only oppose and neutral; a neutral one keeps the state.
  await page.getByRole("button", { name: vi.contribution.challenge_open, exact: true }).click();
  await page.getByLabel(vi.contribution.challenge.content).fill(ROOT);
  await page.getByLabel(vi.contribution.challenge.stance).click();
  await expect(page.getByRole("option")).toHaveCount(2);
  await expect(page.getByRole("option", { name: vi.stance.favorable })).toHaveCount(0);
  await page.getByRole("option", { name: vi.stance.neutral }).click();
  await page.getByRole("button", { name: vi.contribution.challenge.submit }).click();

  const root = page.getByRole("listitem").filter({ hasText: ROOT });
  await expect(root).toBeVisible();
  await root.getByRole("button", { name: vi.contribution.reply.open, exact: true }).click();
  await root.getByLabel(vi.contribution.reply.label).fill(REPLY);
  await root.getByRole("button", { name: vi.contribution.reply.submit }).click();

  // The reply sits inside the challenge's own list item, and the state did not move.
  await expect(root.getByRole("listitem").filter({ hasText: REPLY })).toBeVisible();
  await expect(summary.getByText(vi.state.Favorable, { exact: true })).toBeVisible();
  await page.reload();
  await expect(root.getByRole("listitem").filter({ hasText: REPLY })).toBeVisible();
  await expect(summary.getByText(vi.state.Favorable, { exact: true })).toBeVisible();
});
