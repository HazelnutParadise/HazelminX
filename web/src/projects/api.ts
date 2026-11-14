/**
 * Projects, environments and their Compose unit (core/internal/api/projects.go, ticket 044.1), the unit's host
 * check and deployments (core/internal/api/compose_deploy.go, ticket 044.2), and the change plan a deployment
 * goes through (core/internal/api/change_plans.go, ticket 016.1). Core leaves out or nulls optional fields its
 * OpenAPI document does not mark, so read bodies are widened with Maybe and every reader handles null.
 */
import { apiFetch } from "../api/client";
import type { components } from "../api/schema.gen";
import type { Maybe } from "../nodes/monitor/api";

type S = components["schemas"];
export type Project = S["Project"];
export type Environment = S["projects.Environment"];
export type Unit = Maybe<S["projects.Unit"]>;
export type UnitFile = NonNullable<Unit["files"]>[number];
export type Analysis = Maybe<S["Analysis"]>;
export type Finding = Maybe<S["Finding"]>;
export type Preview = Maybe<S["Preview"]>;
export type Plan = Maybe<S["Plan"]>;
export type PlanCheck = Maybe<S["plans.Check"]>;
export type Job = Maybe<S["Job"]>;
export type Step = Maybe<S["Step"]>;
export type Deployment = Maybe<S["Deployment"]>;
export type EngineEnvironment = Maybe<S["Environment"]>;
export type UnitInput = S["UnitInput"];
export type FileInput = S["FileInput"];

/** Finding.severity (projects.Severity*), in the order findings are listed. */
export const SEVERITIES = ["error", "high-risk", "warning", "kept"] as const;

/** Preview.target.state (projects.Target*). */
export const TARGET_READY = "ready";

/** Plan.state (plans.State*). */
export const PLAN = {
  awaiting: "awaiting_approval",
  approved: "approved",
  rejected: "rejected",
  withdrawn: "withdrawn",
  expired: "expired",
  invalidated: "invalidated",
  executed: "executed",
} as const;

/** Job states that will not change any more (jobs.State*). */
export const JOB_ENDED = ["succeeded", "failed", "rolled_back", "cancelled", "needs_attention", "unknown"];

/** The change-plan kind of a Compose deployment (composedeploy.KindDeploy). */
export const DEPLOY_KIND = "compose.deploy";

/** Engine environments a unit can deploy to: the host's root-owned engines (composedeploy.RootfulEnvironments). */
export const ROOTFUL_ENVIRONMENTS = ["env:/var/run/docker.sock", "env:/run/docker.sock", "env:/run/podman/podman.sock"];

/** Core's limits on a unit's files (projects.MaxFiles, MaxFileBytes, MaxUnitBytes). */
export const LIMITS = { files: 200, fileBytes: 256 << 10, unitBytes: 768 << 10 } as const;

const BASE = "/management/v1";
const enc = encodeURIComponent;
const ifMatch = (version: number | null | undefined) => ({ "If-Match": `"v${version ?? 0}"` });
const env = (p: string, e: string) => `${BASE}/projects/${enc(p)}/environments/${enc(e)}`;

/** A fresh request id for calls Core deduplicates (host checks, plan execution). */
export function requestId(): string {
  return crypto.randomUUID();
}

export const projectsApi = {
  list: (signal?: AbortSignal) =>
    apiFetch<S["ListResponse_projects.Project"]>(`${BASE}/projects?limit=200`, { signal }),
  get: (id: string) => apiFetch<S["projectResponse"]>(`${BASE}/projects/${enc(id)}`),
  create: (body: S["ProjectInput"]) => apiFetch<S["projectResponse"]>(`${BASE}/projects`, { method: "POST", body }),
  environments: (id: string, signal?: AbortSignal) =>
    apiFetch<S["ListResponse_projects.Environment"]>(`${BASE}/projects/${enc(id)}/environments?limit=200`, { signal }),
  createEnvironment: (id: string, body: S["EnvironmentInput"]) =>
    apiFetch<S["projectEnvironmentResponse"]>(`${BASE}/projects/${enc(id)}/environments`, { method: "POST", body }),

  unit: (p: string, e: string) => apiFetch<{ unit: Unit; analysis: Analysis }>(`${env(p, e)}/compose`),
  importUnit: (p: string, e: string, body: UnitInput) =>
    apiFetch<{ unit: Unit; analysis: Analysis }>(`${env(p, e)}/compose`, { method: "POST", body }),
  replaceUnit: (p: string, e: string, version: number, body: UnitInput) =>
    apiFetch<{ unit: Unit; analysis: Analysis }>(`${env(p, e)}/compose`, {
      method: "PUT",
      body,
      headers: ifMatch(version),
    }),
  check: (p: string, e: string, body: UnitInput) =>
    apiFetch<{ analysis: Analysis }>(`${env(p, e)}/compose/check`, { method: "POST", body }),
  preview: (p: string, e: string) => apiFetch<{ preview: Preview }>(`${env(p, e)}/compose/preview`),
  hostCheck: (p: string, e: string, rid: string) =>
    apiFetch<S["JobAccepted"]>(`${env(p, e)}/compose/host-checks`, { method: "POST", body: { requestId: rid } }),
  deployments: (p: string, e: string) =>
    apiFetch<{ deployments: Deployment[] | null }>(`${env(p, e)}/compose/deployments`),

  engines: (nodeId: string) => apiFetch<Maybe<S["hostEnginesResponse"]>>(`${BASE}/nodes/${enc(nodeId)}/engines`),
};

export const plansApi = {
  createDeploy: (projectId: string, environmentId: string) =>
    apiFetch<{ plan: Plan }>(`${BASE}/change-plans`, {
      method: "POST",
      body: { kind: DEPLOY_KIND, params: { projectId, environmentId } },
    }),
  get: (id: string) => apiFetch<{ plan: Plan }>(`${BASE}/change-plans/${enc(id)}`),
  approve: (id: string, digest: string) =>
    apiFetch<{ plan: Plan }>(`${BASE}/change-plans/${enc(id)}/approve`, { method: "POST", body: { digest } }),
  reject: (id: string, reason: string) =>
    apiFetch<{ plan: Plan }>(`${BASE}/change-plans/${enc(id)}/reject`, { method: "POST", body: { reason } }),
  withdraw: (id: string, reason: string) =>
    apiFetch<{ plan: Plan }>(`${BASE}/change-plans/${enc(id)}/withdraw`, { method: "POST", body: { reason } }),
  execute: (id: string, digest: string, rid: string) =>
    apiFetch<S["JobAccepted"]>(`${BASE}/change-plans/${enc(id)}/execute`, {
      method: "POST",
      body: { digest, requestId: rid },
    }),
};

export const jobsApi = {
  get: (id: string) => apiFetch<{ job: Job }>(`${BASE}/jobs/${enc(id)}`),
};

export const projectKeys = {
  all: ["projects"] as const,
  list: ["projects", "list"] as const,
  one: (id: string) => ["projects", "one", id] as const,
  environments: (id: string) => ["projects", "environments", id] as const,
  unit: (p: string, e: string) => ["projects", "unit", p, e] as const,
  preview: (p: string, e: string) => ["projects", "preview", p, e] as const,
  deployments: (p: string, e: string) => ["projects", "deployments", p, e] as const,
  engines: (nodeId: string) => ["nodes", "engines", nodeId] as const,
  plan: (id: string) => ["change-plans", id] as const,
  job: (id: string) => ["jobs", id] as const,
};
