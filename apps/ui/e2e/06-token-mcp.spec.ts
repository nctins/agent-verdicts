import { expect, test } from "@playwright/test";
import vi from "../messages/vi.json";
import { login } from "./stack";

test("an Agent token shows once and opens /mcp/<project> through nginx", async ({ page, request }) => {
  await login(page, "admin", "admin-pass-123", "/manage/actors/");
  await page.getByRole("button", { name: vi.manage.people.add }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("radio", { name: vi.manage.actor.option.agent }).check();
  await dialog.getByLabel(vi.manage.actor.label.name).fill("Bot");
  await dialog.getByLabel(vi.manage.people.later).check(); // 09 gives Bot its role
  await dialog.getByRole("button", { name: vi.manage.people.create }).click();
  const secret = dialog.getByLabel(vi.admin.actor.created_token);
  await expect(secret).toHaveValue(/\S{16,}/);
  const token = await secret.inputValue();
  await dialog.getByRole("button", { name: vi.admin.once.done }).click();

  await page.getByRole("link", { name: "Bot", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: /Bot/ })).toBeVisible();
  await page.getByRole("tab", { name: vi.admin_ui.actor.tab_login }).click();
  await expect(page.getByRole("cell", { name: vi.manage.credential.list.status.live })).toBeVisible();
  await expect(page.locator(`input[value="${token}"]`)).toHaveCount(0);
  await expect(page.getByText(token)).toHaveCount(0);

  const init = (path: string, token: string) =>
    request.post(path, {
      headers: { Authorization: `Bearer ${token}`, Accept: "application/json, text/event-stream" },
      data: {
        jsonrpc: "2.0",
        id: 1,
        method: "initialize",
        params: { protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "e2e", version: "0" } },
      },
    });

  // Bot has no role yet (09 gives it one), so the project is not readable for it: 404, as for a missing one.
  expect((await init("/mcp/e2e", token)).status()).toBe(404);
  // There is no project-less endpoint any more.
  expect((await init("/mcp", token)).status()).toBe(404);

  // A second agent gets a contributor role on e2e in the same modal and can connect to that project.
  await page.goto("/manage/actors/");
  await page.getByRole("button", { name: vi.manage.people.add }).click();
  const second = page.getByRole("dialog");
  await second.getByRole("radio", { name: vi.manage.actor.option.agent }).check();
  await second.getByLabel(vi.manage.actor.label.name).fill("Scout");
  await second.getByLabel(vi.manage.access.role).selectOption({ label: "contributor" });
  await second.getByLabel(vi.manage.access.scope_some).check();
  await second.getByLabel("e2e", { exact: true }).check();
  await second.getByRole("button", { name: vi.manage.people.create }).click();
  const scoutToken = await second.getByLabel(vi.admin.actor.created_token).inputValue();
  await second.getByRole("button", { name: vi.admin.once.done }).click();

  const res = await init("/mcp/e2e", scoutToken);
  expect(res.status()).toBe(200);
  const text = await res.text();
  const json = text.trimStart().startsWith("{")
    ? JSON.parse(text)
    : JSON.parse(text.split("\n").find((l) => l.startsWith("data:"))!.slice(5));
  expect(json.result.serverInfo).toBeTruthy();
});
