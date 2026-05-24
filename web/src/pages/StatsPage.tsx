import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'

interface Stats {
  total_requests: number
  success_count: number
  error_count: number
  models_used: Record<string, number>
  uptime_seconds: number
}

export default function StatsPage() {
  const { t } = useTranslation()
  const [stats, setStats] = useState<Stats | null>(null)

  useEffect(() => {
    fetch('/api/stats')
      .then((r) => r.json())
      .then(setStats)
      .catch(() => {})
  }, [])

  if (!stats) return <div className="text-slate-400">{t('common.loading')}</div>

  const formatUptime = (s: number) => {
    const h = Math.floor(s / 3600)
    const m = Math.floor((s % 3600) / 60)
    return t('stats.hoursMinutes', { h, m })
  }

  return (
    <div className="space-y-6">
      <h2 className="text-xl font-bold text-white">{t('stats.title')}</h2>

      <div className="grid grid-cols-3 gap-4">
        <div className="bg-slate-800 rounded-xl p-4 border border-slate-700">
          <div className="text-2xl font-bold text-white">{stats.total_requests}</div>
          <div className="text-sm text-slate-400">{t('stats.totalRequests')}</div>
        </div>
        <div className="bg-slate-800 rounded-xl p-4 border border-slate-700">
          <div className="text-2xl font-bold text-green-400">{stats.success_count}</div>
          <div className="text-sm text-slate-400">{t('stats.successful')}</div>
        </div>
        <div className="bg-slate-800 rounded-xl p-4 border border-slate-700">
          <div className="text-2xl font-bold text-red-400">{stats.error_count}</div>
          <div className="text-sm text-slate-400">{t('stats.errors')}</div>
        </div>
      </div>

      <div className="bg-slate-800 rounded-xl p-4 border border-slate-700">
        <h3 className="text-sm font-medium text-slate-300 mb-3">{t('stats.uptime')}</h3>
        <div className="text-lg text-white">{formatUptime(stats.uptime_seconds)}</div>
      </div>

      <div className="bg-slate-800 rounded-xl p-4 border border-slate-700">
        <h3 className="text-sm font-medium text-slate-300 mb-3">{t('stats.modelsUsed')}</h3>
        <div className="space-y-2">
          {Object.entries(stats.models_used || {}).map(([model, count]) => (
            <div key={model} className="flex items-center justify-between text-sm">
              <span className="font-mono text-white">{model}</span>
              <span className="text-slate-400">{t('stats.requests', { count })}</span>
            </div>
          ))}
          {Object.keys(stats.models_used || {}).length === 0 && (
            <div className="text-slate-500 text-sm">{t('stats.noRequests')}</div>
          )}
        </div>
      </div>
    </div>
  )
}
