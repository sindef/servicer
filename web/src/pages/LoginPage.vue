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
    <div class="content-band login-card">
      <div class="login-card-header">
        <span class="brand-mark">S</span>
        <div>
          <h1>Sign in</h1>
          <p class="muted">Servicer</p>
        </div>
      </div>

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
          <button class="button primary login-submit" @click="beginOIDCLogin(providerDetails.name, returnTo)">
            Continue with {{ providerDetails.displayName }}
          </button>
        </div>

        <form v-else class="form-grid login-form" @submit.prevent="submitPasswordLogin">
          <label style="grid-column: 1 / -1">
            Username
            <input v-model="loginForm.username" type="text" autocomplete="username" />
          </label>
          <label style="grid-column: 1 / -1">
            Password
            <input v-model="loginForm.password" type="password" autocomplete="current-password" />
          </label>
          <p v-if="loginError" class="error-text" style="grid-column: 1 / -1">{{ loginError }}</p>
          <button class="button primary login-submit" :disabled="loginLoading || !providerDetails" type="submit">
            Sign in
          </button>
        </form>
      </section>
    </div>
  </div>
</template>
