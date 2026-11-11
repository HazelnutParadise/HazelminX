/**
 * WebAuthn glue: Core sends go-webauthn options as JSON ({"publicKey": {...}} with base64url byte fields)
 * and expects the credential back in the same JSON shape (core/internal/api/mfa_routes.go).
 */

export function base64urlToBytes(value: string): Uint8Array<ArrayBuffer> {
  const b64 = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = b64 + "=".repeat((4 - (b64.length % 4)) % 4);
  const bin = atob(padded);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export function bytesToBase64url(value: ArrayBuffer | ArrayBufferView): string {
  const bytes =
    value instanceof ArrayBuffer
      ? new Uint8Array(value)
      : new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

interface JsonDescriptor {
  id: string;
  type: string;
  transports?: string[];
}

type JsonObject = Record<string, unknown>;

function publicKeyOf(options: unknown): JsonObject {
  const pk = (options as { publicKey?: unknown } | null)?.publicKey;
  if (!pk || typeof pk !== "object") throw new TypeError("WebAuthn options have no publicKey");
  return pk as JsonObject;
}

function descriptors(list: unknown): PublicKeyCredentialDescriptor[] | undefined {
  if (!Array.isArray(list)) return undefined;
  return (list as JsonDescriptor[]).map((d) => ({
    ...d,
    type: "public-key",
    id: base64urlToBytes(d.id),
    transports: d.transports as AuthenticatorTransport[] | undefined,
  }));
}

/** navigator.credentials.create() options from Core's JSON. */
export function toCreationOptions(options: unknown): CredentialCreationOptions {
  const pk = publicKeyOf(options);
  const user = pk.user as { id: string; name: string; displayName: string };
  return {
    publicKey: {
      ...(pk as unknown as PublicKeyCredentialCreationOptions),
      challenge: base64urlToBytes(pk.challenge as string),
      user: { ...user, id: base64urlToBytes(user.id) },
      excludeCredentials: descriptors(pk.excludeCredentials),
    },
  };
}

/** navigator.credentials.get() options from Core's JSON. */
export function toRequestOptions(options: unknown): CredentialRequestOptions {
  const pk = publicKeyOf(options);
  return {
    publicKey: {
      ...(pk as unknown as PublicKeyCredentialRequestOptions),
      challenge: base64urlToBytes(pk.challenge as string),
      allowCredentials: descriptors(pk.allowCredentials),
    },
  };
}

/** The credential as the JSON Core parses (same shape as PublicKeyCredential.toJSON()). */
export function credentialToJSON(cred: PublicKeyCredential): JsonObject {
  const r = cred.response as AuthenticatorResponse &
    Partial<AuthenticatorAttestationResponse & AuthenticatorAssertionResponse>;
  const response: JsonObject = { clientDataJSON: bytesToBase64url(r.clientDataJSON) };
  if (r.attestationObject) {
    response.attestationObject = bytesToBase64url(r.attestationObject);
    if (typeof r.getTransports === "function") response.transports = r.getTransports();
  }
  if (r.authenticatorData) response.authenticatorData = bytesToBase64url(r.authenticatorData);
  if (r.signature) response.signature = bytesToBase64url(r.signature);
  if (r.userHandle) response.userHandle = bytesToBase64url(r.userHandle);
  return {
    id: cred.id,
    rawId: bytesToBase64url(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    clientExtensionResults: cred.getClientExtensionResults?.() ?? {},
    response,
  };
}

/** Why passkeys cannot be used on this page, or null when they can. */
export type PasskeyBlock = "unsupported" | "ip-address";

export function passkeyBlock(loc: { hostname: string } = window.location): PasskeyBlock | null {
  if (typeof window.PublicKeyCredential === "undefined" || !window.isSecureContext) return "unsupported";
  if (isIpHost(loc.hostname)) return "ip-address";
  return null;
}

/** True for IPv4 and IPv6 literals ("[::1]" included); passkeys need a domain (WebAuthn RP ID). */
export function isIpHost(hostname: string): boolean {
  return /^\d{1,3}(\.\d{1,3}){3}$/.test(hostname) || hostname.includes(":");
}

/** What happened when the browser prompt failed. */
export type PasskeyFailure = "cancelled" | "unsupported" | "failed";

/** The user closed the prompt or it timed out: browsers report both as NotAllowedError, by design. */
export function passkeyFailure(err: unknown): PasskeyFailure | null {
  if (!(err instanceof Error) && !(typeof DOMException !== "undefined" && err instanceof DOMException)) return null;
  const name = (err as { name?: string }).name;
  if (name === "NotAllowedError" || name === "AbortError") return "cancelled";
  if (name === "NotSupportedError" || name === "SecurityError") return "unsupported";
  if (name === "InvalidStateError" || name === "UnknownError" || name === "ConstraintError") return "failed";
  return null;
}

export async function createPasskey(options: unknown): Promise<JsonObject> {
  const cred = (await navigator.credentials.create(toCreationOptions(options))) as PublicKeyCredential | null;
  if (!cred) throw new DOMException("no credential", "NotAllowedError");
  return credentialToJSON(cred);
}

export async function getPasskey(options: unknown): Promise<JsonObject> {
  const cred = (await navigator.credentials.get(toRequestOptions(options))) as PublicKeyCredential | null;
  if (!cred) throw new DOMException("no credential", "NotAllowedError");
  return credentialToJSON(cred);
}

/** A default name for a new passkey: browser and system of this device, e.g. "Chrome · macOS". */
export function defaultPasskeyName(userAgent: string = navigator.userAgent): string {
  const d = describeUserAgent(userAgent);
  return d ?? "Passkey";
}

const BROWSERS: [RegExp, string][] = [
  [/Edg\//, "Edge"],
  [/OPR\//, "Opera"],
  [/Firefox\//, "Firefox"],
  [/Chrome\//, "Chrome"],
  [/Safari\//, "Safari"],
  [/^curl\//, "curl"],
  [/^hx\//i, "hx CLI"],
];

const SYSTEMS: [RegExp, string][] = [
  [/iPhone|iPad|iPod/, "iOS"],
  [/Android/, "Android"],
  [/Mac OS X|Macintosh/, "macOS"],
  [/Windows/, "Windows"],
  [/CrOS/, "ChromeOS"],
  [/Linux/, "Linux"],
];

/** "Browser · System" from a User-Agent header, or null when neither is recognised. */
export function describeUserAgent(userAgent: string): string | null {
  const browser = BROWSERS.find(([re]) => re.test(userAgent))?.[1];
  const system = SYSTEMS.find(([re]) => re.test(userAgent))?.[1];
  const parts = [browser, system].filter(Boolean);
  return parts.length ? parts.join(" · ") : null;
}
