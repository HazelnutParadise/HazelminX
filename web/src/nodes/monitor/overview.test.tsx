import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { afterEach, beforeAll, describe, expect, it, vi } from "vite-plus/test";
import { AppProviders } from "../../app/App";
import { routes } from "../../app/routes";
import { initI18n } from "../../i18n";
import { fakeCore, type FakeCall, type FakeReply } from "../../test/fakeCore";
import { list, node, NOW, ROLES } from "../fixtures";

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
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const B = "/management/v1";
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

const fact = <T,>(value: T | null, source = "") => ({ value, source });
const facts = {
  reported: true,
  collectedAt: "2026-11-12T03:40:00Z",
  receivedAt: "2026-11-12T03:40:01Z",
  facts: {
    hostname: fact("hazel-tw-01", "uname"),
    osName: fact("Ubuntu", "/etc/os-release"),
    osVersion: fact("24.04", "/etc/os-release"),
    osPrettyName: fact("Ubuntu 24.04.2 LTS", "/etc/os-release"),
    kernel: fact<string>(null, "uname"),
    arch: fact("x86_64", "uname"),
    bootTime: fact("2026-11-01T00:00:00Z", "/proc/stat"),
    timezone: fact("Asia/Taipei", "/etc/localtime"),
    timeSynchronized: fact(true, "adjtimex"),
    timeMaxErrorMicros: fact(2500, "adjtimex"),
    cpuModel: fact("AMD EPYC 7B13", "/proc/cpuinfo"),
    logicalCpus: fact(8, "/proc/cpuinfo"),
    memoryTotalBytes: fact(33_554_432_000, "/proc/meminfo"),
  },
};

const usage = (u: number) => ({ usage: u, user: u / 2, system: u / 4, iowait: 1, steal: 0 });

function sample(over: Record<string, unknown> = {}) {
  return {
    seq: 9,
    collectedAt: "2026-11-12T03:42:00Z",
    intervalSeconds: 10,
    cpu: { total: usage(34), cores: [12, 72, 5, 30, 44, 8, 61, 3].map((u, id) => ({ id, usage: usage(u) })) },
    memory: {
      totalBytes: 32 * 2 ** 30,
      availableBytes: 20 * 2 ** 30,
      usedBytes: 12 * 2 ** 30,
      freeBytes: 4 * 2 ** 30,
      buffersBytes: null,
      cachedBytes: 6 * 2 ** 30,
      shmemBytes: null,
      reclaimableSlabBytes: null,
    },
    swap: { totalBytes: null, freeBytes: null, usedBytes: null },
    load: { one: 2.1, five: 1.8, fifteen: 1.5 },
    pressure: { state: "unsupported", cpu: null, memory: null, io: null },
    limits: { tasks: 920, pidMax: 1000, fileHandles: 100, fileMax: 10000 },
    filesystems: [
      {
        mountPoint: "/",
        device: "/dev/nvme0n1p2",
        fsType: "ext4",
        class: "local",
        readOnly: false,
        state: "ok",
        totalBytes: 100 * 2 ** 30,
        freeBytes: 60 * 2 ** 30,
        availableBytes: 60 * 2 ** 30,
        usedBytes: 40 * 2 ** 30,
        inodes: "ok",
        inodesTotal: 1000,
        inodesFree: 0,
      },
    ],
    disks: [{ name: "nvme0n1", class: "physical" }],
    networks: [
      { name: "lo", class: "loopback", rxBytesPerSecond: 1, txBytesPerSecond: 1 },
      { name: "eth0", class: "physical", speedMbps: 1000, rxBytesPerSecond: 2_000_000, txBytesPerSecond: 500_000 },
    ],
    temperatures: [
      { source: "hwmon", chip: "acpitz", label: "", celsius: 36, highCelsius: null, criticalCelsius: 98 },
      { source: "hwmon", chip: "k10temp", label: "Tctl", celsius: 68, highCelsius: null, criticalCelsius: 95 },
    ],
    ...over,
  };
}

