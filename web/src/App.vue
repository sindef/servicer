<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import {
  authConfig,
  authError,
  authReady,
  authSession,
  availableAuthProviders,
  beginOIDCLogin,
  completePasswordLogin,
  logout
} from './auth'

const selectedProvider = ref('')
const loginForm = reactive({
  username: '',
  password: ''
})
const loginError = ref<string | null>(null)
const loginLoading = ref(false)

const providers = computed(() => availableAuthProviders.value)
const providerDetails = computed(() =>
  providers.value.find((provider) => provider.name === selectedProvider.value) ?? null
)

watch(
  providers,
  (nextProviders) => {
    if (!selectedProvider.value && nextProviders.length > 0) {
      selectedProvider.value =
        authConfig.value?.defaultProvider ||
        nextProviders.find((provider) => provider.default)?.name ||
        nextProviders[0]?.name ||
        ''
    }
  },
  { immediate: true }
)

async function submitPasswordLogin() {
  if (!providerDetails.value) return
  loginLoading.value = true
  loginError.value = null
  try {
    await completePasswordLogin(providerDetails.value.name, loginForm.username, loginForm.password)
    loginForm.password = ''
  } catch (err) {
    loginError.value = err instanceof Error ? err.message : 'Login failed'
  } finally {
    loginLoading.value = false
  }
}
</script>

<template>
  <div class="app-shell">
    <aside class="sidebar">
      <RouterLink class="brand" to="/">
        <span class="brand-mark">S</span>
        <span>
          <strong>Servicer</strong>
          <small>Product control plane</small>
        </span>
      </RouterLink>
      <nav class="nav">
        <RouterLink to="/">Overview</RouterLink>
        <RouterLink to="/catalog">Catalog</RouterLink>
        <RouterLink to="/instances">Instances</RouterLink>
        <RouterLink to="/namespace-claims">Namespace claims</RouterLink>
        <RouterLink to="/tenancy">Tenancy</RouterLink>
        <RouterLink to="/audit">Audit</RouterLink>
        <RouterLink to="/admin">Admin</RouterLink>
      </nav>
      <div class="auth-panel">
        <template v-if="authReady && authSession?.authenticated">
          <strong>{{ authSession.name }}</strong>
          <small>{{ authSession.provider || 'authenticated' }} · {{ authSession.roles.join(', ') || 'no platform roles' }}</small>
          <button class="button secondary compact-button" @click="logout">Sign out</button>
        </template>
        <template v-else>
          <strong>Authentication required</strong>
          <small>Select a configured identity provider to continue.</small>
        </template>
      </div>
    </aside>

    <main class="main">
      <section v-if="!authReady" class="content-band">
        <h2>Loading authentication</h2>
        <p class="muted">Connecting to the configured Servicer authentication flow.</p>
      </section>
      <section v-else-if="authError" class="content-band">
        <h2>Authentication error</h2>
        <p class="error-text">{{ authError }}</p>
      </section>
      <section v-else-if="!authSession?.authenticated" class="content-band auth-splash">
        <p class="eyebrow">Authentication</p>
        <h1>Sign in to Servicer</h1>
        <p class="muted">Choose one of the configured authentication providers below.</p>
        <div class="form-grid modal-form-grid">
          <label>
            Provider
            <select v-model="selectedProvider">
              <option v-for="provider in providers" :key="provider.name" :value="provider.name">
                {{ provider.displayName }} ({{ provider.type }})
              </option>
            </select>
          </label>
        </div>
        <div v-if="providerDetails?.type === 'oidc'" class="form-actions">
          <button class="button primary" @click="beginOIDCLogin(providerDetails.name)">
            Continue with {{ providerDetails.displayName }}
          </button>
        </div>
        <form v-else class="form-grid modal-form-grid" @submit.prevent="submitPasswordLogin">
          <label>
            Username
            <input v-model="loginForm.username" type="text" autocomplete="username" />
          </label>
          <label>
            Password
            <input v-model="loginForm.password" type="password" autocomplete="current-password" />
          </label>
          <p v-if="loginError" class="error-text">{{ loginError }}</p>
          <div class="form-actions">
            <button class="button primary" :disabled="loginLoading || !providerDetails" type="submit">
              <span v-if="providerDetails">Sign in with {{ providerDetails.displayName }}</span>
              <span v-else>Sign in</span>
            </button>
          </div>
        </form>
      </section>
      <RouterView v-else />
    </main>
  </div>
</template>
