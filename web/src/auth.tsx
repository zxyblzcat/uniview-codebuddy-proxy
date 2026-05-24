import { createContext, useContext, useState, useCallback, useEffect, type ReactNode } from 'react'

interface AuthCtx {
  authenticated: boolean
  needsPassword: boolean
  login: (password: string) => Promise<boolean>
  authFetch: (input: string | URL, init?: RequestInit) => Promise<Response>
}

const AuthContext = createContext<AuthCtx | null>(null)

export const NO_PASSWORD = '__no_password__'

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setToken] = useState<string | null>(() => sessionStorage.getItem('api_password'))
  const [needsPassword, setNeedsPassword] = useState(true)

  // On mount, check if API_PASSWORD is set
  useEffect(() => {
    fetch('/api/config')
      .then((r) => {
        if (r.status === 401) {
          // Server requires password — show login
          setNeedsPassword(true)
          // Verify stored token if any
          const stored = sessionStorage.getItem('api_password')
          if (stored && stored !== NO_PASSWORD) {
            return fetch('/api/config', { headers: { Authorization: `Bearer ${stored}` } })
              .then((r2) => {
                if (r2.ok) {
                  setToken(stored)
                } else {
                  sessionStorage.removeItem('api_password')
                  setToken(null)
                }
              })
              .catch(() => {
                sessionStorage.removeItem('api_password')
                setToken(null)
              })
          }
          return
        }
        if (!r.ok) {
          // Server error — assume no password needed
          setNeedsPassword(false)
          return
        }
        return r.json().then((data) => {
          if (!data.api_password_set) {
            // No password required — auto-authenticate
            sessionStorage.setItem('api_password', NO_PASSWORD)
            setToken(NO_PASSWORD)
            setNeedsPassword(false)
          } else {
            setNeedsPassword(true)
          }
        })
      })
      .catch(() => {
        // Can't reach server — allow access
        setNeedsPassword(false)
      })
  }, [])

  const authenticated = token !== null

  const login = useCallback(async (password: string): Promise<boolean> => {
    const res = await fetch('/api/config', {
      headers: { Authorization: `Bearer ${password}` },
    })
    if (res.ok) {
      sessionStorage.setItem('api_password', password)
      setToken(password)
      return true
    }
    return false
  }, [])

  const authFetch = useCallback((input: string | URL, init?: RequestInit): Promise<Response> => {
    const headers = new Headers(init?.headers)
    if (token && token !== NO_PASSWORD) {
      headers.set('Authorization', `Bearer ${token}`)
    }
    return fetch(input, { ...init, headers }).then((res) => {
      if (res.status === 401 && token !== NO_PASSWORD) {
        sessionStorage.removeItem('api_password')
        setToken(null)
      }
      return res
    })
  }, [token])

  return (
    <AuthContext.Provider value={{ authenticated, needsPassword, login, authFetch }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