function latest(over: Record<string, unknown> = {}, sampleOver: Record<string, unknown> = {}) {
  return {
    reported: true,
    collectedAt: "2026-11-12T03:42:00Z",
    receivedAt: "2026-11-12T03:42:01Z",
    intervalSeconds: 10,
    stale: false,
    droppedSamples: 0,
    sample: sample(sampleOver),
    exhaustion: {
      pids: { used: 920, limit: 1000, percent: 92, thresholdPercent: 90, exceeded: true },
      fileHandles: { used: 100, limit: 10000, percent: 1, thresholdPercent: 90, exceeded: false },
      memoryPressure: { state: "unsupported", someAvg60: null, thresholdPercent: 10, exceeded: null },
      filesystems: [
        {
          mountPoint: "/",
          state: "ok",
          readOnly: false,
          inodes: "ok",
          inodeUsedPercent: 100,
          inodesExhausted: true,
        },
      ],
    },
    ...over,
  };
}

const point = (at: string, value: number, gapBefore = false, resolutionSeconds = 10) => ({
  at,
  value,
  min: value,
  max: value,
  samples: 1,
  resolutionSeconds,
  gapBefore,
});

type History = (call: FakeCall) => FakeReply;
const noHistory: History = () => ({ status: 200, body: { items: [], page: { limit: 2000 } } });

function coreWith({
  host = tw,
  f = facts as unknown,
  l = latest() as unknown,
  history = noHistory,
}: { host?: typeof tw; f?: unknown; l?: unknown; history?: History } = {}) {
  return fakeCore({
    [`GET ${B}/auth/session`]: session,
    [`GET ${B}/roles`]: { status: 200, body: ROLES },
    [`GET ${B}/nodes`]: { status: 200, body: list([host]) },
    [`GET ${B}/nodes/${host.id}`]: { status: 200, body: { node: host } },
    [`GET ${B}/nodes/${host.id}/host`]: { status: 200, body: f },
    [`GET ${B}/nodes/${host.id}/metrics/latest`]: { status: 200, body: l },
    [`GET ${B}/nodes/${host.id}/metrics/history`]: history,
  });
}

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  render(
    <AppProviders status={{}}>
      <RouterProvider router={router} />
    </AppProviders>,
  );
  return router;
}

const panel = (name: string) => screen.findByRole("region", { name });
const chart = (name: string) => screen.findByRole("figure", { name });

describe("host overview: facts", () => {
  it("shows each fact with where it was read from, and 「未提供」 for one the host did not give", async () => {
    coreWith();
    renderAt("/nodes/tw-01");
    const p = await panel("主機資訊");
    await within(p).findByText("Ubuntu 24.04.2 LTS");
    const kernel = within(p).getByText("核心").closest("div")!;
    expect(within(kernel).getByText("未提供")).toBeInTheDocument();
    expect(within(kernel).getByText("uname")).toBeInTheDocument();
    expect(within(p).getByText("Asia/Taipei")).toBeInTheDocument();
    expect(within(p).getByText("已同步 · 誤差 2.5 毫秒")).toBeInTheDocument();
    expect(within(p).getByText("AMD EPYC 7B13 · 8 核")).toBeInTheDocument();
    expect(within(p).getByText(/收集於/)).toBeInTheDocument();
  });

  it("says the host has not reported yet instead of showing empty values", async () => {
    coreWith({ f: { reported: false, collectedAt: null, receivedAt: null, facts: null } });
    renderAt("/nodes/tw-01");
    expect(await screen.findByText(/這台主機還沒回報主機資訊/)).toBeInTheDocument();
  });
});

