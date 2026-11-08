import { describe, it, expect, vi } from "vite-plus/test";
import { ApiError, apiFetch, readCookie } from "./client";

type FetchArgs = [input: RequestInfo | URL, init?: RequestInit];

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function fakeFetch(respond: () => Response | Promise<Response>) {
  return vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => respond());
}

function depsWith(fetchImpl: ReturnType<typeof fakeFetch>, cookies: string) {
  return { fetchImpl: fetchImpl as unknown as typeof fetch, cookies: () => cookies };
}

function callOf(fetchImpl: ReturnType<typeof fakeFetch>): FetchArgs {
  const call = fetchImpl.mock.calls[0];
  if (!call) throw new Error("fetch was not called");
  return call as FetchArgs;
}

function headersOf(init: RequestInit | undefined): Headers {
  return new Headers(init?.headers);
}

async function rejectionOf(promise: Promise<unknown>): Promise<unknown> {
  try {
    await promise;
  } catch (err) {
    return err;
  }
  throw new Error("expected the promise to reject");
}

describe("readCookie", () => {
  it("returns the decoded value of the named cookie", () => {
    expect(readCookie("a=1; hazelminx_csrf=abc%3D; b=2", "hazelminx_csrf")).toBe("abc=");
  });

  it("returns undefined when the cookie is absent or the string is empty", () => {
    expect(readCookie("a=1; b=2", "hazelminx_csrf")).toBeUndefined();
    expect(readCookie("", "hazelminx_csrf")).toBeUndefined();
  });

  it("does not match a cookie whose name only ends with the requested name", () => {
    expect(readCookie("xhazelminx_csrf=1", "hazelminx_csrf")).toBeUndefined();
  });

  it("keeps everything after the first equals sign", () => {
    expect(readCookie("k=a=b", "k")).toBe("a=b");
  });
});

