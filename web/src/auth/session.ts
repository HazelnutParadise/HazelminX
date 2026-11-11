import { MutationCache, QueryCache, QueryClient, useQuery, useQueryClient } from "@tanstack/react-query";
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
    if (key && key[0] === authKeys.session[0] && key[1] === authKeys.session[1]) return;
    void client.invalidateQueries({ queryKey: authKeys.session });
  }
  return client;
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

/** Signs out, opens the login page (without `next`) and forgets everything cached for the account. */
export function useSignOut() {
  const client = useQueryClient();
  const navigate = useNavigate();
  return async () => {
    try {
      await authApi.logout();
    } catch {
      // Already signed out or Core unreachable: the cookie is cleared or useless either way.
    }
    // Same tick: the router leaves the guarded page before the guard can see the empty session.
    void navigate(AUTH_PATHS.login, { replace: true });
    client.removeQueries();
    client.setQueryData(authKeys.session, null);
  };
}

/** The shell's account menu label: display name and the most privileged role. */
export function useShellUser(session: SessionResponse): NonNullable<ShellStatus["user"]> {
  const { t, i18n } = useTranslation();
  const role = primaryRole(session.assignments);
  const roleLabel = role ? (i18n.exists(`roles.${role}`) ? t(`roles.${role}`) : role) : t("roles.none");
  return { name: session.user.displayName || session.user.username, role: roleLabel };
}