describe("host overview: exhaustion and hardware", () => {
  it("flags PIDs over 90 % and a filesystem out of inodes with their numbers (T-183, T-165)", async () => {
    coreWith();
    renderAt("/nodes/tw-01");
    const alert = await screen.findByText("有資源快用完了");
    const box = alert.closest("[role]") ?? alert.parentElement!;
    expect(within(box as HTMLElement).getByText(/程序數用了 92.0%，超過 90%/)).toBeInTheDocument();
    expect(within(box as HTMLElement).getByText(/\/ 的 inode 用盡/)).toBeInTheDocument();
    const fs = await panel("檔案系統");
    expect(within(fs).getByText("inode 用盡")).toBeInTheDocument();
    expect(within(fs).getByText("40.0 GiB / 100 GiB")).toBeInTheDocument();
  });

  it("does not flag anything when every limit is under its threshold", async () => {
    const l = latest();
    l.exhaustion.pids.exceeded = false;
    l.exhaustion.filesystems[0]!.inodesExhausted = false;
    coreWith({ l });
    renderAt("/nodes/tw-01");
    await panel("CPU");
    expect(screen.queryByText("有資源快用完了")).not.toBeInTheDocument();
  });

  it("lists the four busiest cores and folds the rest under 「全部 8 個」", async () => {
    coreWith();
    renderAt("/nodes/tw-01");
    const cpu = await panel("CPU");
    const lists = within(cpu).getAllByRole("list");
    const shown = within(lists[0]!)
      .getAllByRole("listitem")
      .map((li) => li.textContent);
    expect(shown).toEqual(["核心 172.0%", "核心 661.0%", "核心 444.0%", "核心 330.0%"]);
    expect(within(cpu).getByText("全部 8 個")).toBeInTheDocument();
    expect(within(cpu).getByText("負載 2.10 · 1.80 · 1.50")).toBeInTheDocument();
  });

  it("orders temperature sensors by how close they are to their own critical point and names the source", async () => {
    coreWith();
    renderAt("/nodes/tw-01");
    const temp = await panel("溫度");
    const rows = within(temp).getAllByRole("listitem");
    expect(rows[0]).toHaveTextContent("Tctl");
    expect(rows[0]).toHaveTextContent("k10temp · hwmon");
    expect(rows[0]).toHaveTextContent("68°C / 95°C");
    expect(rows[1]).toHaveTextContent("acpitz");
  });

  it("shows swap the host did not report as 「未提供」, not 0", async () => {
    coreWith();
    renderAt("/nodes/tw-01");
    const mem = await panel("記憶體");
    const swap = within(mem).getByText("Swap").closest("li")!;
    expect(within(swap).getByText("未提供")).toBeInTheDocument();
    expect(within(mem).getByText("12.0 GiB / 32.0 GiB")).toBeInTheDocument();
  });

  it("leaves the loopback interface out of the network card", async () => {
    coreWith();
    renderAt("/nodes/tw-01");
    const net = await panel("網路");
    expect(within(net).getByText("eth0")).toBeInTheDocument();
    expect(within(net).queryByText("lo")).not.toBeInTheDocument();
  });
});

describe("host overview: freshness", () => {
  it("marks stale data while the channel is up, with the last observed time", async () => {
    coreWith({ l: latest({ stale: true }) });
    renderAt("/nodes/tw-01");
    expect(await screen.findByText("資料過期")).toBeInTheDocument();
    expect(screen.getByText(/上次觀測/)).toBeInTheDocument();
  });

  it("does not call an unreachable host's data stale: the header already says 沒有回應", async () => {
    const off = node({
      id: "tw-01",
      name: "hazel-tw-01",
      connection: { state: "offline", lastContactAt: "2026-11-12T03:41:18Z" } as never,
    });
    coreWith({ host: off, l: latest({ stale: true }) });
    renderAt("/nodes/tw-01");
    expect(await screen.findByText(/上次觀測/)).toBeInTheDocument();
    expect(screen.getAllByText("沒有回應").length).toBeGreaterThan(0);
    expect(screen.queryByText("資料過期")).not.toBeInTheDocument();
  });

  it("says the host sent no metrics yet when it never reported", async () => {
    coreWith({ l: { reported: false, stale: false, droppedSamples: 0, sample: null, exhaustion: null } });
    renderAt("/nodes/tw-01");
    expect(await screen.findByText("這台主機還沒回報資源指標。")).toBeInTheDocument();
  });
});

