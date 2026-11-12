/**
 * Hosts and enrollment codes (core/internal/api/nodes.go, ticket 019). Bodies come from the generated OpenAPI
 * schema; transport and errors from apiFetch. Core calls hosts "nodes"; the screens call them 主機.
 */
import { apiFetch } from "../api/client";
import type { components } from "../api/schema.gen";

type S = components["schemas"];
export type Node = S["Node"];
export type Enrollment = S["Enrollment"];
export type NodeMatch = S["NodeMatch"];
export type ClaimedHost = S["ClaimedHost"];
export type AffectedJob = S["affectedJob"];
export type PageMeta = S["PageMeta"];
export type RoleInfo = S["roleInfo"];

/** Node.state values (nodereg.State*). */
export const NODE_STATE = {
  active: "active",
  revoked: "revoked",
  conflict: "identity_conflict",
  removed: "removed",
} as const;

/** Node.connection.state values (nodereg.Conn*). offline only means the channel is down. */
export const CONNECTION = {
  online: "online",
  stale: "stale",
  offline: "offline",
  never: "never_connected",
} as const;

/** Enrollment.state values (nodereg.Enrollment*). */
export const ENROLLMENT = {
  waiting: "waiting",
  expired: "expired",
  revoked: "revoked",
  pending: "pending_approval",
  approved: "approved",
  rejected: "rejected",
  approvalExpired: "approval_expired",
} as const;

/** The four removal modes of POST /nodes/{id}/remove; only unmanage is available today. */
export const REMOVAL_MODES = ["unmanage", "remove-agent", "remove-cluster-node", "delete-workloads"] as const;
export type RemovalMode = (typeof REMOVAL_MODES)[number];

/** Code validity Core accepts (nodereg.MinCodeTTL … MaxCodeTTL), default 10 minutes. */
export const CODE_TTL = { min: 60, max: 3600, default: 600 } as const;

/** Default agent listener port (core/internal/config AgentListenAddr 0.0.0.0:9801). */
export const DEFAULT_AGENT_PORT = 9801;

const BASE = "/management/v1";
const enc = encodeURIComponent;

export interface NodeQuery {
  q?: string;
  tag?: string;
  group?: string;
  /** API state filter; without it removed hosts are left out. */
  state?: string;
}

function query(params: Record<string, string | undefined>): string {
  const sp = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v) sp.set(k, v);
  const s = sp.toString();
  return s ? `?${s}` : "";
}

/** Page size used to read whole lists (the API's maximum). */
const PAGE = 200;

export interface NodeList {
  items: Node[];
  /** False when the API stopped before the end (query cap or timeout). */
  complete: boolean;
  page: PageMeta;
}

/** Reads every page of the host list for `q` (hosts are few; the API caps the walk). */
export async function listAllNodes(q: NodeQuery = {}, signal?: AbortSignal): Promise<NodeList> {
  const items: Node[] = [];
  let cursor: string | undefined;
  for (;;) {
    const r = await apiFetch<S["ListResponse_nodereg.Node"]>(
      `${BASE}/nodes${query({ ...q, limit: String(PAGE), cursor } as Record<string, string | undefined>)}`,
      { signal },
    );
    items.push(...(r.items ?? []));
    if (!r.page.nextCursor || r.page.capped || r.page.timedOut) {
      return { items, complete: !r.page.capped && !r.page.timedOut, page: r.page };
    }
    cursor = r.page.nextCursor;
  }
}

export const nodesApi = {
  list: listAllNodes,
  get: (id: string) => apiFetch<S["nodeResponse"]>(`${BASE}/nodes/${enc(id)}`),
  update: (id: string, version: number, patch: Partial<S["NodePatch"]>) =>
    apiFetch<S["nodeResponse"]>(`${BASE}/nodes/${enc(id)}`, {
      method: "PATCH",
      body: patch,
      headers: { "If-Match": `"v${version}"` },
    }),
  revoke: (id: string, reason: string) =>
    apiFetch<S["nodeResponse"]>(`${BASE}/nodes/${enc(id)}/revoke`, { method: "POST", body: { reason } }),
  rotate: (id: string) =>
    apiFetch<S["nodeResponse"]>(`${BASE}/nodes/${enc(id)}/rotate-certificate`, { method: "POST" }),
  maintenance: (id: string, enabled: boolean, reason: string) =>
    apiFetch<S["maintenanceResponse"]>(`${BASE}/nodes/${enc(id)}/maintenance`, {
      method: "PUT",
      body: { enabled, reason },
    }),
  recheck: (id: string) => apiFetch<S["nodeResponse"]>(`${BASE}/nodes/${enc(id)}/recheck`, { method: "POST" }),
  remove: (id: string, mode: RemovalMode) =>
    apiFetch<S["nodeResponse"]>(`${BASE}/nodes/${enc(id)}/remove`, { method: "POST", body: { mode } }),

  createEnrollment: (body: Partial<S["createEnrollmentRequest"]>) =>
    apiFetch<S["enrollmentCreatedResponse"]>(`${BASE}/node-enrollments`, { method: "POST", body }),
  enrollment: (id: string) => apiFetch<S["enrollmentResponse"]>(`${BASE}/node-enrollments/${enc(id)}`),
  /** Enrollments in one state, newest first (one page of the API's maximum size). */
  enrollments: (state: string, signal?: AbortSignal) =>
    apiFetch<S["ListResponse_nodereg.Enrollment"]>(`${BASE}/node-enrollments${query({ state, limit: String(PAGE) })}`, {
      signal,
    }),
  withdrawEnrollment: (id: string) =>
    apiFetch<S["enrollmentResponse"]>(`${BASE}/node-enrollments/${enc(id)}`, { method: "DELETE" }),
  approve: (id: string, body: Partial<S["approveEnrollmentRequest"]>) =>
    apiFetch<S["nodeResponse"]>(`${BASE}/node-enrollments/${enc(id)}/approve`, { method: "POST", body }),
  reject: (id: string) =>
    apiFetch<S["enrollmentResponse"]>(`${BASE}/node-enrollments/${enc(id)}/reject`, { method: "POST" }),

  roles: () => apiFetch<S["rolesResponse"]>(`${BASE}/roles`),
};

export const nodeKeys = {
  all: ["nodes"] as const,
  list: (q: NodeQuery) => ["nodes", "list", q] as const,
  one: (id: string) => ["nodes", "one", id] as const,
  enrollment: (id: string) => ["node-enrollments", id] as const,
  enrollments: (state: string) => ["node-enrollments", "list", state] as const,
  roles: ["roles"] as const,
};
