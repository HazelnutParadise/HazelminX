import { useId, useMemo, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { activeLocale } from "../../i18n";
import { browserTimeZone, formatDateTime } from "../../lib/time";
import type { Point } from "./api";
import { niceMax } from "./format";

export interface ChartSeries {
  name: string;
  /** Index into DESIGN.md series-1..4; a single series uses the hazel colour. */
  color: 1 | 2 | 3 | 4 | "hazel";
  points: Point[];
}

interface Props {
  title: string;
  series: ChartSeries[];
  /** Window shown, ms since epoch. */
  from: number;
  to: number;
  format: (v: number) => string | null;
  /** Fixed top of the Y axis (100 for percent); otherwise from the data. */
  yMax?: number;
  /** Smallest top from the data, so near-zero values do not give an axis of identical rounded labels. */
  minTop?: number;
  /** The newest sample is older than three intervals: its point is drawn hollow (DESIGN.md 過期尾段). */
  stale?: boolean;
  /** Under the chart: units and what the series cover. */
  note?: string;
  loading?: boolean;
  error?: boolean;
  /** Core stopped reading before the end of the window. */
  partial?: boolean;
  /** In the head next to the title, e.g. the device picker. */
  extra?: ReactNode;
}

const W = 1000;
const H = 100;

const time = (ms: number, withDate: boolean) =>
  formatDateTime(new Date(ms).toISOString(), activeLocale(), browserTimeZone(), withDate ? "datetime" : "time") ?? "";

/**
 * A time series chart by DESIGN.md「時間序列圖」: the plot stretches in a 1000×100 box with non-scaling lines; axis
 * labels, the hover read-out and the stale marker are HTML on top, so text keeps its size at any width. A point
 * with gapBefore starts a new line: missing stretches stay empty, never zero.
 */
export function TimeChart({
  title,
  series,
  from,
  to,
  format,
  yMax,
  stale,
  note,
  loading,
  error,
  partial,
  extra,
  minTop = 0,
}: Props) {
  const { t } = useTranslation();
  const id = useId();
  const [hover, setHover] = useState<number | null>(null);
  const [table, setTable] = useState(false);
  const span = Math.max(1, to - from);
  const top = useMemo(() => {
    if (yMax != null) return yMax;
    let m = 0;
    for (const s of series) for (const p of s.points) m = Math.max(m, p.value);
    return niceMax(Math.max(m, minTop));
  }, [series, yMax]);
  const x = (ms: number) => ((ms - from) / span) * W;
  const y = (v: number) => H - (Math.min(Math.max(v, 0), top) / top) * H;
  const withDate = span > 24 * 3600_000;
  const empty = series.every((s) => s.points.length === 0);
  const single = series.length === 1;

  const paths = series.map((s) => {
    let d = "";
    let area = "";
    let seg: string[] = [];
    let segStart = 0;
    const flush = (endX: number) => {
      if (seg.length === 0) return;
      d += `M${seg.join("L")}`;
      area += `M${segStart.toFixed(1)},${H}L${seg.join("L")}L${endX.toFixed(1)},${H}Z`;
      seg = [];
    };
    let lastX = 0;
    for (const p of s.points) {
      const px = x(Date.parse(p.at));
      if (p.gapBefore) flush(lastX);
      if (seg.length === 0) segStart = px;
      seg.push(`${px.toFixed(1)},${y(p.value).toFixed(2)}`);
      lastX = px;
    }
    flush(lastX);
    return { d, area };
  });

  /** Hover: the nearest time in the first series that has points, and each series' point within 1.5 steps. */
  const read = useMemo(() => {
    if (hover == null) return null;
    const at = from + hover * span;
    const values = series.map((s) => {
      let best: Point | null = null;
      for (const p of s.points) {
        const d = Math.abs(Date.parse(p.at) - at);
        if (d <= p.resolutionSeconds * 1500 && (!best || d < Math.abs(Date.parse(best.at) - at))) best = p;
      }
      return best;
    });
    const ref = values.find((v) => v != null);
    if (!ref) return null;
    return { at: Date.parse(ref.at), values };
  }, [hover, series, from, span]);

  const ticks = [0, 0.25, 0.5, 0.75, 1];
  const xTicks = [0, 0.25, 0.5, 0.75, 1].map((f) => from + f * span);
  const last = series.map((s) => s.points[s.points.length - 1]);

  return (
    <figure className="hx-chart" aria-labelledby={`${id}-t`}>
      <div className="hx-chart-head">
        <h3 id={`${id}-t`}>{title}</h3>
        {extra}
        {series.length > 1 && (
          <ul className="hx-legend" aria-label={t("nodes.monitor.legend")}>
            {series.map((s) => (
              <li key={s.name}>
                <span className={`hx-swatch hx-c-${s.color}`} aria-hidden="true" />
                {s.name}
              </li>
            ))}
          </ul>
        )}
      </div>
      <div
        className={`hx-chart-plot${loading ? " is-loading" : ""}`}
        role="img"
        aria-label={t("nodes.monitor.chartLabel", { title, from: time(from, true), to: time(to, true) })}
        onPointerMove={(e) => {
          const r = e.currentTarget.getBoundingClientRect();
          setHover(Math.min(1, Math.max(0, (e.clientX - r.left) / r.width)));
        }}
        onPointerLeave={() => setHover(null)}
      >
        <div className="hx-chart-grid" aria-hidden="true">
          {ticks.map((f) => (
            <span key={f} className="hx-chart-ytick" style={{ bottom: `${f * 100}%` }}>
              <span className="hx-chart-ylabel">{format(top * f)}</span>
            </span>
          ))}
        </div>
        <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" aria-hidden="true">
          {paths.map((p, i) => (
            <g key={series[i]!.name} className={`hx-c-${series[i]!.color}`}>
              {single && <path d={p.area} className="hx-chart-area" />}
              <path d={p.d} className="hx-chart-line" vectorEffect="non-scaling-stroke" />
            </g>
          ))}
        </svg>
        {stale &&
          last.map(
            (p, i) =>
              p && (
                <span
                  key={series[i]!.name}
                  className={`hx-chart-stale hx-c-${series[i]!.color}`}
                  style={{ left: `${(x(Date.parse(p.at)) / W) * 100}%`, top: `${(y(p.value) / H) * 100}%` }}
                  aria-hidden="true"
                />
              ),
          )}
        {read && (
          <>
            <span className="hx-chart-cross" style={{ left: `${(x(read.at) / W) * 100}%` }} aria-hidden="true" />
            <div
              className={`hx-chart-tip${(read.at - from) / span > 0.5 ? " is-left" : ""}`}
              style={{ left: `${(x(read.at) / W) * 100}%` }}
              aria-hidden="true"
            >
              <div className="hx-chart-tip-time">{time(read.at, withDate)}</div>
              {series.map((s, i) => (
                <div key={s.name} className="hx-chart-tip-row">
                  <span className={`hx-swatch hx-c-${s.color}`} />
                  <span>{s.name}</span>
                  <span className="hx-mono">
                    {read.values[i] ? format(read.values[i]!.value) : t("nodes.monitor.noPoint")}
                  </span>
                </div>
              ))}
            </div>
          </>
        )}
        {!loading && (empty || error) && (
          <p className="hx-chart-empty">{error ? t("nodes.monitor.historyFailed") : t("nodes.monitor.noHistory")}</p>
        )}
      </div>
      <div className="hx-chart-x" aria-hidden="true">
        {xTicks.map((ms, i) => (
          <span key={i}>{time(ms, withDate)}</span>
        ))}
      </div>
      <div className="hx-chart-foot">
        {note && <span>{note}</span>}
        {partial && <span>{t("nodes.monitor.partial")}</span>}
        {!empty && (
          <button type="button" className="hx-chart-tablebtn" aria-expanded={table} onClick={() => setTable((v) => !v)}>
            {table ? t("nodes.monitor.hideTable") : t("nodes.monitor.showTable")}
          </button>
        )}
      </div>
      {table && !empty && <ChartTable title={title} series={series} format={format} withDate={withDate} />}
    </figure>
  );
}

/** Every point as a row (DESIGN.md「每張圖都有表格檢視入口」); a time with no point in a series shows 「—」. */
function ChartTable({
  title,
  series,
  format,
  withDate,
}: {
  title: string;
  series: ChartSeries[];
  format: (v: number) => string | null;
  withDate: boolean;
}) {
  const { t } = useTranslation();
  const rows = useMemo(() => {
    const byTime = new Map<string, (Point | undefined)[]>();
    series.forEach((s, i) => {
      for (const p of s.points) {
        const row = byTime.get(p.at) ?? new Array<Point | undefined>(series.length);
        row[i] = p;
        byTime.set(p.at, row);
      }
    });
    return [...byTime.entries()].sort((a, b) => Date.parse(b[0]) - Date.parse(a[0]));
  }, [series]);
  return (
    <div className="hx-table-wrap hx-chart-table">
      <table className="hx-table">
        <caption className="hx-visually-hidden">{title}</caption>
        <thead>
          <tr>
            <th scope="col">{t("nodes.monitor.time")}</th>
            {series.map((s) => (
              <th key={s.name} scope="col">
                {s.name}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map(([at, ps]) => (
            <tr key={at}>
              <td className="hx-mono">
                {time(Date.parse(at), withDate)}
                {ps.some((p) => p?.gapBefore) && <span className="hx-sub"> {t("nodes.monitor.afterGap")}</span>}
              </td>
              {series.map((s, i) => (
                <td key={s.name} className="hx-mono">
                  {ps[i] ? format(ps[i]!.value) : "—"}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
