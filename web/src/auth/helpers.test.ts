import { describe, expect, it } from "vite-plus/test";
import { formatRelative } from "../lib/time";
import { primaryRole, type Assignment } from "./api";
import { formatWait, groupSecret } from "./hooks";
import { safeNext, withNext } from "./nextPath";
import {
  base64urlToBytes,
  bytesToBase64url,
  credentialToJSON,
  describeUserAgent,
  isIpHost,
  passkeyFailure,
  toCreationOptions,
  toRequestOptions,
} from "./webauthn";

describe("safeNext", () => {
  it.each([
    ["/settings", "/settings"],
    ["/nodes/hazel-tw-01/files?path=%2Fetc#top", "/nodes/hazel-tw-01/files?path=%2Fetc#top"],
    ["/account/security", "/account/security"],
  ])("keeps the in-app path %s", (raw, want) => {
    expect(safeNext(raw)).toBe(want);
  });

  it.each([
    [null],
    [""],
    ["settings"],
    ["//evil.example/x"],
    ["/\\evil.example"],
    ["https://evil.example/"],
    ["javascript:alert(1)"],
    ["/ok\u0000"],
    ["/a\\b"],
    ["/login"],
    ["/login?next=/x"],
    ["/setup"],
    ["/enroll/anything"],
  ])("falls back to / for %j (no open redirect, no loop)", (raw) => {
    expect(safeNext(raw)).toBe("/");
  });

  it("does not mistake a page that only starts like a sign-in path", () => {
    expect(safeNext("/loginx")).toBe("/loginx");
  });

  it("withNext omits next for the overview and encodes everything else", () => {
    expect(withNext("/login", "/")).toBe("/login");
    expect(withNext("/login", "/settings?a=1&b=2")).toBe("/login?next=%2Fsettings%3Fa%3D1%26b%3D2");
    expect(withNext("/login", "//evil")).toBe("/login");
  });
});

describe("primaryRole", () => {
  const a = (role: string) => ({ role }) as Assignment;
  it("picks the most privileged role", () => {
    expect(primaryRole([a("auditor"), a("host_operator"), a("project_admin")])).toBe("host_operator");
    expect(primaryRole([a("owner"), a("auditor")])).toBe("owner");
  });
  it("is undefined without assignments and keeps unknown roles", () => {
    expect(primaryRole([])).toBeUndefined();
    expect(primaryRole([a("custom")])).toBe("custom");
  });
});

describe("small formatters", () => {
  it("formatWait shows m:ss", () => {
    expect(formatWait(272)).toBe("4:32");
    expect(formatWait(5)).toBe("0:05");
  });
  it("groupSecret splits the key in fours", () => {
    expect(groupSecret("JBSWY3DPEHPK3PXP")).toBe("JBSW Y3DP EHPK 3PXP");
    expect(groupSecret("JBS WY3")).toBe("JBSW Y3");
  });
  it("formatRelative says just now, minutes, hours and days", () => {
    const now = new Date("2026-11-12T12:00:00Z");
    expect(formatRelative("2026-11-12T11:59:30Z", now, "zh-Hant-TW", "剛剛")).toBe("剛剛");
    expect(formatRelative("2026-11-12T11:55:00Z", now, "zh-Hant-TW", "剛剛")).toBe("5 分鐘前");
    expect(formatRelative("2026-11-12T10:00:00Z", now, "zh-Hant-TW", "剛剛")).toBe("2 小時前");
    expect(formatRelative("2026-11-11T11:00:00Z", now, "zh-Hant-TW", "剛剛")).toBe("昨天");
    expect(formatRelative(undefined, now, "zh-Hant-TW", "剛剛")).toBeNull();
    expect(formatRelative("not a time", now, "zh-Hant-TW", "剛剛")).toBeNull();
  });
});

