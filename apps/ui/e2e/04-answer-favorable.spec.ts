import { expect, test } from "@playwright/test";
import vi from "../messages/vi.json";
import { login, readState, saveState } from "./stack";

test("answer, one Favorable click from someone else: the answer turns Favorable, the question resolved", async ({ page }) => {
  const { questionUrl } = readState();
  await login(page, "admin", "admin-pass-123", questionUrl);
  const form = page.getByRole("region", { name: vi.contribution.answer.heading });
  await form.getByRole("button", { name: vi.contribution.answer.open }).click();
  await form.getByLabel(vi.contribution.create.title).fill("Tối đa 8 lần");
  await form.getByRole("button", { name: vi.contribution.answer.submit }).click();

  const answers = page.getByRole("region", { name: new RegExp("^" + vi.contribution.answers_heading) });
  const card = answers.getByRole("listitem").filter({ hasText: "Tối đa 8 lần" });
  await expect(card).toBeVisible();
  const href = await card.getByRole("link", { name: "Tối đa 8 lần" }).getAttribute("href");
  saveState({ answerUrl: href! });

  // Alice, who asked, supports it: the answer's author supporting it would not count.
  await page.context().clearCookies();
  await login(page, "alice", "alice-pass-1234", questionUrl);
  await expect(card).toBeVisible();
  const favorable = card.getByRole("button", { name: new RegExp("^" + vi.stance.favorable) });
  await favorable.click(); // picks; nothing is sent until confirmed
  await card.getByRole("button", { name: new RegExp("^" + vi.contribution.confirm.submit.replace("{stance}", vi.stance.favorable)) }).click();
  // Optimistic: no wait for the next poll.
  await expect(card.getByText(vi.state.Favorable, { exact: true })).toBeVisible({ timeout: 1000 });
  await expect(favorable).toHaveAttribute("aria-pressed", "true");
  await expect(favorable).toBeEnabled(); // the lock lifts once the write settles

  const summary = page.getByRole("group", { name: vi.contribution.status_label });
  await expect
    .poll(
      async () => {
        await page.reload();
        await expect(summary.getByText(vi.kind.question, { exact: true })).toBeVisible();
        return summary.getByText(vi.question_status.resolved, { exact: true }).isVisible();
      },
      { timeout: 20_000, intervals: [1000] },
    )
    .toBe(true);
});
