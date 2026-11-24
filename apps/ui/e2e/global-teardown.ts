import { compose } from "./stack";

export default function globalTeardown() {
  if (process.env.E2E_KEEP !== "1") compose(["down", "-v"]); // only this project's own volumes
}
