import { render, screen, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { afterEach, beforeAll, describe, expect, it, vi } from "vite-plus/test";
import { AppProviders } from "../../app/App";
import { routes } from "../../app/routes";
import { initI18n } from "../../i18n";
import { fakeCore } from "../../test/fakeCore";
import { list, node, NOW, ROLES } from "../fixtures";
import type { FSUsage } from "./api";
import { bootMode, breakdown, daysToFull, ruleScope } from "./model";

beforeAll(() => {
  initI18n("zh-TW");
  window.matchMedia ??= (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList;
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const B = "/management/v1";
const GiB = 1024 ** 3;
const session = {
  status: 200,
  body: {
    kind: "session",
    user: { id: "u1", username: "tim", displayName: "", status: "active", createdAt: NOW, updatedAt: NOW, version: 1 },
    assignments: [{ id: "a1", userId: "u1", role: "owner", scope: { kind: "global", id: "*" }, createdAt: NOW }],
    secondFactorEnrollmentRequired: false,
  },
};
const tw = node({ id: "tw-01", name: "hazel-tw-01" });

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

const cap = (total: number, used: number, avail: number) => ({
  totalBytes: total,
  usedBytes: used,
  availableBytes: avail,
  inodesTotal: 1000,
  inodesFree: 500,
  readOnly: false,
});

const view = {
  reported: true,
  collectedAt: "2026-11-12T03:40:00Z",
  receivedAt: "2026-11-12T03:40:01Z",
  usageMeasuredAt: "2026-11-12T00:00:00Z",
  usageError: null,
  backupStaging: "notApplicable",
  unreadable: [],
  devices: [
    dev({
      name: "sdb",
      dev: "8:16",
      sizeBytes: 1024 * GiB,
      model: "WD Red Plus",
      transport: "sata",
      health: { state: "notCollected", reason: "not read yet" },
    }),
    dev({
      name: "sdb1",
      dev: "8:17",
      kind: "partition",
      parent: "sdb",
      sizeBytes: 931 * GiB,
      fsType: "ext4",
      fsUuid: "5a7c-e2",
    }),
    dev({
      name: "sdd",
      dev: "8:48",
      sizeBytes: 256 * GiB,
      removable: true,
      health: { state: "notCollected", reason: "not read yet" },
    }),
    dev({ name: "sdd1", dev: "8:49", kind: "partition", parent: "sdd", sizeBytes: 256 * GiB, fsType: "exfat" }),
  ],
  configs: [
    {
      origin: "fstab",
      file: "/etc/fstab",
      line: 9,
      spec: "UUID=5a7c-e2",
      reference: "uuid",
      mountPoint: "/mnt/data",
      fsType: "ext4",
      options: "defaults",
      boot: "auto",
      resolvesTo: "/dev/sdb1",
    },
    {
      origin: "fstab",
      file: "/etc/fstab",
      line: 11,
      spec: "UUID=9d2c-07",
      reference: "uuid",
      mountPoint: "/mnt/old",
      fsType: "ext4",
      options: "defaults",
      boot: "auto",
      resolvesTo: null,
    },
  ],
  mounts: [
    {
      mountPoint: "/mnt/data",
      source: "/dev/sdb1",
      device: "/dev/sdb1",
      dev: "8:17",
      fsType: "ext4",
      class: "local",
      options: "rw",
      readOnly: false,
      reference: "uuid",
      configs: [0],
      capacity: cap(931 * GiB, 412 * GiB, 519 * GiB),
      growthBytesPerDay: 2 * GiB,
      usage: {
        state: "complete",
        measuredAt: "2026-11-12T00:00:00Z",
        usedBytes: 412 * GiB,
        unreadableDirs: 0,
        categories: [
          { category: "appData", bytes: 278 * GiB, paths: [] },
          { category: "database", bytes: 84 * GiB, paths: [] },
          { category: "other", bytes: 50 * GiB, paths: [] },
        ],
      },
      rule: rule({ builtIn: false, nodeId: "tw-01" }),
      alerts: [],
    },
    {
      mountPoint: "/mnt/scratch",
      source: "/dev/sdd1",
      device: "/dev/sdd1",
      dev: "8:49",
      fsType: "exfat",
      class: "local",
      options: "rw",
      readOnly: false,
      reference: "device",
      configs: [],
      capacity: cap(100 * GiB, 97 * GiB, 3 * GiB),
      growthBytesPerDay: null,
      usage: {
        state: "partial",
        measuredAt: "2026-11-12T00:00:00Z",
        usedBytes: 97 * GiB,
        unreadableDirs: 2,
        categories: [{ category: "containerImages", bytes: 40 * GiB, paths: [] }],
      },
      rule: rule(),
      alerts: [{ kind: "availableLow", level: "critical", message: "3.0% available" }],
    },
  ],
  swaps: [],
  problems: [
    { kind: "missingDevice", mountPoint: "/mnt/old", configs: [1] },
    { kind: "notMounted", mountPoint: "/mnt/old", configs: [1] },
    { kind: "deviceName", mountPoint: "/mnt/scratch", configs: [] },
  ],
};

function coreWith(body: unknown) {
  return fakeCore({
    [`GET ${B}/auth/session`]: session,
    [`GET ${B}/roles`]: { status: 200, body: ROLES },
    [`GET ${B}/nodes`]: { status: 200, body: list([tw]) },
    [`GET ${B}/nodes/${tw.id}`]: { status: 200, body: { node: tw } },
    [`GET ${B}/nodes/${tw.id}/storage`]: { status: 200, body },
  });
}

function renderAt(path: string) {
  render(
    <AppProviders status={{}}>
      <RouterProvider router={createMemoryRouter(routes, { initialEntries: [path] })} />
    </AppProviders>,
  );
}

const panel = (name: string | RegExp) => screen.findByRole("region", { name });

describe("disks tab", () => {
  it("is a tab of the host page", async () => {
    coreWith(view);
    renderAt("/nodes/tw-01");
    const tab = await screen.findByRole("link", { name: "磁碟" });
    expect(tab).toHaveAttribute("href", "/nodes/tw-01/disks");
  });

  it("lists disks and partitions with identity, and health as 尚未讀取, never 健康", async () => {
    coreWith(view);
    renderAt("/nodes/tw-01/disks");
    const p = await panel("磁碟與分割區");
    const sdb = within(p).getByText("sdb").closest("tr")!;
    expect(within(sdb).getByText("WD Red Plus · SATA")).toBeInTheDocument();
    expect(within(sdb).getAllByText("尚未讀取")).toHaveLength(2);
    const sdb1 = within(p).getByText("sdb1").closest("tr")!;
    expect(within(sdb1).getByText("UUID 5a7c-e2")).toBeInTheDocument();
    expect(within(sdb1).getByText("/mnt/data")).toBeInTheDocument();
    expect(within(p).getAllByText("健康", { exact: false })).toHaveLength(1); // the column head only
  });

  it("marks only the mount named by device name, and says where each mount is configured", async () => {
    coreWith(view);
    renderAt("/nodes/tw-01/disks");
    const p = await panel(/^掛載 ·/);
    const data = within(p).getByText("/mnt/data").closest("tr")!;
    expect(within(data).queryByRole("img", { name: /裝置名稱掛載/ })).toBeNull();
    expect(within(data).getByText("/etc/fstab:9")).toBeInTheDocument();
    expect(within(data).getByText("開機自動掛載")).toBeInTheDocument();
    const scratch = within(p).getByText("/mnt/scratch").closest("tr")!;
    expect(within(scratch).getByRole("img", { name: /裝置名稱掛載/ })).toBeInTheDocument();
    expect(within(scratch).getByText("手動掛上")).toBeInTheDocument();
    expect(within(scratch).getByText("沒有設定")).toBeInTheDocument();
    expect(within(scratch).getByText("可用空間剩 3.0%（3.0 GiB）")).toBeInTheDocument();
  });

  it("lists conflicts with the file and line, leaving the device-name warning to the mount", async () => {
    coreWith(view);
    renderAt("/nodes/tw-01/disks");
    const p = await panel("設定衝突 · 2");
    expect(within(p).getByText("指向不存在的磁碟")).toBeInTheDocument();
    expect(within(p).getByText("開機該掛卻沒掛上")).toBeInTheDocument();
    expect(within(p).getAllByText("/etc/fstab:11 · UUID=9d2c-07")).toHaveLength(2);
  });

  it("draws a complete measurement as a bar in the fixed order, and a partial one as lower bounds without a bar", async () => {
    coreWith(view);
    renderAt("/nodes/tw-01/disks");
    const p = await panel("磁碟用量");
    const data = within(p).getByRole("article", { name: "/mnt/data" });
    const bar = within(data).getByRole("img");
    expect(bar).toHaveAccessibleName("應用資料 278 GiB、資料庫 84.0 GiB、其他 50.0 GiB、可用 519 GiB");
    expect(within(data).getByText("告警規則：這台主機")).toBeInTheDocument();
    expect(within(data).getByText("約 259 天後寫滿")).toBeInTheDocument();
    const scratch = within(p).getByRole("article", { name: "/mnt/scratch" });
    expect(within(scratch).queryByRole("img", { name: /可用/ })).toBeNull();
    expect(within(scratch).getByText("量測不完整")).toBeInTheDocument();
    expect(within(scratch).getByText(/2 個目錄讀不到/)).toBeInTheDocument();
    expect(within(scratch).getByText("至少 40.0 GiB")).toBeInTheDocument();
    expect(within(scratch).queryByText("其他")).toBeNull();
  });

  it("says the host has not reported instead of showing an empty inventory", async () => {
    coreWith({ ...view, reported: false, devices: [], mounts: [], problems: [] });
    renderAt("/nodes/tw-01/disks");
    expect(await screen.findByText(/還沒回報磁碟清單/)).toBeInTheDocument();
    expect(screen.queryByText("0")).toBeNull();
  });

  it("says when usage was never measured and why the last request failed", async () => {
    coreWith({
      ...view,
      usageMeasuredAt: null,
      usageError: { code: "EXECUTOR_UNREACHABLE", message: "x", at: "2026-11-12T03:00:00Z" },
      mounts: [{ ...view.mounts[0], usage: null }],
    });
    renderAt("/nodes/tw-01/disks");
    expect(await screen.findByText("用量還沒量過 · 每 6 小時量一次")).toBeInTheDocument();
    expect(screen.getByText(/量測失敗：連不到高權限執行器/)).toBeInTheDocument();
    const card = within(await panel("磁碟用量")).getByRole("article", { name: "/mnt/data" });
    expect(within(card).getByText("還沒量過用量分類。")).toBeInTheDocument();
    expect(within(card).queryByRole("img", { name: /可用/ })).toBeNull();
  });
});

describe("storage model", () => {
  const usage = (state: string, categories: [string, number][]): FSUsage => ({
    state,
    measuredAt: NOW,
    usedBytes: 100,
    unreadableDirs: 0,
    categories: categories.map(([category, b]) => ({ category, bytes: b, paths: [] })),
  });

  it("orders categories the fixed way and leaves out empty ones", () => {
    const b = breakdown(
      usage("complete", [
        ["other", 5],
        ["osAndLogs", 10],
        ["appData", 80],
        ["database", 0],
      ]),
      20,
    );
    expect(b.segments.map((s) => s.category)).toEqual(["appData", "osAndLogs", "other"]);
    expect(b.scale).toBe(115);
  });

  it("never draws a partial or unmeasured breakdown and never lists other for a partial one", () => {
    const p = breakdown(
      usage("partial", [
        ["appData", 80],
        ["other", 5],
      ]),
      20,
    );
    expect(p.scale).toBeNull();
    expect(p.segments.map((s) => s.category)).toEqual(["appData"]);
    expect(breakdown(usage("unmeasured", [["appData", 1]]), 20)).toMatchObject({ state: "unmeasured", segments: [] });
    expect(breakdown(null, 20).state).toBe("none");
  });

  it("reads boot mode, rule scope and days to full", () => {
    expect(bootMode([])).toBe("manual");
    expect(bootMode([{ boot: "onDemand" } as never])).toBe("onDemand");
    expect(ruleScope(null)).toBe("builtIn");
    expect(ruleScope(rule({ builtIn: false, mountPoint: "/data" }) as never)).toBe("mountPoint");
    expect(ruleScope(rule({ builtIn: false, nodeId: "n", mountPoint: "/data" }) as never)).toBe("nodeMountPoint");
    expect(ruleScope(rule({ builtIn: false }) as never)).toBe("default");
    expect(daysToFull(10, 0)).toBeNull();
    expect(daysToFull(10, -1)).toBeNull();
    expect(daysToFull(10, 3)).toBe(3);
    expect(daysToFull(null, 3)).toBeNull();
  });
});
