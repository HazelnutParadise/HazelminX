import AxeBuilder from "@axe-core/playwright";
import type { Page } from "@playwright/test";

export type Mode = "dark" | "light";
export type Lang = "zh-TW" | "en";

/** Seeds the same localStorage keys public/boot.js reads, before any page script runs. */
export async function seedPrefs(page: Page, prefs: { mode?: Mode; lang?: Lang; sidebar?: "open" | "collapsed" }) {
  await page.addInitScript((p) => {
    if (sessionStorage.getItem("hx.e2e.seeded")) return;
    sessionStorage.setItem("hx.e2e.seeded", "1");
    if (p.mode) localStorage.setItem("hx.mode", p.mode);
    if (p.lang) localStorage.setItem("hx.lang", p.lang);
    if (p.sidebar) localStorage.setItem("hx.sidebar", p.sidebar);
  }, prefs);
}

/** WCAG 2.1 A/AA through axe-core: contrast (with real composited backgrounds), names, lang, landmarks. */
export async function axeViolations(page: Page): Promise<string[]> {
  const r = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
  return r.violations.flatMap((v) =>
    v.nodes.map((n) => `${v.id}: ${n.target.join(" ")} — ${n.failureSummary?.split("\n")[1] ?? ""}`),
  );
}

/**
 * Text contrast computed directly: every element with its own visible text against the composited
 * background of its ancestors. axe marks text over pseudo-elements or color-mix() fills as "incomplete"
 * rather than failing it (it missed Kumo's white-on-hazel primary button), so this check fills that gap.
 * Only disabled controls are exempt (WCAG 1.4.3 inactive components); opacity is multiplied into the text color.
 */
export async function contrastIssues(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const cv = document.createElement("canvas");
    cv.width = cv.height = 1;
    const ctx = cv.getContext("2d", { willReadFrequently: true })!;
    const rgba = (c: string): [number, number, number, number] => {
      ctx.clearRect(0, 0, 1, 1);
      ctx.fillStyle = "#000";
      ctx.fillStyle = c;
      ctx.fillRect(0, 0, 1, 1);
      const d = ctx.getImageData(0, 0, 1, 1).data;
      return [d[0], d[1], d[2], d[3] / 255];
    };
    const over = (top: number[], bottom: number[]) => {
      const a = top[3];
      return [0, 1, 2].map((i) => top[i] * a + bottom[i] * (1 - a)).concat(1);
    };
    const lum = (c: number[]) => {
      const [r, g, b] = c.slice(0, 3).map((v) => {
        const s = v / 255;
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
      });
      return 0.2126 * r + 0.7152 * g + 0.0722 * b;
    };
    const out: string[] = [];
    for (const el of document.querySelectorAll<HTMLElement>("body *")) {
      const text = [...el.childNodes].find((n) => n.nodeType === 3 && n.textContent!.trim());
      if (!text) continue;
      const cs = getComputedStyle(el);
      // Measure the text itself: wrappers with display: contents have no box of their own.
      const range = document.createRange();
      range.selectNodeContents(text);
      let r = range.getBoundingClientRect();
      if (cs.visibility === "hidden" || r.width === 0 || r.height === 0) continue;
      if (el.closest("[disabled], [aria-disabled='true'], [hidden]")) continue;
      let opacity = 1;
      for (let p: HTMLElement | null = el; p; p = p.parentElement) opacity *= parseFloat(getComputedStyle(p).opacity);
      // Hit-testing only works inside the viewport: bring the text into view first.
      if (r.top < 0 || r.bottom > innerHeight || r.left < 0 || r.right > innerWidth) {
        el.scrollIntoView({ block: "center", inline: "center" });
        r = range.getBoundingClientRect();
      }
      // What is painted under the text: the hit-test stack below the element at its center. It includes
      // absolutely positioned siblings (Kumo paints button gradients on a sibling span) and skips anything
      // stacked on top of the text (a sticky bar covering it).
      const hits = document.elementsFromPoint(r.left + r.width / 2, r.top + r.height / 2) as HTMLElement[];
      const at = hits.findIndex((n) => n === el || n.contains(el) || el.contains(n));
      const stack = at === -1 ? [] : hits.slice(at).filter((n) => n !== el && !el.contains(n));
      if (!stack.length) for (let p = el.parentElement; p; p = p.parentElement) stack.push(p);
      const layers: number[][] = [];
      // Gradient fills are checked against every color stop: worst case wins.
      let stops: number[][] = [];
      let unknown = false;
      for (const p of [el, ...stack]) {
        const ps = getComputedStyle(p);
        if (ps.backgroundImage !== "none" && !stops.length) {
          const found = ps.backgroundImage.match(
            /(?:rgba?|oklch|oklab|lab|lch|hsla?|color)\([^()]*\)|#[0-9a-f]{3,8}\b/gi,
          );
          if (found && !/url\(/.test(ps.backgroundImage)) stops = found.map(rgba);
          else unknown = true;
        }
        const bg = rgba(ps.backgroundColor);
        if (bg[3] > 0) layers.push(bg);
        if (bg[3] >= 1) break;
      }
      if (unknown) continue;
      let base = [255, 255, 255, 1];
      for (const l of layers.reverse()) base = over(l, base);
      const size = parseFloat(cs.fontSize);
      const large = size >= 24 || (size >= 18.66 && parseInt(cs.fontWeight) >= 700);
      for (const bg of stops.length ? stops.map((s) => over(s, base)) : [base]) {
        const c = rgba(cs.color);
        const fg = over([c[0], c[1], c[2], c[3] * opacity], bg);
        const [a, b] = [lum(fg), lum(bg)].sort((x, y) => y - x);
        const ratio = (a + 0.05) / (b + 0.05);
        if (ratio < (large ? 3 : 4.5)) {
          out.push(`CONTRAST ${ratio.toFixed(2)}: ${el.textContent!.trim().slice(0, 30)}`);
          break;
        }
      }
    }
    return [...new Set(out)];
  });
}

