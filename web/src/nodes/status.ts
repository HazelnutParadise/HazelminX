/**
 * How a host's API record reads on screen: the status pill, the certificate pill, what has not reached the host,
 * the list filters and who may change hosts. Pure functions, unit-tested in status.test.ts.
 */
import type { StatusTone } from "../components/StatusPill";
import { activeLocale } from "../i18n";
import { browserTimeZone, formatDateTime } from "../lib/time";
import { CONNECTION, NODE_STATE, type Node, type RoleInfo } from "./api";

/** One status pill: an i18n key under `nodes.status`, its tone, and the time it refers to. */
export interface HostStatus {
  key: string;
  tone: StatusTone;
}

/**
 * The host's main status. Trust states (removed, revoked, identity conflict) win over maintenance, maintenance
 * wins over the channel. Offline and stale only describe the channel; nothing here says the host is off.
 */
export function hostStatus(n: Node): HostStatus {
  if (n.state === NODE_STATE.removed) return { key: "removed", tone: "info" };
  if (n.state === NODE_STATE.revoked)
    return { key: n.revocation?.pending ? "revokedPending" : "revoked", tone: "danger" };
  if (n.state === NODE_STATE.conflict) return { key: "conflict", tone: "danger" };
  if (n.maintenance.enabled) return { key: "maintenance", tone: "info" };
  switch (n.connection.state) {
    case CONNECTION.online:
      return { key: "online", tone: "ok" };
    case CONNECTION.stale:
      return { key: "stale", tone: "warn" };
    case CONNECTION.offline:
      return { key: "offline", tone: "danger" };
    case CONNECTION.never:
      return { key: "never", tone: "warn" };
    default:
      return { key: "unknown", tone: "warn" };
  }
}

/** A host that stopped answering: an active host, not in maintenance, whose channel went stale or down. */
export function isUnreachable(n: Node): boolean {
  return (
    n.state === NODE_STATE.active &&
    !n.maintenance.enabled &&
    (n.connection.state === CONNECTION.offline || n.connection.state === CONNECTION.stale)
  );
}

/** Certificate pill: rotation in progress first (it says what the host still has to pick up). */
export function certificateStatus(n: Node): HostStatus {
  const r = n.certificate.rotation;
  if (r?.error) return { key: "rotationFailed", tone: "danger" };
  if (r) return { key: r.deliveredAt ? "rotationDelivered" : "rotationPending", tone: "info" };
  switch (n.certificate.state) {
    case "valid":
      return { key: "valid", tone: "ok" };
    case "expiring":
      return { key: "expiring", tone: "warn" };
    case "expired":
      return { key: "expired", tone: "danger" };
    default:
      return { key: "unknown", tone: "warn" };
  }
}

/** pendingSync values (nodereg.Sync*) in display order; unknown ones are kept so nothing is hidden. */
const SYNC_ORDER = ["revocation", "removal", "certificate-rotation", "maintenance-recheck"];

export function pendingSync(n: Node): string[] {
  const list = n.pendingSync ?? [];
  return [...list].sort((a, b) => {
    const ia = SYNC_ORDER.indexOf(a);
    const ib = SYNC_ORDER.indexOf(b);
    return (ia === -1 ? SYNC_ORDER.length : ia) - (ib === -1 ? SYNC_ORDER.length : ib);
  });
}

// ── List filters (written into the URL) ─────────────────────────────────────────────────────────

/** Status filter values of the list; `unreachable` is also what the shell's host bar links to. */
export const STATUS_FILTERS = [
  "all",
  "unreachable",
  "online",
  "never",
  "maintenance",
  "revoked",
  "conflict",
  "removed",
] as const;
export type StatusFilter = (typeof STATUS_FILTERS)[number];

export interface HostFilters {
  q: string;
  tag: string;
  group: string;
  status: StatusFilter;
}

export function readFilters(sp: URLSearchParams): HostFilters {
  const status = sp.get("status") ?? "all";
  return {
    q: sp.get("q") ?? "",
    tag: sp.get("tag") ?? "",
    group: sp.get("group") ?? "",
    status: (STATUS_FILTERS as readonly string[]).includes(status) ? (status as StatusFilter) : "all",
  };
}

/** Search params for `f`; defaults are left out so the plain list URL stays /nodes. */
export function writeFilters(f: HostFilters): URLSearchParams {
  const sp = new URLSearchParams();
  if (f.q.trim()) sp.set("q", f.q.trim());
  if (f.tag) sp.set("tag", f.tag);
  if (f.group) sp.set("group", f.group);
  if (f.status !== "all") sp.set("status", f.status);
  return sp;
}

