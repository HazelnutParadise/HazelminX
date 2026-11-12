import { beforeAll, describe, expect, it } from "vite-plus/test";
import { initI18n } from "../i18n";
import zh from "../i18n/locales/zh-TW.json";
import { decide, enrollCommand } from "./enroll";
import { node } from "./fixtures";
import {
  apiState,
  availableActions,
  canDo,
  certificateStatus,
  hasFilters,
  hostStatus,
  isUnreachable,
  matchesStatus,
  parseTags,
  pendingSync,
  readFilters,
  tagsAndGroups,
  writeFilters,
} from "./status";

beforeAll(() => initI18n("zh-TW"));

describe("host status", () => {
  it.each([
    [{ connection: { state: "online" } }, "online", "ok"],
    [{ connection: { state: "stale" } }, "stale", "warn"],
    [{ connection: { state: "offline" } }, "offline", "danger"],
    [{ connection: { state: "never_connected" } }, "never", "warn"],
    [{ maintenance: { enabled: true, reason: "" }, connection: { state: "offline" } }, "maintenance", "info"],
    [{ state: "revoked", revocation: { pending: false } }, "revoked", "danger"],
    [{ state: "revoked", revocation: { pending: true } }, "revokedPending", "danger"],
    [{ state: "identity_conflict" }, "conflict", "danger"],
    [{ state: "removed" }, "removed", "info"],
  ])("%j reads as %s", (over, key, tone) => {
    expect(hostStatus(node({ id: "a", ...(over as object) }))).toEqual({ key, tone });
  });

  it("never says the host is off: offline and stale only describe the channel", () => {
    const words = JSON.stringify([zh.nodes.status, zh.nodes.statusNote]);
    for (const banned of ["關機", "已停止", "當機", "停機"]) expect(words).not.toContain(banned);
    expect(zh.nodes.statusNote.offline).toBe("通道中斷");
  });

  it("counts a host as unreachable only when it is active, not in maintenance, and stale or offline", () => {
    expect(isUnreachable(node({ id: "a", connection: { state: "offline" } as never }))).toBe(true);
    expect(isUnreachable(node({ id: "a", connection: { state: "stale" } as never }))).toBe(true);
    expect(isUnreachable(node({ id: "a", connection: { state: "online" } as never }))).toBe(false);
    expect(isUnreachable(node({ id: "a", connection: { state: "never_connected" } as never }))).toBe(false);
    expect(
      isUnreachable(
        node({ id: "a", connection: { state: "offline" } as never, maintenance: { enabled: true } as never }),
      ),
    ).toBe(false);
    expect(isUnreachable(node({ id: "a", state: "revoked", connection: { state: "offline" } as never }))).toBe(false);
  });

  it("puts a rotation before the certificate's own state", () => {
    const c = (certificate: object) => certificateStatus(node({ id: "a", certificate: certificate as never })).key;
    expect(c({ state: "valid" })).toBe("valid");
    expect(c({ state: "expiring" })).toBe("expiring");
    expect(c({ state: "expired" })).toBe("expired");
    expect(c({ state: "valid", rotation: { newSerial: "2" } })).toBe("rotationPending");
    expect(c({ state: "valid", rotation: { newSerial: "2", deliveredAt: "2026-11-12T00:00:00Z" } })).toBe(
      "rotationDelivered",
    );
    expect(c({ state: "valid", rotation: { newSerial: "2", error: "x" } })).toBe("rotationFailed");
  });

  it("lists what has not reached the host in a fixed order and keeps unknown entries", () => {
    expect(
      pendingSync(
        node({ id: "a", pendingSync: ["maintenance-recheck", "something-new", "revocation", "certificate-rotation"] }),
      ),
    ).toEqual(["revocation", "certificate-rotation", "maintenance-recheck", "something-new"]);
    expect(pendingSync(node({ id: "a", pendingSync: undefined as never }))).toEqual([]);
  });

  it("offers only the actions Core allows in each state", () => {
    expect(availableActions(node({ id: "a" }))).toEqual(["edit", "maintenanceOn", "rotate", "revoke", "remove"]);
    expect(availableActions(node({ id: "a", maintenance: { enabled: true } as never }))).toEqual([
      "edit",
      "maintenanceOff",
      "rotate",
      "revoke",
      "remove",
    ]);
    expect(availableActions(node({ id: "a", state: "identity_conflict" }))).toEqual(["edit", "revoke", "remove"]);
    expect(availableActions(node({ id: "a", state: "revoked" }))).toEqual(["edit", "remove"]);
    expect(availableActions(node({ id: "a", state: "removed" }))).toEqual([]);
  });
});

