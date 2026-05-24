import { useState, useEffect } from 'react'

interface Config {
  port: number
  api_password_set: boolean
  cache_enabled: boolean
  cache_ttl: number
  base_url: string
}

export default function ConfigPage() {
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

  if (!config) return <div className="text-slate-400">Loading...</div>

  return (
    <div className="space-y-6">
      <h2 className="text-xl font-bold text-white">Configuration</h2>

      {/* Read-only config */}
      <div className="bg-slate-800 rounded-xl p-4 border border-slate-700 space-y-3">
        <h3 className="text-sm font-medium text-slate-300">Server</h3>
        <div className="grid grid-cols-2 gap-3 text-sm">
          <div className="text-slate-400">Port</div>
          <div className="text-white">{config.port}</div>
          <div className="text-slate-400">Upstream URL</div>
          <div className="text-white font-mono text-xs">{config.base_url}</div>
          <div className="text-slate-400">API Password</div>
          <div className="text-white">{config.api_password_set ? 'Set' : 'Not set'}</div>
        </div>
      </div>

      {/* Editable config */}
      <div className="bg-slate-800 rounded-xl p-4 border border-slate-700 space-y-4">
        <h3 className="text-sm font-medium text-slate-300">Cache Settings</h3>
        <div className="space-y-3">
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={cacheEnabled}
              onChange={(e) => setCacheEnabled(e.target.checked)}
              className="rounded"
            />
            <span className="text-white">Enable response cache</span>
          </label>
          <div className="flex items-center gap-3">
            <span className="text-sm text-slate-400">Cache TTL (seconds):</span>
            <input
              type="number"
              value={cacheTTL}
              onChange={(e) => setCacheTTL(Number(e.target.value))}
              className="bg-slate-900 border border-slate-600 rounded-lg px-3 py-1.5 text-sm text-white w-24 focus:outline-none focus:border-blue-500"
            />
          </div>
        </div>
        <button onClick={saveConfig} disabled={saving} className="btn btn-primary">
          {saving ? 'Saving...' : 'Save Changes'}
        </button>
      </div>
    </div>
  )
}
