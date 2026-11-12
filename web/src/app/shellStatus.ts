import { createContext, useContext } from "react";

/**
 * Values the shell chrome displays. Each field is optional: undefined means "no data yet",
 * and the shell then shows nothing or "unknown" instead of a fake zero (T-118).
 * Later tickets fill this from the management API and the event stream.
 */
export interface ShellStatus {
  /** UTC RFC 3339 time of the newest observation on the current page. */
  observedAt?: string;
  /** Auto-refresh interval of the current page in seconds. */
  refreshSeconds?: number;
  /** Open items in events & alerts that need attention. */
  attention?: number;
  /** Jobs currently running. */
  jobs?: number;
  /** Hosts that stopped responding (maintenance excluded); filled by src/nodes/hooks.ts useUnreachableHosts. */
  unreachableHosts?: { id: string; name: string }[];
  /** Signed-in user (set by the route guard from GET /auth/session; Core has no e-mail field yet). */
  user?: { name: string; role: string; email?: string };
  /** True when nobody is signed in: the top bar shows only the brand, theme and language. */
  signedOut?: boolean;
}

export const ShellStatusContext = createContext<ShellStatus>({});

export function useShellStatus(): ShellStatus {
  return useContext(ShellStatusContext);
}
