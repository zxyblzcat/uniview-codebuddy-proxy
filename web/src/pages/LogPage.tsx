import { useEffect, useRef, useState } from 'react'

export default function LogPage() {
  const [logs, setLogs] = useState<string[]>([])
  const [connected, setConnected] = useState(false)
  const [autoScroll, setAutoScroll] = useState(true)
  const containerRef = useRef<HTMLDivElement>(null)
  const wsRef = useRef<EventSource | null>(null)

  useEffect(() => {
    const es = new EventSource('/api/logs/stream')
    wsRef.current = es
    es.onopen = () => setConnected(true)
    es.onmessage = (e) => {
      setLogs((prev) => {
        const next = [...prev, e.data]
        return next.length > 500 ? next.slice(-500) : next
      })
    }
    es.onerror = () => setConnected(false)
    return () => es.close()
  }, [])

  useEffect(() => {
    if (autoScroll && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight
    }
  }, [logs, autoScroll])

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-bold text-white">Live Logs</h2>
        <div className="flex items-center gap-3">
          <label className="flex items-center gap-1.5 text-sm text-slate-400">
            <input type="checkbox" checked={autoScroll} onChange={(e) => setAutoScroll(e.target.checked)} />
            Auto-scroll
          </label>
          <button onClick={() => setLogs([])} className="btn btn-secondary text-xs">Clear</button>
          <span className={`text-xs ${connected ? 'text-green-400' : 'text-red-400'}`}>
            {connected ? 'Connected' : 'Disconnected'}
          </span>
        </div>
      </div>
      <div
        ref={containerRef}
        className="bg-slate-950 rounded-xl border border-slate-700 p-4 h-[70vh] overflow-y-auto font-mono text-xs text-slate-300 space-y-0.5"
      >
        {logs.length === 0 ? (
          <div className="text-slate-500">Waiting for logs...</div>
        ) : (
          logs.map((log, i) => (
            <div key={i} className="whitespace-pre-wrap break-all hover:bg-slate-800/50 rounded px-1">
              {log}
            </div>
          ))
        )}
      </div>
    </div>
  )
}
