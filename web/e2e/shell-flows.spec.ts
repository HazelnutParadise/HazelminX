import { expect, test } from "@playwright/test";
import { seedPrefs } from "./scan";

test.describe("preferences", () => {
  test("defaults: Traditional Chinese, dark mode", async ({ page }) => {
    await page.goto("/");
    await expect(page.locator("html")).toHaveAttribute("lang", "zh-Hant-TW");
    await expect(page.locator("html")).toHaveAttribute("data-mode", "dark");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("總覽");
    const bg = await page.evaluate(() => getComputedStyle(document.documentElement).backgroundColor);
    expect(bg).toBe("rgb(22, 20, 18)");
  });

  test("theme toggle switches mode, keeps it across reloads", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("button", { name: "切換成淺色" }).click();
    await expect(page.locator("html")).toHaveAttribute("data-mode", "light");
    expect(await page.evaluate(() => getComputedStyle(document.documentElement).backgroundColor)).toBe(
      "rgb(246, 242, 234)",
    );
    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("data-mode", "light");
    await page.getByRole("button", { name: "切換成深色" }).click();
    await expect(page.locator("html")).toHaveAttribute("data-mode", "dark");
  });

  test("language switch translates the shell and persists", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/settings");
    await page.getByRole("button", { name: /· 擁有者$/ }).click();
    await page.getByRole("menuitem", { name: "語言 · 繁體中文" }).click();
    await page.getByRole("menuitemradio", { name: "English" }).click();
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Platform settings");
    await expect(
      page.getByRole("navigation", { name: "Main navigation" }).getByRole("link", { name: "Hosts" }),
    ).toBeVisible();
    await expect(page).toHaveTitle("Platform settings · HazelminX");
    await page.reload();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Platform settings");
  });

  test("unknown counts and observation time are not shown as zero", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");
    await expect(page.locator(".hx-obs")).toHaveText("觀測 —");
    await expect(page.locator(".hx-badge, .hx-count")).toHaveCount(0);
    await expect(page.locator("body")).not.toContainText(/\b0 (個工作|件)/);
  });
});

test.describe("navigation", () => {
  test("sidebar groups follow DESIGN.md information architecture", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");
    const nav = page.getByRole("navigation", { name: "主導覽" });
    // The shell renders once Core has confirmed the session.
    await nav.getByRole("link").first().waitFor();
    const names = await nav.getByRole("link").allInnerTexts();
    expect(names.map((n) => n.trim())).toEqual([
      "總覽",
      "工作中心",
      "事件與告警",
      "專案與服務",
      "應用商店",
      "網站與入口",
      "資料庫",
      "AI",
      "API 代理",
      "主機",
      "容器",
      "Kubernetes",
      "儲存與備份",
      "計畫任務",
      "安全防護",
      "流量分析",
      "日誌",
      "身分與存取",
      "平台設定",
    ]);
    for (const g of ["應用", "基礎設施", "安全與紀錄", "管理"])
      await expect(nav.getByText(g, { exact: true })).toBeVisible();
  });

  test("current page is marked and nested paths keep their section", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/nodes/hazel-tw-01/files");
    const current = page.getByRole("navigation", { name: "主導覽" }).locator('[aria-current="page"]');
    await expect(current).toHaveCount(1);
    await expect(current).toHaveText("主機");
  });

  test("sidebar collapses to icons with names and remembers it", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");
    await page.getByRole("heading", { level: 1 }).waitFor();
    const nav = page.getByRole("navigation", { name: "主導覽" });
    const before = (await nav.boundingBox())!.width;
    await page.getByRole("button", { name: "收合側欄" }).click();
    await expect(page.getByRole("button", { name: "展開側欄" })).toBeVisible();
    await expect.poll(async () => (await nav.boundingBox())!.width).toBeLessThan(80);
    expect(before).toBeGreaterThan(200);
    await expect(nav.getByRole("link", { name: "容器" })).toBeVisible();
    await page.reload();
    await expect(page.getByRole("button", { name: "展開側欄" })).toBeVisible();
  });

  test("phone: chip row scrolls the current page into view", async ({ page }) => {
    await page.setViewportSize({ width: 360, height: 800 });
    await page.goto("/settings");
    const row = page.locator(".hx-chiprow");
    const chip = page.getByRole("navigation", { name: "主導覽" }).getByRole("link", { name: "平台設定" });
    await expect(chip).toHaveAttribute("aria-current", "page");
    const r = (await row.boundingBox())!;
    const c = (await chip.boundingBox())!;
    expect(c.x).toBeGreaterThanOrEqual(r.x - 1);
    expect(c.x + c.width).toBeLessThanOrEqual(r.x + r.width + 1);
  });

  test("unknown path shows the 404 page inside the shell", async ({ page }) => {
    await page.goto("/no-such-page");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("找不到這個頁面");
    await expect(page.getByText("/no-such-page")).toBeVisible();
    await expect(page.getByRole("main").getByRole("button", { name: "搜尋", exact: true })).toBeVisible();
    await page.getByRole("link", { name: "回總覽" }).click();
    await expect(page).toHaveURL(/\/$/);
  });
});

