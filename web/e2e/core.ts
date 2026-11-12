import { createHmac, generateKeyPairSync, randomBytes } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { request, type APIRequestContext, type Browser, type BrowserContext, type Page } from "@playwright/test";

/** Where e2e/start-core.sh keeps the Core binary, its data, its log and what the first-run test saves. */
export const E2E_DIR = process.env.HAZELMINX_E2E_DIR ?? join(dirname(fileURLToPath(import.meta.url)), "..", ".e2e");
export const OWNER_STATE = join(E2E_DIR, "owner.json");
export const OWNER_SECRETS = join(E2E_DIR, "owner-secrets.json");

/** The one-time setup code: HAZELMINX_E2E_SETUP_CODE, or what the Core started for this run printed. */
export function setupCode(): string {
  if (process.env.HAZELMINX_E2E_SETUP_CODE) return process.env.HAZELMINX_E2E_SETUP_CODE;
  const log = join(E2E_DIR, "core.log");
  if (!existsSync(log)) throw new Error(`no ${log}; set HAZELMINX_E2E_SETUP_CODE when testing an external Core`);
  const m = /"setupCode": "([^"]+)"/.exec(readFileSync(log, "utf8"));
  if (!m) throw new Error(`no setup code in ${log}: Core was not fresh`);
  return m[1]!;
}

export interface OwnerSecrets {
  username: string;
  password: string;
  totpSecret: string;
  recoveryCodes: string[];
}

export function ownerSecrets(): OwnerSecrets {
  return JSON.parse(readFileSync(OWNER_SECRETS, "utf8")) as OwnerSecrets;
}

// ── TOTP (RFC 6238, SHA-1, 6 digits, 30 s), same as core/internal/iam/totp.go ──────────────────────

function base32(secret: string): Buffer {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let bits = "";
  for (const ch of secret.replace(/[\s=]/g, "").toUpperCase())
    bits += alphabet.indexOf(ch).toString(2).padStart(5, "0");
  const out: number[] = [];
  for (let i = 0; i + 8 <= bits.length; i += 8) out.push(parseInt(bits.slice(i, i + 8), 2));
  return Buffer.from(out);
}

export function totpAt(secret: string, step: number): string {
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(step));
  const h = createHmac("sha1", base32(secret)).update(counter).digest();
  const o = h[h.length - 1]! & 15;
  return String((h.readUInt32BE(o) & 0x7fffffff) % 1_000_000).padStart(6, "0");
}

const lastStep = new Map<string, number>();

/**
 * A code Core will accept now. Core takes the current step ±1 but never the same step twice per account,
 * so a second code within 30 s uses the next step, and a third waits for the clock.
 */
export async function nextTotp(secret: string): Promise<string> {
  for (;;) {
    const now = Math.floor(Date.now() / 30_000);
    const step = Math.max(now, (lastStep.get(secret) ?? -1) + 1);
    if (step <= now + 1) {
      lastStep.set(secret, step);
      return totpAt(secret, step);
    }
    await new Promise((r) => setTimeout(r, 1000));
  }
}

// ── Management API as a given user ───────────────────────────────────────────────────────────────

export const BASE_URL = process.env.HAZELMINX_E2E_URL ?? "http://localhost:5173";

async function csrfOf(ctx: APIRequestContext): Promise<string> {
  const state = await ctx.storageState();
  return state.cookies.find((c) => c.name === "hazelminx_csrf")?.value ?? "";
}

/** POSTs/PUTs/DELETEs with the CSRF header, throwing on an error status. */
export async function call(ctx: APIRequestContext, method: string, path: string, data?: unknown): Promise<unknown> {
  const res = await ctx.fetch(`/management/v1${path}`, {
    method,
    data,
    headers: { "X-CSRF-Token": await csrfOf(ctx), Origin: new URL(BASE_URL).origin },
  });
  const text = await res.text();
  if (!res.ok()) throw new Error(`${method} ${path} → ${res.status()} ${text}`);
  return text ? (JSON.parse(text) as unknown) : undefined;
}

export interface TestUser {
  username: string;
  password: string;
  role: string;
  totpSecret?: string;
  recoveryCodes?: string[];
}

/** An account created by the owner, unique per call. Scopes are global (roles only matter here for enforcement). */
export async function createUser(role: string, prefix = role): Promise<TestUser> {
  const owner = await request.newContext({ baseURL: BASE_URL, storageState: OWNER_STATE });
  try {
    const username = `${prefix.replace(/_/g, "-")}-${randomBytes(3).toString("hex")}`;
    const password = `pw-${randomBytes(8).toString("hex")}`;
    const created = (await call(owner, "POST", "/users", { username, password, displayName: "" })) as {
      user: { id: string };
    };
    await call(owner, "POST", `/users/${created.user.id}/roles`, { role, scope: { kind: "global", id: "*" } });
    return { username, password, role };
  } finally {
    await owner.dispose();
  }
}