describe("host overview: history", () => {
  it("reads one hour of raw points by default and 5-minute points for 7 days", async () => {
    const core = coreWith();
    renderAt("/nodes/tw-01");
    await chart("CPU 使用率");
    const cpu = () =>
      core.calls.filter((c) => c.path.endsWith("/metrics/history") && c.query.get("series") === "cpu.usage");
    await waitFor(() => expect(cpu().length).toBeGreaterThan(0));
    const first = cpu()[0]!.query;
    expect(first.get("resolution")).toBe("raw");
    expect(Date.parse(first.get("to")!) - Date.parse(first.get("from")!)).toBe(3600_000);
    expect(screen.getByText("每點 10 秒")).toBeInTheDocument();

    await userEvent.setup().click(screen.getByRole("button", { name: "7 天" }));
    await waitFor(() => expect(cpu().some((c) => c.query.get("resolution") === "5m")).toBe(true));
    const week = cpu().find((c) => c.query.get("resolution") === "5m")!.query;
    expect(Date.parse(week.get("to")!) - Date.parse(week.get("from")!)).toBe(7 * 86400_000);
    expect(screen.getByRole("button", { name: "7 天" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByText("每點 5 分鐘平均")).toBeInTheDocument();
  });

  it("draws a gap as a break in the line, never a drop to zero, and the table marks it", async () => {
    const t0 = Date.now() - 30 * 60_000;
    const at = (min: number) => new Date(t0 + min * 60_000).toISOString();
    coreWith({
      history: (c) =>
        c.query.get("series") === "cpu.usage"
          ? {
              status: 200,
              body: {
                items: [point(at(0), 20), point(at(1), 30), point(at(10), 40, true), point(at(11), 50)],
                page: { limit: 2000 },
              },
            }
          : noHistory(c),
    });
    renderAt("/nodes/tw-01");
    const fig = await chart("CPU 使用率");
    await waitFor(() => {
      const line = fig.querySelector("path.hx-chart-line")!;
      expect(line.getAttribute("d")!.match(/M/g)).toHaveLength(2);
    });
    await userEvent.setup().click(within(fig).getByRole("button", { name: "表格檢視" }));
    const rows = within(within(fig).getByRole("table")).getAllByRole("row");
    expect(rows).toHaveLength(5);
    expect(rows.filter((r) => r.textContent?.includes("（前面有缺口）"))).toHaveLength(1);
    expect(rows[1]).toHaveTextContent("50%");
  });

  it("says a period has no data instead of drawing a flat line", async () => {
    coreWith();
    renderAt("/nodes/tw-01");
    const fig = await chart("負載平均");
    expect(await within(fig).findByText("這段時間沒有資料")).toBeInTheDocument();
    expect(fig.querySelector("path.hx-chart-line")!.getAttribute("d")).toBe("");
  });

  it("reads the disk and interface the latest sample named", async () => {
    const core = coreWith();
    renderAt("/nodes/tw-01");
    await chart("磁碟 I/O");
    await waitFor(() =>
      expect(
        core.calls.some((c) => c.query.get("series") === "disk.readBytesPerSecond" && c.query.get("key") === "nvme0n1"),
      ).toBe(true),
    );
    expect(
      core.calls.some((c) => c.query.get("series") === "network.rxBytesPerSecond" && c.query.get("key") === "eth0"),
    ).toBe(true);
  });
});

describe("host page tabs", () => {
  it("opens 概況 from the host list row and keeps the ticket 125 panels under 系統", async () => {
    coreWith();
    const router = renderAt("/nodes");
    const u = userEvent.setup();
    await u.click(await screen.findByRole("link", { name: "hazel-tw-01" }));
    const tabs = await screen.findByRole("navigation", { name: "主機分頁" });
    expect(within(tabs).getByRole("link", { name: "概況" })).toHaveAttribute("aria-current", "page");
    expect(router.state.location.pathname).toBe("/nodes/tw-01");
    await u.click(within(tabs).getByRole("link", { name: "系統" }));
    expect(await panel("主機憑證")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/nodes/tw-01/system");
  });

  it("says when the tab does not exist", async () => {
    coreWith();
    renderAt("/nodes/tw-01/nope");
    expect(await screen.findByText("這台主機沒有這個分頁")).toBeInTheDocument();
  });
});
