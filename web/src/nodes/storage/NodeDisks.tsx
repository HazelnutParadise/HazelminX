import { Table } from "@cloudflare/kumo/components/table";
import { WarningIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Notice } from "../../auth/parts";
import { StatusPill, type StatusTone } from "../../components/StatusPill";
import { activeLocale } from "../../i18n";
import { formatRelative } from "../../lib/time";
import { useNow } from "../../shell/useNow";
import type { Node } from "../api";
import { REFRESH_MS } from "../hooks";
import { NodeError } from "../parts";
import { at } from "../status";
import { bytes, meterTone, percent } from "../monitor/format";
import "../monitor/monitor.css";
import { storageApi, storageKeys, type Alert, type Device, type MountView, type StorageView } from "./api";
import {
  bootMode,
  breakdown,
  configLocation,
  configsAt,
  daysToFull,
  hasUsageCard,
  isDeviceName,
  pct,
  ruleScope,
  type UsageBreakdown,
} from "./model";
import "./storage.css";

/** The inventory changes rarely (the agent re-sends it when it changes); usage is measured every 6 hours. */
const STORAGE_MS = 60_000;
/** Problems listed in the conflicts table; deviceName is marked on the mount itself. */
const CONFLICTS = new Set(["duplicateMountPoint", "missingDevice", "notMounted"]);

/**
 * The host page's 磁碟 tab (board NodeDisks, usage cards of board Storage; ticket 030.2): disks and partitions,
 * mounts with how each names its disk and where it is configured, configuration conflicts, usage by category and
 * capacity alerts. Mount changes (edit, unmount, switch to UUID, format) belong to tickets 031 and 032.
 */
export function NodeDisks({ node }: { node: Node }) {
  const { t } = useTranslation();
  const q = useQuery({
    queryKey: storageKeys.view(node.id),
    queryFn: () => storageApi.view(node.id),
    refetchInterval: (s) => (s.state.data?.reported ? STORAGE_MS : REFRESH_MS),
  });
  const v = q.data;
  if (!v) {
    return q.isError ? (
      <Notice tone="danger">
        {t("nodes.disks.loadFailed")} <NodeError error={q.error} />
      </Notice>
    ) : (
      <p className="hx-note">{t("nodes.disks.loading")}</p>
    );
  }
  if (!v.reported) {
    return (
      <section className="hx-panel" aria-labelledby="hx-disks-none">
        <div className="hx-panel-head">
          <h2 id="hx-disks-none">{t("nodes.disks.title")}</h2>
        </div>
        <div className="hx-panel-body">
          <p className="hx-note">{t("nodes.disks.notReported")}</p>
        </div>
      </section>
    );
  }
  const mounts = v.mounts ?? [];
  return (
    <div className="hx-mon">
      <Freshness view={v} />
      {q.isError && (
        <Notice tone="warn">
          {t("nodes.disks.refreshFailed")} <NodeError error={q.error} />
        </Notice>
      )}
      {(v.unreadable ?? []).length > 0 && (
        <Notice tone="warn">
          {t("nodes.disks.unreadable", { list: (v.unreadable ?? []).join(t("nodes.listSeparator")) })}
        </Notice>
      )}
      <Summary view={v} />
      <DevicesPanel view={v} />
      <MountsPanel view={v} />
      <ConflictsPanel view={v} />
      <UsagePanel view={v} mounts={mounts.filter(hasUsageCard)} />
    </div>
  );
}

function useAgo() {
  const { t } = useTranslation();
  const now = useNow(30_000);
  return (iso: string | null | undefined) =>
    iso ? (formatRelative(iso, now, activeLocale(), t("nodes.disks.justNow")) ?? at(iso) ?? null) : null;
}

function Freshness({ view: v }: { view: StorageView }) {
  const { t } = useTranslation();
  const ago = useAgo();
  return (
    <div className="hx-mon-fresh">
      <span className="hx-note" title={at(v.collectedAt) ?? undefined}>
        {t("nodes.disks.collected", { time: ago(v.collectedAt) })}
      </span>
      <span className="hx-note" title={at(v.usageMeasuredAt) ?? undefined}>
        {v.usageMeasuredAt
          ? t("nodes.disks.usageMeasured", { time: ago(v.usageMeasuredAt) })
          : t("nodes.disks.usageNever")}
      </span>
      {v.usageError && (
        <StatusPill tone="warn">
          {t(`nodes.disks.usageError.${v.usageError.code}`, {
            defaultValue: t("nodes.disks.usageError.other"),
            time: ago(v.usageError.at),
          })}
        </StatusPill>
      )}
    </div>
  );
}

