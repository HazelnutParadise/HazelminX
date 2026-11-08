import { describe, it, expect } from "vite-plus/test";
import en from "../i18n/locales/en.json";
import zhTW from "../i18n/locales/zh-TW.json";
import { NAV_GROUPS, NAV_ORDER, PAGES, pageForPath } from "./navigation";

function pick(tree: unknown, key: string): unknown {
  if (typeof tree !== "object" || tree === null) return undefined;
  return (tree as Record<string, unknown>)[key];
}

function isNonEmptyString(value: unknown): boolean {
  return typeof value === "string" && value.trim() !== "";
}

describe("pageForPath", () => {
  it.each([
    ["/", "overview"],
    ["/nodes", "nodes"],
    ["/nodes/abc/files", "nodes"],
    ["/terminal", "terminal"],
    ["/nodesx", undefined],
    ["/unknown", undefined],
  ] as const)("maps %j to %s", (path, expected) => {
    expect(pageForPath(path)).toBe(expected);
  });
});

describe("NAV_ORDER", () => {
  it("has no duplicate entries", () => {
    expect(new Set(NAV_ORDER).size).toBe(NAV_ORDER.length);
  });

  it("contains every page except the top-bar ones (terminal, account security)", () => {
    const expected = Object.keys(PAGES)
      .filter((key) => key !== "terminal" && key !== "accountSecurity")
      .sort();
    expect([...NAV_ORDER].sort()).toEqual(expected);
  });
});

describe("navigation labels", () => {
  it("has a non-empty nav label for every page in zh-TW and en", () => {
    const keys = Object.keys(PAGES);
    expect(keys.filter((k) => !isNonEmptyString(pick(pick(zhTW, "nav"), k)))).toEqual([]);
    expect(keys.filter((k) => !isNonEmptyString(pick(pick(en, "nav"), k)))).toEqual([]);
  });

  it("has a non-empty navGroup label for every nav group in zh-TW and en", () => {
    const keys = NAV_GROUPS.map((g) => g.key);
    expect(keys.filter((k) => !isNonEmptyString(pick(pick(zhTW, "navGroup"), k)))).toEqual([]);
    expect(keys.filter((k) => !isNonEmptyString(pick(pick(en, "navGroup"), k)))).toEqual([]);
  });
});
