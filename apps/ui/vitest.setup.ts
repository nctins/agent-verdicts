// @testing-library/jest-dom is not installed (not in the allowed dependency list).
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// Auto-cleanup needs vitest globals, which are off.
afterEach(cleanup);