function Stat({ label, value, sub, tone }: { label: string; value: ReactNode; sub?: ReactNode; tone?: "danger" }) {
  return (
    <div className="hx-disk-stat">
      <span className="hx-sub">{label}</span>
      <span className="hx-disk-stat-num">{value}</span>
      {sub && <span className={tone === "danger" ? "hx-disk-stat-sub is-danger" : "hx-disk-stat-sub"}>{sub}</span>}
    </div>
  );
}

function Summary({ view: v }: { view: StorageView }) {
  const { t } = useTranslation();
  const disks = (v.devices ?? []).filter((d) => d.kind === "disk");
  const mounts = v.mounts ?? [];
  const auto = mounts.filter((m) => bootMode(configsAt(v.configs, m.configs)) === "auto").length;
  const conflicts = (v.problems ?? []).filter((p) => CONFLICTS.has(p.kind ?? "")).length;
  const byName = mounts.filter((m) => isDeviceName(m.reference));
  const alerts = mounts.reduce((s, m) => s + (m.alerts ?? []).length, 0);
  return (
    <section className="hx-panel" aria-label={t("nodes.disks.summary")}>
      <div className="hx-disk-stats">
        <Stat label={t("nodes.disks.disks")} value={disks.length} sub={t("nodes.disks.healthNotRead")} />
        <Stat
          label={t("nodes.disks.mounts")}
          value={mounts.length}
          sub={
            <>
              {t("nodes.disks.autoCount", { count: auto })}
              {conflicts > 0 && (
                <>
                  {" · "}
                  <span className="is-danger">{t("nodes.disks.conflictCount", { count: conflicts })}</span>
                </>
              )}
            </>
          }
        />
        <Stat
          label={t("nodes.disks.byDeviceName")}
          value={byName.length}
          sub={byName.length ? byName.map((m) => m.mountPoint).join(t("nodes.listSeparator")) : undefined}
          tone={byName.length ? "danger" : undefined}
        />
        <Stat
          label={t("nodes.disks.alerts")}
          value={alerts}
          sub={alerts ? undefined : t("nodes.disks.noAlerts")}
          tone={alerts ? "danger" : undefined}
        />
      </div>
    </section>
  );
}

function Panel({
  id,
  title,
  extra,
  children,
}: {
  id: string;
  title: ReactNode;
  extra?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="hx-panel" aria-labelledby={id}>
      <div className="hx-panel-head">
        <h2 id={id}>{title}</h2>
        {extra}
      </div>
      {children}
    </section>
  );
}

function NotRead({ reason }: { reason?: string | null }) {
  const { t } = useTranslation();
  return (
    <span className="hx-np" title={reason ?? undefined}>
      {t("nodes.disks.notRead")}
    </span>
  );
}

/** The mount points on a block device; mountinfo names the device by its kernel name (`sdb1`). */
function mountsOf(d: Device, mounts: MountView[]) {
  return mounts.filter((m) => m.device === d.name || m.device === `/dev/${d.name}`).map((m) => m.mountPoint);
}

