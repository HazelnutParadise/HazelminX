import createClient, { type Middleware } from "openapi-fetch";
import { CSRF_COOKIE, CSRF_HEADER, readCookie } from "./client";

const UNSAFE = new Set(["POST", "PUT", "PATCH", "DELETE"]);

/** Adds the CSRF header to unsafe methods, same rule as apiFetch (core/internal/api/iam_routes.go). */
export const csrfMiddleware = (cookies: () => string = () => document.cookie): Middleware => ({
  onRequest({ request }) {
    if (!UNSAFE.has(request.method)) return request;
    const token = readCookie(cookies(), CSRF_COOKIE);
    if (token) request.headers.set(CSRF_HEADER, token);
    return request;
  },
});

/**
 * Typed management API client. `Paths` comes from src/api/schema.gen.ts, produced by
 * `vpr api:gen` from Core's /management/v1/openapi.json (ticket 010 publishes it).
 */
export function createManagementClient<Paths extends {}>(opts: { baseUrl?: string; fetch?: typeof fetch } = {}) {
  const client = createClient<Paths>({
    baseUrl: opts.baseUrl ?? "",
    credentials: "same-origin",
    headers: { Accept: "application/json" },
    fetch: opts.fetch,
  });
  client.use(csrfMiddleware());
  return client;
}
