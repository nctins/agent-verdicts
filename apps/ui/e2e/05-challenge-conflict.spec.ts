import { expect, test } from "@playwright/test";
import vi from "../messages/vi.json";
import { login, readState } from "./stack";

test("a challenge that opposes (the default) puts the answer in Conflict", async ({ page }) => {
  const { answerUrl } = readState();
  await login(page, "alice", "alice-pass-1234", answerUrl);
  const header = page.getByRole("group", { name: vi.contribution.status_label });
  await expect(header.getByText(vi.state.Favorable, { exact: true })).toBeVisible();
  // The reply box sits at the bottom of the thread, like a forum's; the side panel links to it.
  await page.getByRole("button", { name: vi.contribution.challenge_open, exact: true }).click();
  await page.getByLabel(vi.contribution.challenge.content).fill("Tài liệu mới nói 10 lần");
  await expect(page.getByLabel(vi.contribution.challenge.stance)).toHaveText(vi.stance.unfavorable);
  await page.getByRole("button", { name: vi.contribution.challenge.submit }).click();
  await expect(header.getByText(vi.state.Conflict, { exact: true })).toBeVisible();
  await page.reload();
  await expect(header.getByText(vi.state.Conflict, { exact: true })).toBeVisible();
});
