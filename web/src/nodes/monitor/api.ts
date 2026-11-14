/**
 * Host facts and metrics (core/internal/api/host_metrics.go, ticket 020.1). Core sends null for every value the
 * host did not provide, but its OpenAPI document does not mark pointer fields nullable, so the generated types
 * are widened here with `Maybe` and every reader handles null.
 */
import { apiFetch } from "../../api/client";
import type { components } from "../../api/schema.gen";

type S = components["schemas"];

/** T with every field, at any depth, possibly null (what Core really sends for values it could not read). */
export type Maybe<T> = T extends (infer U)[]
  ? Maybe<U>[]
  : T extends object
    ? { [K in keyof T]: Maybe<T[K]> | null }
    : T | null;

export type HostFacts = Maybe<S["hostFactsResponse"]>;
export type LatestMetrics = Maybe<S["latestMetricsResponse"]>;
export type Sample = NonNullable<LatestMetrics["sample"]>;
export type Exhaustion = NonNullable<LatestMetrics["exhaustion"]>;
export type Point = S["Point"];

/** History resolutions Core keeps (hostmetrics.ResolutionRaw, Resolution5m). */
export type Resolution = "raw" | "5m";

/** The board's time ranges (DESIGN.md「時間範圍」) and the resolution each reads: raw is kept 48 h, 5m 90 days. */
export const RANGES = [
  { key: "live", seconds: 5 * 60, resolution: "raw" },
  { key: "1h", seconds: 3600, resolution: "raw" },
  { key: "12h", seconds: 12 * 3600, resolution: "5m" },
  { key: "24h", seconds: 24 * 3600, resolution: "5m" },
  { key: "7d", seconds: 7 * 86400, resolution: "5m" },
  { key: "30d", seconds: 30 * 86400, resolution: "5m" },
] as const satisfies readonly { key: string; seconds: number; resolution: Resolution }[];
export type RangeKey = (typeof RANGES)[number]["key"];

export interface HistoryQuery {
  series: string;
  key?: string;
  resolution: Resolution;
  from: string;
  to: string;
}

const BASE = "/management/v1";
const enc = encodeURIComponent;
/** The history list's maximum page size (metricsHistoryList.MaxLimit). */
const PAGE = 2000;

export interface History {
  points: Point[];
  /** False when Core stopped before the end (query cap or timeout). */
  complete: boolean;
}

/** Every point of one series in [from, to), oldest first, read page by page. */
export async function readHistory(nodeId: string, q: HistoryQuery, signal?: AbortSignal): Promise<History> {
  const points: Point[] = [];
  let cursor: string | undefined;
  for (;;) {
    const sp = new URLSearchParams({ series: q.series, resolution: q.resolution, from: q.from, to: q.to });
    sp.set("limit", String(PAGE));
    if (q.key) sp.set("key", q.key);
    if (cursor) sp.set("cursor", cursor);
    const r = await apiFetch<S["ListResponse_hostmetrics.Point"]>(
      `${BASE}/nodes/${enc(nodeId)}/metrics/history?${sp.toString()}`,
      { signal },
    );
    points.push(...(r.items ?? []));
    if (!r.page.nextCursor || r.page.capped || r.page.timedOut) {
      return { points, complete: !r.page.capped && !r.page.timedOut };
    }
    cursor = r.page.nextCursor;
  }
}

export const monitorApi = {
  facts: (id: string) => apiFetch<HostFacts>(`${BASE}/nodes/${enc(id)}/host`),
  latest: (id: string) => apiFetch<LatestMetrics>(`${BASE}/nodes/${enc(id)}/metrics/latest`),
  history: readHistory,
};

export const monitorKeys = {
  facts: (id: string) => ["nodes", "host", id] as const,
  latest: (id: string) => ["nodes", "metrics", "latest", id] as const,
  history: (id: string, range: RangeKey, series: string, key = "") =>
    ["nodes", "metrics", "history", id, range, series, key] as const,
};
