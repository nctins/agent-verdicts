import { execFileSync } from "node:child_process";
import { BASE_URL, compose, ROOT, setupCode, waitReady, writeEnv } from "./stack";

// Same build commands as scripts/release.sh, tagged e2e.
const BUILDS = [
  ["api", "-f", "docker/api/Dockerfile", "--target", "production", "apps/api"],
  ["ui", "-f", "docker/ui/Dockerfile", "--target", "production", "--build-context", "dockerui=docker/ui", "apps/ui"],
  ["db", "docker/db"],
];

export default async function globalSetup() {
  if (process.env.E2E_SKIP_BUILD !== "1")
    for (const [name, ...args] of BUILDS)
      execFileSync("docker", ["build", ...args, "-t", `agentverdicts-${name}:e2e`], { cwd: ROOT, stdio: "inherit" });
  writeEnv();
  compose(["down", "-v"]); // a stack left by an interrupted run
  compose(["up", "-d"]);
  await waitReady(BASE_URL);
  process.env.AV_SETUP_CODE = setupCode();
}
