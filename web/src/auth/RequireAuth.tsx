import { Navigate, useLocation } from "react-router";
import { ShellStatusContext, useShellStatus } from "../app/shellStatus";
import { useUnreachableHosts } from "../nodes/hooks";
import type { SessionResponse } from "./api";
import { AUTH_PATHS, withNext } from "./nextPath";
import { useSession, useShellUser, type SessionState } from "./session";

/**
 * Route guard for every page inside the shell. Signed out → login (back here afterwards); held to
 * second-factor enrolment → enrolment; otherwise the shell gets the signed-in user.
 */
export function RequireAuth({
  children,
  fallback,
}: {
  children: React.ReactNode;
  fallback: (s: SessionState) => React.ReactNode;
}) {
  const state = useSession();
  const location = useLocation();
  const here = location.pathname + location.search + location.hash;
  if (state.status === "signed-out") return <Navigate to={withNext(AUTH_PATHS.login, here)} replace />;
  if (state.status !== "signed-in") return fallback(state);
  if (state.session.secondFactorEnrollmentRequired) return <Navigate to={withNext(AUTH_PATHS.enroll, here)} replace />;
  return <SignedInStatus session={state.session}>{children}</SignedInStatus>;
}

function SignedInStatus({ session, children }: { session: SessionResponse; children: React.ReactNode }) {
  const outer = useShellStatus();
  const user = useShellUser(session);
  const unreachableHosts = useUnreachableHosts() ?? outer.unreachableHosts;
  return (
    <ShellStatusContext.Provider value={{ ...outer, user, unreachableHosts, signedOut: false }}>
      {children}
    </ShellStatusContext.Provider>
  );
}
