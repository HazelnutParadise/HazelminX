import { useEffect, useState } from "react";
import { useNow } from "../shell/useNow";
import { isApiError } from "./api";

/** Digits of a TOTP code (Core: iam.TOTPDigits). */
export const OTP_LENGTH = 6;

/**
 * Seconds left of a `TOO_MANY_ATTEMPTS` wait for the given error, counting down once a second; 0 when there
 * is none. The wait starts when the error first reaches this hook.
 */
export function useRetryCountdown(error: unknown): number {
  const now = useNow(1000).getTime();
  const [tracked, setTracked] = useState<{ error: unknown; until: number }>({ error: undefined, until: 0 });
  const deadline = (e: unknown) => (isApiError(e, "TOO_MANY_ATTEMPTS") ? now + (e.retryAfterSeconds ?? 60) * 1000 : 0);
  // Adjusting state while rendering when the input changed (react.dev "storing information from previous renders").
  if (tracked.error !== error) setTracked({ error, until: deadline(error) });
  const until = tracked.error === error ? tracked.until : deadline(error);
  return Math.max(0, Math.ceil((until - now) / 1000));
}

export function formatWait(seconds: number): string {
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}

/** Groups a TOTP secret in fours for reading aloud and typing (JBSW Y3DP …). */
export function groupSecret(secret: string): string {
  return secret.replace(/\s+/g, "").replace(/(.{4})(?=.)/g, "$1 ");
}

/** Copies text and reports 「已複製」 for two seconds. */
export function useCopy(): [boolean, (text: string) => Promise<void>] {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const id = window.setTimeout(() => setCopied(false), 2000);
    return () => window.clearTimeout(id);
  }, [copied]);
  return [
    copied,
    async (text: string) => {
      await navigator.clipboard.writeText(text);
      setCopied(true);
    },
  ];
}
