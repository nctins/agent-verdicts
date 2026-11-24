import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import type { Page } from "@playwright/test";
import vi from "../messages/vi.json";

// The plan says avE2E; compose only accepts lowercase project names.
export const PROJECT = "ave2e";
export const PORT = 18090;
export const BASE_URL = `http://localhost:${PORT}`;
export const ROOT = resolve(__dirname, "../../..");
// Fixed path so teardown finds the same .env as setup.
export const ENV_FILE = join(tmpdir(), "agentverdicts-e2e", ".env");

export function compose(args: string[]): string {
  return execFileSync(
    "docker",
    ["compose", "-p", PROJECT, "--env-file", ENV_FILE, "-f", join(ROOT, "deploy/compose.yaml"), ...args],
    { cwd: ROOT, encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] },
  );
}

export function writeEnv(): void {
  mkdirSync(dirname(ENV_FILE), { recursive: true });
  const pw = "e2e" + Math.floor(Math.random() * 1e9);
  writeFileSync(ENV_FILE, `VERSION=e2e\nPOSTGRES_PASSWORD=${pw}\nPORT=${PORT}\n`);
}

/** The first-run code the api logs (api/cmd/agentverdicts/main.go). */
export function setupCode(): string {
  const m = compose(["logs", "api"]).match(/enter code ([A-Z0-9]{6})/);
  if (!m) throw new Error("no setup code in api logs");
  return m[1];
}

export async function waitReady(url: string, timeoutMs = 120_000): Promise<void> {
  const end = Date.now() + timeoutMs;
  for (;;) {
    try {
      if ((await fetch(url + "/api/v1/session")).ok) return;
    } catch {
      // not up yet
    }
    if (Date.now() > end) throw new Error(`${url} not ready after ${timeoutMs} ms`);
    await new Promise((r) => setTimeout(r, 1000));
  }
}

// Specs run in order and hand names/tokens to the next through this file.
const STATE = resolve(__dirname, "../test-results/state.json");
export type State = Record<string, string>;
export const readState = (): State => {
  try {
    return JSON.parse(readFileSync(STATE, "utf8"));
  } catch {
    return {};
  }
};
export const saveState = (patch: State): void => {
  mkdirSync(dirname(STATE), { recursive: true });
  writeFileSync(STATE, JSON.stringify({ ...readState(), ...patch }, null, 2));
};

export async function login(page: Page, username: string, password: string, next = "/") {
  await page.goto("/login/?next=" + encodeURIComponent(next));
  await page.getByLabel(vi.auth.login.label.username).fill(username);
  await page.getByLabel(vi.auth.login.label.password).fill(password);
  await page.getByRole("button", { name: vi.auth.login.button.submit }).click();
  await page.waitForURL((u) => !u.pathname.startsWith("/login"));
}
