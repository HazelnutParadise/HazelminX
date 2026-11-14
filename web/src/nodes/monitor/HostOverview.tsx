import { useQueries, useQuery } from "@tanstack/react-query";
import { useMemo, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router";
import { Notice } from "../../auth/parts";
import { StatusPill } from "../../components/StatusPill";
import { formatRelative } from "../../lib/time";
import { activeLocale } from "../../i18n";
import { useNow } from "../../shell/useNow";
import { CONNECTION, type Node } from "../api";
import { REFRESH_MS } from "../hooks";
import { NodeError } from "../parts";
import { at } from "../status";
import {
  monitorApi,
  monitorKeys,
  RANGES,
  type Exhaustion,
  type HostFacts,
  type LatestMetrics,
  type RangeKey,
  type Sample,
} from "./api";
import { bytes, celsius, meterTone, num, percent, rate, ratio } from "./format";
import { TimeChart, type ChartSeries } from "./TimeChart";
import "./monitor.css";

/** Facts change rarely (the agent re-reads them every 60 s). */
const FACTS_MS = 60_000;
/** A card lists this many rows before folding the rest (ticket 020.4「一組超過 4 個」). */
const FOLD = 4;

/**
 * The host page's 概況 tab (board NodeDetail, ticket 020.4): host facts, limits that run out before space does,
 * hardware cards from the latest sample, and history charts. GPU, disk health, containers and processes belong to
 * tickets 020.2, 020.3, 039.3 and 023 and are not shown until their data exists.
 */
export function HostOverview({ node }: { node: Node }) {
  const { t } = useTranslation();
  const facts = useQuery({
    queryKey: monitorKeys.facts(node.id),
    queryFn: () => monitorApi.facts(node.id),
    refetchInterval: FACTS_MS,
  });
  const latest = useQuery({
    queryKey: monitorKeys.latest(node.id),
    queryFn: () => monitorApi.latest(node.id),
    refetchInterval: REFRESH_MS,
  });
  const l = latest.data;
  const offline = node.connection.state !== CONNECTION.online;
  return (
    <div className="hx-mon">
      <Freshness latest={l} offline={offline} error={latest.error} />
      <FactsPanel facts={facts.data} error={facts.error} loading={facts.isPending} />
      {l?.reported && l.sample && l.exhaustion && <ExhaustionNotice x={l.exhaustion} />}
      {l?.reported && l.sample ? (
        <Hardware sample={l.sample} exhaustion={l.exhaustion} />
      ) : (
        !latest.isPending &&
        !latest.error && (
          <section className="hx-panel" aria-labelledby="hx-mon-hw">
            <div className="hx-panel-head">
              <h2 id="hx-mon-hw">{t("nodes.monitor.hardware")}</h2>
            </div>
            <div className="hx-panel-body">
              <p className="hx-note">{t("nodes.monitor.noSample")}</p>
            </div>
          </section>
        )
      )}
      <Charts nodeId={node.id} latest={l} />
    </div>
  );
}

/** 「資料更新於」 and the stale state: the channel may be up while samples stopped (DESIGN.md 資料過期 vs 沒有回應). */
function Freshness({ latest: l, offline, error }: { latest?: LatestMetrics; offline: boolean; error: unknown }) {
  const { t } = useTranslation();
  const now = useNow(5_000);
  if (error) return <NodeError error={error} />;
  if (!l?.reported) return null;
  const ago = formatRelative(l.receivedAt ?? undefined, now, activeLocale(), t("nodes.justNow"));
  return (
    <div className="hx-mon-fresh">
      {l.stale && !offline && <StatusPill tone="warn">{t("nodes.monitor.stale")}</StatusPill>}
      <span className="hx-note">
        {l.stale
          ? t("nodes.monitor.lastObserved", { time: at(l.collectedAt), ago })
          : t("nodes.monitor.updated", { time: at(l.collectedAt), count: l.intervalSeconds ?? 0 })}
      </span>
      {(l.droppedSamples ?? 0) > 0 && (
        <span className="hx-note">{t("nodes.monitor.dropped", { count: l.droppedSamples ?? 0 })}</span>
      )}
    </div>
  );
}

const NP = "nodes.monitor.notProvided";

/** A value or 「未提供」, with where it was read from on the second line. */
function FactValue({ value, source }: { value: string | null; source?: string | null }) {
  const { t } = useTranslation();
  return (
    <>
      {value ?? <span className="hx-np">{t(NP)}</span>}
      {source && <span className="hx-mon-source hx-mono">{source}</span>}
    </>
  );
}

function FactsPanel({ facts, error, loading }: { facts?: HostFacts; error: unknown; loading: boolean }) {
  const { t } = useTranslation();
  const f = facts?.reported ? facts.facts : null;
  const v = <T,>(x: { value: T | null; source: string | null } | null | undefined) => x?.value ?? null;
  const src = (x: { source: string | null } | null | undefined) => x?.source ?? null;
  const sync = f?.timeSynchronized;
  const err = f?.timeMaxErrorMicros?.value;
  const os = v(f?.osPrettyName) ?? ([v(f?.osName), v(f?.osVersion)].filter(Boolean).join(" ") || null);
  const cpus = v(f?.logicalCpus);
  return (
    <section className="hx-panel" aria-labelledby="hx-mon-facts">
      <div className="hx-panel-head">
        <h2 id="hx-mon-facts">{t("nodes.monitor.facts")}</h2>
        {facts?.reported && (
          <span className="hx-note">{t("nodes.monitor.collected", { time: at(facts.collectedAt) })}</span>
        )}
      </div>
      <div className="hx-panel-body">
        {error ? (
          <NodeError error={error} />
        ) : loading ? null : !f ? (
          <p className="hx-note">{t("nodes.monitor.noFacts")}</p>
        ) : (
          <dl className="hx-mon-facts">
            <div>
              <dt>{t("nodes.monitor.os")}</dt>
              <dd>
                <FactValue value={os} source={src(f.osPrettyName)} />
              </dd>
            </div>
            <div>
              <dt>{t("nodes.monitor.kernel")}</dt>
              <dd className="hx-mono">
                <FactValue value={v(f.kernel)} source={src(f.kernel)} />
              </dd>
            </div>
            <div>
              <dt>{t("nodes.monitor.arch")}</dt>
              <dd className="hx-mono">
                <FactValue value={v(f.arch)} source={src(f.arch)} />
              </dd>
            </div>
            <div>
              <dt>{t("nodes.monitor.bootTime")}</dt>
              <dd>
                <FactValue value={at(v(f.bootTime)) ?? null} source={src(f.bootTime)} />
              </dd>
            </div>
            <div>
              <dt>{t("nodes.monitor.timezone")}</dt>
              <dd className="hx-mono">
                <FactValue value={v(f.timezone)} source={src(f.timezone)} />
              </dd>
            </div>
            <div>
              <dt>{t("nodes.monitor.timeSync")}</dt>
              <dd>
                <FactValue
                  value={
                    sync?.value == null
                      ? null
                      : sync.value
                        ? err == null
                          ? t("nodes.monitor.synced")
                          : t("nodes.monitor.syncedError", { ms: (err / 1000).toFixed(1) })
                        : t("nodes.monitor.notSynced")
                  }
                  source={src(sync)}
                />
              </dd>
            </div>
            <div>
              <dt>{t("nodes.monitor.cpu")}</dt>
              <dd>
                <FactValue
                  value={
                    [v(f.cpuModel), cpus == null ? null : t("nodes.monitor.cores", { count: cpus })]
                      .filter(Boolean)
                      .join(" · ") || null
                  }
                  source={src(f.cpuModel)}
                />
              </dd>
            </div>
            <div>
              <dt>{t("nodes.monitor.memory")}</dt>
              <dd>
                <FactValue value={bytes(v(f.memoryTotalBytes))} source={src(f.memoryTotalBytes)} />
              </dd>
            </div>
          </dl>
        )}
      </div>
    </section>
  );
}

/** ROB-009: limits that run out before space does, each with its number (T-165, T-183). */
function ExhaustionNotice({ x }: { x: Exhaustion }) {
  const { t } = useTranslation();
  const items: string[] = [];
  if (x.pids?.exceeded)
    items.push(t("nodes.monitor.x.pids", { pct: percent(x.pids.percent), limit: x.pids.thresholdPercent }));
  if (x.fileHandles?.exceeded)
    items.push(
      t("nodes.monitor.x.files", { pct: percent(x.fileHandles.percent), limit: x.fileHandles.thresholdPercent }),
    );
  if (x.memoryPressure?.exceeded)
    items.push(
      t("nodes.monitor.x.pressure", {
        pct: percent(x.memoryPressure.someAvg60),
        limit: x.memoryPressure.thresholdPercent,
      }),
    );
  for (const fs of x.filesystems ?? []) {
    if (!fs) continue;
    if (fs.inodesExhausted) items.push(t("nodes.monitor.x.inodes", { mount: fs.mountPoint }));
    if (fs.readOnly) items.push(t("nodes.monitor.x.readOnly", { mount: fs.mountPoint }));
  }
  if (items.length === 0) return null;
  return (
    <Notice tone="danger">
      <strong>{t("nodes.monitor.x.title")}</strong>
      <ul className="hx-mon-xlist">
        {items.map((s) => (
          <li key={s}>{s}</li>
        ))}
      </ul>
    </Notice>
  );
}

function Meter({ pct, label }: { pct: number | null; label: string }) {
  return (
    <span
      className={`hx-meter is-${meterTone(pct)}`}
      role="meter"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={pct == null ? undefined : Math.round(pct)}
    >
      <span style={{ width: `${Math.min(100, Math.max(0, pct ?? 0))}%` }} />
    </span>
  );
}

interface BarRow {
  key: string;
  name: string;
  sub?: string;
  /** 0–100 for the bar; null draws an empty track. */
  pct: number | null;
  value: ReactNode;
  flag?: ReactNode;
}

/** Name, meter and number per row; past FOLD rows the rest fold under 「全部 N 個」 (ticket 020.4). */
function Bars({ rows, label }: { rows: BarRow[]; label: string }) {
  const { t } = useTranslation();
  const row = (r: BarRow) => (
    <li key={r.key} className="hx-bar">
      <span className="hx-bar-name">
        <span className="hx-mono">{r.name}</span>
        {r.sub && <span className="hx-sub">{r.sub}</span>}
      </span>
      <Meter pct={r.pct} label={`${label} ${r.name}`} />
      <span className="hx-bar-value hx-mono">
        {r.value}
        {r.flag}
      </span>
    </li>
  );
  return (
    <>
      <ul className="hx-bars">{rows.slice(0, FOLD).map(row)}</ul>
      {rows.length > FOLD && (
        <details className="hx-bars-more">
          <summary>{t("nodes.monitor.showAll", { count: rows.length })}</summary>
          <ul className="hx-bars">{rows.slice(FOLD).map(row)}</ul>
        </details>
      )}
    </>
  );
}

function Card({ id, title, extra, children }: { id: string; title: string; extra?: ReactNode; children: ReactNode }) {
  return (
    <section className="hx-panel hx-hw" aria-labelledby={id}>
      <div className="hx-panel-head">
        <h3 id={id}>{title}</h3>
        {extra && <span className="hx-note">{extra}</span>}
      </div>
      <div className="hx-panel-body">{children}</div>
    </section>
  );
}

function NotProvided() {
  const { t } = useTranslation();
  return <span className="hx-np">{t(NP)}</span>;
}

/** Sort key: highest use first, rows without a number last. */
const byPct = (a: BarRow, b: BarRow) => (b.pct ?? -1) - (a.pct ?? -1);

function Hardware({ sample: s, exhaustion: x }: { sample: Sample; exhaustion: Exhaustion | null }) {
  const { t } = useTranslation();
  const total = s.cpu?.total;
  const cores = (s.cpu?.cores ?? []).filter((c): c is NonNullable<typeof c> => c != null);
  const mem = s.memory;
  const swap = s.swap;
  const memPct = ratio(mem?.usedBytes, mem?.totalBytes);
  const swapPct = ratio(swap?.usedBytes, swap?.totalBytes);
  const fsState = new Map((x?.filesystems ?? []).map((f) => [f?.mountPoint, f]));
  const fsRows: BarRow[] = (s.filesystems ?? [])
    .filter((f): f is NonNullable<typeof f> => f != null)
    .map((f) => {
      const st = fsState.get(f.mountPoint);
      const pct = ratio(f.usedBytes, f.totalBytes);
      const inode = st?.inodeUsedPercent;
      return {
        key: f.mountPoint ?? "",
        name: f.mountPoint ?? "",
        sub: [f.device, f.fsType, inode == null ? null : t("nodes.monitor.inodes", { pct: percent(inode, 0) })]
          .filter(Boolean)
          .join(" · "),
        pct,
        value:
          f.state !== "ok" ? (
            t(`nodes.monitor.fsState.${f.state}`, { defaultValue: f.state ?? "" })
          ) : pct == null ? (
            <NotProvided />
          ) : (
            `${bytes(f.usedBytes)} / ${bytes(f.totalBytes)}`
          ),
        flag: (
          <>
            {f.readOnly && <StatusPill tone="danger">{t("nodes.monitor.readOnly")}</StatusPill>}
            {st?.inodesExhausted && <StatusPill tone="danger">{t("nodes.monitor.inodesFull")}</StatusPill>}
          </>
        ),
      };
    })
    .sort(byPct);
  const nets = (s.networks ?? []).filter((n): n is NonNullable<typeof n> => n != null && n.class !== "loopback");
  const netMax = Math.max(0, ...nets.map((n) => (n.rxBytesPerSecond ?? 0) + (n.txBytesPerSecond ?? 0)));
  const netRows: BarRow[] = nets
    .map((n) => {
      const sum =
        n.rxBytesPerSecond == null && n.txBytesPerSecond == null
          ? null
          : (n.rxBytesPerSecond ?? 0) + (n.txBytesPerSecond ?? 0);
      const cap = n.speedMbps ? (n.speedMbps * 1e6) / 8 : null;
      return {
        key: n.name ?? "",
        name: n.name ?? "",
        sub: [
          t(`nodes.monitor.netClass.${n.class}`, { defaultValue: n.class ?? "" }),
          n.speedMbps ? t("nodes.monitor.linkSpeed", { mbps: n.speedMbps }) : null,
        ]
          .filter(Boolean)
          .join(" · "),
        pct: sum == null ? null : cap ? ratio(sum, cap) : netMax > 0 ? ratio(sum, netMax) : 0,
        value:
          sum == null ? (
            <NotProvided />
          ) : (
            t("nodes.monitor.rxtx", { rx: rate(n.rxBytesPerSecond) ?? "—", tx: rate(n.txBytesPerSecond) ?? "—" })
          ),
      };
    })
    .sort(byPct);
  const temps = (s.temperatures ?? []).filter((v): v is NonNullable<typeof v> => v != null);
  const tempRows: BarRow[] = temps
    .map((v, i) => {
      const limit = v.criticalCelsius ?? v.highCelsius;
      return {
        key: `${v.chip}/${v.label}/${i}`,
        name: v.label || v.chip || "",
        sub: [v.chip, v.source].filter(Boolean).join(" · "),
        pct: ratio(v.celsius, limit),
        value: limit == null ? celsius(v.celsius) : `${celsius(v.celsius)} / ${celsius(limit)}`,
      };
    })
    .sort(byPct);
  const coreRows: BarRow[] = cores
    .map((c) => ({
      key: String(c.id),
      name: t("nodes.monitor.core", { id: c.id }),
      pct: c.usage?.usage ?? null,
      value: percent(c.usage?.usage) ?? <NotProvided />,
    }))
    .sort(byPct);
  const lim: BarRow[] = [
    {
      key: "pids",
      name: t("nodes.monitor.pids"),
      pct: x?.pids?.percent ?? null,
      value: x?.pids?.percent == null ? <NotProvided /> : `${x.pids.used} / ${x.pids.limit}`,
    },
    {
      key: "files",
      name: t("nodes.monitor.fileHandles"),
      pct: x?.fileHandles?.percent ?? null,
      value: x?.fileHandles?.percent == null ? <NotProvided /> : `${x.fileHandles.used} / ${x.fileHandles.limit}`,
    },
  ];
  const mp = x?.memoryPressure;
  return (
    <section aria-labelledby="hx-mon-hw">
      <h2 id="hx-mon-hw" className="hx-mon-h2">
        {t("nodes.monitor.hardware")}
      </h2>
      <div className="hx-hw-grid">
        <Card
          id="hx-hw-cpu"
          title={t("nodes.monitor.cpu")}
          extra={t("nodes.monitor.load", {
            one: num(s.load?.one) ?? "—",
            five: num(s.load?.five) ?? "—",
            fifteen: num(s.load?.fifteen) ?? "—",
          })}
        >
          {total ? (
            <div className="hx-hw-big">
              <span className="hx-hw-num">{percent(total.usage)}</span>
              <Meter pct={total.usage ?? null} label={t("nodes.monitor.cpu")} />
              <span className="hx-sub">
                {t("nodes.monitor.cpuSplit", {
                  user: percent(total.user) ?? "—",
                  system: percent(total.system) ?? "—",
                  iowait: percent(total.iowait) ?? "—",
                  steal: percent(total.steal) ?? "—",
                })}
              </span>
            </div>
          ) : (
            <p className="hx-note">{t("nodes.monitor.firstSample")}</p>
          )}
          {coreRows.length > 0 && <Bars rows={coreRows} label={t("nodes.monitor.cpu")} />}
        </Card>
        <Card id="hx-hw-mem" title={t("nodes.monitor.memory")}>
          <ul className="hx-bars">
            <li className="hx-bar">
              <span className="hx-bar-name">{t("nodes.monitor.ram")}</span>
              <Meter pct={memPct} label={t("nodes.monitor.ram")} />
              <span className="hx-bar-value hx-mono">
                {memPct == null ? <NotProvided /> : `${bytes(mem?.usedBytes)} / ${bytes(mem?.totalBytes)}`}
              </span>
            </li>
            <li className="hx-bar">
              <span className="hx-bar-name">{t("nodes.monitor.swap")}</span>
              <Meter pct={swapPct} label={t("nodes.monitor.swap")} />
              <span className="hx-bar-value hx-mono">
                {swap?.totalBytes === 0 ? (
                  t("nodes.monitor.noSwap")
                ) : swapPct == null ? (
                  <NotProvided />
                ) : (
                  `${bytes(swap?.usedBytes)} / ${bytes(swap?.totalBytes)}`
                )}
              </span>
            </li>
          </ul>
          <dl className="hx-mon-kv">
            <div>
              <dt>{t("nodes.monitor.cached")}</dt>
              <dd className="hx-mono">{bytes(mem?.cachedBytes) ?? <NotProvided />}</dd>
            </div>
            <div>
              <dt>{t("nodes.monitor.available")}</dt>
              <dd className="hx-mono">{bytes(mem?.availableBytes) ?? <NotProvided />}</dd>
            </div>
          </dl>
        </Card>
        <Card id="hx-hw-limits" title={t("nodes.monitor.limits")}>
          <Bars rows={lim} label={t("nodes.monitor.limits")} />
          <dl className="hx-mon-kv">
            <div>
              <dt>{t("nodes.monitor.memPressure")}</dt>
              <dd className="hx-mono">
                {mp?.state === "ok" ? (
                  percent(mp.someAvg60)
                ) : (
                  <span className="hx-np">
                    {t(`nodes.monitor.pressureState.${mp?.state ?? "unreadable"}`, { defaultValue: t(NP) })}
                  </span>
                )}
              </dd>
            </div>
          </dl>
        </Card>
        <Card
          id="hx-hw-fs"
          title={t("nodes.monitor.filesystems")}
          extra={t("nodes.monitor.count", { count: fsRows.length })}
        >
          {fsRows.length ? <Bars rows={fsRows} label={t("nodes.monitor.filesystems")} /> : <NotProvided />}
        </Card>
        <Card
          id="hx-hw-net"
          title={t("nodes.monitor.network")}
          extra={t("nodes.monitor.count", { count: netRows.length })}
        >
          {netRows.length ? <Bars rows={netRows} label={t("nodes.monitor.network")} /> : <NotProvided />}
        </Card>
        <Card
          id="hx-hw-temp"
          title={t("nodes.monitor.temperature")}
          extra={t("nodes.monitor.sensors", { count: tempRows.length })}
        >
          {tempRows.length ? (
            <Bars rows={tempRows} label={t("nodes.monitor.temperature")} />
          ) : (
            <p className="hx-note">{t("nodes.monitor.noSensors")}</p>
          )}
        </Card>
      </div>
    </section>
  );
}

// ── History ─────────────────────────────────────────────────────────────────────────────────────

interface SeriesSpec {
  series: string;
  key?: string;
  name: string;
  color: ChartSeries["color"];
}

/** Reads one chart's series for the range; refetched as often as the range's points change. */
function useSeries(nodeId: string, range: RangeKey, specs: SeriesSpec[], window: { from: number; to: number }) {
  const r = RANGES.find((x) => x.key === range)!;
  const qs = useQueries({
    queries: specs.map((s) => ({
      queryKey: [...monitorKeys.history(nodeId, range, s.series, s.key), window.to] as const,
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        monitorApi.history(
          nodeId,
          {
            series: s.series,
            key: s.key,
            resolution: r.resolution,
            from: new Date(window.from).toISOString(),
            to: new Date(window.to).toISOString(),
          },
          signal,
        ),
      placeholderData: (prev: unknown) => prev as never,
    })),
  });
  return {
    series: specs.map((s, i) => ({ name: s.name, color: s.color, points: qs[i]?.data?.points ?? [] })),
    loading: qs.some((q) => q.isFetching),
    error: qs.some((q) => q.isError) && qs.every((q) => !q.data),
    partial: qs.some((q) => q.data && !q.data.complete),
  };
}

/** The window end, moved forward on each refresh tick of the range (10 s raw, 60 s at 5 minutes). */
function useWindow(range: RangeKey) {
  const r = RANGES.find((x) => x.key === range)!;
  const now = useNow(r.resolution === "raw" ? REFRESH_MS : 60_000);
  const step = r.resolution === "raw" ? REFRESH_MS : 60_000;
  const to = Math.ceil(now.getTime() / step) * step;
  return { from: to - r.seconds * 1000, to };
}

function Charts({ nodeId, latest }: { nodeId: string; latest?: LatestMetrics }) {
  const { t } = useTranslation();
  const [params, setParams] = useSearchParams();
  const range = (RANGES.find((r) => r.key === params.get("range"))?.key ?? "1h") as RangeKey;
  const r = RANGES.find((x) => x.key === range)!;
  const win = useWindow(range);
  const s = latest?.sample;
  const disks = (s?.disks ?? [])
    .filter((d): d is NonNullable<typeof d> => d != null && d.class !== "loop")
    .map((d) => d.name ?? "");
  const nets = (s?.networks ?? [])
    .filter((n): n is NonNullable<typeof n> => n != null && n.class === "physical")
    .map((n) => n.name ?? "");
  const [disk, setDisk] = useState<string | null>(null);
  const [net, setNet] = useState<string | null>(null);
  const d = disk && disks.includes(disk) ? disk : disks[0];
  const n = net && nets.includes(net) ? net : nets[0];
  const temps = useMemo(
    () =>
      (s?.temperatures ?? [])
        .slice(0, 16)
        .filter((v): v is NonNullable<typeof v> => v != null)
        .map((v) => ({
          key: `${v.chip}/${v.label}`,
          name: v.label || v.chip || "",
          r: ratio(v.celsius, v.criticalCelsius ?? v.highCelsius) ?? 0,
        }))
        .sort((a, b) => b.r - a.r)
        .slice(0, 4),
    [s?.temperatures],
  );
  const stale = latest?.stale ?? false;
  const resNote =
    r.resolution === "raw"
      ? t("nodes.monitor.resRaw", { count: latest?.intervalSeconds ?? 10 })
      : t("nodes.monitor.res5m");
  return (
    <section aria-labelledby="hx-mon-hist" className="hx-mon-hist">
      <div className="hx-mon-hist-head">
        <h2 id="hx-mon-hist" className="hx-mon-h2">
          {t("nodes.monitor.history")}
        </h2>
        <div className="hx-seg" role="group" aria-label={t("nodes.monitor.range")}>
          {RANGES.map((x) => (
            <button
              key={x.key}
              type="button"
              aria-pressed={x.key === range}
              onClick={() =>
                setParams(
                  (p) => {
                    p.set("range", x.key);
                    return p;
                  },
                  { replace: true },
                )
              }
            >
              {t(`nodes.monitor.ranges.${x.key}`)}
            </button>
          ))}
        </div>
        <span className="hx-note">{resNote}</span>
      </div>
      <div className="hx-chart-grid2">
        <SeriesChart
          nodeId={nodeId}
          range={range}
          win={win}
          stale={stale}
          title={t("nodes.monitor.cpuUsage")}
          specs={[{ series: "cpu.usage", name: t("nodes.monitor.cpu"), color: "hazel" }]}
          format={(v) => percent(v, 0)}
          yMax={100}
        />
        <SeriesChart
          nodeId={nodeId}
          range={range}
          win={win}
          stale={stale}
          title={t("nodes.monitor.memory")}
          specs={[
            { series: "memory.usedBytes", name: t("nodes.monitor.used"), color: 1 },
            { series: "memory.cachedBytes", name: t("nodes.monitor.cached"), color: 2 },
            { series: "memory.availableBytes", name: t("nodes.monitor.available"), color: 3 },
          ]}
          format={bytes}
          note={t("nodes.monitor.memNote")}
        />
        <SeriesChart
          nodeId={nodeId}
          range={range}
          win={win}
          stale={stale}
          title={t("nodes.monitor.diskIo")}
          picker={
            d ? <KeyPicker label={t("nodes.monitor.device")} value={d} options={disks} onChange={setDisk} /> : null
          }
          specs={
            d
              ? [
                  { series: "disk.readBytesPerSecond", key: d, name: t("nodes.monitor.read"), color: 1 },
                  { series: "disk.writeBytesPerSecond", key: d, name: t("nodes.monitor.write"), color: 2 },
                ]
              : []
          }
          format={rate}
        />
        <SeriesChart
          nodeId={nodeId}
          range={range}
          win={win}
          stale={stale}
          title={t("nodes.monitor.network")}
          picker={n ? <KeyPicker label={t("nodes.monitor.iface")} value={n} options={nets} onChange={setNet} /> : null}
          specs={
            n
              ? [
                  { series: "network.rxBytesPerSecond", key: n, name: t("nodes.monitor.rx"), color: 1 },
                  { series: "network.txBytesPerSecond", key: n, name: t("nodes.monitor.tx"), color: 2 },
                ]
              : []
          }
          format={rate}
          note={t("nodes.monitor.netNote")}
        />
        <SeriesChart
          nodeId={nodeId}
          range={range}
          win={win}
          stale={stale}
          title={t("nodes.monitor.loadAvg")}
          specs={[
            { series: "load.one", name: t("nodes.monitor.load1"), color: 1 },
            { series: "load.five", name: t("nodes.monitor.load5"), color: 2 },
            { series: "load.fifteen", name: t("nodes.monitor.load15"), color: 3 },
          ]}
          format={(v) => num(v, 1)}
        />
        <SeriesChart
          nodeId={nodeId}
          range={range}
          win={win}
          stale={stale}
          title={t("nodes.monitor.temperature")}
          specs={temps.map((v, i) => ({
            series: "temperature.celsius",
            key: v.key,
            name: v.name,
            color: (i + 1) as 1 | 2 | 3 | 4,
          }))}
          format={celsius}
          note={temps.length ? t("nodes.monitor.tempNote") : undefined}
        />
      </div>
    </section>
  );
}

function KeyPicker({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: string[];
  onChange: (v: string) => void;
}) {
  if (options.length < 2) return <span className="hx-note hx-mono">{value}</span>;
  return (
    <label className="hx-mon-pick">
      <span className="hx-visually-hidden">{label}</span>
      <select value={value} onChange={(e) => onChange(e.target.value)}>
        {options.map((o) => (
          <option key={o} value={o}>
            {o}
          </option>
        ))}
      </select>
    </label>
  );
}

function SeriesChart({
  nodeId,
  range,
  win,
  stale,
  title,
  specs,
  picker,
  ...rest
}: {
  nodeId: string;
  range: RangeKey;
  win: { from: number; to: number };
  stale: boolean;
  title: string;
  specs: SeriesSpec[];
  picker?: ReactNode;
  format: (v: number) => string | null;
  yMax?: number;
  note?: string;
}) {
  const h = useSeries(nodeId, range, specs, win);
  return (
    <TimeChart
      extra={picker}
      title={title}
      series={h.series}
      from={win.from}
      to={win.to}
      stale={stale}
      loading={h.loading}
      error={h.error}
      partial={h.partial}
      {...rest}
    />
  );
}
