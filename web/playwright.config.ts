import { defineConfig, devices } from "@playwright/test";

/**
 * Layout and accessibility scans of the shell (DESIGN.md「可量測的驗收」).
 * By default runs against the dev server, which also serves the dev-only component gallery.
 * Set HAZELMINX_E2E_URL to scan a running Core with the embedded production build instead.
 */
const external = process.env.HAZELMINX_E2E_URL;

export default defineConfig({
  testDir: "e2e",
  timeout: 60_000,
  fullyParallel: true,
  reporter: [["list"]],
  use: {
    baseURL: external ?? "http://127.0.0.1:5173",
    ...devices["Desktop Chrome"],
  },
  webServer: external
    ? undefined
    : {
        command: "./node_modules/.bin/vp dev --port 5173 --strictPort --host 127.0.0.1",
        url: "http://127.0.0.1:5173",
        reuseExistingServer: true,
        timeout: 60_000,
      },
});