describe("webauthn glue", () => {
  it("base64url round-trips bytes without padding", () => {
    const bytes = new Uint8Array([0, 251, 255, 62, 63, 1]);
    const enc = bytesToBase64url(bytes);
    expect(enc).not.toMatch(/[+/=]/);
    expect([...base64urlToBytes(enc)]).toEqual([...bytes]);
  });

  it("turns Core's creation options into browser options", () => {
    const opts = toCreationOptions({
      publicKey: {
        rp: { id: "panel.example.tw", name: "HazelminX" },
        user: { id: bytesToBase64url(new Uint8Array([1, 2, 3])), name: "tim", displayName: "tim" },
        challenge: bytesToBase64url(new Uint8Array([9, 9])),
        pubKeyCredParams: [{ type: "public-key", alg: -7 }],
        excludeCredentials: [{ type: "public-key", id: bytesToBase64url(new Uint8Array([7])) }],
      },
    });
    const pk = opts.publicKey!;
    expect([...new Uint8Array(pk.challenge as ArrayBuffer)]).toEqual([9, 9]);
    expect([...new Uint8Array(pk.user.id as ArrayBuffer)]).toEqual([1, 2, 3]);
    expect([...new Uint8Array(pk.excludeCredentials![0]!.id as ArrayBuffer)]).toEqual([7]);
    expect(pk.rp.id).toBe("panel.example.tw");
  });

  it("turns Core's request options into browser options", () => {
    const opts = toRequestOptions({
      publicKey: {
        challenge: bytesToBase64url(new Uint8Array([4])),
        rpId: "panel.example.tw",
        allowCredentials: [{ type: "public-key", id: bytesToBase64url(new Uint8Array([5])), transports: ["internal"] }],
      },
    });
    expect([...new Uint8Array(opts.publicKey!.challenge as ArrayBuffer)]).toEqual([4]);
    expect(opts.publicKey!.allowCredentials![0]!.transports).toEqual(["internal"]);
  });

  it("rejects options without publicKey", () => {
    expect(() => toRequestOptions({})).toThrow(TypeError);
  });

  it("serialises an assertion the way Core parses it", () => {
    const buf = (n: number) => new Uint8Array([n]).buffer;
    const cred = {
      id: "AQ",
      rawId: buf(1),
      type: "public-key",
      authenticatorAttachment: "platform",
      getClientExtensionResults: () => ({}),
      response: { clientDataJSON: buf(2), authenticatorData: buf(3), signature: buf(4), userHandle: buf(5) },
    } as unknown as PublicKeyCredential;
    expect(credentialToJSON(cred)).toEqual({
      id: "AQ",
      rawId: "AQ",
      type: "public-key",
      authenticatorAttachment: "platform",
      clientExtensionResults: {},
      response: { clientDataJSON: "Ag", authenticatorData: "Aw", signature: "BA", userHandle: "BQ" },
    });
  });

  it("serialises an attestation with its transports", () => {
    const buf = (n: number) => new Uint8Array([n]).buffer;
    const cred = {
      id: "AQ",
      rawId: buf(1),
      type: "public-key",
      authenticatorAttachment: null,
      getClientExtensionResults: () => ({ credProps: { rk: true } }),
      response: { clientDataJSON: buf(2), attestationObject: buf(6), getTransports: () => ["internal", "hybrid"] },
    } as unknown as PublicKeyCredential;
    expect(credentialToJSON(cred).response).toEqual({
      clientDataJSON: "Ag",
      attestationObject: "Bg",
      transports: ["internal", "hybrid"],
    });
  });

  it("classifies browser failures", () => {
    expect(passkeyFailure(new DOMException("closed", "NotAllowedError"))).toBe("cancelled");
    expect(passkeyFailure(new DOMException("x", "SecurityError"))).toBe("unsupported");
    expect(passkeyFailure(new DOMException("x", "InvalidStateError"))).toBe("failed");
    expect(passkeyFailure(new Error("other"))).toBeNull();
    expect(passkeyFailure("nope")).toBeNull();
  });

  it("knows IP literals cannot carry passkeys", () => {
    expect(isIpHost("203.0.113.22")).toBe(true);
    expect(isIpHost("[::1]")).toBe(true);
    expect(isIpHost("::1")).toBe(true);
    expect(isIpHost("panel.example.tw")).toBe(false);
    expect(isIpHost("localhost")).toBe(false);
  });

  it("names devices from the User-Agent", () => {
    expect(
      describeUserAgent(
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15",
      ),
    ).toBe("Safari · macOS");
    expect(
      describeUserAgent(
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36 Edg/140.0",
      ),
    ).toBe("Edge · Windows");
    expect(describeUserAgent("Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0")).toBe(
      "Firefox · Linux",
    );
    expect(describeUserAgent("curl/8.7.1")).toBe("curl");
    expect(describeUserAgent("")).toBeNull();
  });
});
