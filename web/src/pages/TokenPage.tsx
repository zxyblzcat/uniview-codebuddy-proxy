import { useState, useEffect } from 'react'

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
    if (!confirm(`Delete token for ${userId}?`)) return
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

  const statusBadge = (t: TokenInfo) => {
    if (t.unavailable) return <span className="badge badge-red">Unavailable</span>
    if (t.expired) return <span className="badge badge-red">Expired</span>
    if (t.in_cooldown) return <span className="badge badge-yellow">Cooldown</span>
    return <span className="badge badge-green">Active</span>
  }

  if (loading) return <div className="text-slate-400">Loading...</div>

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-bold text-white">Token Management</h2>
        <div className="flex gap-2">
          <button onClick={startAuth} className="btn btn-primary">+ OAuth Login</button>
        </div>
      </div>

      {/* Manual Token Input */}
      <div className="bg-slate-800 rounded-xl p-4 border border-slate-700">
        <h3 className="text-sm font-medium text-slate-300 mb-2">Add Token Manually</h3>
        <div className="flex gap-2">
          <input
            type="text"
            value={manualToken}
            onChange={(e) => setManualToken(e.target.value)}
            placeholder="Paste bearer token..."
            className="flex-1 bg-slate-900 border border-slate-600 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:border-blue-500"
          />
          <button onClick={addManualToken} className="btn btn-primary">Add</button>
        </div>
      </div>

      {/* Token List */}
      {tokens.length === 0 ? (
        <div className="bg-slate-800 rounded-xl p-8 border border-slate-700 text-center text-slate-400">
          No tokens found. Add one manually or start OAuth login.
        </div>
      ) : (
        <div className="space-y-3">
          {tokens.map((t) => (
            <div key={t.user_id} className="bg-slate-800 rounded-xl p-4 border border-slate-700">
              <div className="flex items-start justify-between">
                <div>
                  <div className="flex items-center gap-2 mb-1">
                    <span className="font-mono text-sm text-white">{t.user_id}</span>
                    {statusBadge(t)}
                  </div>
                  <div className="text-xs text-slate-400 space-x-4">
                    <span>Created: {formatTime(t.created_at)}</span>
                    <span>Expires: {formatTime(t.expires_at)}</span>
                  </div>
                </div>
                <div className="flex gap-2">
                  <button onClick={() => refreshToken(t.user_id)} className="btn btn-secondary text-xs">Refresh</button>
                  <button onClick={() => deleteToken(t.user_id)} className="btn btn-danger text-xs">Delete</button>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