function DevicesPanel({ view: v }: { view: StorageView }) {
  const { t } = useTranslation();
  const devices = v.devices ?? [];
  const mounts = v.mounts ?? [];
  const swaps = v.swaps ?? [];
  // Disks first, each followed by its partitions, in the order the agent listed them.
  const rows = devices
    .filter((d) => d.kind === "disk")
    .flatMap((d) => [d, ...devices.filter((p) => p.kind === "partition" && p.parent === d.name)]);
  const orphans = devices.filter((d) => !rows.includes(d));
  const all = [...rows, ...orphans];
  return (
    <Panel id="hx-disks-devices" title={t("nodes.disks.devicesTitle")}>
      {all.length === 0 ? (
        <div className="hx-panel-body">
          <p className="hx-note">{t("nodes.disks.noDevices")}</p>
        </div>
      ) : (
        <div className="hx-table-wrap" tabIndex={0} role="group" aria-label={t("nodes.disks.scrollTable")}>
          <Table className="hx-table hx-disk-table">
            <Table.Header>
              <Table.Row>
                <Table.Head>{t("nodes.disks.col.device")}</Table.Head>
                <Table.Head>{t("nodes.disks.col.size")}</Table.Head>
                <Table.Head>{t("nodes.disks.col.fs")}</Table.Head>
                <Table.Head>{t("nodes.disks.col.mountedAt")}</Table.Head>
                <Table.Head className="hx-disk-c3">{t("nodes.disks.col.health")}</Table.Head>
                <Table.Head className="hx-disk-c3">{t("nodes.disks.col.temp")}</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {all.map((d) => {
                const part = d.kind === "partition";
                const on = mountsOf(d, mounts);
                const swap = swaps.find((s) => s.name === `/dev/${d.name}`);
                const ids = [
                  d.fsUuid ? `UUID ${d.fsUuid}` : null,
                  d.fsLabel ? t("nodes.disks.labelValue", { label: d.fsLabel }) : null,
                ].filter(Boolean);
                const about = part
                  ? ids.join(" · ")
                  : [d.model, d.transport?.toUpperCase(), d.removable ? t("nodes.disks.removable") : null]
                      .filter(Boolean)
                      .join(" · ");
                return (
                  <Table.Row key={d.name ?? d.dev}>
                    <Table.Cell>
                      <div className={part ? "hx-disk-part" : undefined}>
                        <div className="hx-name hx-mono hx-disk-nowrap">{d.name}</div>
                        {about && <div className="hx-sub hx-mono">{about}</div>}
                      </div>
                    </Table.Cell>
                    <Table.Cell className="hx-mono hx-disk-nowrap">{bytes(d.sizeBytes) ?? <NotProvided />}</Table.Cell>
                    <Table.Cell className="hx-mono">
                      {d.fsType ?? (part ? "—" : t(`nodes.disks.class.${d.class}`, { defaultValue: d.class ?? "—" }))}
                    </Table.Cell>
                    <Table.Cell className="hx-mono hx-disk-pre">
                      {on.length ? on.join("\n") : swap ? "swap" : "—"}
                    </Table.Cell>
                    <Table.Cell className="hx-disk-c3">
                      {d.health ? (
                        d.health.state === "notCollected" ? (
                          <NotRead reason={d.health.reason} />
                        ) : (
                          t(`nodes.disks.health.${d.health.state}`, { defaultValue: d.health.state })
                        )
                      ) : (
                        "—"
                      )}
                    </Table.Cell>
                    <Table.Cell className="hx-disk-c3">{part ? "—" : <NotRead />}</Table.Cell>
                  </Table.Row>
                );
              })}
            </Table.Body>
          </Table>
        </div>
      )}
    </Panel>
  );
}

function NotProvided() {
  const { t } = useTranslation();
  return <span className="hx-np">{t("nodes.monitor.notProvided")}</span>;
}

/** How a mount names its disk; a local mount by kernel device name gets the warning icon, others nothing. */
function Reference({ mount: m, spec }: { mount: MountView; spec: string }) {
  const { t } = useTranslation();
  const warn = isDeviceName(m.reference);
  // The spec already says UUID=, LABEL=, /dev/disk/by-…; only a device name and a network share get a word.
  const label = m.reference === "device" || m.reference === "network" ? t(`nodes.disks.ref.${m.reference}`) : null;
  return (
    <span className={warn ? "hx-disk-ref is-warn" : "hx-disk-ref"}>
      {warn && (
        <WarningIcon
          className="hx-disk-warn"
          size={14}
          weight="bold"
          role="img"
          aria-label={t("nodes.disks.deviceNameWarn")}
        >
          <title>{t("nodes.disks.deviceNameWarn")}</title>
        </WarningIcon>
      )}
      <span className="hx-mono">{spec}</span>
      {label && <span className="hx-sub"> · {label}</span>}
    </span>
  );
}

const BOOT_TONE: Record<string, StatusTone> = { auto: "ok", onDemand: "ok", noauto: "warn", manual: "warn" };

