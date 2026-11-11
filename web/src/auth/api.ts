/**
 * Sign-in, setup, second factors and own sessions (core/internal/api/iam_routes.go, mfa_routes.go).
 * Bodies come from the generated OpenAPI schema; transport and errors from apiFetch.
 */
import { apiFetch, ApiError } from "../api/client";
import type { components } from "../api/schema.gen";

type S = components["schemas"];
export type User = S["User"];
export type Assignment = S["Assignment"];
export type LoginResponse = S["loginResponse"];
export type SessionResponse = S["sessionResponse"];
export type FactorSummary = S["FactorSummary"];
export type PasskeyInfo = S["PasskeyInfo"];
export type TotpEnrollment = S["TOTPEnrollment"];
export type EnrollmentResult = S["EnrollmentResult"];
export type SessionInfo = S["SessionInfo"];
export type SessionSettings = S["SessionSettings"];

/** Second-factor methods as Core names them (iam.MethodTOTP, MethodPasskey, MethodRecovery). */
export type FactorMethod = "totp" | "passkey" | "recovery-code";

/** Proof sent with the second login step or a reauthentication. */
export interface FactorProof {
  method: FactorMethod;
  code?: string;
  credential?: unknown;
}

const BASE = "/management/v1";

export const authApi = {
  setupStatus: () => apiFetch<S["setupStatusResponse"]>(`${BASE}/setup`),
  setup: (body: S["setupRequest"]) => apiFetch<LoginResponse>(`${BASE}/setup`, { method: "POST", body }),
  login: (body: S["loginRequest"]) => apiFetch<LoginResponse>(`${BASE}/auth/login`, { method: "POST", body }),
  loginPasskeyOptions: (challengeToken: string) =>
    apiFetch<unknown>(`${BASE}/auth/login/passkey-options`, { method: "POST", body: { challengeToken } }),
  secondFactor: (challengeToken: string, proof: FactorProof) =>
    apiFetch<LoginResponse>(`${BASE}/auth/login/second-factor`, { method: "POST", body: { challengeToken, ...proof } }),
  session: () => apiFetch<SessionResponse>(`${BASE}/auth/session`),
  logout: () => apiFetch<void>(`${BASE}/auth/logout`, { method: "POST" }),
  reauthenticate: (password: string, proof?: FactorProof) =>
    apiFetch<void>(`${BASE}/auth/reauthenticate`, { method: "POST", body: { password, ...proof } }),
  reauthPasskeyOptions: () => apiFetch<unknown>(`${BASE}/auth/reauthenticate/passkey-options`, { method: "POST" }),

  factors: () => apiFetch<FactorSummary>(`${BASE}/me/second-factors`),
  beginTotp: () => apiFetch<TotpEnrollment>(`${BASE}/me/second-factors/totp`, { method: "POST" }),
  confirmTotp: (code: string) =>
    apiFetch<EnrollmentResult>(`${BASE}/me/second-factors/totp/confirm`, { method: "POST", body: { code } }),
  removeTotp: () => apiFetch<void>(`${BASE}/me/second-factors/totp`, { method: "DELETE" }),
  passkeyOptions: () => apiFetch<unknown>(`${BASE}/me/second-factors/passkeys/options`, { method: "POST" }),
  registerPasskey: (name: string, credential: unknown) =>
    apiFetch<EnrollmentResult>(`${BASE}/me/second-factors/passkeys`, { method: "POST", body: { name, credential } }),
  removePasskey: (id: string) =>
    apiFetch<void>(`${BASE}/me/second-factors/passkeys/${encodeURIComponent(id)}`, { method: "DELETE" }),
  regenerateRecoveryCodes: () => apiFetch<S["recoveryCodesResponse"]>(`${BASE}/me/recovery-codes`, { method: "POST" }),

  sessions: () => apiFetch<S["sessionsResponse"]>(`${BASE}/me/sessions`),
  revokeSession: (id: string) => apiFetch<void>(`${BASE}/me/sessions/${encodeURIComponent(id)}`, { method: "DELETE" }),
  revokeOtherSessions: () => apiFetch<S["revokedResponse"]>(`${BASE}/me/sessions/revoke-others`, { method: "POST" }),
  sessionSettings: () => apiFetch<SessionSettings>(`${BASE}/me/session-settings`),
  putSessionSettings: (idleTimeoutSeconds: number) =>
    apiFetch<SessionSettings>(`${BASE}/me/session-settings`, { method: "PUT", body: { idleTimeoutSeconds } }),
};

/** React Query keys of the data above. */
export const authKeys = {
  session: ["auth", "session"] as const,
  setup: ["auth", "setup"] as const,
  factors: ["me", "second-factors"] as const,
  sessions: ["me", "sessions"] as const,
  sessionSettings: ["me", "session-settings"] as const,
};

export function isApiError(err: unknown, code?: string): err is ApiError {
  return err instanceof ApiError && (code === undefined || err.code === code);
}

/** Roles ordered from most to least privileged (core/internal/iam/iam.go Roles). */
export const ROLE_ORDER = ["owner", "platform_admin", "host_operator", "project_admin", "db_operator", "auditor"];

/** The most privileged role among the assignments, or undefined when there are none. */
export function primaryRole(assignments: readonly Assignment[]): string | undefined {
  const roles = assignments.map((a) => a.role);
  return ROLE_ORDER.find((r) => roles.includes(r)) ?? roles[0];
}

/** How many recovery codes Core issues per set (engineering/architecture.md 第二驗證). */
export const RECOVERY_CODES_PER_SET = 10;
