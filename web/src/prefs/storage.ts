/**
 * Per-browser preferences. public/boot.js reads the same keys before first paint; keep both in sync.
 * Storage may be unavailable (private mode, policy): reads fall back to defaults and writes are dropped.
 */
export const PREF_KEYS = {
  mode: "hx.mode",
  lang: "hx.lang",
  sidebar: "hx.sidebar",
} as const;

export function readPref(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function writePref(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // Not persisted; the choice still applies to this page.
  }
}
