import { expect, test } from "@playwright/test";
import {
  BASE_URL,
  createUser,
  enrollTotp,
  nextTotp,
  signedInContext,
  typePassword,
  virtualAuthenticator,
  type TestUser,
} from "./core";
import { scanIssues, watchErrors } from "./scan";

const h1 = (page: import("@playwright/test").Page) => page.getByRole("heading", { level: 1 });

test.describe("signed out", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test("any page goes to login, sign-in returns to it, sign-out goes back to login", async ({ page }) => {
    const errors = watchErrors(page, { signedOut: true });
    const user = await createUser("auditor");
    await page.goto("/storage/backups/files?path=%2Fetc");
    await expect(h1(page)).toHaveText("登入管理介面");
    await expect(page).toHaveURL(/\/login\?next=%2Fstorage%2Fbackups%2Ffiles%3Fpath%3D%252Fetc$/);
    await expect(page.locator("header.hx-top")).not.toContainText("搜尋或下指令");
    await expect(page.getByRole("button", { name: "語言" })).toBeVisible();
    for (const width of [1440, 360]) {
      await page.setViewportSize({ width, height: 900 });
      expect(await scanIssues(page), `login @${width}`).toEqual([]);
    }
    await page.setViewportSize({ width: 1440, height: 900 });

    await typePassword(page, user);
    await expect(h1(page)).toHaveText("儲存與備份");
    await expect(page).toHaveURL(/\/storage\/backups\/files\?path=%2Fetc$/);
    await expect(page.getByRole("button", { name: `${user.username} · 稽核者` })).toBeVisible();

    await page.getByRole("button", { name: `${user.username} · 稽核者` }).click();
    await page.getByRole("menuitem", { name: "登出" }).click();
    await expect(h1(page)).toHaveText("登入管理介面");
    await expect(page).toHaveURL(/\/login$/);
    // The old session is gone: opening a page asks to sign in again.
    await page.goto("/settings");
    await expect(page).toHaveURL(/\/login\?next=%2Fsettings$/);
    expect(errors).toEqual([]);
  });

  test("a wrong password says so without revealing which part was wrong", async ({ page }) => {
    const user = await createUser("auditor");
    await page.goto("/login");
    await typePassword(page, { ...user, password: "wrong-password-123" });
    await expect(page.getByText("帳號或密碼不對")).toBeVisible();
    await typePassword(page, { ...user, username: "nobody-here" });
    await expect(page.getByText("帳號或密碼不對")).toBeVisible();
  });

  test("second step with the authenticator: a wrong code, then the right one", async ({ page }) => {
    const user = await enrollTotp(await createUser("auditor"));
    await page.goto("/settings");
    await typePassword(page, user);
    await expect(h1(page)).toHaveText("驗證身分");
    await expect(page.getByText(`${user.username} · 第二步`)).toBeVisible();
    // This account has no passkey.
    await expect(page.getByRole("button", { name: "使用 Passkey" })).toHaveCount(0);
    expect(await scanIssues(page)).toEqual([]);

    const code = page.getByRole("textbox", { name: "輸入驗證器上的 6 位數" });
    await code.fill("000000");
    await expect(page.getByText("碼不對")).toBeVisible();
    await code.fill(await nextTotp(user.totpSecret!));
    await expect(h1(page)).toHaveText("平台設定");
  });

  test("second step with a recovery code, which then cannot be used again", async ({ page, browser }) => {
    const user = await enrollTotp(await createUser("auditor"));
    const recovery = user.recoveryCodes![0]!;
    await page.goto("/login");
    await typePassword(page, user);
    await page.getByRole("button", { name: "使用復原碼" }).click();
    await page.getByLabel("復原碼", { exact: true }).fill(recovery);
    await page.getByRole("button", { name: "確認" }).click();
    await expect(h1(page)).toHaveText("總覽");

    const other = await (await browser.newContext()).newPage();
    await other.goto("/login");
    await typePassword(other, user);
    await other.getByRole("button", { name: "使用復原碼" }).click();
    await other.getByLabel("復原碼", { exact: true }).fill(recovery);
    await other.getByRole("button", { name: "確認" }).click();
    await expect(other.getByText("碼不對")).toBeVisible();
    await other.context().close();
  });

  test("a role that needs a second factor is held to enrolment until it adds one", async ({ page }) => {
    const errors = watchErrors(page, { signedOut: true });
    const user = await createUser("host_operator");
    await page.goto("/containers");
    await typePassword(page, user);
    await expect(h1(page)).toHaveText("先設定二次驗證");
    await expect(page).toHaveURL(/\/enroll\?next=%2Fcontainers$/);
    await expect(page.getByText("主機維運者必須使用二次驗證")).toBeVisible();
    await expect(page.getByText("設定完成前其他頁面都不能開啟")).toBeVisible();
    for (const width of [1440, 360]) {
      await page.setViewportSize({ width, height: 900 });
      expect(await scanIssues(page), `enroll @${width}`).toEqual([]);
    }
    await page.setViewportSize({ width: 1440, height: 900 });

    // Other pages stay closed.
    await page.goto("/settings");
    await expect(h1(page)).toHaveText("先設定二次驗證");

    await page.getByText("無法掃描？顯示金鑰").click();
    const secret = (await page.locator(".hx-secret").innerText()).replace(/\s/g, "");
    await page.getByRole("textbox", { name: "輸入 App 上的 6 位數" }).fill(await nextTotp(secret));
    await expect(h1(page)).toHaveText("存好復原碼");
    await page.getByRole("checkbox", { name: "我已經存在安全的地方" }).click();
    await page.getByRole("button", { name: "繼續" }).click();
    await expect(h1(page)).toHaveText("平台設定");
    expect(errors).toEqual([]);
  });

  test("passkey: add one, sign in with it, see it listed", async ({ page }) => {
    const auth = await virtualAuthenticator(page);
    const user: TestUser = await createUser("auditor");
    await page.goto("/account/security");
    await typePassword(page, user);
    await expect(h1(page)).toHaveText(`帳號安全 · ${user.username}`);

    await page.getByRole("button", { name: "新增 Passkey" }).click();
    const dialog = page.getByRole("dialog", { name: "新增 Passkey" });
    await expect(dialog.getByText("這個網址可以使用 Passkey")).toBeVisible();
    expect(await scanIssues(page)).toEqual([]);
    await dialog.getByLabel("裝置名稱", { exact: true }).fill("E2E virtual key");
    await dialog.getByRole("button", { name: "開始" }).click();

    // First factor of the account: the recovery codes come once.
    const codes = page.getByRole("dialog", { name: "存好復原碼" });
    await expect(codes.locator(".hx-codes-list li")).toHaveCount(10);
    await codes.getByRole("checkbox", { name: "我已經存在安全的地方" }).click();
    await codes.getByRole("button", { name: "完成" }).click();
    const factors = page.getByRole("region", { name: "二次驗證 · 1" });
    await expect(factors.getByText("E2E virtual key")).toBeVisible();
    await expect(factors.getByText(/^localhost · .* 綁定$/)).toBeVisible();
    expect(await auth.credentials()).toHaveLength(1);

    await page.getByRole("button", { name: `${user.username} · 稽核者` }).click();
    await page.getByRole("menuitem", { name: "登出" }).click();
    await typePassword(page, user);
    await expect(h1(page)).toHaveText("驗證身分");
    await page.getByRole("button", { name: "使用 Passkey" }).click();
    await expect(h1(page)).toHaveText("總覽");

    await page.goto("/account/security");
    await expect(page.getByRole("region", { name: "二次驗證 · 1" }).getByText("剛剛")).toBeVisible();
    await expect(page.getByRole("region", { name: /登入中的裝置/ }).getByText("目前這一個")).toBeVisible();
  });

  test("passkey: a panel opened by IP address explains why it cannot add one", async ({ page }) => {
    const user = await createUser("auditor");
    const byIp = new URL("/account/security", BASE_URL);
    byIp.hostname = "127.0.0.1";
    await page.goto(byIp.href);
    await typePassword(page, user);
    await page.getByRole("button", { name: "新增 Passkey" }).click();
    const dialog = page.getByRole("dialog", { name: "新增 Passkey" });
    await expect(dialog.getByText("用 IP 開的面板不能綁定")).toBeVisible();
    await expect(dialog.getByText("127.0.0.1 · Passkey 需要網域")).toBeVisible();
    await expect(dialog.getByRole("button", { name: "開始" })).toBeDisabled();
  });
});

