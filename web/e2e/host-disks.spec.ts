import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { randomBytes } from "node:crypto";
import { expect, test, type Page } from "@playwright/test";
import { E2E_DIR, enrollAs, fakeHost } from "./core";
import { scanIssues, seedPrefs, watchErrors } from "./scan";

/**
 * The host page's 磁碟 tab against a real Core (ticket 030.2). A host that never connects shows the "not reported"
 * state. A host whose storage reply carries a full inventory (Core's reply for that one request is replaced, because
 * a usage measurement needs the privileged executor, which the test container does not run) shows every state of
 * the tab: device-name warning, conflicts, a complete bar, a partial measurement and alerts. With
 * HAZELMINX_E2E_AGENT_BIN set, the real agent in a Linux container reports its own inventory over the channel.
 */

const uniq = (p: string) => `${p}-${randomBytes(3).toString("hex")}`;
const SHOTS = join(E2E_DIR, "shots");
const GiB = 1024 ** 3;

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

async function enrolled(page: Page, prefix: string) {
  const name = uniq(prefix);
  await page.goto("/nodes/new");
  await page.getByLabel("顯示名稱").fill(name);
  await page.getByRole("button", { name: "產生註冊碼" }).click();
  const command = page.getByLabel("註冊指令");
  await expect(command).toBeVisible();
  const code = /HZX-[0-9A-Z]{4}(?:-[0-9A-Z]{4}){3}/.exec((await command.textContent()) ?? "")![0];
  return { name, code };
}

async function openDisks(page: Page, name: string) {
  await page.goto(`/nodes?q=${encodeURIComponent(name)}`);
  await page.getByRole("link", { name, exact: true }).click();
  await page.getByRole("navigation", { name: "主機分頁" }).getByRole("link", { name: "磁碟" }).click();
  await expect(page).toHaveURL(/\/disks$/);
}

test.describe.configure({ mode: "serial" });

test.beforeEach(async ({ page }) => {
  await seedPrefs(page, { mode: "dark", lang: "zh-TW" });
  await page.setViewportSize({ width: 1440, height: 900 });
});

test("a host that never reported: the disks tab says so, without zeros", async ({ page }) => {
  const errors = watchErrors(page);
  const { name, code } = await enrolled(page, "e2e-disks-quiet");
  expect((await enrollAs(fakeHost(), code)).status).toBe(202);
  await page.getByRole("button", { name: "是這台，加入" }).click();
  await expect(page.getByText(`${name} 已加入`)).toBeVisible();
  const res = page.waitForResponse((r) => r.url().endsWith("/storage"));
  await openDisks(page, name);
  expect((await res).status()).toBe(200);
  await expect(page.getByText(/這台主機還沒回報磁碟清單/)).toBeVisible();
  await scanScreen(page, errors, "disks · never reported", "disks-quiet");
});

const rule = (over: Record<string, unknown> = {}) => ({
  nodeId: "",
  mountPoint: "",
  warnAvailablePercent: 15,
  criticalAvailablePercent: 5,
  criticalAvailableBytes: 2 * GiB,
  warnInodesFreePercent: 10,
  criticalInodesFreePercent: 3,
  warnFullWithinDays: 14,
  updatedAt: "0001-01-01T00:00:00Z",
  updatedBy: "",
  builtIn: true,
  ...over,
});
const dev = (over: Record<string, unknown>) => ({
  kind: "disk",
  parent: null,
  class: "physical",
  sizeBytes: null,
  model: null,
  serial: null,
  transport: null,
  rotational: null,
  removable: null,
  readOnly: null,
  fsType: null,
  fsUuid: null,
  fsLabel: null,
  partUuid: null,
  partLabel: null,
  holders: [],
  health: null,
  ...over,
});
const notRead = { state: "notCollected", reason: "SMART and NVMe health are not read yet (ticket 020.3)" };
const cap = (total: number, used: number, avail: number, inodesFree = 500_000) => ({
  totalBytes: total,
  usedBytes: used,
  availableBytes: avail,
  inodesTotal: 1_000_000,
  inodesFree,
  readOnly: false,
});
const cfg = (line: number, spec: string, reference: string, mountPoint: string, fsType: string, boot = "auto") => ({
  origin: "fstab",
  file: "/etc/fstab",
  line,
  spec,
  reference,
  mountPoint,
  fsType,
  options: "defaults",
  boot,
  resolvesTo: null,
});
const mount = (over: Record<string, unknown>) => ({
  source: "",
  device: null,
  dev: "0:0",
  class: "local",
  options: "rw",
  readOnly: false,
  configs: [],
  capacity: null,
  growthBytesPerDay: null,
  usage: null,
  rule: rule(),
  alerts: [],
  ...over,
});
const measured = "2026-11-14T06:00:00Z";

