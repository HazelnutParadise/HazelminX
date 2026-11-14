import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { afterEach, beforeAll, describe, expect, it, onTestFinished, vi } from "vite-plus/test";
import { AppProviders } from "../app/App";
import { routes } from "../app/routes";
import { initI18n } from "../i18n";
import { apiError, fakeCore, type FakeReply } from "../test/fakeCore";
import { signOutRetry } from "./session";

beforeAll(() => {
  initI18n("zh-TW");
  // The shell picks desktop or phone navigation from a media query; jsdom has no matchMedia.
  window.matchMedia ??= (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList;
});

afterEach(() => {
  vi.unstubAllGlobals();
  signOutRetry.delaysMs = [1000, 3000, 6000];
});

const NOW = "2026-11-12T03:42:06Z";
const user = {
  id: "u1",
  username: "tim",
  displayName: "",
  status: "active",
  createdAt: NOW,
  updatedAt: NOW,
  version: 1,
};

function sessionBody(role = "owner", enrollmentRequired = false) {
  return {
    kind: "session",
    user,
    assignments: [{ id: "a1", userId: "u1", role, scope: { kind: "global" }, createdAt: NOW }],
    secondFactorEnrollmentRequired: enrollmentRequired,
  };
}

const signedIn = (enrollmentRequired = false): FakeReply => ({
  status: 200,
  body: { step: "signed-in", user, secondFactorEnrollmentRequired: enrollmentRequired },
});

/** A Core whose session flips when sign-in succeeds. */
function coreWithSession(initial: { signedIn: boolean; role?: string; enroll?: boolean }) {
  const state = { ...initial };
  const core = fakeCore({
    "GET /management/v1/setup": { status: 200, body: { needsSetup: false } },
    "GET /management/v1/auth/session": () =>
      state.signedIn ? { status: 200, body: sessionBody(state.role, state.enroll) } : apiError(401, "UNAUTHENTICATED"),
    "POST /management/v1/auth/logout": () => {
      state.signedIn = false;
      return { status: 204 };
    },
  });
  return { core, state };
}

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  render(
    <AppProviders status={{}}>
      <RouterProvider router={router} />
    </AppProviders>,
  );
  return router;
}

const heading = (name: string) => screen.findByRole("heading", { level: 1, name });

