import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { randomBytes } from "node:crypto";
import { expect, test, type Page } from "@playwright/test";
import { E2E_DIR, enrollAs, fakeHost } from "./core";
import { scanIssues, seedPrefs, watchErrors } from "./scan";

/**
 * The host page's 概況 tab against a real Core (ticket 020.4). The first flow uses a host enrolled through
 * Core's agent listener that never connects, so every block shows its "not reported" state. The second runs
 * the real agent in a Linux container when HAZELMINX_E2E_AGENT_BIN names a Linux build of hazelminx-agent
 * (and Docker is available): it enrolls with a code from the screen, reports facts and metrics over the
 * channel, and the page is checked and photographed with that data. Run it with
 * HAZELMINX_E2E_AGENT_ADDR=0.0.0.0:19891 so the container can reach Core's agent listener.
 */

const h1 = (page: Page) => page.locator("main h1").first();
const uniq = (p: string) => `${p}-${randomBytes(3).toString("hex")}`;
const SHOTS = join(E2E_DIR, "shots");

async function scanScreen(page: Page, errors: string[], label: string, shot?: string) {
  for (const [width, mode] of [
    [1440, "dark"],
    [360, "dark"],
    [1440, "light"],
    [360, "light"],
  ] as const) {
    await page.setViewportSize({ width, height: 900 });
    await page.evaluate((m) => document.documentElement.setAttribute("data-mode", m), mode);
    expect(await scanIssues(page), `${label} @${width} ${mode}`).toEqual([]);
    if (shot) {
      mkdirSync(SHOTS, { recursive: true });
      await page.screenshot({ path: join(SHOTS, `${shot}-${width}-${mode}.png`), fullPage: true });
    }
  }
  await page.evaluate(() => document.documentElement.setAttribute("data-mode", "dark"));
  await page.setViewportSize({ width: 1440, height: 900 });
  expect(errors, `${label}: page and console errors`).toEqual([]);
}

async function createCode(page: Page, name: string) {
  await page.goto("/nodes/new");
  await page.getByLabel("顯示名稱").fill(name);
  await page.getByRole("button", { name: "產生註冊碼" }).click();
  const command = page.getByLabel("註冊指令");
  await expect(command).toBeVisible();
  return /HZX-[0-9A-Z]{4}(?:-[0-9A-Z]{4}){3}/.exec((await command.textContent()) ?? "")![0];
}

test.describe.configure({ mode: "serial" });

test.beforeEach(async ({ page }) => {
  await seedPrefs(page, { mode: "dark", lang: "zh-TW" });
  await page.setViewportSize({ width: 1440, height: 900 });
});

test("a host that never reported: the overview says so in every block, without zeros", async ({ page }) => {
  const errors = watchErrors(page);
  const name = uniq("e2e-quiet");
  const code = await createCode(page, name);
  expect((await enrollAs(fakeHost(), code)).status).toBe(202);
  await page.getByRole("button", { name: "是這台，加入" }).click();
  await expect(page.getByText(`${name} 已加入`)).toBeVisible();

  // From the host list row to the host page.
  await page.goto(`/nodes?q=${encodeURIComponent(name)}`);
  await page.getByRole("link", { name, exact: true }).click();
  await expect(h1(page)).toHaveText(name);
  const tabs = page.getByRole("navigation", { name: "主機分頁" });
  await expect(tabs.getByRole("link", { name: "概況" })).toHaveAttribute("aria-current", "page");

  await expect(page.getByText(/這台主機還沒回報主機資訊/)).toBeVisible();
  await expect(page.getByText("這台主機還沒回報資源指標。")).toBeVisible();
  await expect(page.getByRole("figure", { name: "CPU 使用率" }).getByText("這段時間沒有資料")).toBeVisible();
  await scanScreen(page, errors, "overview · never reported");

  // Every range asks Core with a window it keeps; none is refused.
  for (const range of ["即時（5 分）", "12 小時", "30 天"]) {
    const res = page.waitForResponse((r) => r.url().includes("/metrics/history") && r.url().includes("cpu.usage"));
    await page.getByRole("button", { name: range }).click();
    expect((await res).status(), range).toBe(200);
  }
  await expect(page).toHaveURL(/range=30d/);

  await tabs.getByRole("link", { name: "系統" }).click();
  await expect(page.getByRole("region", { name: "主機憑證" })).toBeVisible();
  await scanScreen(page, errors, "system tab");
});

const AGENT = process.env.HAZELMINX_E2E_AGENT_BIN;

test.describe("with a real agent", () => {
  test.skip(!AGENT, "set HAZELMINX_E2E_AGENT_BIN to a Linux hazelminx-agent build");
  test.setTimeout(240_000);
  let agent: ChildProcess | undefined;
  const container = `hzx-e2e-agent-${randomBytes(3).toString("hex")}`;
  test.afterAll(() => {
    agent?.kill();
    try {
      execFileSync("docker", ["rm", "-f", container], { stdio: "ignore" });
    } catch {
      /* already gone */
    }
  });

  test("facts, hardware and trends from the agent's reports", async ({ page }) => {
    const errors = watchErrors(page);
    const name = uniq("e2e-live");
    const code = await createCode(page, name);
    const port = (process.env.HAZELMINX_E2E_AGENT_ADDR ?? "0.0.0.0:19891").split(":").pop();
    const url = `https://host.docker.internal:${port}`;
    const cmd = [
      `/a --config /tmp/none.toml --state-dir /tmp/hx --core-url ${url} enroll --core-url ${url} --code ${code}`,
      `exec /a --config /tmp/none.toml --state-dir /tmp/hx --core-url ${url} --log-format text run`,
    ].join(" && ");
    agent = spawn(
      "docker",
      [
        "run",
        "--rm",
        "--name",
        container,
        "--user",
        "65534:65534",
        "-v",
        `${AGENT}:/a:ro`,
        "hzx-ledger-test:trixie",
      ].concat(["sh", "-c", cmd]),
      { stdio: "inherit" },
    );
    await page.getByRole("button", { name: "是這台，加入" }).click({ timeout: 60_000 });
    await expect(page.getByText(`${name} 已加入`)).toBeVisible();
    await page.getByRole("link", { name: "查看這台主機" }).click();

    const facts = page.getByRole("region", { name: "主機資訊" });
    await expect(facts.getByText("aarch64").or(facts.getByText("x86_64"))).toBeVisible({ timeout: 60_000 });
    await expect(facts.getByText(/^\/proc|^uname|^\/etc/).first()).toBeVisible();
    const cpu = page.getByRole("region", { name: "CPU" });
    await expect(cpu.getByText(/^\d+\.\d%$/).first()).toBeVisible({ timeout: 60_000 });
    await expect(page.getByText(/資料更新於/)).toBeVisible();

    // A few raw points, then the chart has a line.
    await page.getByRole("button", { name: "即時（5 分）" }).click();
    await expect
      .poll(
        async () =>
          (await page.getByRole("figure", { name: "CPU 使用率" }).locator("path.hx-chart-line").getAttribute("d")) ??
          "",
        {
          timeout: 90_000,
        },
      )
      .toMatch(/^M.*L/);
    await page.getByRole("figure", { name: "CPU 使用率" }).getByRole("button", { name: "表格檢視" }).click();
    await expect(page.getByRole("figure", { name: "CPU 使用率" }).getByRole("table")).toBeVisible();
    await scanScreen(page, errors, "overview · live", "overview-live");
  });
});
