import { BrowserRouter, Routes, Route, NavLink } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { AuthProvider, useAuth } from './auth'
import TokenPage from './pages/TokenPage'
import LogPage from './pages/LogPage'
import ConfigPage from './pages/ConfigPage'
import StatsPage from './pages/StatsPage'
import LoginPage from './pages/LoginPage'

function AppInner() {
  const { t, i18n } = useTranslation()
  const { authenticated, needsPassword, authFetch } = useAuth()
  const isZh = i18n.language.startsWith('zh')

  const switchLang = async () => {
    const next = isZh ? 'en' : 'zh-CN'
    try {
      const res = await authFetch('/api/locale', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ locale: next }),
      })
      if (res.ok) i18n.changeLanguage(next)
    } catch { /* ignore */ }
  }

  if (!authenticated) return needsPassword ? <LoginPage /> : <div className="text-slate-400 flex items-center justify-center min-h-screen">{t('common.loading')}</div>

  return (
    <BrowserRouter basename="/admin">
      <div className="min-h-screen bg-slate-900">
        <nav className="bg-slate-800 border-b border-slate-700">
          <div className="max-w-7xl mx-auto px-4 py-3 flex items-center justify-between">
            <h1 className="text-lg font-bold text-blue-400">UniviewCodeBuddyProxy</h1>
            <div className="flex gap-1 items-center">
              {[
                { to: '/', label: t('nav.tokens'), end: true },
                { to: '/logs', label: t('nav.logs') },
                { to: '/config', label: t('nav.config') },
                { to: '/stats', label: t('nav.stats') },
              ].map((l) => (
                <NavLink
                  key={l.to}
                  to={l.to}
                  end={l.end}
                  className={({ isActive }) =>
                    `px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
                      isActive ? 'bg-blue-600 text-white' : 'text-slate-300 hover:bg-slate-700'
                    }`
                  }
                >
                  {l.label}
                </NavLink>
              ))}
              <button
                onClick={switchLang}
                className="ml-2 px-2 py-1 rounded-md text-xs font-medium text-slate-300 hover:bg-slate-700 border border-slate-600 transition-colors"
              >
                {isZh ? 'EN' : '中'}
              </button>
            </div>
          </div>
        </nav>
        <main className="max-w-7xl mx-auto px-4 py-6">
          <Routes>
            <Route path="/" element={<TokenPage />} />
            <Route path="/logs" element={<LogPage />} />
            <Route path="/config" element={<ConfigPage />} />
            <Route path="/stats" element={<StatsPage />} />
          </Routes>
        </main>
      </div>
    </BrowserRouter>
  )
}

export default function App() {
  return (
    <AuthProvider>
      <AppInner />
    </AuthProvider>
  )
}