function MountsPanel({ view: v }: { view: StorageView }) {
  const { t } = useTranslation();
  const mounts = v.mounts ?? [];
  const swaps = v.swaps ?? [];
  const conflicts = (v.problems ?? []).filter((p) => CONFLICTS.has(p.kind ?? "")).length;
  return (
    <Panel
      id="hx-disks-mounts"
      title={t("nodes.disks.mountsTitle", { count: mounts.length })}
      extra={
        conflicts ? (
          <StatusPill tone="warn">{t("nodes.disks.conflictCount", { count: conflicts })}</StatusPill>
        ) : undefined
      }
    >
      {mounts.length + swaps.length === 0 ? (
        <div className="hx-panel-body">
          <p className="hx-note">{t("nodes.disks.noMounts")}</p>
        </div>
      ) : (
        <div className="hx-table-wrap" tabIndex={0} role="group" aria-label={t("nodes.disks.scrollTable")}>
          <Table className="hx-table hx-disk-table">
            <Table.Header>
              <Table.Row>
                <Table.Head>{t("nodes.disks.col.mountPoint")}</Table.Head>
                <Table.Head className="hx-disk-c3">{t("nodes.disks.col.fs")}</Table.Head>
                <Table.Head>{t("nodes.disks.col.use")}</Table.Head>
                <Table.Head>{t("nodes.disks.col.boot")}</Table.Head>
                <Table.Head className="hx-disk-c5">{t("nodes.disks.col.config")}</Table.Head>
                <Table.Head>{t("nodes.disks.col.alerts")}</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {mounts.map((m) => {
                const cs = configsAt(v.configs, m.configs);
                const boot = bootMode(cs);
                const c = m.capacity;
                const p = pct(c?.usedBytes, c?.totalBytes);
                return (
                  <Table.Row key={m.mountPoint}>
                    <Table.Cell>
                      <div className="hx-name hx-mono">{m.mountPoint}</div>
                      <div className="hx-sub">
                        <Reference mount={m} spec={cs[0]?.spec ?? m.source ?? ""} />
                      </div>
                    </Table.Cell>
                    <Table.Cell className="hx-mono hx-disk-c3">
                      {m.fsType}
                      {m.readOnly && <div className="hx-sub">{t("nodes.disks.readOnly")}</div>}
                    </Table.Cell>
                    <Table.Cell>
                      {c && c.usedBytes != null && c.totalBytes != null ? (
                        <span className="hx-disk-use">
                          <span
                            className={`hx-meter is-${meterTone(p)}`}
                            role="img"
                            aria-label={percent(p) ?? undefined}
                          >
                            <span style={{ width: `${Math.min(100, p ?? 0)}%` }} />
                          </span>
                          <span className="hx-mono">
                            {bytes(c.usedBytes)} / {bytes(c.totalBytes)}
                          </span>
                        </span>
                      ) : (
                        <NotProvided />
                      )}
                    </Table.Cell>
                    <Table.Cell>
                      <StatusPill tone={BOOT_TONE[boot]}>{t(`nodes.disks.boot.${boot}`)}</StatusPill>
                    </Table.Cell>
                    <Table.Cell className="hx-disk-c5 hx-mono hx-disk-nowrap">
                      {cs.length ? (
                        cs.map(configLocation).join(" · ")
                      ) : (
                        <span className="hx-sub">{t("nodes.disks.noConfig")}</span>
                      )}
                    </Table.Cell>
                    <Table.Cell>
                      <Alerts mount={m} />
                    </Table.Cell>
                  </Table.Row>
                );
              })}
              {swaps.map((s) => {
                const cs = configsAt(v.configs, s.configs);
                const boot = cs.length ? "auto" : "manual";
                return (
                  <Table.Row key={`swap:${s.name}`}>
                    <Table.Cell>
                      <div className="hx-name hx-mono">swap</div>
                      <div className="hx-sub hx-mono">{s.name}</div>
                    </Table.Cell>
                    <Table.Cell className="hx-mono hx-disk-nowrap hx-disk-c3">
                      {t(`nodes.disks.swapKind.${s.kind}`, { defaultValue: s.kind ?? "" })}
                    </Table.Cell>
                    <Table.Cell className="hx-mono">{bytes(s.sizeBytes) ?? <NotProvided />}</Table.Cell>
                    <Table.Cell>
                      <StatusPill tone={BOOT_TONE[boot]}>{t(`nodes.disks.swapBoot.${boot}`)}</StatusPill>
                    </Table.Cell>
                    <Table.Cell className="hx-disk-c5 hx-mono hx-disk-nowrap">
                      {cs.length ? (
                        cs.map(configLocation).join(" · ")
                      ) : (
                        <span className="hx-sub">{t("nodes.disks.noConfig")}</span>
                      )}
                    </Table.Cell>
                    <Table.Cell>—</Table.Cell>
                  </Table.Row>
                );
              })}
            </Table.Body>
          </Table>
        </div>
      )}
    </Panel>
  );
}