/** Signs `user` in through the API (password, then TOTP when they have one). */
export async function apiSignIn(user: TestUser): Promise<APIRequestContext> {
  const ctx = await request.newContext({ baseURL: BASE_URL });
  const res = (await call(ctx, "POST", "/auth/login", { username: user.username, password: user.password })) as {
    step: string;
    challengeToken?: string;
  };
  if (res.step === "second-factor") {
    await call(ctx, "POST", "/auth/login/second-factor", {
      challengeToken: res.challengeToken,
      method: "totp",
      code: await nextTotp(user.totpSecret!),
    });
  }
  return ctx;
}

/** Gives `user` an authenticator app through the API and keeps its secret and recovery codes. */
export async function enrollTotp(user: TestUser): Promise<TestUser> {
  const ctx = await apiSignIn(user);
  try {
    const e = (await call(ctx, "POST", "/me/second-factors/totp")) as { secret: string };
    const r = (await call(ctx, "POST", "/me/second-factors/totp/confirm", { code: await nextTotp(e.secret) })) as {
      recoveryCodes?: string[];
    };
    return { ...user, totpSecret: e.secret, recoveryCodes: r.recoveryCodes };
  } finally {
    // Leave no session behind: tests count the account's signed-in devices.
    await call(ctx, "POST", "/auth/logout").catch(() => undefined);
    await ctx.dispose();
  }
}

/** A browser context signed in as `user` through the API. */
export async function signedInContext(browser: Browser, user: TestUser): Promise<BrowserContext> {
  const api = await apiSignIn(user);
  const storageState = await api.storageState();
  await api.dispose();
  return browser.newContext({ storageState });
}

/** Fills the login form; stops after the password step. */
export async function typePassword(page: Page, user: TestUser) {
  await page.getByLabel("帳號", { exact: true }).fill(user.username);
  await page.getByLabel("密碼", { exact: true }).fill(user.password);
  await page.getByRole("button", { name: "繼續" }).click();
}

/** A WebAuthn authenticator in Chromium (CDP): CTAP2, internal, resident keys, user verification. */
export async function virtualAuthenticator(page: Page) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("WebAuthn.enable");
  const { authenticatorId } = await cdp.send("WebAuthn.addVirtualAuthenticator", {
    options: {
      protocol: "ctap2",
      transport: "internal",
      hasResidentKey: true,
      hasUserVerification: true,
      isUserVerified: true,
      automaticPresenceSimulation: true,
    },
  });
  return {
    async credentials() {
      const r = await cdp.send("WebAuthn.getCredentials", { authenticatorId });
      return r.credentials;
    },
  };
}

// ── A host enrolling through Core's agent listener (contracts/README.md 註冊) ─────────────────────

/** Core's agent listener: e2e/start-core.sh's HAZELMINX_E2E_AGENT_ADDR, or HAZELMINX_E2E_AGENT_URL for an external Core. */
export const AGENT_URL =
  process.env.HAZELMINX_E2E_AGENT_URL ?? `https://${process.env.HAZELMINX_E2E_AGENT_ADDR ?? "127.0.0.1:19891"}`;

export interface FakeHost {
  publicKey: string;
  hostname: string;
  machineId: string;
}

/** A host identity: a new Ed25519 key (raw public key, base64url without padding) and made-up host facts. */
export function fakeHost(over: Partial<FakeHost> = {}): FakeHost {
  const { publicKey } = generateKeyPairSync("ed25519");
  const x = (publicKey.export({ format: "jwk" }) as { x: string }).x;
  const tag = randomBytes(3).toString("hex");
  return { publicKey: x, hostname: `e2e-host-${tag}`, machineId: randomBytes(16).toString("hex"), ...over };
}

/**
 * Sends POST /agent/v1/enroll as `hazelminx-agent enroll` does after it fetched the CA. The listener's
 * certificate comes from Core's own CA, which this test does not pin, so TLS verification is off here.
 */
export async function enrollAs(
  host: FakeHost,
  code: string,
): Promise<{ status: number; body: Record<string, unknown> }> {
  const ctx = await request.newContext({ baseURL: AGENT_URL, ignoreHTTPSErrors: true });
  try {
    const res = await ctx.post("/agent/v1/enroll", {
      data: {
        code,
        nodePublicKey: host.publicKey,
        hostname: host.hostname,
        machineId: host.machineId,
        os: "Debian 13",
        arch: "arm64",
        agentVersion: "0.1.0",
      },
    });
    return { status: res.status(), body: (await res.json()) as Record<string, unknown> };
  } finally {
    await ctx.dispose();
  }
}