describe("apiFetch", () => {
  const session = "/management/v1/auth/session";

  it("sends a GET with the path, method, credentials and Accept header, and returns the parsed JSON", async () => {
    const fetchImpl = fakeFetch(() => jsonResponse({ user: "u1" }));
    const result = await apiFetch<{ user: string }>(session, {}, depsWith(fetchImpl, ""));

    expect(result).toEqual({ user: "u1" });
    const [url, init] = callOf(fetchImpl);
    expect(url).toBe(session);
    expect(init?.method).toBe("GET");
    expect(init?.credentials).toBe("same-origin");
    expect(headersOf(init).get("Accept")).toBe("application/json");
    expect(headersOf(init).has("X-CSRF-Token")).toBe(false);
    expect(headersOf(init).has("Content-Type")).toBe(false);
  });

  it("does not send the CSRF header on GET even when the csrf cookie exists", async () => {
    const fetchImpl = fakeFetch(() => jsonResponse({}));
    await apiFetch(session, {}, depsWith(fetchImpl, "hazelminx_csrf=tok"));

    expect(headersOf(callOf(fetchImpl)[1]).has("X-CSRF-Token")).toBe(false);
  });

  it("sends a POST with the CSRF header, JSON content type and serialized body", async () => {
    const fetchImpl = fakeFetch(() => jsonResponse({ ok: true }));
    await apiFetch("/management/v1/x", { method: "POST", body: { a: 1 } }, depsWith(fetchImpl, "hazelminx_csrf=tok"));

    const [, init] = callOf(fetchImpl);
    expect(init?.method).toBe("POST");
    expect(headersOf(init).get("X-CSRF-Token")).toBe("tok");
    expect(headersOf(init).get("Content-Type")).toBe("application/json");
    expect(init?.body).toBe(JSON.stringify({ a: 1 }));
  });

  it.each(["PUT", "PATCH", "DELETE"] as const)("sends the CSRF header on %s", async (method) => {
    const fetchImpl = fakeFetch(() => jsonResponse({}));
    await apiFetch("/management/v1/x", { method }, depsWith(fetchImpl, "hazelminx_csrf=tok"));

    const [, init] = callOf(fetchImpl);
    expect(init?.method).toBe(method);
    expect(headersOf(init).get("X-CSRF-Token")).toBe("tok");
  });

  it("does not invent a CSRF header on POST when the csrf cookie is missing", async () => {
    const fetchImpl = fakeFetch(() => jsonResponse({}));
    await apiFetch("/management/v1/x", { method: "POST", body: { a: 1 } }, depsWith(fetchImpl, "other=1"));

    expect(headersOf(callOf(fetchImpl)[1]).has("X-CSRF-Token")).toBe(false);
  });

  it("merges request headers into the outgoing headers", async () => {
    const fetchImpl = fakeFetch(() => jsonResponse({}));
    await apiFetch("/management/v1/x", { headers: { "X-Custom": "1" } }, depsWith(fetchImpl, ""));

    const headers = headersOf(callOf(fetchImpl)[1]);
    expect(headers.get("X-Custom")).toBe("1");
    expect(headers.get("Accept")).toBe("application/json");
  });

  it("resolves undefined for a 204 response", async () => {
    const fetchImpl = fakeFetch(() => new Response(null, { status: 204 }));
    await expect(apiFetch("/management/v1/x", { method: "DELETE" }, depsWith(fetchImpl, ""))).resolves.toBeUndefined();
  });

  it("resolves undefined for a 200 response with an empty body", async () => {
    const fetchImpl = fakeFetch(() => new Response("", { status: 200 }));
    await expect(apiFetch("/management/v1/x", {}, depsWith(fetchImpl, ""))).resolves.toBeUndefined();
  });

  it("rejects a 403 JSON error with every field of the error body", async () => {
    const raw = JSON.stringify({
      code: "FORBIDDEN",
      message: "m",
      traceId: "t1",
      retryable: false,
      retryAfterSeconds: 5,
      suggestedActions: ["a"],
    });
    const fetchImpl = fakeFetch(() => new Response(raw, { status: 403 }));
    const err = await rejectionOf(apiFetch("/management/v1/x", {}, depsWith(fetchImpl, "")));

    expect(err).toBeInstanceOf(ApiError);
    const apiErr = err as ApiError;
    expect(apiErr.status).toBe(403);
    expect(apiErr.code).toBe("FORBIDDEN");
    expect(apiErr.message).toBe("m");
    expect(apiErr.traceId).toBe("t1");
    expect(apiErr.retryable).toBe(false);
    expect(apiErr.retryAfterSeconds).toBe(5);
    expect(apiErr.suggestedActions).toEqual(["a"]);
    expect(apiErr.raw).toBe(raw);
  });

  it("takes the trace id from the X-Trace-ID header when the error body has none", async () => {
    const fetchImpl = fakeFetch(
      () =>
        new Response(JSON.stringify({ code: "X", message: "m" }), {
          status: 500,
          headers: { "X-Trace-ID": "h1" },
        }),
    );
    const err = (await rejectionOf(apiFetch("/management/v1/x", {}, depsWith(fetchImpl, "")))) as ApiError;

    expect(err).toBeInstanceOf(ApiError);
    expect(err.traceId).toBe("h1");
  });

  it("wraps a non-JSON error body as HTTP_<status> and keeps the original text", async () => {
    const body = "<html>Bad Gateway</html>";
    const fetchImpl = fakeFetch(() => new Response(body, { status: 502 }));
    const err = (await rejectionOf(apiFetch("/management/v1/x", {}, depsWith(fetchImpl, "")))) as ApiError;

    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(502);
    expect(err.code).toBe("HTTP_502");
    expect(err.raw).toBe(body);
  });

  it("wraps a 2xx response whose body is not JSON (e.g. a proxy page) as INVALID_RESPONSE", async () => {
    const body = "<html>login</html>";
    const fetchImpl = fakeFetch(() => new Response(body, { status: 200 }));
    const err = (await rejectionOf(apiFetch("/management/v1/x", {}, depsWith(fetchImpl, "")))) as ApiError;
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(200);
    expect(err.code).toBe("INVALID_RESPONSE");
    expect(err.raw).toBe(body);
  });

  it("rejects protocol-relative paths so the CSRF token never leaves this origin", async () => {
    const fetchImpl = fakeFetch(() => jsonResponse({}));
    await expect(
      apiFetch("//evil.example/x", { method: "POST" }, depsWith(fetchImpl, "hazelminx_csrf=tok")),
    ).rejects.toThrow(TypeError);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("rethrows the abort reason when the caller's signal aborted with one (e.g. a timeout)", async () => {
    const controller = new AbortController();
    const reason = new DOMException("t", "TimeoutError");
    controller.abort(reason);
    const fetchImpl = fakeFetch(() => {
      throw reason;
    });
    const err = await rejectionOf(apiFetch("/management/v1/x", { signal: controller.signal }, depsWith(fetchImpl, "")));
    expect(err).toBe(reason);
  });

  it("wraps a connection drop while reading the body as NETWORK_ERROR with the status and trace id", async () => {
    const stream = new ReadableStream({
      start(c) {
        c.enqueue(new TextEncoder().encode("{"));
        c.error(new TypeError("network error"));
      },
    });
    const fetchImpl = fakeFetch(() => new Response(stream, { status: 200, headers: { "X-Trace-ID": "h9" } }));
    const err = (await rejectionOf(apiFetch("/management/v1/x", {}, depsWith(fetchImpl, "")))) as ApiError;
    expect(err).toBeInstanceOf(ApiError);
    expect(err.code).toBe("NETWORK_ERROR");
    expect(err.status).toBe(200);
    expect(err.traceId).toBe("h9");
  });

  it("wraps a network failure as status 0 with code NETWORK_ERROR", async () => {
    const fetchImpl = fakeFetch(() => {
      throw new TypeError("Failed to fetch");
    });
    const err = (await rejectionOf(apiFetch("/management/v1/x", {}, depsWith(fetchImpl, "")))) as ApiError;

    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(0);
    expect(err.code).toBe("NETWORK_ERROR");
    expect(err.message).toBe("Failed to fetch");
  });

  it("rethrows an AbortError DOMException as the same object", async () => {
    const abort = new DOMException("The operation was aborted.", "AbortError");
    const fetchImpl = fakeFetch(() => {
      throw abort;
    });
    const err = await rejectionOf(apiFetch("/management/v1/x", {}, depsWith(fetchImpl, "")));

    expect(err).toBe(abort);
    expect(err).not.toBeInstanceOf(ApiError);
  });

  it("passes the request signal to fetch", async () => {
    const controller = new AbortController();
    const fetchImpl = fakeFetch(() => jsonResponse({}));
    await apiFetch("/management/v1/x", { signal: controller.signal }, depsWith(fetchImpl, ""));

    expect(callOf(fetchImpl)[1]?.signal).toBe(controller.signal);
  });

  it("rejects with a TypeError when the path does not start with a slash", async () => {
    const fetchImpl = fakeFetch(() => jsonResponse({}));
    await expect(apiFetch("management/v1/x", {}, depsWith(fetchImpl, ""))).rejects.toBeInstanceOf(TypeError);
  });
});
