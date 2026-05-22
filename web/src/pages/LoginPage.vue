<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  authConfig,
  authError,
  authReady,
  authSession,
  availableAuthProviders,
  beginOIDCLogin,
  completePasswordLogin
} from '../auth'

const route = useRoute()
const router = useRouter()

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
const returnTo = computed(() => {
  const candidate = typeof route.query.returnTo === 'string' ? route.query.returnTo : '/'
  return candidate.startsWith('/') ? candidate : '/'
})

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

watch(
  () => authSession.value?.authenticated,
  async (authenticated) => {
    if (authenticated) {
      await router.replace(returnTo.value)
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
    await router.replace(returnTo.value)
  } catch (err) {
    loginError.value = err instanceof Error ? err.message : 'Login failed'
  } finally {
    loginLoading.value = false
  }
}
</script>

<template>
  <div class="login-shell">
    <section class="login-hero">
      <div class="login-brand">
        <span class="brand-mark">S</span>
        <div>
          <strong>Servicer</strong>
          <small>Enterprise product control plane</small>
        </div>
      </div>
      <p class="eyebrow">Authentication</p>
      <h1>Access the control plane</h1>
      <p class="muted login-copy">
        Sign in with one of the enterprise identity methods configured for this platform.
      </p>
      <div class="login-feature-list">
        <div class="login-feature">
          <strong>Tenant-scoped access</strong>
          <span>Role bindings and ownership determine the exact product surfaces each user can operate.</span>
        </div>
        <div class="login-feature">
          <strong>Multiple identity sources</strong>
          <span>Choose from local accounts, OpenID Connect, or LDAP depending on platform policy.</span>
        </div>
      </div>
    </section>

    <section class="login-panel">
      <div class="content-band login-card">
        <p class="eyebrow">Sign in</p>
        <h2>Authenticate to continue</h2>
        <p class="muted">The application shell remains hidden until an authenticated session is established.</p>

        <section v-if="!authReady" class="login-state">
          <p class="muted">Loading authentication providers...</p>
        </section>

        <section v-else-if="authError" class="login-state">
          <p class="error-text">{{ authError }}</p>
        </section>

        <section v-else-if="providers.length === 0" class="login-state">
          <p class="error-text">No authentication providers are enabled.</p>
        </section>

        <section v-else class="stack-gap">
          <div class="provider-picker">
            <button
              v-for="provider in providers"
              :key="provider.name"
              class="provider-option"
              :class="{ active: selectedProvider === provider.name }"
              @click="selectedProvider = provider.name"
            >
              <strong>{{ provider.displayName }}</strong>
              <span>{{ provider.type.toUpperCase() }}</span>
            </button>
          </div>

          <div v-if="providerDetails?.type === 'oidc'" class="stack-gap">
            <div class="login-method-card">
              <h3>{{ providerDetails.displayName }}</h3>
              <p class="muted">Use browser redirect sign-in through your configured identity provider.</p>
            </div>
            <div class="form-actions">
              <button class="button primary" @click="beginOIDCLogin(providerDetails.name, returnTo)">
                Continue with {{ providerDetails.displayName }}
              </button>
            </div>
          </div>

          <form v-else class="form-grid modal-form-grid" @submit.prevent="submitPasswordLogin">
            <div class="login-method-card" style="grid-column: 1 / -1">
              <h3>{{ providerDetails?.displayName || 'Credentials' }}</h3>
              <p class="muted">
                Enter your username and password for the selected
                {{ providerDetails?.type === 'ldap' ? 'directory' : 'local' }} provider.
              </p>
            </div>
            <label>
              Username
              <input v-model="loginForm.username" type="text" autocomplete="username" />
            </label>
            <label>
              Password
              <input v-model="loginForm.password" type="password" autocomplete="current-password" />
            </label>
            <p v-if="loginError" class="error-text" style="grid-column: 1 / -1">{{ loginError }}</p>
            <div class="form-actions" style="grid-column: 1 / -1">
              <button class="button primary" :disabled="loginLoading || !providerDetails" type="submit">
                Sign in
              </button>
            </div>
          </form>
        </section>
      </div>
    </section>
  </div>
</template>