/** A host with every state the tab has: what the boards NodeDisks and Storage draw for hazel-tw-01. */
const full = (nodeId: string) => ({
  reported: true,
  collectedAt: new Date().toISOString(),
  receivedAt: new Date().toISOString(),
  usageMeasuredAt: measured,
  usageError: null,
  backupStaging: "notApplicable",
  unreadable: [],
  devices: [
    dev({
      name: "nvme0n1",
      dev: "259:0",
      sizeBytes: 100 * GiB,
      model: "Samsung 980",
      transport: "nvme",
      health: notRead,
    }),
    dev({
      name: "nvme0n1p1",
      dev: "259:1",
      kind: "partition",
      parent: "nvme0n1",
      sizeBytes: 512 * 1024 ** 2,
      fsType: "vfat",
      fsUuid: "7C1A-0F22",
    }),
    dev({
      name: "nvme0n1p2",
      dev: "259:2",
      kind: "partition",
      parent: "nvme0n1",
      sizeBytes: 94 * GiB,
      fsType: "ext4",
      fsUuid: "0b7e…41c9",
    }),
    dev({ name: "sdb", dev: "8:16", sizeBytes: 1024 * GiB, model: "WD Red Plus", transport: "sata", health: notRead }),
    dev({
      name: "sdb1",
      dev: "8:17",
      kind: "partition",
      parent: "sdb",
      sizeBytes: 931 * GiB,
      fsType: "ext4",
      fsUuid: "5a7c…e2",
    }),
    dev({
      name: "sdd",
      dev: "8:48",
      sizeBytes: 256 * GiB,
      model: "SanDisk",
      transport: "usb",
      removable: true,
      health: notRead,
    }),
    dev({
      name: "sdd1",
      dev: "8:49",
      kind: "partition",
      parent: "sdd",
      sizeBytes: 256 * GiB,
      fsType: "exfat",
      fsUuid: "64F2-91AB",
      fsLabel: "SCRATCH",
    }),
  ],
  configs: [
    cfg(5, "UUID=0b7e…41c9", "uuid", "/", "ext4"),
    cfg(9, "UUID=5a7c…e2", "uuid", "/mnt/data", "ext4"),
    cfg(10, "hazel-tw-01:/mnt/data/nfs", "network", "/mnt/nfs/backup", "nfs4", "onDemand"),
    cfg(11, "UUID=9d2c…07", "uuid", "/mnt/old", "ext4"),
    cfg(12, "/swapfile", "other", "none", "swap"),
  ],
  mounts: [
    mount({
      mountPoint: "/",
      source: "/dev/nvme0n1p2",
      device: "/dev/nvme0n1p2",
      fsType: "ext4",
      reference: "uuid",
      configs: [0],
      capacity: cap(94 * GiB, 36 * GiB, 58 * GiB),
      growthBytesPerDay: 0.07 * GiB,
      usage: {
        state: "complete",
        measuredAt: measured,
        usedBytes: 36 * GiB,
        unreadableDirs: 0,
        categories: [
          { category: "containerImages", bytes: 15 * GiB, paths: ["/var/lib/docker"] },
          { category: "osAndLogs", bytes: 17 * GiB, paths: ["/usr", "/var/log"] },
          { category: "other", bytes: 4 * GiB, paths: [] },
        ],
      },
    }),
    mount({
      mountPoint: "/mnt/data",
      source: "/dev/sdb1",
      device: "/dev/sdb1",
      fsType: "ext4",
      reference: "uuid",
      configs: [1],
      capacity: cap(931 * GiB, 412 * GiB, 519 * GiB),
      growthBytesPerDay: 1.3 * GiB,
      usage: {
        state: "complete",
        measuredAt: measured,
        usedBytes: 412 * GiB,
        unreadableDirs: 0,
        categories: [
          { category: "appData", bytes: 278 * GiB, paths: [] },
          { category: "database", bytes: 84 * GiB, paths: [] },
          { category: "backupStaging", bytes: 38 * GiB, paths: [] },
          { category: "containerImages", bytes: 12 * GiB, paths: [] },
        ],
      },
      rule: rule({ builtIn: false, nodeId }),
    }),
    mount({
      mountPoint: "/mnt/nfs/backup",
      source: "hazel-tw-01:/mnt/data/nfs",
      fsType: "nfs4",
      class: "network",
      reference: "network",
      configs: [2],
      capacity: cap(200 * GiB, 19 * GiB, 181 * GiB),
    }),
    mount({
      mountPoint: "/mnt/scratch",
      source: "/dev/sdd1",
      device: "/dev/sdd1",
      fsType: "exfat",
      reference: "device",
      capacity: cap(256 * GiB, 248 * GiB, 8 * GiB, 20_000),
      growthBytesPerDay: 1.1 * GiB,
      usage: {
        state: "partial",
        measuredAt: measured,
        usedBytes: 248 * GiB,
        unreadableDirs: 3,
        categories: [
          { category: "appData", bytes: 120 * GiB, paths: [] },
          { category: "containerImages", bytes: 31 * GiB, paths: [] },
        ],
      },
      alerts: [
        { kind: "availableLow", level: "warning", message: "3.1% available" },
        { kind: "inodesLow", level: "critical", message: "2.0% inodes free" },
        { kind: "fullSoon", level: "warning", message: "full in about 7 days at the 7-day trend" },
      ],
    }),
  ],
  swaps: [{ name: "/swapfile", kind: "file", sizeBytes: 4 * GiB, priority: -2, configs: [4] }],
  problems: [
    { kind: "missingDevice", mountPoint: "/mnt/old", configs: [3] },
    { kind: "notMounted", mountPoint: "/mnt/old", configs: [3] },
    { kind: "deviceName", mountPoint: "/mnt/scratch", configs: [] },
  ],
});

