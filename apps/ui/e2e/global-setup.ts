import { execFileSync } from "node:child_process";
import { BASE_URL, compose, ROOT, setupCode, waitReady, writeEnv } from "./stack";

// Same builds as scripts/release.sh, tagged e2e; stack.ts points IMAGE_REPO at them.
const BUILDS = [
  ["agent-verdicts", "-f", "docker/api/Dockerfile", "--target", "production", "apps/api"],
  ["agent-verdicts-web", "-f", "docker/ui/Dockerfile", "--target", "production", "--build-context", "dockerui=docker/ui", "apps/ui"],
];

export default async function globalSetup() {
  if (process.env.E2E_SKIP_BUILD !== "1")
    for (const [name, ...args] of BUILDS)
      execFileSync("docker", ["build", ...args, "-t", `${name}:e2e`], { cwd: ROOT, stdio: "inherit" });
  writeEnv();
  compose(["down", "-v"]); // a stack left by an interrupted run
  compose(["up", "-d"]);
  await waitReady(BASE_URL);
  process.env.AV_SETUP_CODE = setupCode();
}
