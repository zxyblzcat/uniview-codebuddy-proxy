import { useEffect, useRef, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { NO_PASSWORD, useAuth } from '../auth'

const MAX_LOGS = 500
const BASE_RETRY_MS = 2000
const MAX_RETRY_MS = 30000

interface LogEntry {
  id: number
  text: string
}

export default function LogPage() {
  const { t } = useTranslation()
  const { authFetch } = useAuth()
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [connected, setConnected] = useState(false)
  const [autoScroll, setAutoScroll] = useState(true)
  type FilterMode = 'all' | 'debug' | 'hide-debug'
  const [filter, setFilter] = useState<FilterMode>('all')
  const containerRef = useRef<HTMLDivElement>(null)
  const retryRef = useRef(BASE_RETRY_MS)
  const logIdRef = useRef(0)
  const retryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const esRef = useRef<EventSource | null>(null)
  const mountedRef = useRef(true)

  const buildSSEUrl = useCallback(() => {
    const stored = sessionStorage.getItem('api_password')
    if (stored && stored !== NO_PASSWORD) {
      return `/api/logs/stream?api_key=${encodeURIComponent(stored)}`
    }
    return '/api/logs/stream'
  }, [])

  const connect = useCallback(() => {
    const es = new EventSource(buildSSEUrl())
    esRef.current = es
    es.onopen = () => {
      if (!mountedRef.current) return
      setConnected(true)
      setLogs([] as LogEntry[])
      retryRef.current = BASE_RETRY_MS
    }
    es.onmessage = (e) => {
      if (!mountedRef.current) return
      logIdRef.current++
      const id = logIdRef.current
      setLogs((prev) => {
        const next = [...prev, { id, text: e.data }]
        return next.length > MAX_LOGS ? next.slice(-MAX_LOGS) : next
      })
    }
    es.onerror = () => {
      if (!mountedRef.current) return
      setConnected(false)
      es.close()
      esRef.current = null
      const delay = retryRef.current
      retryRef.current = Math.min(delay * 2, MAX_RETRY_MS)
      retryTimerRef.current = setTimeout(connect, delay)
    }
    return es
  }, [buildSSEUrl])

  useEffect(() => {
    mountedRef.current = true
    const es = connect()
    return () => {
      mountedRef.current = false
      es.close()
      if (esRef.current) esRef.current.close()
      if (retryTimerRef.current) clearTimeout(retryTimerRef.current)
    }
  }, [connect])

  useEffect(() => {
    if (autoScroll && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight
    }
  }, [logs, autoScroll])

  const clearLogs = () => {
    setLogs([] as LogEntry[])
    authFetch('/api/logs', { method: 'DELETE' }).catch(() => {})
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-bold text-white">{t('logs.title')}</h2>
        <div className="flex items-center gap-3">
          <label className="flex items-center gap-1.5 text-sm text-slate-400">
            <input type="checkbox" checked={autoScroll} onChange={(e) => setAutoScroll(e.target.checked)} />
            {t('logs.autoScroll')}
          </label>
          <div className="flex items-center gap-1 bg-slate-800 rounded-lg p-0.5">
            <button
              onClick={() => setFilter('all')}
              className={`px-2 py-0.5 rounded-md text-xs font-medium transition-colors ${
                filter === 'all' ? 'bg-blue-600 text-white' : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              {t('logs.filterAll')}
            </button>
            <button
              onClick={() => setFilter('debug')}
              className={`px-2 py-0.5 rounded-md text-xs font-medium transition-colors ${
                filter === 'debug' ? 'bg-blue-600 text-white' : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              {t('logs.filterDebug')}
            </button>
            <button
              onClick={() => setFilter('hide-debug')}
              className={`px-2 py-0.5 rounded-md text-xs font-medium transition-colors ${
                filter === 'hide-debug' ? 'bg-blue-600 text-white' : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              {t('logs.filterHideDebug')}
            </button>
          </div>
          <button onClick={clearLogs} className="btn btn-secondary text-xs">{t('logs.clear')}</button>
          <span className={`text-xs ${connected ? 'text-green-400' : 'text-red-400'}`}>
            {connected ? t('logs.connected') : t('logs.disconnected')}
          </span>
        </div>
      </div>
      <div
        ref={containerRef}
        className="bg-slate-950 rounded-xl border border-slate-700 p-4 h-[70vh] overflow-y-auto font-mono text-xs text-slate-300 space-y-0.5"
      >
        {(() => {
          const filteredLogs = logs.filter((log) => {
            if (filter === 'all') return true
            if (filter === 'debug') return log.text.startsWith('[DEBUG]')
            if (filter === 'hide-debug') return !log.text.startsWith('[DEBUG]')
            return true
          })
          return filteredLogs.length === 0 ? (
            <div className="text-slate-500">{t('logs.waiting')}</div>
          ) : (
            filteredLogs.map((log) => (
              <div key={log.id} className="whitespace-pre-wrap break-all hover:bg-slate-800/50 rounded px-1">
                {log.text}
              </div>
            ))
          )
        })()}
      </div>
    </div>
  )
}