export function hasFilters(f: HostFilters): boolean {
  return Boolean(f.q.trim() || f.tag || f.group || f.status !== "all");
}

/** The API state filter a status filter needs (removed hosts are only listed when asked for). */
export function apiState(status: StatusFilter): string | undefined {
  switch (status) {
    case "removed":
      return NODE_STATE.removed;
    case "revoked":
      return NODE_STATE.revoked;
    case "conflict":
      return NODE_STATE.conflict;
    default:
      return undefined;
  }
}

/** Status filters the API cannot apply, done on the rows it returned. */
export function matchesStatus(n: Node, status: StatusFilter): boolean {
  switch (status) {
    case "all":
    case "removed":
    case "revoked":
    case "conflict":
      return true;
    case "unreachable":
      return isUnreachable(n);
    case "online":
      return n.state === NODE_STATE.active && !n.maintenance.enabled && n.connection.state === CONNECTION.online;
    case "never":
      return n.state === NODE_STATE.active && !n.maintenance.enabled && n.connection.state === CONNECTION.never;
    case "maintenance":
      return n.state === NODE_STATE.active && n.maintenance.enabled;
  }
}

/** Distinct tags and groups across hosts, sorted, for the filter menus. */
export function tagsAndGroups(nodes: Node[]): { tags: string[]; groups: string[] } {
  const tags = new Set<string>();
  const groups = new Set<string>();
  for (const n of nodes) {
    for (const t of n.tags ?? []) tags.add(t);
    if (n.group) groups.add(n.group);
  }
  const cmp = (a: string, b: string) => a.localeCompare(b);
  return { tags: [...tags].sort(cmp), groups: [...groups].sort(cmp) };
}

/** Tags typed as text: split on commas (half or full width) and new lines, trimmed, de-duplicated. */
export function parseTags(text: string): string[] {
  const out: string[] = [];
  for (const part of text.split(/[,，、\n]/)) {
    const t = part.trim();
    if (t && !out.includes(t)) out.push(t);
  }
  return out;
}

// ── Permissions ─────────────────────────────────────────────────────────────────────────────────

export interface Assignment {
  role: string;
  scope: { kind: string; id?: string };
}

/**
 * Whether the signed-in user's roles grant `action` on a host (or, without `nodeId`, on the global scope),
 * the same rule as iam.Authorize. Core checks again on every write; this only decides what to disable.
 */
export function canDo(
  action: string,
  assignments: Assignment[],
  roles: RoleInfo[] | undefined,
  nodeId?: string,
): boolean {
  if (!roles) return false;
  const grants = new Map(roles.map((r) => [r.role, r.actions]));
  return assignments.some((a) => {
    const covers =
      a.scope.kind === "global" || (nodeId !== undefined && a.scope.kind === "node" && a.scope.id === nodeId);
    if (!covers) return false;
    const actions = grants.get(a.role) ?? [];
    return actions.includes("*") || actions.includes(action);
  });
}

export const NODES_WRITE = "nodes:write";

/** One-line explanation under the status pill, only for states whose pill alone could be misread. */
export function hostStatusNote(node: Node, t: (k: string) => string): string | undefined {
  const key = hostStatus(node).key;
  if (key === "offline") return t("nodes.statusNote.offline");
  if (key === "stale") return t("nodes.statusNote.stale");
  if (key === "revokedPending") return t("nodes.statusNote.revokedPending");
  return undefined;
}

/** Absolute local time of an RFC 3339 value, or undefined. */
export function at(iso: string | null | undefined): string | undefined {
  if (!iso) return undefined;
  return formatDateTime(iso, activeLocale(), browserTimeZone()) ?? iso;
}

/** Actions a host offers; each opens one dialog. */
export type NodeAction = "edit" | "maintenanceOn" | "maintenanceOff" | "rotate" | "revoke" | "remove";

/** Which actions make sense for a host in its current state (Core refuses the others with PRECHECK_FAILED). */
export function availableActions(n: Node): NodeAction[] {
  if (n.state === NODE_STATE.removed) return [];
  const out: NodeAction[] = ["edit"];
  if (n.state === NODE_STATE.active) {
    out.push(n.maintenance.enabled ? "maintenanceOff" : "maintenanceOn", "rotate");
  }
  if (n.state === NODE_STATE.active || n.state === NODE_STATE.conflict) out.push("revoke");
  out.push("remove");
  return out;
}
