import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import LanguageDetector from 'i18next-browser-languagedetector'

import zhCN from '../locales/zh-CN.json'
import en from '../locales/en.json'

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      'zh-CN': { translation: zhCN },
      en: { translation: en },
    },
    fallbackLng: 'en',
    interpolation: { escapeValue: false },
    detection: {
      order: ['localStorage', 'querystring', 'navigator'],
      lookupLocalStorage: 'i18nextLng',
      lookupQuerystring: 'lang',
      caches: ['localStorage'],
    },
  })

i18n.on('languageChanged', (lng: string) => {
  document.documentElement.lang = lng.startsWith('zh') ? 'zh-CN' : 'en'
})

export default i18n
