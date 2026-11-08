import { expect, test } from "@playwright/test";
import { axeViolations, contrastIssues, layoutIssues, seedPrefs, type Lang, type Mode } from "./scan";

const WIDTHS = [360, 768, 1024, 1280, 1440, 1920];
const DEV = !process.env.HAZELMINX_E2E_URL;

/** Shell pages: an ordinary page, a nested path, the 404 page and (dev server only) the shared component gallery. */
const PAGES = ["/", "/settings", "/nodes/hazel-tw-01/files", "/no-such-page", ...(DEV ? ["/__dev/components"] : [])];

interface Case {
  path: string;
  width: number;
  mode: Mode;
  lang: Lang;
  sidebar: "open" | "collapsed";
}

const cases: Case[] = [];
for (const path of PAGES) {
  for (const width of WIDTHS) cases.push({ path, width, mode: "dark", lang: "zh-TW", sidebar: "open" });
  for (const width of [360, 1440]) {
    cases.push({ path, width, mode: "light", lang: "zh-TW", sidebar: "open" });
    cases.push({ path, width, mode: "dark", lang: "en", sidebar: "open" });
    cases.push({ path, width, mode: "light", lang: "en", sidebar: "open" });
  }
  for (const mode of ["dark", "light"] as const)
    cases.push({ path, width: 1440, mode, lang: "zh-TW", sidebar: "collapsed" });
}

for (const c of cases) {
  test(`scan ${c.path} @${c.width} ${c.mode} ${c.lang} ${c.sidebar}`, async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    // React reports errors caught by an error boundary on the console, not as page errors.
    page.on("console", (m) => {
      if (m.type() === "error") errors.push(m.text());
    });
    await page.setViewportSize({ width: c.width, height: 900 });
    await seedPrefs(page, { mode: c.mode, lang: c.lang, sidebar: c.sidebar });
    await page.goto(c.path);
    await page.locator("main h1").first().waitFor();
    await page.evaluate(() => document.fonts.ready);

    await expect(page.locator("html")).toHaveAttribute("data-mode", c.mode);
    await expect(page.locator("html")).toHaveAttribute("lang", c.lang === "en" ? "en" : "zh-Hant-TW");

    expect(errors, "page and console errors").toEqual([]);
    await expect(page.locator(".hx-crash"), "error boundary must not be showing").toHaveCount(0);
    expect(await axeViolations(page), "axe WCAG 2.1 AA").toEqual([]);
    expect(await contrastIssues(page), "text contrast").toEqual([]);
    expect(await layoutIssues(page), "layout").toEqual([]);
  });
}