describe("list filters", () => {
  it("reads and writes the URL, leaving defaults out", () => {
    const f = readFilters(new URLSearchParams("q=+lab+&tag=arm64&group=home&status=unreachable"));
    expect(f).toEqual({ q: " lab ", tag: "arm64", group: "home", status: "unreachable" });
    expect(writeFilters(f).toString()).toBe("q=lab&tag=arm64&group=home&status=unreachable");
    expect(writeFilters(readFilters(new URLSearchParams())).toString()).toBe("");
    expect(readFilters(new URLSearchParams("status=bogus")).status).toBe("all");
    expect(hasFilters(readFilters(new URLSearchParams()))).toBe(false);
    expect(hasFilters(readFilters(new URLSearchParams("q=%20")))).toBe(false);
  });

  it("asks the API for trust states and filters the channel states itself", () => {
    expect(apiState("removed")).toBe("removed");
    expect(apiState("revoked")).toBe("revoked");
    expect(apiState("conflict")).toBe("identity_conflict");
    expect(apiState("unreachable")).toBeUndefined();
    const off = node({ id: "a", connection: { state: "offline" } as never });
    const on = node({ id: "b" });
    const never = node({ id: "c", connection: { state: "never_connected" } as never });
    const maint = node({ id: "d", maintenance: { enabled: true } as never });
    const rows = [off, on, never, maint];
    expect(rows.filter((n) => matchesStatus(n, "unreachable")).map((n) => n.id)).toEqual(["a"]);
    expect(rows.filter((n) => matchesStatus(n, "online")).map((n) => n.id)).toEqual(["b"]);
    expect(rows.filter((n) => matchesStatus(n, "never")).map((n) => n.id)).toEqual(["c"]);
    expect(rows.filter((n) => matchesStatus(n, "maintenance")).map((n) => n.id)).toEqual(["d"]);
    expect(rows.filter((n) => matchesStatus(n, "all"))).toHaveLength(4);
  });

  it("collects tags and groups for the menus", () => {
    expect(
      tagsAndGroups([
        node({ id: "a", tags: ["lab", "arm64"], group: "home" }),
        node({ id: "b", tags: ["lab"], group: "" }),
        node({ id: "c", tags: [], group: "dc" }),
      ]),
    ).toEqual({ tags: ["arm64", "lab"], groups: ["dc", "home"] });
  });

  it("splits typed tags on half- and full-width commas", () => {
    expect(parseTags(" lab, arm64，lab、 edge\n")).toEqual(["lab", "arm64", "edge"]);
    expect(parseTags(" , ")).toEqual([]);
  });
});

describe("permissions", () => {
  const roles = [
    { role: "owner", actions: ["*"] },
    { role: "platform_admin", actions: ["nodes:read", "nodes:write"] },
    { role: "auditor", actions: ["nodes:read"] },
  ];
  it("follows iam.Authorize: a role that grants the action on a scope that covers the host", () => {
    const g = (role: string) => [{ role, scope: { kind: "global", id: "*" } }];
    expect(canDo("nodes:write", g("owner"), roles)).toBe(true);
    expect(canDo("nodes:write", g("platform_admin"), roles)).toBe(true);
    expect(canDo("nodes:write", g("auditor"), roles)).toBe(false);
    const scoped = [{ role: "platform_admin", scope: { kind: "node", id: "n1" } }];
    expect(canDo("nodes:write", scoped, roles, "n1")).toBe(true);
    expect(canDo("nodes:write", scoped, roles, "n2")).toBe(false);
    expect(canDo("nodes:write", scoped, roles)).toBe(false);
    expect(canDo("nodes:write", g("owner"), undefined)).toBe(false);
  });
});

describe("enrollment", () => {
  it("builds the agent's enroll command, quoting a group the shell would split", () => {
    expect(enrollCommand("https://panel.example.tw:9801 ", "HZX-7Q4M-2KD9-PX81-C3VB", "")).toBe(
      "hazelminx-agent enroll \\\n  --core-url https://panel.example.tw:9801 \\\n  --code HZX-7Q4M-2KD9-PX81-C3VB",
    );
    expect(enrollCommand("https://a:9801", "HZX-1", "home lab")).toContain("--group 'home lab'");
    expect(enrollCommand("https://a:9801", "HZX-1", "it's")).toContain(`--group 'it'\\''s'`);
    expect(enrollCommand("https://a:9801", "HZX-1", "dc-1")).toContain("--group dc-1");
  });

  it("decides what approving does with the hosts it resembles", () => {
    const m = (reason: string, over: object = {}) => ({
      nodeId: `n-${reason}`,
      name: reason,
      state: "active",
      reason,
      keyRetired: false,
      ...over,
    });
    expect(decide([])).toEqual({ blocked: null, sameKey: null, machine: [] });
    expect(decide([m("same_key")]).sameKey?.nodeId).toBe("n-same_key");
    expect(decide([m("same_machine_id")]).machine).toHaveLength(1);
    // The same key wins: approving keeps that host, no choice needed.
    expect(decide([m("same_key"), m("same_machine_id")]).machine).toEqual([]);
    expect(decide([m("same_key", { keyRetired: true })]).blocked?.nodeId).toBe("n-same_key");
  });
});
