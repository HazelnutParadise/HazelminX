import { vi } from "vite-plus/test";

/** One recorded request to the fake management API. */
export interface FakeCall {
  method: string;
  path: string;
  /** Query parameters of the request URL. */
  query: URLSearchParams;
  body: unknown;
}

export interface FakeReply {
  status: number;
  body?: unknown;
}

type Route = FakeReply | ((call: FakeCall) => FakeReply);

/** Error body in Core's format (core/internal/api `apiError`). */
export function apiError(status: number, code: string, extra: Record<string, unknown> = {}): FakeReply {
  return { status, body: { code, message: code.toLowerCase(), retryable: false, ...extra } };
}

/**
 * Replaces global fetch with a tiny Core: routes are keyed "METHOD /path" and may be replaced while a test
 * runs (`core.on(...)`). Unknown routes answer 404 NOT_FOUND, like Core.
 */
export function fakeCore(routes: Record<string, Route> = {}) {
  const table = new Map<string, Route>(Object.entries(routes));
  const calls: FakeCall[] = [];
  const fetchImpl = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(input instanceof Request ? input.url : input.toString(), "http://localhost");
    const method = init?.method ?? "GET";
    const body = typeof init?.body === "string" ? (JSON.parse(init.body) as unknown) : undefined;
    const call = { method, path: url.pathname, query: url.searchParams, body };
    calls.push(call);
    const route = table.get(`${method} ${url.pathname}`);
    const reply = route === undefined ? apiError(404, "NOT_FOUND") : typeof route === "function" ? route(call) : route;
    if (reply.status === 204 || reply.body === undefined) return new Response(null, { status: reply.status });
    return new Response(JSON.stringify(reply.body), {
      status: reply.status,
      headers: { "Content-Type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fetchImpl);
  return {
    calls,
    on(key: string, route: Route) {
      table.set(key, route);
    },
    /** Requests made to `key` ("METHOD /path"). */
    callsTo(key: string) {
      return calls.filter((c) => `${c.method} ${c.path}` === key);
    },
  };
}
