/**
 * Management API transport (/management/v1). Cookie session + CSRF header, same as core/internal/api/iam_routes.go.
 */

export const CSRF_COOKIE = "hazelminx_csrf";
export const CSRF_HEADER = "X-CSRF-Token";
export const TRACE_HEADER = "X-Trace-ID";
const CSRF_METHODS = new Set(["POST", "PUT", "PATCH", "DELETE"]);

/** Error body Core returns (core/internal/api/iam_routes.go `apiError`). */
export interface ApiErrorBody {
  code: string;
  message: string;
  requestId?: string;
  traceId?: string;
  jobId?: string;
  retryable?: boolean;
  retryAfterSeconds?: number;
  suggestedActions?: string[];
}

/** Every failed call surfaces as this error; `raw` keeps the original response text for display. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly traceId?: string;
  readonly requestId?: string;
  readonly retryable: boolean;
  readonly retryAfterSeconds?: number;
  readonly suggestedActions: string[];
  readonly raw: string;

  constructor(init: {
    status: number;
    code: string;
    message: string;
    raw: string;
    traceId?: string;
    requestId?: string;
    retryable?: boolean;
    retryAfterSeconds?: number;
    suggestedActions?: string[];
  }) {
    super(init.message);
    this.name = "ApiError";
    this.status = init.status;
    this.code = init.code;
    this.raw = init.raw;
    this.traceId = init.traceId;
    this.requestId = init.requestId;
    this.retryable = init.retryable ?? false;
    this.retryAfterSeconds = init.retryAfterSeconds;
    this.suggestedActions = init.suggestedActions ?? [];
  }
}

/** Reads a cookie value from a `document.cookie` style string; undefined when absent. */
export function readCookie(cookieString: string, name: string): string | undefined {
  for (const part of cookieString.split(";")) {
    const eq = part.indexOf("=");
    if (eq === -1) continue;
    if (part.slice(0, eq).trim() !== name) continue;
    const raw = part.slice(eq + 1).trim();
    try {
      return decodeURIComponent(raw);
    } catch {
      return raw;
    }
  }
  return undefined;
}

export interface ApiRequest {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  /** Serialized as JSON when present. */
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
}

/**
 * Calls the management API and returns the parsed JSON body (undefined for 204).
 * Throws ApiError for HTTP errors and network failures. `fetchImpl` and `cookies` are injectable for tests.
 */
export async function apiFetch<T>(
  path: string,
  req: ApiRequest = {},
  deps: { fetchImpl?: typeof fetch; cookies?: () => string } = {},
): Promise<T> {
  if (!path.startsWith("/")) throw new TypeError(`apiFetch path must start with "/": ${path}`);
  const fetchImpl = deps.fetchImpl ?? ((input, init) => fetch(input, init));
  const cookies = deps.cookies ?? (() => document.cookie);
  const method = req.method ?? "GET";

  const headers = new Headers({ Accept: "application/json" });
  for (const [k, v] of Object.entries(req.headers ?? {})) headers.set(k, v);
  if (CSRF_METHODS.has(method)) {
    const csrf = readCookie(cookies(), CSRF_COOKIE);
    if (csrf !== undefined) headers.set(CSRF_HEADER, csrf);
  }
  const init: RequestInit = { method, credentials: "same-origin", headers, signal: req.signal };
  if (req.body !== undefined) {
    headers.set("Content-Type", "application/json");
    init.body = JSON.stringify(req.body);
  }

  let res: Response;
  try {
    res = await fetchImpl(path, init);
  } catch (err) {
    const name = typeof err === "object" && err !== null && "name" in err ? String(err.name) : "Error";
    if (name === "AbortError") throw err;
    const message = typeof err === "object" && err !== null && "message" in err ? String(err.message) : String(err);
    throw new ApiError({ status: 0, code: "NETWORK_ERROR", message, raw: `${name}: ${message}` });
  }

  const text = await res.text();
  if (res.ok) {
    if (res.status === 204 || text === "") return undefined as T;
    try {
      return JSON.parse(text) as T;
    } catch {
      // Something between the browser and Core (a proxy, a login portal) answered instead.
      throw new ApiError({
        status: res.status,
        code: "INVALID_RESPONSE",
        message: "response body is not JSON",
        raw: text,
        traceId: res.headers.get(TRACE_HEADER) ?? undefined,
      });
    }
  }

  const headerTrace = res.headers.get(TRACE_HEADER) ?? undefined;
  let body: Partial<ApiErrorBody> | undefined;
  try {
    const parsed: unknown = JSON.parse(text);
    if (parsed && typeof parsed === "object" && typeof (parsed as ApiErrorBody).code === "string") {
      body = parsed as Partial<ApiErrorBody>;
    }
  } catch {
    // not JSON; fall through to the generic error
  }
  throw new ApiError({
    status: res.status,
    code: body?.code ?? `HTTP_${res.status}`,
    message: body?.message ?? (res.statusText || text),
    raw: text,
    traceId: body?.traceId ?? headerTrace,
    requestId: body?.requestId,
    retryable: body?.retryable,
    retryAfterSeconds: body?.retryAfterSeconds,
    suggestedActions: body?.suggestedActions,
  });
}
