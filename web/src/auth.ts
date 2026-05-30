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
  credentialRevealReauthPath?: string
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
  roles?: string[]
  groups?: string[]
  tenants?: TenantRoleSummary[]
  authenticated: boolean
}

export const authConfig = ref<AuthConfig | null>(null)
export const authSession = ref<AuthSession | null>(null)
export type AuthState = 'loading' | 'error' | 'authenticated' | 'anonymous'
export const authState = ref<AuthState>('loading')
export const authError = ref<string | null>(null)
export const authReady = computed(() => authState.value !== 'loading')

export const availableAuthProviders = computed(() => authConfig.value?.providers ?? [])
export const sessionRoles = computed(() => authSession.value?.roles ?? [])
export const sessionTenantRoles = computed(() => authSession.value?.tenants ?? [])

export function hasPlatformRole(...roles: string[]) {
  const currentRoles = authSession.value?.roles ?? []
  return currentRoles.includes('platform-admin') || roles.some((role) => currentRoles.includes(role))
}

export function hasAnyTenantRole(...roles: string[]) {
  return (authSession.value?.tenants ?? []).some((tenant) =>
    tenant.roles.some((role) => roles.includes(role))
  )
}

export function canViewInstances() {
  return hasPlatformRole() || hasAnyTenantRole('tenant-admin', 'tenant-operator', 'service-consumer')
}

export function canViewTenancy() {
  return hasPlatformRole() || (authSession.value?.tenants ?? []).length > 0
}

export function canViewAudit() {
  return hasPlatformRole('auditor')
}

export function canManageRepositories() {
  return hasPlatformRole() || hasAnyTenantRole('tenant-admin', 'tenant-operator')
}

export function canViewAdminShell() {
  return hasPlatformRole('catalog-admin', 'cluster-admin') || canManageRepositories()
}

export function canViewAuthAdmin() {
  return hasPlatformRole()
}

export function canViewClusterAdmin() {
  return hasPlatformRole('cluster-admin')
}

export function canViewCatalogAdmin() {
  return hasPlatformRole('catalog-admin')
}

export async function initializeAuth() {
  authState.value = 'loading'
  authError.value = null
  try {
    authConfig.value = await fetchJSON<AuthConfig>('/api/auth/config')
  } catch (err) {
    authConfig.value = null
    authSession.value = null
    authError.value = describeError(err, 'Failed to load authentication configuration')
    authState.value = 'error'
    return
  }
  try {
    authSession.value = normalizeAuthSession(await fetchJSON<AuthSession>('/api/auth/session', {
      headers: authHeaders()
    }))
    authState.value = authSession.value.authenticated ? 'authenticated' : 'anonymous'
  } catch (err) {
    authError.value = describeError(err, 'Failed to establish authentication session')
    authSession.value = null
    authState.value = 'anonymous'
  }
}

export async function retryAuthInitialization() {
  await initializeAuth()
}

export function markSessionExpired(message = 'Session expired. Sign in again to continue.') {
  authSession.value = null
  authError.value = message
  authState.value = 'anonymous'
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
  authSession.value = normalizeAuthSession(await fetchJSON<AuthSession>('/api/auth/session', {
    headers: authHeaders()
  }))
  authError.value = null
  authState.value = authSession.value.authenticated ? 'authenticated' : 'anonymous'
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
    throw new Error(message || `Request failed (${response.status})`)
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

function normalizeAuthSession(session: AuthSession): AuthSession {
  return {
    ...session,
    roles: session.roles ?? [],
    groups: session.groups ?? [],
    tenants: session.tenants ?? []
  }
}

function describeError(err: unknown, fallback: string) {
  return err instanceof Error && err.message ? err.message : fallback
}

if (typeof window !== 'undefined') {
  window.addEventListener('servicer:auth-expired', () => {
    markSessionExpired()
  })
}
