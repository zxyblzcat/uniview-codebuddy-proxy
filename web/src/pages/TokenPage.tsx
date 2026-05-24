import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'

interface TokenInfo {
  user_id: string
  expires_at: number
  created_at: number
  expired: boolean
  unavailable: boolean
  in_cooldown: boolean
  cooldown_until: string
}

export default function TokenPage() {
  const { t } = useTranslation()
  const [tokens, setTokens] = useState<TokenInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [manualToken, setManualToken] = useState('')

  const fetchTokens = async () => {
    try {
      const res = await fetch('/auth/tokens')
      const data = await res.json()
      setTokens(data.tokens || [])
    } catch {
      setTokens([])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchTokens() }, [])

  const addManualToken = async () => {
    if (!manualToken.trim()) return
    try {
      const res = await fetch('/auth/manual', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ bearer_token: manualToken.trim() }),
      })
      if (res.ok) {
        setManualToken('')
        fetchTokens()
      }
    } catch { /* ignore */ }
  }

  const startAuth = async () => {
    try {
      const res = await fetch('/auth/start')
      const data = await res.json()
      if (data.auth_url) {
        window.open(data.auth_url, '_blank')
      }
    } catch { /* ignore */ }
  }

  const deleteToken = async (userId: string) => {
    if (!confirm(t('tokens.deleteConfirm', { userId }))) return
    try {
      await fetch(`/auth/tokens/${encodeURIComponent(userId)}`, { method: 'DELETE' })
      fetchTokens()
    } catch { /* ignore */ }
  }

  const refreshToken = async (userId: string) => {
    try {
      await fetch(`/auth/tokens/${encodeURIComponent(userId)}/refresh`, { method: 'POST' })
      fetchTokens()
    } catch { /* ignore */ }
  }

  const formatTime = (ts: number) => {
    if (!ts) return '-'
    return new Date(ts * 1000).toLocaleString()
  }

  const statusBadge = (tk: TokenInfo) => {
    if (tk.unavailable) return <span className="badge badge-red">{t('tokens.unavailable')}</span>
    if (tk.expired) return <span className="badge badge-red">{t('tokens.expired')}</span>
    if (tk.in_cooldown) return <span className="badge badge-yellow">{t('tokens.cooldown')}</span>
    return <span className="badge badge-green">{t('tokens.active')}</span>
  }

  if (loading) return <div className="text-slate-400">{t('common.loading')}</div>

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-bold text-white">{t('tokens.title')}</h2>
        <div className="flex gap-2">
          <button onClick={startAuth} className="btn btn-primary">{t('tokens.oauthLogin')}</button>
        </div>
      </div>

      <div className="bg-slate-800 rounded-xl p-4 border border-slate-700">
        <h3 className="text-sm font-medium text-slate-300 mb-2">{t('tokens.addManually')}</h3>
        <div className="flex gap-2">
          <input
            type="text"
            value={manualToken}
            onChange={(e) => setManualToken(e.target.value)}
            placeholder={t('tokens.placeholder')}
            className="flex-1 bg-slate-900 border border-slate-600 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:border-blue-500"
          />
          <button onClick={addManualToken} className="btn btn-primary">{t('common.add')}</button>
        </div>
      </div>

      {tokens.length === 0 ? (
        <div className="bg-slate-800 rounded-xl p-8 border border-slate-700 text-center text-slate-400">
          {t('tokens.empty')}
        </div>
      ) : (
        <div className="space-y-3">
          {tokens.map((tk) => (
            <div key={tk.user_id} className="bg-slate-800 rounded-xl p-4 border border-slate-700">
              <div className="flex items-start justify-between">
                <div>
                  <div className="flex items-center gap-2 mb-1">
                    <span className="font-mono text-sm text-white">{tk.user_id}</span>
                    {statusBadge(tk)}
                  </div>
                  <div className="text-xs text-slate-400 space-x-4">
                    <span>{t('tokens.created')}{formatTime(tk.created_at)}</span>
                    <span>{t('tokens.expires')}{formatTime(tk.expires_at)}</span>
                  </div>
                </div>
                <div className="flex gap-2">
                  <button onClick={() => refreshToken(tk.user_id)} className="btn btn-secondary text-xs">{t('common.refresh')}</button>
                  <button onClick={() => deleteToken(tk.user_id)} className="btn btn-danger text-xs">{t('common.delete')}</button>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