test("a host with every storage state: warning, conflicts, complete and partial usage, alerts", async ({ page }) => {
  const errors = watchErrors(page);
  const { name, code } = await enrolled(page, "e2e-disks-full");
  const host = fakeHost();
  expect((await enrollAs(host, code)).status).toBe(202);
  await page.getByRole("button", { name: "是這台，加入" }).click();
  await expect(page.getByText(`${name} 已加入`)).toBeVisible();
  // Core answers the request (so the route, auth and permission are real); its body is replaced.
  await page.route("**/management/v1/nodes/*/storage", async (route) => {
    const res = await route.fetch();
    expect(res.status()).toBe(200);
    const id = /nodes\/([^/]+)\/storage/.exec(route.request().url())![1];
    await route.fulfill({ response: res, json: full(decodeURIComponent(id)) });
  });
  await openDisks(page, name);

  const mounts = page.getByRole("region", { name: /^掛載 ·/ });
  const scratch = mounts.getByRole("row").filter({ hasText: "/mnt/scratch" });
  await expect(scratch.getByRole("img", { name: /裝置名稱掛載/ })).toBeVisible();
  await expect(
    mounts
      .getByRole("row")
      .filter({ hasText: "/mnt/data" })
      .getByRole("img", { name: /裝置名稱/ }),
  ).toHaveCount(0);
  await expect(mounts.getByRole("row").filter({ hasText: "/mnt/nfs/backup" }).getByText("用到才掛載")).toBeVisible();
  await expect(page.getByRole("region", { name: "設定衝突 · 2" }).getByText("指向不存在的磁碟")).toBeVisible();
  const usage = page.getByRole("region", { name: "磁碟用量" });
  await expect(usage.getByRole("article")).toHaveCount(3);
  await expect(usage.getByRole("article", { name: "/mnt/data" }).getByRole("img")).toHaveAccessibleName(
    "應用資料 278 GiB、資料庫 84.0 GiB、備份暫存 38.0 GiB、容器映像 12.0 GiB、可用 519 GiB",
  );
  const partial = usage.getByRole("article", { name: "/mnt/scratch" });
  await expect(partial.getByText("量測不完整")).toBeVisible();
  await expect(partial.getByRole("img", { name: /可用/ })).toHaveCount(0);
  await scanScreen(page, errors, "disks · full", "disks-full");
});

const AGENT = process.env.HAZELMINX_E2E_AGENT_BIN;

test.describe("with a real agent", () => {
  test.skip(!AGENT, "set HAZELMINX_E2E_AGENT_BIN to a Linux hazelminx-agent build");
  test.setTimeout(240_000);
  let agent: ChildProcess | undefined;
  const container = `hzx-e2e-disks-${randomBytes(3).toString("hex")}`;
  test.afterAll(() => {
    agent?.kill();
    try {
      execFileSync("docker", ["rm", "-f", container], { stdio: "ignore" });
    } catch {
      /* already gone */
    }
  });

  test("the agent's own inventory, with usage not measured (no privileged executor in the container)", async ({
    page,
  }) => {
    const errors = watchErrors(page);
    const { name, code } = await enrolled(page, "e2e-disks-live");
    const port = (process.env.HAZELMINX_E2E_AGENT_ADDR ?? "0.0.0.0:19891").split(":").pop();
    const url = `https://host.docker.internal:${port}`;
    const cmd = [
      `/a --config /tmp/none.toml --state-dir /tmp/hx --core-url ${url} enroll --core-url ${url} --code ${code}`,
      `exec /a --config /tmp/none.toml --state-dir /tmp/hx --core-url ${url} --log-format text run`,
    ].join(" && ");
    // Its own machine-id: the overview's real-agent test runs the same image, and Core keeps one host per machine.
    const machineId = join(E2E_DIR, `${container}.machine-id`);
    mkdirSync(E2E_DIR, { recursive: true });
    writeFileSync(machineId, `${randomBytes(16).toString("hex")}\n`);
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
        "-v",
        `${machineId}:/etc/machine-id:ro`,
        "hzx-ledger-test:trixie",
      ].concat(["sh", "-c", cmd]),
      { stdio: "inherit" },
    );
    await page.getByRole("button", { name: "是這台，加入" }).click({ timeout: 60_000 });
    await expect(page.getByText(`${name} 已加入`)).toBeVisible();
    await openDisks(page, name);
    const mounts = page.getByRole("region", { name: /^掛載 ·/ });
    await expect(mounts.getByRole("row").nth(1)).toBeVisible({ timeout: 60_000 });
    await expect(page.getByText(/磁碟清單更新於/)).toBeVisible();
    await expect(page.getByText(/用量還沒量過|量測失敗/).first()).toBeVisible();
    await scanScreen(page, errors, "disks · live", "disks-live");
  });
});
