import { describe, it, expect } from "vite-plus/test";
import { DEFAULT_LANGUAGE, LANGUAGES, isLanguage } from "./index";

const PLURAL_SUFFIX = /_(zero|one|two|few|many|other)$/;
const INTERPOLATION = /\{\{\s*([\w.]+)[^}]*\}\}/g;

/** Flattens a nested locale tree into dotted keys. */
function flatten(tree: unknown, prefix = ""): Record<string, string> {
  if (typeof tree === "string") return { [prefix]: tree };
  if (typeof tree !== "object" || tree === null) return {};
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(tree)) {
    Object.assign(out, flatten(value, prefix ? `${prefix}.${key}` : key));
  }
  return out;
}

/** Strips i18next plural suffixes so `shell.count_one` and `shell.count_other` both count as `shell.count`. */
function baseKey(key: string): string {
  return key.replace(PLURAL_SUFFIX, "");
}

/** Maps each base key to the sorted, de-duplicated interpolation variables used across its variants. */
function variablesByBaseKey(flat: Record<string, string>): Record<string, string[]> {
  const out: Record<string, Set<string>> = {};
  for (const [key, value] of Object.entries(flat)) {
    const set = (out[baseKey(key)] ??= new Set<string>());
    for (const match of value.matchAll(INTERPOLATION)) {
      set.add(match[1]!);
    }
  }
  return Object.fromEntries(Object.entries(out).map(([k, set]) => [k, [...set].sort()]));
}

const defaultLanguage = LANGUAGES.find((l) => l.code === DEFAULT_LANGUAGE);
if (!defaultLanguage) throw new Error("DEFAULT_LANGUAGE is not registered in LANGUAGES");
const zhFlat = flatten(defaultLanguage.resources);
const zhKeys = [...new Set(Object.keys(zhFlat).map(baseKey))].sort();

describe("locale completeness", () => {
  const others = LANGUAGES.filter((l) => l.code !== DEFAULT_LANGUAGE);

  it("has at least one non-default language to check", () => {
    expect(others.length).toBeGreaterThan(0);
  });

  it.each(others.map((l) => [l.code, l] as const))("%s has exactly the keys of zh-TW", (_code, lang) => {
    const keys = [...new Set(Object.keys(flatten(lang.resources)).map(baseKey))].sort();
    expect(keys).toEqual(zhKeys);
  });

  it.each(LANGUAGES.map((l) => [l.code, l] as const))("%s has only non-empty string values", (_code, lang) => {
    const flat = flatten(lang.resources);
    const bad = Object.entries(flat)
      .filter(([, value]) => typeof value !== "string" || value.trim() === "")
      .map(([key]) => key);
    expect(bad).toEqual([]);
  });

  it.each(others.map((l) => [l.code, l] as const))(
    "%s uses the same interpolation variables as zh-TW for every key",
    (_code, lang) => {
      expect(variablesByBaseKey(flatten(lang.resources))).toEqual(variablesByBaseKey(zhFlat));
    },
  );
});

describe("language registry", () => {
  it("defaults to zh-TW", () => {
    expect(DEFAULT_LANGUAGE).toBe("zh-TW");
  });

  it("recognizes registered language codes", () => {
    expect(isLanguage("en")).toBe(true);
    expect(isLanguage("zh-TW")).toBe(true);
  });

  it("rejects unregistered or missing codes", () => {
    expect(isLanguage("fr")).toBe(false);
    expect(isLanguage(null)).toBe(false);
    expect(isLanguage(undefined)).toBe(false);
  });
});
