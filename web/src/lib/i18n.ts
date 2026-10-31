import i18n from "i18next";
import { initReactI18next } from "react-i18next";

import en from "@/locales/en.json";
import zh from "@/locales/zh.json";

export const LANG_STORAGE_KEY = "singbox-ui-language";
export type SupportedLanguage = "zh" | "en";

export function detectLanguage(): SupportedLanguage {
  try {
    const saved = localStorage.getItem(LANG_STORAGE_KEY);
    if (saved === "zh" || saved === "en") return saved;
  } catch {
    /* storage unavailable; fall through to browser language */
  }
  const nav = typeof navigator !== "undefined" ? navigator.language : "";
  return nav.toLowerCase().startsWith("zh") ? "zh" : "en";
}

export function dateLocale(lang?: string): string {
  return (lang ?? i18n.language) === "en" ? "en-US" : "zh-CN";
}

export function setLanguage(lang: SupportedLanguage): Promise<unknown> {
  try {
    localStorage.setItem(LANG_STORAGE_KEY, lang);
  } catch {
    /* storage unavailable; language still applies for this session */
  }
  return i18n.changeLanguage(lang);
}

void i18n.use(initReactI18next).init({
  resources: { zh: { translation: zh }, en: { translation: en } },
  lng: detectLanguage(),
  fallbackLng: "zh",
  interpolation: { escapeValue: false },
});

export default i18n;
