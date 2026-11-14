import { randomBytes } from "node:crypto";
import { expect, test, type Page } from "@playwright/test";
import { createUser, enrollAs, fakeHost, signedInContext, type FakeHost } from "./core";
import { scanIssues, seedPrefs, watchErrors } from "./scan";

/**
 * Hosts against a real Core (ticket 125). The host side of enrollment is simulated through Core's agent
 * listener (POST /agent/v1/enroll), so approved hosts never open the agent channel: they stay 「從未連線」.
 * Connected, stale, offline and identity-conflict hosts are covered by the unit tests (src/nodes).
 */

/** Scans the screen at desktop and phone width, dark and light, then returns to desktop dark. */
async function scanScreen(page: Page, errors: string[], label: string) {
  for (const [width, mode] of [
    [1440, "dark"],
    [360, "dark"],
    [1440, "light"],
  ] as const) {
    await page.setViewportSize({ width, height: 900 });
    await page.evaluate((m) => document.documentElement.setAttribute("data-mode", m), mode);
    expect(await scanIssues(page), `${label} @${width} ${mode}`).toEqual([]);
  }
  await page.evaluate(() => document.documentElement.setAttribute("data-mode", "dark"));
  await page.setViewportSize({ width: 1440, height: 900 });
  expect(errors, `${label}: page and console errors`).toEqual([]);
}

const h1 = (page: Page) => page.locator("main h1").first();
const uniq = (p: string) => `${p}-${randomBytes(3).toString("hex")}`;

/** Creates a code through the add-host screen and returns it, read from the command on screen. */
async function createCode(page: Page, settings: { name?: string; tags?: string; group?: string } = {}) {
  await page.goto("/nodes/new");
  await expect(h1(page)).toHaveText("新增主機");
  if (settings.name) await page.getByLabel("顯示名稱").fill(settings.name);
  if (settings.tags) await page.getByLabel("標籤").fill(settings.tags);
  if (settings.group) await page.getByLabel("群組").fill(settings.group);
  await page.getByRole("button", { name: "產生註冊碼" }).click();
  const command = page.getByLabel("註冊指令");
  await expect(command).toBeVisible();
  const code = /HZX-[0-9A-Z]{4}(?:-[0-9A-Z]{4}){3}/.exec((await command.textContent()) ?? "")?.[0];
  expect(code, "the command carries the code").toBeTruthy();
  return code!;
}

/** Adds a host end to end: code, the host's request, approve. Returns the host page's URL. */
async function addHost(page: Page, name: string, host: FakeHost = fakeHost()) {
  const code = await createCode(page, { name });
  expect((await enrollAs(host, code)).status).toBe(202);
  await page.getByRole("button", { name: "是這台，加入" }).click();
  await expect(page.getByText(`${name} 已加入`)).toBeVisible();
  await page.getByRole("link", { name: "查看這台主機" }).click();
  await expect(h1(page)).toHaveText(name);
  return { url: page.url(), host };
}

/** Opens a host's row menu on the list and picks an action. */
async function rowAction(page: Page, name: string, action: string) {
  await page.goto(`/nodes?q=${encodeURIComponent(name)}`);
  await page.getByRole("button", { name: `${name} 的更多操作` }).click();
  await page.getByRole("menuitem", { name: action }).click();
  return page.getByRole("dialog");
}

function row(page: Page, name: string) {
  return page.getByRole("row").filter({ has: page.getByRole("link", { name, exact: true }) });
}

test.describe.configure({ mode: "serial" });

test.beforeEach(async ({ page }) => {
  await seedPrefs(page, { mode: "dark", lang: "zh-TW" });
  await page.setViewportSize({ width: 1440, height: 900 });
});

