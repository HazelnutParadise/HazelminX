import { MutationCache, QueryCache, QueryClient, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router";
import { ApiError } from "../api/client";
import type { ShellStatus } from "../app/shellStatus";
import { authApi, authKeys, isApiError, primaryRole, type SessionResponse } from "./api";
import { AUTH_PATHS } from "./nextPath";

export type SessionState =
  | { status: "loading" }
  | { status: "signed-out" }
  | { status: "signed-in"; session: SessionResponse }
  | { status: "error"; error: ApiError };

/** Codes that mean the browser no longer has a usable session, so the guard must look again. */
const SESSION_LOST = new Set(["UNAUTHENTICATED", "SECOND_FACTOR_ENROLLMENT_REQUIRED"]);

/**
 * The app's QueryClient. Any query or mutation that finds the session gone (expired, revoked elsewhere,
 * held to second-factor enrolment) refreshes the session, and the route guard then sends the user to the
 * right screen, keeping the page they were on as `next`.
 */
export function createQueryClient(): QueryClient {
  const client: QueryClient = new QueryClient({
    queryCache: new QueryCache({ onError: (err, query) => onApiError(err, query.queryKey) }),
    mutationCache: new MutationCache({ onError: (err) => onApiError(err, undefined) }),
    defaultOptions: { queries: { retry: (n, err) => !(err instanceof ApiError && err.status > 0) && n < 2 } },
  });
  function onApiError(err: unknown, key: readonly unknown[] | undefined) {
    if (!(err instanceof ApiError) || !SESSION_LOST.has(err.code)) return;
    if (key && isSessionKey(key)) return;
    // Whoever signs in next must not see this account's cached factors, devices or settings.
    if (err.code === "UNAUTHENTICATED") forgetAccountData(client);
    void client.invalidateQueries({ queryKey: authKeys.session });
  }
  return client;
}

function isSessionKey(key: readonly unknown[]): boolean {
  return key[0] === authKeys.session[0] && key[1] === authKeys.session[1];
}

/** Drops every cached query except the session itself, so one account's data never shows for another. */
export function forgetAccountData(client: QueryClient) {
  client.removeQueries({ predicate: (q) => !isSessionKey(q.queryKey) });
}

export function useSession(): SessionState {
  const q = useQuery({
    queryKey: authKeys.session,
    queryFn: async () => {
      try {
        return await authApi.session();
      } catch (err) {
        if (isApiError(err, "UNAUTHENTICATED")) return null;
        throw err;
      }
    },
    staleTime: 30_000,
  });
  if (q.isPending) return { status: "loading" };
  if (q.isError) return { status: "error", error: toApiError(q.error) };
  return q.data ? { status: "signed-in", session: q.data } : { status: "signed-out" };
}

function toApiError(err: unknown): ApiError {
  if (err instanceof ApiError) return err;
  const message = err instanceof Error ? err.message : String(err);
  return new ApiError({ status: 0, code: "NETWORK_ERROR", message, raw: message });
}

/**
 * Signs out, opens the login page (without `next`) and forgets everything cached for the account. When Core
 * did not confirm the sign-out (unreachable, or any error but "already signed out"), the session may still
 * be valid: the login page keeps trying on its own ({@link useSignOutRetry}) and only when that fails too
 * says so and offers to try again, instead of quietly signing the user back in.
 */
export function useSignOut() {
  const client = useQueryClient();
  const navigate = useNavigate();
  return async () => {
    let failed = false;
    try {
      await authApi.logout();
    } catch (err) {
      failed = !isApiError(err, "UNAUTHENTICATED");
    }
    // Same tick: the router leaves the guarded page before the guard can see the empty session.
    void navigate(AUTH_PATHS.login, { replace: true, state: failed ? { signOutFailed: true } : undefined });
    client.removeQueries();
    client.setQueryData(authKeys.session, null);
  };
}

/** Waits before each automatic retry of an unconfirmed sign-out. Tests shorten them. */
export const signOutRetry = { delaysMs: [1000, 3000, 6000] };

/**
 * Retries a sign-out Core did not confirm, without the user asking: after each delay of {@link signOutRetry},
 * and at once when the browser comes back online. `onConfirmed` runs when Core confirms (or says the session
 * is already gone). The result is "retrying" while attempts remain and "failed" when the last one failed;
 * a new `attempt` (the manual retry) starts over.
 */
export function useSignOutRetry(active: boolean, attempt: string, onConfirmed: () => void): "retrying" | "failed" {
  const [failedFor, setFailedFor] = useState<string | null>(null);
  const confirmed = useRef(onConfirmed);
  useEffect(() => {
    confirmed.current = onConfirmed;
  });
  useEffect(() => {
    if (!active) return;
    // One run of retries; cleanup stops it.
    const run: { stopped: boolean; busy: boolean; n: number; timer?: ReturnType<typeof setTimeout> } = {
      stopped: false,
      busy: false,
      n: 0,
    };
    const schedule = () => {
      const delay = signOutRetry.delaysMs[run.n];
      if (delay === undefined) {
        setFailedFor(attempt);
        return;
      }
      run.n += 1;
      run.timer = setTimeout(() => void tryOnce(), delay);
    };
    const tryOnce = async () => {
      if (run.stopped || run.busy) return;
      run.busy = true;
      const ok = await authApi.logout().then(
        () => true,
        (err: unknown) => isApiError(err, "UNAUTHENTICATED"),
      );
      run.busy = false;
      if (run.stopped) return;
      if (ok) {
        run.stopped = true;
        confirmed.current();
        return;
      }
      schedule();
    };
    const online = () => {
      clearTimeout(run.timer);
      void tryOnce();
    };
    schedule();
    window.addEventListener("online", online);
    return () => {
      run.stopped = true;
      clearTimeout(run.timer);
      window.removeEventListener("online", online);
    };
  }, [active, attempt]);
  return failedFor === attempt ? "failed" : "retrying";
}

/** The shell's account menu label: display name and the most privileged role. */
export function useShellUser(session: SessionResponse): NonNullable<ShellStatus["user"]> {
  const { t, i18n } = useTranslation();
  const role = primaryRole(session.assignments);
  const roleLabel = role ? (i18n.exists(`roles.${role}`) ? t(`roles.${role}`) : role) : t("roles.none");
  return { name: session.user.displayName || session.user.username, role: roleLabel };
}
