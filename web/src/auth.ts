import { computed, ref } from 'vue'

export interface AuthProviderView {
  name: string
  displayName: string
  type: 'local' | 'oidc' | 'ldap'
  default?: boolean
}

export interface AuthConfig {
  mode: 'multi'
  loginPath?: string
  logoutPath?: string
  callbackPath?: string
  defaultProvider?: string
  providers?: AuthProviderView[]
}

export interface TenantRoleSummary {
  tenantName: string
  roles: string[]
}

export interface AuthSession {
  mode: 'multi'
  name: string
  email?: string
  userName?: string
  provider?: string
  roles: string[]
  groups: string[]
  tenants?: TenantRoleSummary[]
  authenticated: boolean
}

export const authConfig = ref<AuthConfig | null>(null)
export const authSession = ref<AuthSession | null>(null)
export const authReady = ref(false)
export const authError = ref<string | null>(null)

export const availableAuthProviders = computed(() => authConfig.value?.providers ?? [])

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
  const headers: Record<string, string> = {
    Accept: 'application/json',
    'Content-Type': 'application/json'
  }
  const token = csrfToken()
  if (token) {
    headers['X-CSRF-Token'] = token
  }
  return headers
}

export function beginOIDCLogin(provider?: string, returnTo?: string | Event) {
  const targetPath =
    typeof returnTo === 'string' ? returnTo : window.location.pathname + window.location.search
  const path = authConfig.value?.loginPath || '/api/auth/login'
  const url = new URL(path, window.location.origin)
  url.searchParams.set('returnTo', targetPath)
  if (provider) {
    url.searchParams.set('provider', provider)
  }
  window.location.assign(url.toString())
}

export async function completePasswordLogin(provider: string, username: string, password: string) {
  await fetchJSON<AuthSession>('/api/auth/login', {
    method: 'POST',
    headers: authHeaders(),
    body: JSON.stringify({ provider, username, password })
  })
  authSession.value = await fetchJSON<AuthSession>('/api/auth/session', {
    headers: authHeaders()
  })
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

function csrfToken(): string {
  return document.cookie
    .split(';')
    .map((part) => part.trim())
    .find((part) => part.startsWith('servicer_csrf='))
    ?.split('=')
    .slice(1)
    .join('=') || ''
}
