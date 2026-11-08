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
    await page.getByRole("button", { name: "帳號" }).click();
    await page.getByRole("menuitemradio", { name: "English" }).click();
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Platform settings");
    await expect(
      page.getByRole("navigation", { name: "Main navigation" }).getByRole("link", { name: "Hosts & nodes" }),
    ).toBeVisible();
    await expect(page).toHaveTitle("Platform settings · HazelminX");
    await page.reload();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Platform settings");
  });

  test("unknown counts and observation time are not shown as zero", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");
    await expect(page.getByText("尚未觀測")).toBeVisible();
    await expect(page.locator(".hx-badge, .hx-count")).toHaveCount(0);
    await expect(page.locator("body")).not.toContainText(/\b0 (個工作|件)/);
  });
});

test.describe("navigation", () => {
  test("sidebar groups follow DESIGN.md information architecture", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");
    const nav = page.getByRole("navigation", { name: "主導覽" });
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
      "主機與節點",
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
    await expect(current).toContainText("主機與節點");
  });

  test("sidebar collapses to icons with names and remembers it", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");
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
    await page.getByRole("link", { name: "回到總覽" }).click();
    await expect(page).toHaveURL(/\/$/);
  });
});

test.describe("keyboard", () => {
  test("first Tab reaches the skip link, which moves focus to main", async ({ page }) => {
    await page.goto("/");
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
