import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import { PREF_KEYS, readPref, writePref } from "../prefs/storage";
import en from "./locales/en.json";
import zhTW from "./locales/zh-TW.json";

/**
 * Supported languages. To add one: add `locales/<code>.json` with every key of zh-TW.json
 * (src/i18n/locales.test.ts enforces this) and one entry here.
 */
export const LANGUAGES = [
  { code: "zh-TW", htmlLang: "zh-Hant-TW", label: "繁體中文", resources: zhTW },
  { code: "en", htmlLang: "en", label: "English", resources: en },
] as const;

export type LanguageCode = (typeof LANGUAGES)[number]["code"];

export const DEFAULT_LANGUAGE: LanguageCode = "zh-TW";

export function isLanguage(code: string | null | undefined): code is LanguageCode {
  return LANGUAGES.some((l) => l.code === code);
}

export function htmlLang(code: LanguageCode): string {
  return LANGUAGES.find((l) => l.code === code)!.htmlLang;
}

/** BCP 47 tag for Intl formatting of the active language. */
export function activeLocale(): string {
  return htmlLang(isLanguage(i18n.language) ? i18n.language : DEFAULT_LANGUAGE);
}

export function initI18n(lng: LanguageCode = savedLanguage()) {
  void i18n.use(initReactI18next).init({
    resources: Object.fromEntries(LANGUAGES.map((l) => [l.code, { translation: l.resources }])),
    lng,
    fallbackLng: DEFAULT_LANGUAGE,
    interpolation: { escapeValue: false },
    returnNull: false,
  });
  document.documentElement.setAttribute("lang", htmlLang(lng));
  return i18n;
}

export function savedLanguage(): LanguageCode {
  const v = readPref(PREF_KEYS.lang);
  return isLanguage(v) ? v : DEFAULT_LANGUAGE;
}

export async function setLanguage(code: LanguageCode): Promise<void> {
  await i18n.changeLanguage(code);
  document.documentElement.setAttribute("lang", htmlLang(code));
  writePref(PREF_KEYS.lang, code);
}

export default i18n;
