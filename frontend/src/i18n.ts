import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

import en from './locales/en.json'
import ru from './locales/ru.json'

export const LANG_STORAGE_KEY = 'dps150.lang'
export const SUPPORTED_LANGS = ['ru', 'en'] as const
export type Lang = (typeof SUPPORTED_LANGS)[number]

function initialLang(): Lang {
  const stored =
    typeof localStorage !== 'undefined' ? localStorage.getItem(LANG_STORAGE_KEY) : null
  return stored === 'en' || stored === 'ru' ? stored : 'ru'
}

// Keep <html lang> in sync with the UI language (index.html ships a static
// `lang="en"` while RU is primary) so screen readers and the browser pick
// the right pronunciation/hyphenation rules.
function applyHtmlLang(lang: Lang) {
  if (typeof document !== 'undefined') {
    document.documentElement.lang = lang
  }
}

const startLang = initialLang()

i18n.use(initReactI18next).init({
  resources: {
    en: { translation: en },
    ru: { translation: ru },
  },
  lng: startLang,
  fallbackLng: 'en',
  interpolation: {
    escapeValue: false,
  },
})

applyHtmlLang(startLang)

// Persist the language so the choice survives reloads.
export function setLang(lang: Lang) {
  void i18n.changeLanguage(lang)
  applyHtmlLang(lang)
  if (typeof localStorage !== 'undefined') {
    localStorage.setItem(LANG_STORAGE_KEY, lang)
  }
}

export default i18n
