/** Number formats for host metrics. null means the host did not provide the value; callers show 「未提供」. */

const UNITS = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];

/** 1536 → "1.5 KiB"; binary units like the host tools (free, df -h). */
export function bytes(v: number | null | undefined): string | null {
  if (v == null || !Number.isFinite(v)) return null;
  let n = Math.abs(v);
  let i = 0;
  while (n >= 1024 && i < UNITS.length - 1) {
    n /= 1024;
    i++;
  }
  const digits = i === 0 || n >= 100 ? 0 : 1;
  return `${v < 0 ? "-" : ""}${n.toFixed(digits)} ${UNITS[i]}`;
}

export function rate(v: number | null | undefined): string | null {
  const b = bytes(v);
  return b == null ? null : `${b}/s`;
}

export function percent(v: number | null | undefined, digits = 1): string | null {
  if (v == null || !Number.isFinite(v)) return null;
  return `${v.toFixed(digits)}%`;
}

export function num(v: number | null | undefined, digits = 2): string | null {
  if (v == null || !Number.isFinite(v)) return null;
  return v.toFixed(digits);
}

export function celsius(v: number | null | undefined): string | null {
  if (v == null || !Number.isFinite(v)) return null;
  return `${Math.round(v)}°C`;
}

/** used / total as a percentage; null when either is missing or total is 0. */
export function ratio(used: number | null | undefined, total: number | null | undefined): number | null {
  if (used == null || total == null || total <= 0) return null;
  return (used / total) * 100;
}

/** Meter tone by DESIGN.md: neutral below 70 %, warn from 70 %, danger from 90 %. */
export function meterTone(pct: number | null): "use" | "warn" | "danger" {
  if (pct == null || pct < 70) return "use";
  return pct < 90 ? "warn" : "danger";
}

/** A nice upper bound for a Y axis that starts at 0 (1, 2, 2.5, 5 × 10ⁿ). */
export function niceMax(v: number): number {
  if (!(v > 0)) return 1;
  const p = 10 ** Math.floor(Math.log10(v));
  for (const m of [1, 2, 2.5, 5, 10]) if (v <= m * p) return m * p;
  return 10 * p;
}