/** One alert in words, with the numbers taken from the mount itself (Core's message is English). */
function alertText(a: Alert, m: MountView, t: TFunction) {
  const c = m.capacity;
  switch (a.kind) {
    case "availableLow":
      return t("nodes.disks.alert.availableLow", {
        pct: percent(pct(c?.availableBytes, c?.totalBytes)),
        bytes: bytes(c?.availableBytes),
      });
    case "inodesLow":
      return t("nodes.disks.alert.inodesLow", { pct: percent(pct(c?.inodesFree, c?.inodesTotal)) });
    case "fullSoon":
      return t("nodes.disks.alert.fullSoon", { count: daysToFull(c?.availableBytes, m.growthBytesPerDay) ?? 0 });
    default:
      return a.message ?? a.kind ?? "";
  }
}

function Alerts({ mount: m }: { mount: MountView }) {
  const { t } = useTranslation();
  const list = m.alerts ?? [];
  if (list.length === 0) return <span className="hx-sub">—</span>;
  return (
    <span className="hx-disk-alerts">
      {list.map((a) => (
        <StatusPill key={a.kind} tone={a.level === "critical" ? "danger" : "warn"}>
          {alertText(a, m, t)}
        </StatusPill>
      ))}
    </span>
  );
}

function ConflictsPanel({ view: v }: { view: StorageView }) {
  const { t } = useTranslation();
  const list = (v.problems ?? []).filter((p) => CONFLICTS.has(p.kind ?? ""));
  if (list.length === 0) return null;
  return (
    <Panel id="hx-disks-conflicts" title={t("nodes.disks.conflictsTitle", { count: list.length })}>
      <div className="hx-table-wrap" tabIndex={0} role="group" aria-label={t("nodes.disks.scrollTable")}>
        <Table className="hx-table hx-disk-table">
          <Table.Header>
            <Table.Row>
              <Table.Head>{t("nodes.disks.col.problem")}</Table.Head>
              <Table.Head>{t("nodes.disks.col.writtenAt")}</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {list.map((p, i) => {
              const cs = configsAt(v.configs, p.configs);
              return (
                <Table.Row key={`${p.kind}:${p.mountPoint}:${i}`}>
                  <Table.Cell>
                    <div className="hx-name">{t(`nodes.disks.problem.${p.kind}`)}</div>
                    <div className="hx-sub hx-mono">{p.mountPoint}</div>
                  </Table.Cell>
                  <Table.Cell className="hx-mono">
                    {cs.length ? cs.map((c) => `${configLocation(c)} · ${c.spec}`).join("\n") : "—"}
                  </Table.Cell>
                </Table.Row>
              );
            })}
          </Table.Body>
        </Table>
      </div>
    </Panel>
  );
}

const SWATCH: Record<string, string> = {
  appData: "hx-cat-1",
  database: "hx-cat-2",
  backupStaging: "hx-cat-3",
  containerImages: "hx-cat-4",
  osAndLogs: "hx-cat-os",
  other: "hx-cat-other",
};

function UsagePanel({ view: v, mounts }: { view: StorageView; mounts: MountView[] }) {
  const { t } = useTranslation();
  if (mounts.length === 0) return null;
  return (
    <Panel id="hx-disks-usage" title={t("nodes.disks.usageTitle")}>
      <div className="hx-disk-cards">
        {mounts.map((m) => (
          <UsageCard key={m.mountPoint} mount={m} configs={configsAt(v.configs, m.configs)} />
        ))}
      </div>
    </Panel>
  );
}

