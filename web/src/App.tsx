import { BrowserRouter, Routes, Route, NavLink } from 'react-router-dom'
import TokenPage from './pages/TokenPage'
import LogPage from './pages/LogPage'
import ConfigPage from './pages/ConfigPage'
import StatsPage from './pages/StatsPage'

function App() {
  return (
    <BrowserRouter basename="/admin">
      <div className="min-h-screen bg-slate-900">
        <nav className="bg-slate-800 border-b border-slate-700">
          <div className="max-w-7xl mx-auto px-4 py-3 flex items-center justify-between">
            <h1 className="text-lg font-bold text-blue-400">CodeBuddy Proxy</h1>
            <div className="flex gap-1">
              {[
                { to: '/', label: 'Tokens' },
                { to: '/logs', label: 'Logs' },
                { to: '/config', label: 'Config' },
                { to: '/stats', label: 'Stats' },
              ].map((l) => (
                <NavLink
                  key={l.to}
                  to={l.to}
                  end={l.to === '/'}
                  className={({ isActive }) =>
                    `px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
                      isActive ? 'bg-blue-600 text-white' : 'text-slate-300 hover:bg-slate-700'
                    }`
                  }
                >
                  {l.label}
                </NavLink>
              ))}
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

export default App