/**
 * Touch targets (DESIGN.md: ≥ 44px on phones, ≥ 24px on desktop), page overflow, clipped text,
 * and untranslated i18n keys. Port of the checks in docs/design/tools/scan_layout.py plus target size.
 */
export async function layoutIssues(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const out: string[] = [];
    const w = window.innerWidth;
    const min = w < 768 ? 44 : 24;
    const label = (el: Element) =>
      (el.getAttribute("aria-label") || (el as HTMLElement).innerText || el.tagName)
        .trim()
        .replace(/\s+/g, " ")
        .slice(0, 40);
    const visible = (el: Element) => {
      const cs = getComputedStyle(el);
      const r = el.getBoundingClientRect();
      return cs.display !== "none" && cs.visibility !== "hidden" && r.width > 0 && r.height > 0;
    };
    for (const el of document.querySelectorAll(
      "a[href], button, input, select, textarea, summary, [role=button], [role=tab], [role=menuitem]",
    )) {
      if (!visible(el) || el.closest("[inert]")) continue;
      const r = el.getBoundingClientRect();
      if (r.width < min - 0.5 || r.height < min - 0.5)
        out.push(`TARGET ${Math.round(r.width)}×${Math.round(r.height)} <${min}: ${label(el)}`);
    }
    if (document.documentElement.scrollWidth > w)
      out.push(`PAGE-OVERFLOW +${document.documentElement.scrollWidth - w}`);
    for (const el of document.querySelectorAll("header *, main *, nav *, [role=status] *")) {
      if (!visible(el) || !el.textContent?.trim()) continue;
      const cs = getComputedStyle(el);
      const clips = cs.overflow === "hidden" || cs.overflowX === "hidden" || cs.overflowX === "clip";
      if (clips && el.scrollWidth > el.clientWidth + 2 && el.children.length === 0) out.push(`CLIP: ${label(el)}`);
    }
    const text = document.body.innerText;
    const key = text.match(
      /\b(app|nav|navGroup|shell|hostBar|saveBar|componentDown|page|notFound|crash|raw|errors|kumo)\.[a-zA-Z_]+\b/,
    );
    if (key) out.push(`I18N-KEY: ${key[0]}`);
    return [...new Set(out)];
  });
}