describe("sign-in", () => {
  it("sends a signed-out visitor to login and back to the page after a password-only sign-in", async () => {
    const { core, state } = coreWithSession({ signedIn: false });
    core.on("POST /management/v1/auth/login", () => {
      state.signedIn = true;
      return signedIn();
    });
    const router = renderAt("/settings?tab=https");
    await heading("登入管理介面");
    expect(router.state.location.pathname).toBe("/login");
    expect(router.state.location.search).toBe("?next=%2Fsettings%3Ftab%3Dhttps");

    const u = userEvent.setup();
    await u.type(screen.getByLabelText("帳號"), "tim");
    await u.type(screen.getByLabelText("密碼"), "correct horse battery");
    await u.click(screen.getByRole("button", { name: "繼續" }));

    await heading("平台設定");
    expect(router.state.location.pathname + router.state.location.search).toBe("/settings?tab=https");
    expect(core.callsTo("POST /management/v1/auth/login")[0]!.body).toEqual({
      username: "tim",
      password: "correct horse battery",
    });
  });

  it("says the password is wrong without saying which part", async () => {
    const { core } = coreWithSession({ signedIn: false });
    core.on("POST /management/v1/auth/login", apiError(401, "INVALID_CREDENTIALS"));
    renderAt("/login");
    const u = userEvent.setup();
    await u.type(await screen.findByLabelText("帳號"), "tim");
    await u.type(screen.getByLabelText("密碼"), "nope nope nope");
    await u.click(screen.getByRole("button", { name: "繼續" }));
    expect(await screen.findByText("帳號或密碼不對")).toBeInTheDocument();
    expect(screen.getByLabelText("密碼")).toHaveValue("");
  });

  it("counts down a throttle and keeps the button off meanwhile", async () => {
    const { core } = coreWithSession({ signedIn: false });
    core.on("POST /management/v1/auth/login", apiError(429, "TOO_MANY_ATTEMPTS", { retryAfterSeconds: 272 }));
    renderAt("/login");
    const u = userEvent.setup();
    await u.type(await screen.findByLabelText("帳號"), "tim");
    await u.type(screen.getByLabelText("密碼"), "whatever-123456");
    await u.click(screen.getByRole("button", { name: "繼續" }));
    expect(await screen.findByText("錯太多次 · 4:32 後再試")).toBeInTheDocument();
    await u.type(screen.getByLabelText("密碼"), "again-123456");
    expect(screen.getByRole("button", { name: "繼續" })).toBeDisabled();
  });

  it("second step: a wrong code is reported, the right one signs in; no passkey button without a passkey", async () => {
    const { core, state } = coreWithSession({ signedIn: false });
    core.on("POST /management/v1/auth/login", {
      status: 200,
      body: {
        step: "second-factor",
        challengeToken: "ch-1",
        methods: ["totp", "recovery-code"],
        challengeExpiresAt: "2099-01-01T00:00:00Z",
        secondFactorEnrollmentRequired: false,
      },
    });
    core.on("POST /management/v1/auth/login/second-factor", (c) => {
      const body = c.body as { code: string };
      if (body.code === "111111") return apiError(401, "SECOND_FACTOR_INVALID");
      state.signedIn = true;
      return signedIn();
    });
    const router = renderAt("/nodes");
    const u = userEvent.setup();
    await u.type(await screen.findByLabelText("帳號"), "tim");
    await u.type(screen.getByLabelText("密碼"), "correct horse battery");
    await u.click(screen.getByRole("button", { name: "繼續" }));

    await heading("驗證身分");
    expect(screen.getByText("tim · 第二步")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "使用 Passkey" })).toBeNull();
    const code = screen.getByRole("textbox", { name: "輸入驗證器上的 6 位數" });
    await u.type(code, "111111");
    expect(await screen.findByText("碼不對")).toBeInTheDocument();
    expect(code).toHaveValue("");

    await u.type(code, "222222");
    await heading("主機");
    expect(router.state.location.pathname).toBe("/nodes");
    expect(core.callsTo("POST /management/v1/auth/login/second-factor").map((c) => c.body)).toEqual([
      { challengeToken: "ch-1", method: "totp", code: "111111" },
      { challengeToken: "ch-1", method: "totp", code: "222222" },
    ]);
  });

  it("second step: a recovery code is sent as recovery-code", async () => {
    const { core, state } = coreWithSession({ signedIn: false });
    core.on("POST /management/v1/auth/login", {
      status: 200,
      body: {
        step: "second-factor",
        challengeToken: "ch-2",
        methods: ["totp", "recovery-code"],
        secondFactorEnrollmentRequired: false,
      },
    });
    core.on("POST /management/v1/auth/login/second-factor", () => {
      state.signedIn = true;
      return signedIn();
    });
    renderAt("/login");
    const u = userEvent.setup();
    await u.type(await screen.findByLabelText("帳號"), "tim");
    await u.type(screen.getByLabelText("密碼"), "correct horse battery");
    await u.click(screen.getByRole("button", { name: "繼續" }));
    await u.click(await screen.findByRole("button", { name: "使用復原碼" }));
    await u.type(screen.getByLabelText("復原碼"), " 7KQ2-9MXD ");
    await u.click(screen.getByRole("button", { name: "確認" }));
    await heading("總覽");
    expect(core.callsTo("POST /management/v1/auth/login/second-factor")[0]!.body).toEqual({
      challengeToken: "ch-2",
      method: "recovery-code",
      code: "7KQ2-9MXD",
    });
  });

  it("second step: an expired challenge returns to the password with a notice", async () => {
    const { core } = coreWithSession({ signedIn: false });
    core.on("POST /management/v1/auth/login", {
      status: 200,
      body: { step: "second-factor", challengeToken: "ch-3", methods: ["totp"], secondFactorEnrollmentRequired: false },
    });
    core.on("POST /management/v1/auth/login/second-factor", apiError(401, "LOGIN_CHALLENGE_INVALID"));
    renderAt("/login");
    const u = userEvent.setup();
    await u.type(await screen.findByLabelText("帳號"), "tim");
    await u.type(screen.getByLabelText("密碼"), "correct horse battery");
    await u.click(screen.getByRole("button", { name: "繼續" }));
    await u.type(await screen.findByRole("textbox", { name: "輸入驗證器上的 6 位數" }), "123456");
    await heading("登入管理介面");
    expect(screen.getByText("這次登入已逾時 · 請重新輸入密碼")).toBeInTheDocument();
    expect(screen.getByLabelText("帳號")).toHaveValue("tim");
  });

  it("second step: shows the passkey button when the account has one for this address", async () => {
    vi.stubGlobal("PublicKeyCredential", function PublicKeyCredential() {});
    vi.stubGlobal("isSecureContext", true);
    const { core } = coreWithSession({ signedIn: false });
    core.on("POST /management/v1/auth/login", {
      status: 200,
      body: {
        step: "second-factor",
        challengeToken: "ch-4",
        methods: ["passkey", "recovery-code"],
        secondFactorEnrollmentRequired: false,
      },
    });
    renderAt("/login");
    const u = userEvent.setup();
    await u.type(await screen.findByLabelText("帳號"), "tim");
    await u.type(screen.getByLabelText("密碼"), "correct horse battery");
    await u.click(screen.getByRole("button", { name: "繼續" }));
    expect(await screen.findByRole("button", { name: "使用 Passkey" })).toBeInTheDocument();
    expect(screen.queryByRole("textbox", { name: "輸入驗證器上的 6 位數" })).toBeNull();
    expect(screen.getByRole("button", { name: "使用復原碼" })).toBeInTheDocument();
  });
});