test("add a host: the code is shown once, the host's report is checked, then it joins", async ({ page }) => {
  const errors = watchErrors(page);
  const name = uniq("e2e-pi");
  const host = fakeHost();

  await page.goto("/nodes");
  await expect(h1(page)).toHaveText("主機");
  await page.getByRole("link", { name: "新增主機" }).first().click();
  await expect(page).toHaveURL(/\/nodes\/new$/);
  await scanScreen(page, errors, "add host · settings");

  const code = await createCode(page, { name, tags: "e2e, arm64" });
  await expect(page).toHaveURL(/\/nodes\/new\?enrollment=/);
  expect(page.url()).not.toContain(code);
  await expect(page.getByText("註冊碼只顯示這一次。離開或重新整理這一頁之後就看不到了。")).toBeVisible();
  await expect(page.getByText(/^還有 \d+:\d\d 有效$/)).toBeVisible();
  await scanScreen(page, errors, "add host · run");

  // A reload keeps the enrollment but never the code.
  await page.reload();
  await expect(page.getByText(/註冊碼只在產生時顯示一次/)).toBeVisible();
  expect(await page.locator("body").textContent()).not.toContain(code);

  // The host uses the code; the waiting screen turns into the check by itself.
  const claimed = await enrollAs(host, code);
  expect(claimed.status).toBe(202);
  expect(claimed.body.status).toBe("pending");
  await expect(page.getByText(host.hostname, { exact: true })).toBeVisible({ timeout: 10_000 });
  for (const v of [host.machineId, "Debian 13 · arm64", "0.1.0"])
    await expect(page.getByText(v, { exact: true })).toBeVisible();
  await expect(page.getByText("沒有相同金鑰或相同 machine-id 的既有主機。")).toBeVisible();
  await scanScreen(page, errors, "add host · check");

  await page.getByRole("button", { name: "是這台，加入" }).click();
  await expect(page.getByText(`${name} 已加入`)).toBeVisible();
  await scanScreen(page, errors, "add host · done");

  // The host asking again now gets its certificate.
  const approved = await enrollAs(host, code);
  expect(approved.status).toBe(200);
  expect(approved.body.status).toBe("approved");

  await page.getByRole("link", { name: "查看這台主機" }).click();
  await expect(h1(page)).toHaveText(name);
  await expect(page.locator(".hx-node-status")).toContainText("從未連線");
  await page.getByRole("navigation", { name: "主機分頁" }).getByRole("link", { name: "系統" }).click();
  await expect(page.getByText(host.hostname, { exact: true })).toBeVisible();
  await scanScreen(page, errors, "host page");

  await page.goto(`/nodes?q=${encodeURIComponent(name)}`);
  await expect(row(page, name)).toContainText("從未連線");
  await expect(row(page, name)).toContainText("有效");
  await expect(row(page, name)).toContainText("e2e");
  await scanScreen(page, errors, "host list");
});

test("search, status and tag filters live in the URL; an empty match says why", async ({ page }) => {
  const errors = watchErrors(page);
  const name = uniq("e2e-filter");
  await addHost(page, name);

  await page.goto("/nodes");
  await page.getByLabel("搜尋").fill(name);
  await expect(page).toHaveURL(new RegExp(`\\?q=${name}$`));
  await expect(page.getByRole("link", { name, exact: true })).toBeVisible();
  await page.getByRole("button", { name: "從未連線" }).click();
  await expect(page).toHaveURL(new RegExp(`\\?q=${name}&status=never$`));
  await expect(page.getByRole("link", { name, exact: true })).toBeVisible();

  await page.getByRole("button", { name: "沒有回應" }).click();
  await expect(page.getByText("沒有符合條件的主機")).toBeVisible();
  await expect(page.getByText(`目前的條件：搜尋「${name}」、沒有回應`)).toBeVisible();
  await scanScreen(page, errors, "host list · no match");

  await page.reload();
  await expect(page.getByRole("button", { name: "沒有回應" })).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "清除篩選" }).click();
  await expect(page).toHaveURL(/\/nodes$/);
});

