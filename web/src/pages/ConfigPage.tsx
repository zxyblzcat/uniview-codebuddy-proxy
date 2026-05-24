import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../auth'

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
  const { authFetch } = useAuth()
  const [config, setConfig] = useState<Config | null>(null)
  const [cacheEnabled, setCacheEnabled] = useState(false)
  const [cacheTTL, setCacheTTL] = useState(300)
  const [saving, setSaving] = useState(false)
  const [saveMsg, setSaveMsg] = useState<{ ok: boolean; text: string } | null>(null)

  useEffect(() => {
    authFetch('/api/config')
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
    setSaveMsg(null)
    try {
      const res = await authFetch('/api/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ cache_enabled: cacheEnabled, cache_ttl: cacheTTL }),
      })
      if (res.ok) {
        setSaveMsg({ ok: true, text: t('common.saved') })
      } else {
        setSaveMsg({ ok: false, text: t('common.saveFailed') })
      }
    } catch {
      setSaveMsg({ ok: false, text: t('common.saveFailed') })
    }
    setSaving(false)
  }

  const changeLocale = async (locale: string) => {
    try {
      const res = await authFetch('/api/locale', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ locale }),
      })
      if (res.ok) i18n.changeLanguage(locale)
    } catch { /* best-effort */ }
  }

  const isZh = i18n.language.startsWith('zh')

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
              isZh ? 'bg-blue-600 text-white' : 'bg-slate-700 text-slate-300 hover:bg-slate-600'
            }`}
          >
            中文
          </button>
          <button
            onClick={() => changeLocale('en')}
            className={`px-3 py-1.5 rounded-lg text-sm font-medium transition-colors ${
              !isZh ? 'bg-blue-600 text-white' : 'bg-slate-700 text-slate-300 hover:bg-slate-600'
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
        <div className="flex items-center gap-3">
          <button onClick={saveConfig} disabled={saving} className="btn btn-primary">
            {saving ? t('common.saving') : t('common.save')}
          </button>
          {saveMsg && (
            <span className={`text-xs ${saveMsg.ok ? 'text-green-400' : 'text-red-400'}`}>
              {saveMsg.text}
            </span>
          )}
        </div>
      </div>
    </div>
  )
}
