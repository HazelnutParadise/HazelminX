/** Pure readings of a storage view, kept apart from the screen so each rule has its own test. */
import { CATEGORIES, type Category, type FSUsage, type MountConfig, type MountView, type Rule } from "./api";

/** References that keep pointing at the same disk after it moves to another port (protocol storage.rs). */
export function isDeviceName(reference: string | null | undefined): boolean {
  return reference === "device";
}

/** Where an entry is written: `/etc/fstab:9`, or the unit file of a systemd mount. */
export function configLocation(c: MountConfig): string {
  return c.line != null ? `${c.file}:${c.line}` : (c.file ?? "");
}

/** The configuration entries a mount, swap area or problem points at; indexes outside the list are skipped. */
export function configsAt(
  all: MountConfig[] | null | undefined,
  idx: (number | null)[] | null | undefined,
): MountConfig[] {
  const list = all ?? [];
  return (idx ?? []).flatMap((i) => (i != null && list[i] ? [list[i]] : []));
}

/**
 * What happens at boot: the first entry's boot mode (auto, onDemand, noauto), or `manual` when nothing configures
 * the mount, so it was mounted by hand and will not come back after a reboot.
 */
export type BootMode = "auto" | "onDemand" | "noauto" | "manual";
export function bootMode(configs: MountConfig[]): BootMode {
  const b = configs[0]?.boot;
  return b === "auto" || b === "onDemand" || b === "noauto" ? b : configs.length ? "auto" : "manual";
}

/** Which scope the applied rule came from. */
export type RuleScope = "builtIn" | "default" | "node" | "mountPoint" | "nodeMountPoint";
export function ruleScope(r: Rule | null | undefined): RuleScope {
  if (!r || r.builtIn) return "builtIn";
  if (r.nodeId && r.mountPoint) return "nodeMountPoint";
  if (r.nodeId) return "node";
  if (r.mountPoint) return "mountPoint";
  return "default";
}

export interface Segment {
  category: Category;
  bytes: number;
}

/**
 * The usage bar of a filesystem. Only a complete measurement draws a bar: its categories add up to the used space
 * and the rest of the filesystem is available. A partial one lists lower bounds without a bar, so nothing looks
 * complete; an unmeasured one has no breakdown.
 */
export interface UsageBreakdown {
  state: "none" | "unmeasured" | "partial" | "complete";
  segments: Segment[];
  /** Bar scale: used plus available; null when the bar is not drawn. */
  scale: number | null;
  available: number | null;
}

export function breakdown(usage: FSUsage | null | undefined, available: number | null | undefined): UsageBreakdown {
  if (!usage) return { state: "none", segments: [], scale: null, available: null };
  const state = usage.state === "complete" || usage.state === "partial" ? usage.state : "unmeasured";
  const by = new Map((usage.categories ?? []).map((c) => [c?.category, c?.bytes ?? 0]));
  const segments = CATEGORIES.flatMap((category) => {
    const b = by.get(category) ?? 0;
    // A partial measurement never lists other (hoststorage.fsUsage); drop it if a report ever did.
    if (b <= 0 || (category === "other" && state !== "complete")) return [];
    return [{ category, bytes: b }];
  });
  if (state !== "complete")
    return { state, segments: state === "partial" ? segments : [], scale: null, available: null };
  const used = segments.reduce((s, x) => s + x.bytes, 0);
  const free = available ?? 0;
  return { state, segments, scale: used + free > 0 ? used + free : null, available: available ?? null };
}

/** Days until the filesystem is full at the 7-day trend; null when it is not growing or a value is missing. */
export function daysToFull(
  available: number | null | undefined,
  growthPerDay: number | null | undefined,
): number | null {
  if (available == null || growthPerDay == null || !(growthPerDay > 0)) return null;
  return Math.floor(available / growthPerDay);
}

/** Percentage of `part` in `whole`; null when either is missing or whole is 0. */
export function pct(part: number | null | undefined, whole: number | null | undefined): number | null {
  if (part == null || whole == null || whole <= 0) return null;
  return (part / whole) * 100;
}

/** Local filesystems get a usage card; network and virtual ones are not walked (protocol storage.rs). */
export function hasUsageCard(m: MountView): boolean {
  return m.class === "local";
}
