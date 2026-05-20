import { ref } from 'vue'

export interface AuthConfig {
  mode: 'header' | 'oidc'
  allowDemoHeaders?: boolean
  loginPath?: string
  logoutPath?: string
  oidc?: {
    issuerUrl: string
    clientId: string
    scopes?: string[]
    redirectPath?: string
  }
}

export interface AuthSession {
  mode: 'header' | 'oidc'
  name: string
  roles: string[]
  groups: string[]
  authenticated: boolean
  allowDemoHeaders?: boolean
}

export const authConfig = ref<AuthConfig | null>(null)
export const authSession = ref<AuthSession | null>(null)
export const authReady = ref(false)
export const authError = ref<string | null>(null)

export async function initializeAuth() {
  authReady.value = false
  authError.value = null
  authConfig.value = await fetchJSON<AuthConfig>('/api/auth/config')
  try {
    authSession.value = await fetchJSON<AuthSession>('/api/auth/session', {
      headers: authHeaders()
    })
  } catch (err) {
    authError.value = err instanceof Error ? err.message : 'Failed to initialize authentication'
    authSession.value = null
  }
  authReady.value = true
}

export function authHeaders(): HeadersInit {
  const config = authConfig.value
  if (!config || config.mode === 'header') {
    return {
      Accept: 'application/json',
      'Content-Type': 'application/json',
      'X-Servicer-User': localStorage.getItem('servicer.user') || 'demo@servicer.local',
      'X-Servicer-Roles':
        localStorage.getItem('servicer.roles') || 'platform-admin,tenant-operator,service-consumer'
    }
  }
  return {
    Accept: 'application/json',
    'Content-Type': 'application/json'
  }
}

export function beginLogin(returnTo?: string | Event) {
  const targetPath =
    typeof returnTo === 'string' ? returnTo : window.location.pathname + window.location.search
  const path = authConfig.value?.loginPath || '/api/auth/login'
  const url = new URL(path, window.location.origin)
  url.searchParams.set('returnTo', targetPath)
  window.location.assign(url.toString())
}

export function logout(returnTo?: string | Event) {
  const targetPath = typeof returnTo === 'string' ? returnTo : '/'
  const path = authConfig.value?.logoutPath || '/api/auth/logout'
  const url = new URL(path, window.location.origin)
  url.searchParams.set('returnTo', targetPath)
  window.location.assign(url.toString())
}

async function fetchJSON<T>(path: string, init: RequestInit = {}) {
  const response = await fetch(path, init)
  if (!response.ok) {
    const message = await response.text()
    throw new Error(message || `Request failed: ${response.status}`)
  }
  return (await response.json()) as T
}