describe("sign-in edge cases", () => {
  const secondStep = (methods: string[], extra: Record<string, unknown> = {}) => ({
    status: 200,
    body: { step: "second-factor", challengeToken: "ch-x", methods, secondFactorEnrollmentRequired: false, ...extra },
  });

  it("a browser clock far ahead of Core does not end the second step early", async () => {
    const { core, state } = coreWithSession({ signedIn: false });
    // Core says the challenge expires in 5 minutes of its own time, which is already past for this browser.
    core.on(
      "POST /management/v1/auth/login",
      secondStep(["totp"], { challengeExpiresAt: new Date(Date.now() - 60_000).toISOString() }),
    );
    core.on("POST /management/v1/auth/login/second-factor", () => {
      state.signedIn = true;
      return signedIn();
    });
    renderAt("/login");
    const u = userEvent.setup();
    await u.type(await screen.findByLabelText("帳號"), "tim");
    await u.type(screen.getByLabelText("密碼"), "correct horse battery");
    await u.click(screen.getByRole("button", { name: "繼續" }));
    await heading("驗證身分");
    await new Promise((r) => setTimeout(r, 1200));
    expect(screen.queryByText("這次登入已逾時 · 請重新輸入密碼")).toBeNull();
    await u.type(screen.getByRole("textbox", { name: "輸入驗證器上的 6 位數" }), "123456");
    await heading("總覽");
  });

  it("explains a passkey that cannot be used on this page, and an account with no usable method", async () => {
    const { core } = coreWithSession({ signedIn: false });
    core.on("POST /management/v1/auth/login", secondStep(["passkey", "recovery-code"]));
    renderAt("/login");
    const u = userEvent.setup();
    await u.type(await screen.findByLabelText("帳號"), "tim");
    await u.type(screen.getByLabelText("密碼"), "correct horse battery");
    await u.click(screen.getByRole("button", { name: "繼續" }));
    // jsdom has no PublicKeyCredential: the passkey is there but this browser cannot use it.
    expect(await screen.findByText("這個瀏覽器不能使用 Passkey")).toBeInTheDocument();
    expect(screen.getByLabelText("復原碼")).toBeInTheDocument();

    await u.click(screen.getByRole("button", { name: "回上一步" }));
    core.on("POST /management/v1/auth/login", secondStep([]));
    await u.type(screen.getByLabelText("密碼"), "correct horse battery");
    await u.click(screen.getByRole("button", { name: "繼續" }));
    expect(await screen.findByText("這個帳號在這個網址沒有可用的二次驗證，請找管理員協助")).toBeInTheDocument();
  });

  it("the throttle wait is announced once, not every second", async () => {
    const { core } = coreWithSession({ signedIn: false });
    core.on("POST /management/v1/auth/login", apiError(429, "TOO_MANY_ATTEMPTS", { retryAfterSeconds: 90 }));
    renderAt("/login");
    const u = userEvent.setup();
    await u.type(await screen.findByLabelText("帳號"), "tim");
    await u.type(screen.getByLabelText("密碼"), "whatever-123456");
    await u.click(screen.getByRole("button", { name: "繼續" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("錯太多次，1:30 後才能再試");
    expect(screen.getByText("錯太多次 · 1:30 後再試").closest("[aria-hidden]")).not.toBeNull();
  });
});

describe("first run and enrolment", () => {
  it("login opens setup; setup signs in, enrols an authenticator, shows the codes once, then the overview", async () => {
    const state = { needsSetup: true, signedIn: false, enroll: true };
    const core = fakeCore({
      "GET /management/v1/setup": () => ({ status: 200, body: { needsSetup: state.needsSetup } }),
      "GET /management/v1/auth/session": () =>
        state.signedIn ? { status: 200, body: sessionBody("owner", state.enroll) } : apiError(401, "UNAUTHENTICATED"),
      "POST /management/v1/setup": () => {
        state.needsSetup = false;
        state.signedIn = true;
        return { status: 201, body: { step: "signed-in", user, secondFactorEnrollmentRequired: true } };
      },
      "POST /management/v1/me/second-factors/totp": {
        status: 201,
        body: {
          secret: "JBSWY3DPEHPK3PXP",
          uri: "otpauth://totp/HazelminX:tim?secret=JBSWY3DPEHPK3PXP&issuer=HazelminX",
          digits: 6,
          periodSeconds: 30,
          serverTime: new Date().toISOString(),
          expiresAt: new Date(Date.now() + 600_000).toISOString(),
        },
      },
      "POST /management/v1/me/second-factors/totp/confirm": () => {
        state.enroll = false;
        return { status: 200, body: { recoveryCodes: ["7KQ2-9MXD", "P4WN-3RCH"] } };
      },
    });
    const router = renderAt("/login");
    await heading("建立擁有者帳號");
    const u = userEvent.setup();
    await u.type(screen.getByLabelText("設定碼（安裝結束時印在終端機）"), "HZX-SETUP-4KQ2");
    await u.type(screen.getByLabelText("帳號"), "tim");
    await u.type(screen.getByLabelText("密碼"), "short");
    expect(screen.getByText("密碼至少要 12 個字元")).toBeInTheDocument();
    await u.type(screen.getByLabelText("密碼"), "-but-now-long");
    await u.click(screen.getByRole("button", { name: "繼續" }));

    await heading("綁定二次驗證");
    expect(router.state.location.search).toBe("?setup=1");
    // The QR code appears once Core answered the enrolment request.
    expect(await screen.findByRole("img", { name: "驗證器 App 要掃的 QR 碼" })).toBeInTheDocument();
    expect(screen.getByText("JBSW Y3DP EHPK 3PXP")).toBeInTheDocument();
    await u.type(screen.getByRole("textbox", { name: "輸入 App 上的 6 位數" }), "482913");

    await heading("存好復原碼");
    expect(screen.getByText("7KQ2-9MXD")).toBeInTheDocument();
    const cont = screen.getByRole("button", { name: "繼續" });
    expect(cont).toBeDisabled();
    await u.click(screen.getByRole("checkbox", { name: "我已經存在安全的地方" }));
    await u.click(cont);

    await heading("開始使用");
    await u.click(screen.getByRole("button", { name: "先到總覽，之後再說" }));
    await heading("總覽");
    expect(core.callsTo("POST /management/v1/setup")[0]!.body).toEqual({
      setupCode: "HZX-SETUP-4KQ2",
      username: "tim",
      password: "short-but-now-long",
    });
    expect(core.callsTo("POST /management/v1/me/second-factors/totp")).toHaveLength(1);
    expect(core.callsTo("POST /management/v1/me/second-factors/totp/confirm")[0]!.body).toEqual({ code: "482913" });
  });

  it("setup reports a wrong setup code", async () => {
    fakeCore({
      "GET /management/v1/setup": { status: 200, body: { needsSetup: true } },
      "GET /management/v1/auth/session": apiError(401, "UNAUTHENTICATED"),
      "POST /management/v1/setup": apiError(403, "SETUP_CODE_INVALID"),
    });
    renderAt("/setup");
    const u = userEvent.setup();
    await u.type(await screen.findByLabelText("設定碼（安裝結束時印在終端機）"), "wrong");
    await u.type(screen.getByLabelText("帳號"), "tim");
    await u.type(screen.getByLabelText("密碼"), "long-enough-password");
    await u.click(screen.getByRole("button", { name: "繼續" }));
    expect(await screen.findByText("設定碼不對或已過期")).toBeInTheDocument();
  });

  it("holds an account whose role needs a second factor to enrolment, from any page", async () => {
    coreWithSession({ signedIn: true, role: "host_operator", enroll: true });
    const router = renderAt("/containers");
    await heading("先設定二次驗證");
    expect(router.state.location.pathname).toBe("/enroll");
    expect(router.state.location.search).toBe("?next=%2Fcontainers");
    expect(screen.getByText("主機維運者必須使用二次驗證")).toBeInTheDocument();
    expect(screen.getByText("設定完成前其他頁面都不能開啟")).toBeInTheDocument();
  });
});

describe("shell", () => {
  it("shows the signed-in user and signs out to the login page", async () => {
    const { core } = coreWithSession({ signedIn: true });
    const router = renderAt("/settings");
    await heading("平台設定");
    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: "tim · 擁有者" }));
    await u.click(await screen.findByRole("menuitem", { name: "登出" }));
    await heading("登入管理介面");
    expect(router.state.location.pathname + router.state.location.search).toBe("/login");
    expect(core.callsTo("POST /management/v1/auth/logout")).toHaveLength(1);
  });

  it("a session lost elsewhere sends the user to login, keeping the page", async () => {
    const { core, state } = coreWithSession({ signedIn: true });
    core.on("GET /management/v1/me/second-factors", () => {
      state.signedIn = false;
      return apiError(401, "UNAUTHENTICATED");
    });
    const router = renderAt("/account/security");
    await heading("登入管理介面");
    expect(router.state.location.search).toBe("?next=%2Faccount%2Fsecurity");
  });

  it("a sign-out Core did not confirm is retried without asking and then goes through", async () => {
    signOutRetry.delaysMs = [150, 150];
    const { core, state } = coreWithSession({ signedIn: true });
    core.on("POST /management/v1/auth/logout", apiError(502, "PLATFORM_DEGRADED"));
    renderAt("/settings");
    await heading("平台設定");
    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: "tim · 擁有者" }));
    await u.click(await screen.findByRole("menuitem", { name: "登出" }));
    expect(await screen.findByText("還沒確認登出成功，正在重試")).toBeInTheDocument();
    core.on("POST /management/v1/auth/logout", () => {
      state.signedIn = false;
      return { status: 204 };
    });
    await waitFor(() => expect(screen.queryByText("還沒確認登出成功，正在重試")).toBeNull());
    // The retry went through: no warning, no button to press.
    expect(screen.queryByText("沒有確認登出成功，這台裝置可能還在登入狀態")).toBeNull();
    expect(screen.getByRole("heading", { level: 1, name: "登入管理介面" })).toBeInTheDocument();
    expect(core.callsTo("POST /management/v1/auth/logout")).toHaveLength(2);
  });

  it("a sign-out that keeps failing says so after the retries and can be retried by hand", async () => {
    signOutRetry.delaysMs = [10, 10];
    const { core, state } = coreWithSession({ signedIn: true });
    core.on("POST /management/v1/auth/logout", apiError(502, "PLATFORM_DEGRADED"));
    renderAt("/settings");
    await heading("平台設定");
    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: "tim · 擁有者" }));
    await u.click(await screen.findByRole("menuitem", { name: "登出" }));
    expect(await screen.findByText("沒有確認登出成功，這台裝置可能還在登入狀態")).toBeInTheDocument();
    // The session still works, but the login page stays put instead of signing the user back in.
    await new Promise((r) => setTimeout(r, 300));
    expect(screen.getByRole("heading", { level: 1, name: "登入管理介面" })).toBeInTheDocument();

    core.on("POST /management/v1/auth/logout", () => {
      state.signedIn = false;
      return { status: 204 };
    });
    await u.click(screen.getByRole("button", { name: "再登出一次" }));
    await waitFor(() => expect(screen.queryByText("沒有確認登出成功，這台裝置可能還在登入狀態")).toBeNull());
    // The first attempt, two automatic retries, then the one asked for by hand.
    expect(core.callsTo("POST /management/v1/auth/logout")).toHaveLength(4);
  });

  it("explains when Core cannot be reached instead of showing a blank page", async () => {
    fakeCore({ "GET /management/v1/auth/session": apiError(502, "PLATFORM_DEGRADED") });
    renderAt("/");
    expect(await screen.findByRole("heading", { level: 1, name: "沒辦法確認登入狀態" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重新載入" })).toBeInTheDocument();
  });
});

describe("account security", () => {
  const factors = (over: Record<string, unknown> = {}) => ({
    status: 200,
    body: {
      totp: { confirmed: true, createdAt: "2026-11-02T01:00:00Z", confirmedAt: "2026-11-02T01:00:00Z" },
      passkeys: [
        {
          id: "pk1",
          name: "MacBook Pro Touch ID",
          rpId: "panel.example.tw",
          createdAt: "2026-11-02T01:00:00Z",
          lastUsedAt: new Date(Date.now() - 5 * 60_000).toISOString(),
        },
      ],
      recoveryCodesRemaining: 3,
      required: true,
      enrollmentRequired: false,
      ...over,
    },
  });
  const sessions = {
    status: 200,
    body: {
      sessions: [
        {
          id: "s1",
          sourceIp: "203.0.113.7",
          userAgent:
            "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/18.0 Safari/605.1.15",
          secondFactor: "passkey",
          createdAt: NOW,
          lastSeenAt: new Date().toISOString(),
          expiresAt: "2099-01-01T00:00:00Z",
          current: true,
        },
        {
          id: "s2",
          sourceIp: "198.51.100.20",
          userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/140.0 Safari/537.36",
          secondFactor: "totp",
          createdAt: NOW,
          lastSeenAt: new Date(Date.now() - 2 * 3600_000).toISOString(),
          expiresAt: "2099-01-01T00:00:00Z",
          current: false,
        },
      ],
    },
  };
  const settings = {
    status: 200,
    body: { idleTimeoutSeconds: 43200, default: true, minSeconds: 900, maxSeconds: 86400, absoluteMaxSeconds: 86400 },
  };

  function accountCore() {
    const { core } = coreWithSession({ signedIn: true });
    core.on("GET /management/v1/me/second-factors", factors());
    core.on("GET /management/v1/me/sessions", sessions);
    core.on("GET /management/v1/me/session-settings", settings);
    return core;
  }

  it("lists second factors, recovery codes, devices and the idle timeout", async () => {
    accountCore();
    renderAt("/account/security");
    await heading("帳號安全 · tim");
    const f = await screen.findByRole("region", { name: "二次驗證 · 2" });
    expect(within(f).getByText("MacBook Pro Touch ID")).toBeInTheDocument();
    expect(within(f).getByText("5 分鐘前")).toBeInTheDocument();
    expect(within(f).getByText("驗證器 App")).toBeInTheDocument();
    expect(within(f).getByText("從未使用")).toBeInTheDocument();
    // A confirmed authenticator exists, so only passkeys can be added.
    expect(within(f).queryByRole("button", { name: "新增驗證器" })).toBeNull();

    expect(screen.getByText("剩 3 / 10 個")).toBeInTheDocument();
    expect(screen.getByText("已用 7 個")).toBeInTheDocument();

    const d = await screen.findByRole("region", { name: "登入中的裝置 · 2" });
    expect(within(d).getByText("Safari · macOS")).toBeInTheDocument();
    expect(within(d).getByText("目前這一個")).toBeInTheDocument();
    expect(within(d).getByText("2 小時前")).toBeInTheDocument();
    expect(within(d).getByRole("button", { name: "撤銷 Chrome · Windows（198.51.100.20）" })).toBeInTheDocument();

    expect(await screen.findByText("12 小時（預設）")).toBeInTheDocument();
  });

  it("signs out one device or every other device", async () => {
    const core = accountCore();
    core.on("DELETE /management/v1/me/sessions/s2", { status: 204 });
    core.on("POST /management/v1/me/sessions/revoke-others", { status: 200, body: { revoked: 1 } });
    renderAt("/account/security");
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "撤銷 Chrome · Windows（198.51.100.20）" }));
    await waitFor(() => expect(core.callsTo("DELETE /management/v1/me/sessions/s2")).toHaveLength(1));
    await u.click(screen.getByRole("button", { name: "登出其他所有裝置" }));
    await waitFor(() => expect(core.callsTo("POST /management/v1/me/sessions/revoke-others")).toHaveLength(1));
    // Both refresh the list.
    await waitFor(() => expect(core.callsTo("GET /management/v1/me/sessions").length).toBeGreaterThanOrEqual(3));
  });

  it("will not remove the last second factor of a role that requires one", async () => {
    const core = accountCore();
    core.on("GET /management/v1/me/second-factors", factors({ passkeys: [] }));
    renderAt("/account/security");
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "移除 驗證器 App" }));
    const dialog = await screen.findByRole("dialog", { name: "移除 驗證器 App？" });
    expect(within(dialog).getByText("最後一個二次驗證不能移除")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "移除" })).toBeDisabled();
  });

  it("removes a passkey when another factor remains", async () => {
    const core = accountCore();
    core.on("DELETE /management/v1/me/second-factors/passkeys/pk1", { status: 204 });
    renderAt("/account/security");
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "移除 MacBook Pro Touch ID" }));
    const dialog = await screen.findByRole("dialog", { name: "移除 MacBook Pro Touch ID？" });
    expect(within(dialog).getByText("移除後還有 驗證器 App 可以用")).toBeInTheDocument();
    await u.click(within(dialog).getByRole("button", { name: "移除" }));
    await waitFor(() => expect(core.callsTo("DELETE /management/v1/me/second-factors/passkeys/pk1")).toHaveLength(1));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("asks the user to confirm who they are, then regenerates the recovery codes", async () => {
    const core = accountCore();
    let reauthed = false;
    core.on("POST /management/v1/me/recovery-codes", () =>
      reauthed
        ? { status: 201, body: { recoveryCodes: ["AAAA-BBBB", "CCCC-DDDD"] } }
        : apiError(403, "REAUTHENTICATION_REQUIRED"),
    );
    core.on("POST /management/v1/auth/reauthenticate", (c) => {
      const body = c.body as { password: string; code?: string };
      if (body.password !== "correct horse battery") return apiError(401, "INVALID_CREDENTIALS");
      reauthed = true;
      return { status: 204 };
    });
    renderAt("/account/security");
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "重新產生" }));
    const dialog = await screen.findByRole("dialog", { name: "重新產生復原碼前 · 確認是你" });
    await u.type(within(dialog).getByLabelText("密碼"), "wrong password");
    await u.type(within(dialog).getByRole("textbox", { name: "驗證器 6 位數" }), "111111");
    await u.click(within(dialog).getByRole("button", { name: "確認" }));
    expect(await within(dialog).findByText("密碼不對")).toBeInTheDocument();

    await u.clear(within(dialog).getByLabelText("密碼"));
    await u.type(within(dialog).getByLabelText("密碼"), "correct horse battery");
    await u.type(within(dialog).getByRole("textbox", { name: "驗證器 6 位數" }), "222222");
    await u.click(within(dialog).getByRole("button", { name: "確認" }));

    const codes = await screen.findByRole("dialog", { name: "存好復原碼" });
    expect(within(codes).getByText("AAAA-BBBB")).toBeInTheDocument();
    expect(core.callsTo("POST /management/v1/auth/reauthenticate").map((c) => c.body)).toEqual([
      { password: "wrong password", method: "totp", code: "111111" },
      { password: "correct horse battery", method: "totp", code: "222222" },
    ]);
    expect(core.callsTo("POST /management/v1/me/recovery-codes")).toHaveLength(2);
  });

  it("cancelling a new authenticator leaves Core alone (a removal would sign out other devices)", async () => {
    const core = accountCore();
    core.on("GET /management/v1/me/second-factors", factors({ totp: null }));
    core.on("POST /management/v1/me/second-factors/totp", {
      status: 201,
      body: {
        secret: "JBSWY3DPEHPK3PXP",
        uri: "otpauth://totp/HazelminX:tim?secret=JBSWY3DPEHPK3PXP&issuer=HazelminX",
        digits: 6,
        periodSeconds: 30,
        serverTime: new Date().toISOString(),
        expiresAt: new Date(Date.now() + 600_000).toISOString(),
      },
    });
    renderAt("/account/security");
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "新增驗證器" }));
    const dialog = await screen.findByRole("dialog", { name: "新增驗證器" });
    expect(await within(dialog).findByText("JBSW Y3DP EHPK 3PXP")).toBeInTheDocument();
    await u.click(within(dialog).getByRole("button", { name: "取消" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(core.callsTo("DELETE /management/v1/me/second-factors/totp")).toHaveLength(0);
  });

  it("closing the confirmation changes nothing", async () => {
    const core = accountCore();
    core.on("POST /management/v1/me/recovery-codes", apiError(403, "REAUTHENTICATION_REQUIRED"));
    renderAt("/account/security");
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "重新產生" }));
    const dialog = await screen.findByRole("dialog", { name: "重新產生復原碼前 · 確認是你" });
    await u.click(within(dialog).getByRole("button", { name: "取消" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(core.callsTo("POST /management/v1/me/recovery-codes")).toHaveLength(1);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("passkey dialog: a closed browser prompt says nothing was added and lets the user try again", async () => {
    vi.stubGlobal("PublicKeyCredential", function PublicKeyCredential() {});
    vi.stubGlobal("isSecureContext", true);
    const create = vi.fn().mockRejectedValue(new DOMException("closed", "NotAllowedError"));
    // jsdom has no navigator.credentials; give it one for this test only.
    Object.defineProperty(navigator, "credentials", { value: { create }, configurable: true });
    onTestFinished(() => {
      Reflect.deleteProperty(navigator, "credentials");
    });
    const core = accountCore();
    core.on("POST /management/v1/me/second-factors/passkeys/options", {
      status: 200,
      body: {
        publicKey: {
          rp: { id: "localhost", name: "HazelminX" },
          user: { id: "AQ", name: "tim", displayName: "tim" },
          challenge: "AQID",
          pubKeyCredParams: [{ type: "public-key", alg: -7 }],
        },
      },
    });
    renderAt("/account/security");
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "新增 Passkey" }));
    const dialog = await screen.findByRole("dialog", { name: "新增 Passkey" });
    expect(within(dialog).getByText("這個網址可以使用 Passkey")).toBeInTheDocument();
    await u.click(within(dialog).getByRole("button", { name: "開始" }));
    expect(await within(dialog).findByText("你取消了")).toBeInTheDocument();
    expect(within(dialog).getByText("瀏覽器的視窗被關掉，沒有綁定任何東西")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "開始" })).toBeEnabled();
    expect(create).toHaveBeenCalledTimes(1);
    expect(core.callsTo("POST /management/v1/me/second-factors/passkeys")).toHaveLength(0);
  });

  it("passkey dialog: Core refusing an IP-address panel blocks the start button", async () => {
    vi.stubGlobal("PublicKeyCredential", function PublicKeyCredential() {});
    vi.stubGlobal("isSecureContext", true);
    const core = accountCore();
    core.on("POST /management/v1/me/second-factors/passkeys/options", apiError(409, "NOT_AVAILABLE"));
    renderAt("/account/security");
    const u = userEvent.setup();
    await u.click(await screen.findByRole("button", { name: "新增 Passkey" }));
    const dialog = await screen.findByRole("dialog", { name: "新增 Passkey" });
    await u.click(within(dialog).getByRole("button", { name: "開始" }));
    expect(await within(dialog).findByText("用 IP 開的面板不能綁定")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "開始" })).toBeDisabled();
  });
});
