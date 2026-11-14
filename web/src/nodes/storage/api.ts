/**
 * A host's storage (core/internal/api/host_storage.go, ticket 030.1): block devices, mounts with how each is
 * referenced and configured, capacity, the 7-day trend, usage by category and capacity alerts. Core sends null for
 * values it does not have, which its OpenAPI document does not mark, so the types are widened with `Maybe` as the
 * host metrics are (../monitor/api.ts).
 */
import { apiFetch } from "../../api/client";
import type { components } from "../../api/schema.gen";
import type { Maybe } from "../monitor/api";

type S = components["schemas"];

export type StorageView = Maybe<S["View"]>;
export type Device = NonNullable<StorageView["devices"]>[number];
export type MountView = NonNullable<StorageView["mounts"]>[number];
export type MountConfig = NonNullable<StorageView["configs"]>[number];
export type MountProblem = NonNullable<StorageView["problems"]>[number];
export type SwapArea = NonNullable<StorageView["swaps"]>[number];
export type FSUsage = NonNullable<MountView["usage"]>;
export type Rule = MountView["rule"];
export type Alert = NonNullable<MountView["alerts"]>[number];

/** The usage categories in the order every disk lists them (hoststorage.CategoryOrder, DESIGN.md 分段橫條). */
export const CATEGORIES = ["appData", "database", "backupStaging", "containerImages", "osAndLogs", "other"] as const;
export type Category = (typeof CATEGORIES)[number];

const BASE = "/management/v1";

export const storageApi = {
  view: (id: string) => apiFetch<StorageView>(`${BASE}/nodes/${encodeURIComponent(id)}/storage`),
};

export const storageKeys = {
  view: (id: string) => ["nodes", "storage", id] as const,
};
