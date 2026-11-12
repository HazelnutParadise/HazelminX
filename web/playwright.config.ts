import { defineConfig, devices } from "@playwright/test";
import { OWNER_STATE } from "./e2e/core";

/**
 * Browser flows and layout/accessibility scans (DESIGN.md「可量測的驗收」) against a real Core.
 * By default Playwright builds ../core, starts it on an empty data directory (e2e/start-core.sh) and runs
 * the dev server in front of it, which also serves the dev-only component gallery. The first-run project
 * creates the owner through the setup screens; every other test reuses that owner's session.
 * Set HAZELMINX_E2E_URL (and HAZELMINX_E2E_SETUP_CODE of that fresh Core) to test a running Core with the
 * embedded production build instead.
 */
const external = process.env.HAZELMINX_E2E_URL;
const CORE = `http://${process.env.HAZELMINX_E2E_CORE_ADDR ?? "127.0.0.1:19890"}`;

export default defineConfig({
  testDir: "e2e",
  timeout: 60_000,
  fullyParallel: true,
  reporter: [["list"]],
  use: {
    // localhost, not 127.0.0.1: passkeys need a domain as their relying party.
    baseURL: external ?? "http://localhost:5173",
    ...devices["Desktop Chrome"],
  },
  projects: [
    { name: "first-run", testMatch: /first-run\.spec\.ts/ },
    {
      name: "panel",
      testIgnore: /first-run\.spec\.ts/,
      dependencies: ["first-run"],
      use: { storageState: OWNER_STATE },
    },
  ],
  webServer: external
    ? undefined
    : [
        {
          command: "sh e2e/start-core.sh",
          url: `${CORE}/healthz`,
          // Always a fresh Core: the first-run test needs its setup code.
          reuseExistingServer: false,
          timeout: 180_000,
        },
        {
          command: "./node_modules/.bin/vp dev --port 5173 --strictPort --host 127.0.0.1",
          url: "http://127.0.0.1:5173",
          env: { HAZELMINX_CORE_URL: CORE },
          reuseExistingServer: false,
          timeout: 60_000,
        },
      ],
});
