import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../auth'

export default function LoginPage() {
  const { t } = useTranslation()
  const { login } = useAuth()
  const [password, setPassword] = useState('')
  const [error, setError] = useState(false)
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!password.trim()) return
    setLoading(true)
    setError(false)
    const ok = await login(password.trim())
    if (!ok) setError(true)
    setLoading(false)
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-slate-900">
      <form onSubmit={handleSubmit} className="bg-slate-800 rounded-xl p-8 border border-slate-700 w-80 space-y-4">
        <h1 className="text-lg font-bold text-blue-400 text-center">UniviewCodeBuddyProxy</h1>
        <div>
          <label className="block text-sm text-slate-300 mb-1">{t('login.password')}</label>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="w-full bg-slate-900 border border-slate-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500"
            autoFocus
          />
        </div>
        {error && <div className="text-red-400 text-xs">{t('login.invalidPassword')}</div>}
        <button type="submit" disabled={loading} className="btn btn-primary w-full">
          {loading ? t('login.loggingIn') : t('login.submit')}
        </button>
      </form>
    </div>
  )
}
