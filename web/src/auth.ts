import { ref } from 'vue'

export interface AuthConfig {
  mode: 'header' | 'oidc'
  allowDemoHeaders?: boolean
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

const TOKEN_KEY = 'servicer.oidc.token'
const STATE_KEY = 'servicer.oidc.state'
const VERIFIER_KEY = 'servicer.oidc.verifier'
const RETURN_TO_KEY = 'servicer.oidc.return_to'

export const authConfig = ref<AuthConfig | null>(null)
export const authSession = ref<AuthSession | null>(null)
export const authReady = ref(false)
export const authError = ref<string | null>(null)

type OIDCMetadata = {
  authorization_endpoint: string
  token_endpoint: string
  end_session_endpoint?: string
}

export async function initializeAuth() {
  authReady.value = false
  authError.value = null
  authConfig.value = await fetchJSON<AuthConfig>('/api/auth/config')
  if (authConfig.value.mode === 'oidc') {
    if (window.location.pathname === oidcRedirectPath() && window.location.search.includes('code=')) {
      try {
        await completeOIDCCallback()
      } catch (err) {
        clearOIDCState()
        clearOIDCToken()
        authError.value = err instanceof Error ? err.message : 'OIDC callback failed'
        window.history.replaceState({}, '', '/')
      }
    }
    const token = oidcToken()
    if (token) {
      try {
        authSession.value = await fetchAuthSession(token)
      } catch (err) {
        clearOIDCToken()
        authError.value = err instanceof Error ? err.message : 'Failed to restore session'
      }
    }
  } else {
    authSession.value = await fetchAuthSession()
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
  const token = oidcToken()
  return token
    ? {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`
      }
    : {
        Accept: 'application/json',
        'Content-Type': 'application/json'
      }
}

export async function beginLogin(returnTo = window.location.pathname + window.location.search) {
  const config = authConfig.value
  if (!config?.oidc) {
    throw new Error('OIDC login is not configured')
  }
  const metadata = await fetchOIDCMetadata(config.oidc.issuerUrl)
  const state = randomString(24)
  const verifier = randomString(64)
  const challenge = await codeChallenge(verifier)
  sessionStorage.setItem(STATE_KEY, state)
  sessionStorage.setItem(VERIFIER_KEY, verifier)
  sessionStorage.setItem(RETURN_TO_KEY, returnTo)
  const scopes = config.oidc.scopes?.length ? config.oidc.scopes : ['openid', 'profile', 'email']
  const url = new URL(metadata.authorization_endpoint)
  url.searchParams.set('client_id', config.oidc.clientId)
  url.searchParams.set('redirect_uri', window.location.origin + oidcRedirectPath())
  url.searchParams.set('response_type', 'code')
  url.searchParams.set('scope', scopes.join(' '))
  url.searchParams.set('state', state)
  url.searchParams.set('code_challenge', challenge)
  url.searchParams.set('code_challenge_method', 'S256')
  window.location.assign(url.toString())
}

export async function logout() {
  const config = authConfig.value
  const token = oidcToken()
  clearOIDCState()
  clearOIDCToken()
  authSession.value = config?.mode === 'header' ? await fetchAuthSession() : null
  if (config?.mode !== 'oidc' || !config.oidc || !token) {
    window.location.assign('/')
    return
  }
  try {
    const metadata = await fetchOIDCMetadata(config.oidc.issuerUrl)
    if (metadata.end_session_endpoint) {
      const url = new URL(metadata.end_session_endpoint)
      url.searchParams.set('id_token_hint', token)
      url.searchParams.set('post_logout_redirect_uri', window.location.origin + '/')
      window.location.assign(url.toString())
      return
    }
  } catch {
    // Fall back to local logout only.
  }
  window.location.assign('/')
}

function oidcRedirectPath() {
  return authConfig.value?.oidc?.redirectPath || '/auth/callback'
}

function oidcToken() {
  return localStorage.getItem(TOKEN_KEY)
}

function clearOIDCToken() {
  localStorage.removeItem(TOKEN_KEY)
}

function clearOIDCState() {
  sessionStorage.removeItem(STATE_KEY)
  sessionStorage.removeItem(VERIFIER_KEY)
  sessionStorage.removeItem(RETURN_TO_KEY)
}

async function completeOIDCCallback() {
  const config = authConfig.value
  if (!config?.oidc) {
    throw new Error('OIDC login is not configured')
  }
  const params = new URLSearchParams(window.location.search)
  const expectedState = sessionStorage.getItem(STATE_KEY)
  const verifier = sessionStorage.getItem(VERIFIER_KEY)
  const returnTo = sessionStorage.getItem(RETURN_TO_KEY) || '/'
  const state = params.get('state')
  const code = params.get('code')
  const error = params.get('error')
  if (error) {
    throw new Error(params.get('error_description') || `OIDC provider returned ${error}`)
  }
  if (!expectedState || !verifier || !state || state !== expectedState || !code) {
    throw new Error('OIDC callback state validation failed')
  }
  const metadata = await fetchOIDCMetadata(config.oidc.issuerUrl)
  const body = new URLSearchParams({
    grant_type: 'authorization_code',
    client_id: config.oidc.clientId,
    code,
    redirect_uri: window.location.origin + oidcRedirectPath(),
    code_verifier: verifier
  })
  const response = await fetch(metadata.token_endpoint, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body
  })
  if (!response.ok) {
    throw new Error(`OIDC token exchange failed: ${response.status}`)
  }
  const tokenSet = (await response.json()) as { id_token?: string }
  if (!tokenSet.id_token) {
    throw new Error('OIDC token response did not include an id_token')
  }
  localStorage.setItem(TOKEN_KEY, tokenSet.id_token)
  clearOIDCState()
  authSession.value = await fetchAuthSession(tokenSet.id_token)
  window.history.replaceState({}, '', returnTo)
}

async function fetchAuthSession(token?: string) {
  return fetchJSON<AuthSession>('/api/auth/session', {
    headers: token
      ? {
          Accept: 'application/json',
          Authorization: `Bearer ${token}`
        }
      : authHeaders()
  })
}

async function fetchOIDCMetadata(issuerUrl: string) {
  return fetchJSON<OIDCMetadata>(`${issuerUrl.replace(/\/$/, '')}/.well-known/openid-configuration`)
}

async function fetchJSON<T>(path: string, init: RequestInit = {}) {
  const response = await fetch(path, init)
  if (!response.ok) {
    const message = await response.text()
    throw new Error(message || `Request failed: ${response.status}`)
  }
  return (await response.json()) as T
}

function randomString(length: number) {
  const bytes = new Uint8Array(length)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

async function codeChallenge(verifier: string) {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(verifier))
  return base64URLEncode(new Uint8Array(digest))
}

function base64URLEncode(value: Uint8Array) {
  let binary = ''
  for (const byte of value) {
    binary += String.fromCharCode(byte)
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}
