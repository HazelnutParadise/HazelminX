/** Test data in the shape Core returns (core/internal/nodereg Node, Enrollment). Test-only. */
import type { Enrollment, Node } from "./api";

export const NOW = "2026-11-12T03:42:06Z";

export function node(over: Partial<Node> & { id: string }): Node {
  const base: Node = {
    id: over.id,
    name: over.id,
    tags: [],
    group: "",
    fingerprint: `SHA256:${over.id}-fp`,
    hostname: `${over.id}.lan`,
    machineId: `${over.id}-machine`,
    os: "Debian 13",
    arch: "arm64",
    state: "active",
    connection: {
      state: "online",
      lastContactAt: "2026-11-12T03:42:01Z",
      connectedAt: "2026-11-12T01:00:00Z",
      clockSkewSeconds: 0,
      clockSkewWarning: false,
    },
    certificate: { serial: "1a2b", notAfter: "2027-02-10T00:00:00Z", state: "valid", expiresInDays: 90 },
    maintenance: { enabled: false, reason: "" },
    pendingSync: [],
    components: [],
    createdAt: "2026-11-01T00:00:00Z",
    updatedAt: "2026-11-01T00:00:00Z",
    version: 3,
  } as unknown as Node;
  return { ...base, ...over } as Node;
}

export function enrollment(over: Partial<Enrollment> = {}): Enrollment {
  return {
    id: "enr-1",
    codeId: "7Q4M",
    name: "lab-pi-02",
    tags: ["lab"],
    group: "",
    state: "waiting",
    createdBy: "u1",
    createdAt: NOW,
    expiresAt: "2026-11-12T03:52:06Z",
    matchingNodes: [],
    version: 1,
    ...over,
  } as Enrollment;
}

export const claimedHost = {
  claimedAt: "2026-11-12T03:44:12Z",
  claimExpiresAt: "2026-11-13T03:44:12Z",
  fingerprint: "SHA256:3f0b91de",
  hostname: "lab-pi-02",
  machineId: "5c1e0000a09f",
  os: "Debian 13",
  arch: "arm64",
  agentVersion: "0.1.0",
  sourceAddr: "203.0.113.61",
};

/** GET /roles as Core answers it (iam grants). */
export const ROLES = {
  roles: [
    { role: "owner", actions: ["*"] },
    { role: "platform_admin", actions: ["nodes:read", "nodes:write"] },
    { role: "host_operator", actions: ["nodes:read", "host:read", "host:write"] },
    { role: "auditor", actions: ["nodes:read"] },
  ],
};

export function list(items: Node[]) {
  return {
    items,
    page: {
      limit: 200,
      queryCap: 10000,
      timeoutSeconds: 10,
      total: items.length,
      capped: false,
      timedOut: false,
      sort: "name",
    },
  };
}