test("rename, maintenance, certificate rotation and revocation", async ({ page }) => {
  const errors = watchErrors(page);
  const name = uniq("e2e-ops");
  await addHost(page, name);

  // Rename, tag and group.
  let dialog = await rowAction(page, name, "改名與標籤");
  await dialog.getByLabel("標籤").fill("e2e, edge");
  await dialog.getByLabel("群組").fill("taipei");
  await dialog.getByRole("button", { name: "儲存" }).click();
  await expect(dialog).toBeHidden();
  await expect(row(page, name)).toContainText("edge");
  await expect(row(page, name)).toContainText("taipei");

  // Maintenance on lists the unfinished jobs (none here), then off.
  dialog = await rowAction(page, name, "進入維護");
  await dialog.getByLabel("原因").fill("e2e");
  await dialog.getByRole("button", { name: `進入維護 ${name}` }).click();
  await expect(page.getByRole("dialog", { name: `${name} 已進入維護` })).toContainText(
    "這台主機上沒有還沒完成的工作。",
  );
  await page.getByRole("button", { name: "完成" }).click();
  await expect(row(page, name)).toContainText("維護中");
  dialog = await rowAction(page, name, "結束維護");
  await dialog.getByRole("button", { name: `結束維護 ${name}` }).click();
  await expect(dialog).toBeHidden();
  await expect(row(page, name)).toContainText("從未連線");

  // On a phone, a dialog opened at the top of the page stays above the sticky top bar.
  await page.getByRole("link", { name, exact: true }).click();
  await expect(h1(page)).toHaveText(name);
  await page.setViewportSize({ width: 360, height: 800 });
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.getByRole("button", { name: "輪替憑證" }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  expect(await scanIssues(page), "rotate dialog @360 from the top of the host page").toEqual([]);
  await page.getByRole("dialog").getByRole("button", { name: "取消" }).click();
  await page.setViewportSize({ width: 1440, height: 900 });

  // Rotation: the host is not connected, so the new certificate waits.
  dialog = await rowAction(page, name, "輪替憑證");
  await expect(dialog).toContainText("主機不在線，新憑證會在它下次連線時送出。");
  await dialog.getByRole("button", { name: `輪替 ${name} 的憑證` }).click();
  await expect(dialog).toBeHidden();
  await expect(row(page, name)).toContainText("輪替中 · 待送出");
  await expect(row(page, name)).toContainText("新憑證");

  // Revocation asks for the name and then waits for the host to hear of it.
  dialog = await rowAction(page, name, "撤銷");
  await expect(dialog).toContainText("之後它再也連不回來。");
  const confirm = dialog.getByRole("button", { name: `撤銷 ${name}` });
  await expect(confirm).toBeDisabled();
  await dialog.getByLabel(`輸入 ${name} 確認`).fill(name);
  await scanScreen(page, errors, "revoke dialog");
  await confirm.click();
  await expect(dialog).toBeHidden();
  await page.goto(`/nodes?q=${encodeURIComponent(name)}&status=revoked`);
  await expect(row(page, name)).toContainText("已撤銷 · 待送達");
  await expect(row(page, name)).toContainText("撤銷");
  await page.getByRole("link", { name, exact: true }).click();
  await page.getByRole("navigation", { name: "主機分頁" }).getByRole("link", { name: "系統" }).click();
  await expect(page.getByRole("region", { name: "撤銷", exact: true })).toContainText("待送達");
  await scanScreen(page, errors, "revoked host page");
});

test("a new key on a known machine-id replaces that host; stop managing leaves only the record", async ({ page }) => {
  const errors = watchErrors(page);
  const name = uniq("e2e-swap");
  const first = fakeHost();
  await addHost(page, name, first);

  // Same machine, new key (a reinstalled host).
  const code = await createCode(page);
  expect((await enrollAs(fakeHost({ machineId: first.machineId, hostname: first.hostname }), code)).status).toBe(202);
  const approve = page.getByRole("button", { name: "是這台，加入" });
  await expect(page.getByText("同一個 machine-id")).toBeVisible();
  await expect(approve).toBeDisabled();
  await page.getByRole("radio", { name: new RegExp(`取代 ${name}`) }).check();
  await scanScreen(page, errors, "add host · replace choice");
  await approve.click();
  await expect(page.getByRole("link", { name: "查看這台主機" })).toBeVisible();
  await page.goto(`/nodes?q=${encodeURIComponent(name)}`);
  await expect(page.getByRole("link", { name, exact: true })).toHaveCount(1);

  // Stop managing: the only mode Core offers; the other three are shown with the reason.
  const dialog = await rowAction(page, name, "不再管理");
  await expect(dialog.getByRole("radio", { name: /^移除 Agent/ })).toBeDisabled();
  await expect(dialog.getByRole("radio", { name: /^移除叢集節點/ })).toBeDisabled();
  await expect(dialog.getByRole("radio", { name: /^刪除工作負載/ })).toBeDisabled();
  await scanScreen(page, errors, "remove dialog");
  await dialog.getByRole("button", { name: `不再管理 ${name}` }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole("link", { name, exact: true })).toHaveCount(0);
  await page.goto(`/nodes?q=${encodeURIComponent(name)}&status=removed`);
  await expect(row(page, name)).toContainText("不再管理");
});

test("an unused code can be withdrawn; a rejected host stays refused", async ({ page }) => {
  const errors = watchErrors(page);
  const name = uniq("e2e-wait");
  await createCode(page, { name });
  // Leaving the page keeps the code findable from the host list.
  await page.goto("/nodes");
  const enrolling = page.getByRole("region", { name: /^註冊中 · / });
  await expect(enrolling).toContainText(name);
  await scanScreen(page, errors, "host list · enrolling");
  await enrolling.getByRole("link", { name: `開啟註冊碼 ${name}` }).click();
  await expect(page.getByText(/註冊碼只在產生時顯示一次/)).toBeVisible();
  await page.getByRole("button", { name: "收回這個註冊碼" }).click();
  await expect(page.getByText("註冊碼已收回")).toBeVisible();
  await scanScreen(page, errors, "add host · withdrawn");

  const code = await createCode(page);
  const host = fakeHost();
  expect((await enrollAs(host, code)).status).toBe(202);
  await page.getByRole("button", { name: "不是，拒絕" }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: `拒絕 ${host.hostname}` })
    .click();
  await expect(page.getByText("已拒絕這台主機")).toBeVisible();
  const again = await enrollAs(host, code);
  expect(again.status).toBe(403);
  expect(again.body.code).toBe("ENROLLMENT_REJECTED");
});

test("an account without nodes:write sees hosts but cannot change them", async ({ browser }) => {
  const auditor = await createUser("auditor");
  const ctx = await signedInContext(browser, auditor);
  const page = await ctx.newPage();
  try {
    await seedPrefs(page, { mode: "dark", lang: "zh-TW" });
    await page.goto("/nodes");
    await expect(h1(page)).toHaveText("主機");
    const add = page.getByRole("button", { name: /新增主機/ });
    await expect(add).toBeDisabled();
    await expect(add).toContainText("需要主機的寫入權限");
  } finally {
    await ctx.close();
  }
});