function UsageCard({ mount: m, configs }: { mount: MountView; configs: ReturnType<typeof configsAt> }) {
  const { t } = useTranslation();
  const ago = useAgo();
  const c = m.capacity;
  const b = breakdown(m.usage, c?.availableBytes);
  const days = daysToFull(c?.availableBytes, m.growthBytesPerDay);
  const scope = ruleScope(m.rule);
  const growth = m.growthBytesPerDay;
  return (
    <article className="hx-disk-card" aria-labelledby={`hx-disk-card-${m.mountPoint}`}>
      <h3 id={`hx-disk-card-${m.mountPoint}`} className="hx-mono">
        {m.mountPoint}
      </h3>
      <div className="hx-sub">
        <Reference mount={m} spec={configs[0]?.spec ?? m.source ?? ""} /> · <span className="hx-mono">{m.fsType}</span>
      </div>
      <UsageBar b={b} />
      <UsageState mount={m} b={b} ago={ago} />
      <p className="hx-disk-foot">
        <span className="hx-mono">
          {c?.usedBytes != null && c?.totalBytes != null
            ? `${bytes(c.usedBytes)} / ${bytes(c.totalBytes)}`
            : t("nodes.disks.noCapacity")}
        </span>
        {growth != null && (
          <span>{t("nodes.disks.growth", { value: `${growth >= 0 ? "+" : ""}${bytes(growth)}` })}</span>
        )}
        {growth == null && c && <span className="hx-sub">{t("nodes.disks.noTrend")}</span>}
        {days != null && <span>{t("nodes.disks.fullIn", { count: days })}</span>}
      </p>
      {(m.alerts ?? []).length > 0 && (
        <div className="hx-disk-alerts">
          <Alerts mount={m} />
        </div>
      )}
      <p className="hx-sub">{t(`nodes.disks.rule.${scope}`)}</p>
    </article>
  );
}

function UsageBar({ b }: { b: UsageBreakdown }) {
  const { t } = useTranslation();
  if (b.state !== "complete" || b.scale == null) return null;
  const parts = [
    ...b.segments.map((s) => ({
      key: s.category,
      cls: SWATCH[s.category],
      bytes: s.bytes,
      name: t(`nodes.disks.cat.${s.category}`),
    })),
    ...(b.available != null && b.available > 0
      ? [{ key: "free", cls: "hx-cat-free", bytes: b.available, name: t("nodes.disks.available") }]
      : []),
  ];
  return (
    <>
      <div className="hx-disk-bar" role="img" aria-label={parts.map((p) => `${p.name} ${bytes(p.bytes)}`).join("、")}>
        {parts.map((p) => (
          <span key={p.key} className={p.cls} style={{ flexGrow: p.bytes }} />
        ))}
      </div>
      <ul className="hx-legend hx-disk-legend">
        {parts.map((p) => (
          <li key={p.key}>
            <span className={`hx-disk-swatch ${p.cls}`} aria-hidden="true" />
            {p.name} <span className="hx-mono">{bytes(p.bytes)}</span>
          </li>
        ))}
      </ul>
    </>
  );
}

/** The measurement's state: never measured, unmeasured, partial with its reason and lower bounds, or complete. */
function UsageState({
  mount: m,
  b,
  ago,
}: {
  mount: MountView;
  b: UsageBreakdown;
  ago: (iso: string | null | undefined) => string | null;
}) {
  const { t } = useTranslation();
  const u = m.usage;
  if (b.state === "none") return <p className="hx-note">{t("nodes.disks.state.none")}</p>;
  const when = ago(u?.measuredAt);
  if (b.state === "unmeasured") return <p className="hx-note">{t("nodes.disks.state.unmeasured", { time: when })}</p>;
  if (b.state === "complete") return <p className="hx-sub">{t("nodes.disks.state.complete", { time: when })}</p>;
  const dirs = u?.unreadableDirs ?? 0;
  return (
    <>
      <p className="hx-note">
        <StatusPill tone="warn">{t("nodes.disks.state.partial")}</StatusPill>{" "}
        {dirs > 0 ? t("nodes.disks.state.unreadableDirs", { count: dirs }) : t("nodes.disks.state.limit")} ·{" "}
        {t("nodes.disks.state.measured", { time: when })}
      </p>
      {b.segments.length > 0 && (
        <ul className="hx-legend hx-disk-legend">
          {b.segments.map((s) => (
            <li key={s.category}>
              <span className={`hx-disk-swatch ${SWATCH[s.category]}`} aria-hidden="true" />
              {t(`nodes.disks.cat.${s.category}`)}{" "}
              <span className="hx-mono">{t("nodes.disks.atLeast", { value: bytes(s.bytes) })}</span>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
