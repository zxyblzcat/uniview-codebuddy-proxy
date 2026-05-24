import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'

interface Config {
  port: number
  api_password_set: boolean
  cache_enabled: boolean
  cache_ttl: number
  base_url: string
  locale: string
}

export default function ConfigPage() {
  const { t, i18n } = useTranslation()
  const [config, setConfig] = useState<Config | null>(null)
  const [cacheEnabled, setCacheEnabled] = useState(false)
  const [cacheTTL, setCacheTTL] = useState(300)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    fetch('/api/config')
      .then((r) => r.json())
      .then((data) => {
        setConfig(data)
        setCacheEnabled(data.cache_enabled || false)
        setCacheTTL(data.cache_ttl || 300)
      })
      .catch(() => {})
  }, [])

  const saveConfig = async () => {
    setSaving(true)
    try {
      await fetch('/api/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ cache_enabled: cacheEnabled, cache_ttl: cacheTTL }),
      })
    } catch { /* ignore */ }
    setSaving(false)
  }

  const changeLocale = async (locale: string) => {
    i18n.changeLanguage(locale)
    try {
      await fetch('/api/locale', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ locale }),
      })
    } catch { /* ignore */ }
  }

  if (!config) return <div className="text-slate-400">{t('common.loading')}</div>

  return (
    <div className="space-y-6">
      <h2 className="text-xl font-bold text-white">{t('config.title')}</h2>

      <div className="bg-slate-800 rounded-xl p-4 border border-slate-700 space-y-3">
        <h3 className="text-sm font-medium text-slate-300">{t('config.server')}</h3>
        <div className="grid grid-cols-2 gap-3 text-sm">
          <div className="text-slate-400">{t('config.port')}</div>
          <div className="text-white">{config.port}</div>
          <div className="text-slate-400">{t('config.upstreamUrl')}</div>
          <div className="text-white font-mono text-xs">{config.base_url}</div>
          <div className="text-slate-400">{t('config.apiPassword')}</div>
          <div className="text-white">{config.api_password_set ? t('config.set') : t('config.notSet')}</div>
        </div>
      </div>

      <div className="bg-slate-800 rounded-xl p-4 border border-slate-700 space-y-3">
        <h3 className="text-sm font-medium text-slate-300">{t('config.language')}</h3>
        <div className="flex gap-2">
          <button
            onClick={() => changeLocale('zh-CN')}
            className={`px-3 py-1.5 rounded-lg text-sm font-medium transition-colors ${
              i18n.language === 'zh-CN' ? 'bg-blue-600 text-white' : 'bg-slate-700 text-slate-300 hover:bg-slate-600'
            }`}
          >
            中文
          </button>
          <button
            onClick={() => changeLocale('en')}
            className={`px-3 py-1.5 rounded-lg text-sm font-medium transition-colors ${
              i18n.language === 'en' ? 'bg-blue-600 text-white' : 'bg-slate-700 text-slate-300 hover:bg-slate-600'
            }`}
          >
            English
          </button>
        </div>
      </div>

      <div className="bg-slate-800 rounded-xl p-4 border border-slate-700 space-y-4">
        <h3 className="text-sm font-medium text-slate-300">{t('config.cacheSettings')}</h3>
        <div className="space-y-3">
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={cacheEnabled}
              onChange={(e) => setCacheEnabled(e.target.checked)}
              className="rounded"
            />
            <span className="text-white">{t('config.enableCache')}</span>
          </label>
          <div className="flex items-center gap-3">
            <span className="text-sm text-slate-400">{t('config.cacheTtl')}</span>
            <input
              type="number"
              value={cacheTTL}
              onChange={(e) => setCacheTTL(Number(e.target.value))}
              className="bg-slate-900 border border-slate-600 rounded-lg px-3 py-1.5 text-sm text-white w-24 focus:outline-none focus:border-blue-500"
            />
          </div>
        </div>
        <button onClick={saveConfig} disabled={saving} className="btn btn-primary">
          {saving ? t('common.saving') : t('common.save')}
        </button>
      </div>
    </div>
  )
}