test.describe("account security", () => {
  test("lists the owner's factors, codes, devices and idle timeout, and scans clean", async ({ page }) => {
    const errors = watchErrors(page);
    await page.goto("/account/security");
    await expect(h1(page)).toHaveText("帳號安全 · owner");
    await expect(page.getByRole("region", { name: "二次驗證 · 1" }).getByText("驗證器 App")).toBeVisible();
    await expect(page.getByText(/^剩 \d+ \/ 10 個$/)).toBeVisible();
    await expect(page.getByRole("region", { name: /^登入中的裝置 · \d+$/ }).getByText("目前這一個")).toBeVisible();
    await expect(page.getByText("12 小時（預設）")).toBeVisible();
    for (const [width, mode] of [
      [1440, "dark"],
      [1024, "dark"],
      [768, "dark"],
      [360, "dark"],
      [1440, "light"],
      [360, "light"],
    ] as const) {
      await page.setViewportSize({ width, height: 900 });
      await page.evaluate((m) => document.documentElement.setAttribute("data-mode", m), mode);
      expect(await scanIssues(page), `@${width} ${mode}`).toEqual([]);
    }
    // The last factor of an owner cannot be removed.
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.getByRole("button", { name: "移除 驗證器 App" }).click();
    const dialog = page.getByRole("dialog", { name: "移除 驗證器 App？" });
    await expect(dialog.getByText("最後一個二次驗證不能移除")).toBeVisible();
    await expect(dialog.getByRole("button", { name: "移除" })).toBeDisabled();
    expect(await scanIssues(page)).toEqual([]);
    await dialog.getByRole("button", { name: "取消" }).click();
    expect(errors).toEqual([]);
  });

  test("signing out other devices ends their sessions at once", async ({ browser }) => {
    const user = await enrollTotp(await createUser("auditor"));
    const a = await signedInContext(browser, user);
    const b = await signedInContext(browser, user);
    const pageA = await a.newPage();
    const pageB = await b.newPage();
    await pageB.goto("/settings");
    await expect(h1(pageB)).toHaveText("平台設定");

    await pageA.goto("/account/security");
    const devices = pageA.getByRole("region", { name: "登入中的裝置 · 2" });
    await expect(devices).toBeVisible();
    await pageA.getByRole("button", { name: "登出其他所有裝置" }).click();
    await expect(pageA.getByRole("region", { name: "登入中的裝置 · 1" })).toBeVisible();

    await pageB.goto("/nodes");
    await expect(h1(pageB)).toHaveText("登入管理介面");
    await expect(pageB).toHaveURL(/\/login\?next=%2Fnodes$/);
    await a.close();
    await b.close();
  });

  test("revoking one device from the list", async ({ browser }) => {
    const user = await enrollTotp(await createUser("auditor"));
    const a = await signedInContext(browser, user);
    const b = await signedInContext(browser, user);
    const pageA = await a.newPage();
    await pageA.goto("/account/security");
    const devices = pageA.getByRole("region", { name: "登入中的裝置 · 2" });
    await devices.getByRole("button", { name: /^撤銷 / }).click();
    await expect(pageA.getByRole("region", { name: "登入中的裝置 · 1" })).toBeVisible();
    const pageB = await b.newPage();
    await pageB.goto("/settings");
    await expect(h1(pageB)).toHaveText("登入管理介面");
    await a.close();
    await b.close();
  });

  test("the idle timeout is saved", async ({ browser }) => {
    const user = await enrollTotp(await createUser("auditor"));
    const ctx = await signedInContext(browser, user);
    const page = await ctx.newPage();
    await page.goto("/account/security");
    await page.getByRole("combobox", { name: "閒置多久自動登出" }).click();
    await page.getByRole("option", { name: "30 分鐘" }).click();
    await expect(page.getByRole("combobox", { name: "閒置多久自動登出" })).toHaveText(/30 分鐘/);
    await page.reload();
    await expect(page.getByRole("combobox", { name: "閒置多久自動登出" })).toHaveText(/30 分鐘/);
    await ctx.close();
  });
});
