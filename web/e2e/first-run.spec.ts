import { writeFileSync } from "node:fs";
import { expect, test, type Page } from "@playwright/test";
import { nextTotp, OWNER_SECRETS, OWNER_STATE, setupCode, type OwnerSecrets } from "./core";
import { scanIssues, seedPrefs, watchErrors } from "./scan";

/** Scans the screen at desktop and phone width, dark and light, then returns to desktop dark. */
async function scanScreen(page: Page, errors: string[]) {
  for (const [width, mode] of [
    [1440, "dark"],
    [360, "dark"],
    [1440, "light"],
  ] as const) {
    await page.setViewportSize({ width, height: 900 });
    await page.evaluate((m) => document.documentElement.setAttribute("data-mode", m), mode);
    expect(await scanIssues(page), `${page.url()} @${width} ${mode}`).toEqual([]);
  }
  await page.evaluate(() => document.documentElement.setAttribute("data-mode", "dark"));
  await page.setViewportSize({ width: 1440, height: 900 });
  expect(errors, "page and console errors").toEqual([]);
}

// One Core, one owner: this runs once before every other project (playwright.config.ts dependencies).
test("first run: setup creates the owner, enrols an authenticator and saves the recovery codes", async ({ page }) => {
  const errors = watchErrors(page, { signedOut: true });
  await seedPrefs(page, { mode: "dark", lang: "zh-TW" });
  const owner: OwnerSecrets = {
    username: "owner",
    password: "e2e-owner-password-1",
    totpSecret: "",
    recoveryCodes: [],
  };

  // Any page while nobody exists yet → login → setup.
  await page.goto("/settings");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("建立擁有者帳號");
  await expect(page).toHaveURL(/\/setup$/);
  await scanScreen(page, errors);

  await page.getByLabel("設定碼（安裝結束時印在終端機）").fill("not-the-code");
  await page.getByLabel("帳號", { exact: true }).fill(owner.username);
  await page.getByLabel("密碼", { exact: true }).fill(owner.password);
  await page.getByRole("button", { name: "繼續" }).click();
  await expect(page.getByText("設定碼不對或已過期")).toBeVisible();
  errors.length = 0; // the refused setup is logged as a failed request

  await page.getByLabel("設定碼（安裝結束時印在終端機）").fill(setupCode());
  await page.getByRole("button", { name: "繼續" }).click();

  await expect(page.getByRole("heading", { level: 1 })).toHaveText("綁定二次驗證");
  await expect(page).toHaveURL(/\/enroll\?setup=1$/);
  await expect(page.getByRole("img", { name: "驗證器 App 要掃的 QR 碼" })).toBeVisible();
  await page.getByText("無法掃描？顯示金鑰").click();
  owner.totpSecret = (await page.locator(".hx-secret").innerText()).replace(/\s/g, "");
  await expect(page.getByText(/^伺服器時間 \d\d:\d\d:\d\d/)).toBeVisible();
  await scanScreen(page, errors);

  const code = page.getByRole("textbox", { name: "輸入 App 上的 6 位數" });
  await code.fill("000000");
  await expect(page.getByText("碼不對 · 確認手機時間與伺服器相同")).toBeVisible();
  errors.length = 0;
  await code.fill(await nextTotp(owner.totpSecret));

  await expect(page.getByRole("heading", { level: 1 })).toHaveText("存好復原碼");
  owner.recoveryCodes = await page.locator(".hx-codes-list li").allInnerTexts();
  expect(owner.recoveryCodes).toHaveLength(10);
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "下載" }).click();
  expect((await download).suggestedFilename()).toBe("hazelminx-recovery-codes.txt");
  await scanScreen(page, errors);
  await expect(page.getByRole("button", { name: "繼續" })).toBeDisabled();
  await page.getByRole("checkbox", { name: "我已經存在安全的地方" }).click();
  await page.getByRole("button", { name: "繼續" }).click();

  await expect(page.getByRole("heading", { level: 1 })).toHaveText("開始使用");
  await scanScreen(page, errors);
  await page.getByRole("button", { name: "先到總覽，之後再說" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("總覽");
  await expect(page.getByRole("button", { name: "owner · 擁有者" })).toBeVisible();

  // Setup is one-time: the setup screen now sends people to login, which sends a signed-in owner home.
  await page.goto("/setup");
  await expect(page).toHaveURL(/\/$/);

  await page.context().storageState({ path: OWNER_STATE });
  writeFileSync(OWNER_SECRETS, JSON.stringify(owner));
  expect(errors, "page and console errors").toEqual([]);
});
