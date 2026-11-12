/** Paths of the signed-out screens. */
export const AUTH_PATHS = { login: "/login", setup: "/setup", enroll: "/enroll" } as const;

const AUTH_PREFIXES = Object.values(AUTH_PATHS);

/**
 * The page to open after signing in, taken from `?next=`. Only same-origin absolute paths are accepted:
 * "//host", "/\host", schemes and the sign-in screens themselves fall back to "/" (no open redirect, no loop).
 */
export function safeNext(raw: string | null | undefined): string {
  if (!raw || !raw.startsWith("/") || raw.startsWith("//") || raw.startsWith("/\\")) return "/";
  // Control characters and backslashes are never part of an in-app path the router produced.
  for (const ch of raw) {
    const c = ch.charCodeAt(0);
    if (c < 0x20 || c === 0x7f || ch === "\\") return "/";
  }
  const path = raw.split(/[?#]/, 1)[0]!;
  if (AUTH_PREFIXES.some((p) => path === p || path.startsWith(p + "/"))) return "/";
  return raw;
}

/** `/login?next=…` for the page the user tried to open; the bare path when that page is the overview. */
export function withNext(base: string, next: string): string {
  const target = safeNext(next);
  return target === "/" ? base : `${base}?next=${encodeURIComponent(target)}`;
}