test.describe("top bar layout", () => {
  test("desktop: 48px high, no jobs button, terminal visible", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");
    await page.getByRole("heading", { level: 1 }).waitFor();
    const top = page.locator("header.hx-top");
    expect((await top.boundingBox())!.height).toBe(48);
    await expect(top.getByRole("link", { name: "終端機" })).toBeVisible();
    await expect(top.getByRole("link", { name: /進行中的工作/ })).toHaveCount(0);
  });

  test("phone: logo, bell, theme and account on row one, search on row two, terminal in the menu", async ({ page }) => {
    await page.setViewportSize({ width: 360, height: 800 });
    await page.goto("/");
    await page.getByRole("heading", { level: 1 }).waitFor();
    const top = page.locator("header.hx-top");
    const row = async (name: RegExp | string) =>
      (await top.getByRole("button", { name }).or(top.getByRole("link", { name })).first().boundingBox())!.y;
    const logoY = (await top.getByRole("link", { name: "HazelminX 總覽" }).boundingBox())!.y;
    for (const name of [/事件與告警/, "切換成淺色", /· 擁有者$/])
      expect(Math.abs((await row(name)) - logoY)).toBeLessThan(8);
    expect((await top.getByRole("button", { name: "搜尋或下指令" }).boundingBox())!.y).toBeGreaterThan(logoY + 30);
    await expect(top.getByRole("link", { name: "終端機" })).toBeHidden();
    await top.getByRole("button", { name: /· 擁有者$/ }).click();
    await page.getByRole("menuitem", { name: "終端機" }).click();
    await expect(page).toHaveURL(/\/terminal$/);
  });
});

test.describe("keyboard", () => {
  test("first Tab reaches the skip link, which moves focus to main", async ({ page }) => {
    await page.goto("/");
    await page.getByRole("heading", { level: 1 }).waitFor();
    await page.keyboard.press("Tab");
    const skip = page.getByRole("link", { name: "跳到主要內容" });
    await expect(skip).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(page.locator("main#main")).toBeFocused();
  });

  test("Ctrl/⌘K opens search; Enter goes to the page", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");
    // The shortcut listener is attached once React has rendered the shell.
    await page.getByRole("heading", { level: 1 }).waitFor();
    await page.keyboard.press("ControlOrMeta+k");
    const input = page.getByRole("combobox", { name: "搜尋或下指令" });
    await expect(input).toBeFocused();
    await input.fill("平台");
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("平台設定");
  });
});

test("seeded light mode applies before first paint", async ({ page }) => {
  await seedPrefs(page, { mode: "light" });
  await page.goto("/");
  await expect(page.locator("html")).toHaveAttribute("data-mode", "light");
});

test("crossing the phone breakpoint keeps the page mounted (no lost input)", async ({ page }) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  await page.goto("/settings");
  await page.locator("main h1").waitFor();
  await page.evaluate(() => ((document.querySelector("main h1") as HTMLElement & { _mark?: number })._mark = 1));
  await page.setViewportSize({ width: 600, height: 800 });
  await expect(page.locator(".hx-chiprow")).toBeVisible();
  await page.setViewportSize({ width: 1024, height: 800 });
  await expect(page.getByRole("button", { name: "收合側欄" })).toBeVisible();
  expect(await page.evaluate(() => (document.querySelector("main h1") as HTMLElement & { _mark?: number })._mark)).toBe(
    1,
  );
});
