import { createContext, useCallback, useContext } from "react";
import { isApiError, type FactorSummary } from "./api";
import { passkeyBlock } from "./webauthn";

/** Thrown when the user closes the 「確認是你」 dialog instead of proving who they are. */
export class ReauthCancelled extends Error {
  constructor() {
    super("reauthentication cancelled");
    this.name = "ReauthCancelled";
  }
}

export type Ask = (title: string) => Promise<void>;

export const ReauthContext = createContext<Ask>(() => Promise.reject(new ReauthCancelled()));

/**
 * Runs `fn`; when Core asks for a recent authentication, opens the dialog titled `title` and runs `fn`
 * once more after the user passed it. Rejects with ReauthCancelled when they close the dialog.
 */
export function useReauth(): <T>(fn: () => Promise<T>, title: string) => Promise<T> {
  const ask = useContext(ReauthContext);
  return useCallback(
    async <T>(fn: () => Promise<T>, title: string): Promise<T> => {
      try {
        return await fn();
      } catch (err) {
        if (!isApiError(err, "REAUTHENTICATION_REQUIRED")) throw err;
        await ask(title);
        return fn();
      }
    },
    [ask],
  );
}

/** Methods this account can use to reauthenticate on this page. */
export function reauthMethods(f: FactorSummary | undefined, hostname = window.location.hostname) {
  const passkey = !!f?.passkeys.some((p) => p.rpId === hostname) && passkeyBlock() === null;
  const totp = !!f?.totp?.confirmed;
  const recovery = (f?.recoveryCodesRemaining ?? 0) > 0;
  return { passkey, totp, recovery, any: !!f && (f.totp?.confirmed || f.passkeys.length > 0) };
}
