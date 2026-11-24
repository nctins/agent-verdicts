import { defineConfig, devices } from "@playwright/test";
import { BASE_URL } from "./e2e/stack";

// The flows build on each other (setup → users → content): one worker, file order.
export default defineConfig({
  testDir: "e2e",
  workers: 1,
  fullyParallel: false,
  retries: 0,
  globalSetup: "./e2e/global-setup.ts",
  globalTeardown: "./e2e/global-teardown.ts",
  use: { baseURL: BASE_URL, locale: "vi-VN", trace: "retain-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
