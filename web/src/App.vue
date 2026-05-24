<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import {
  authError,
  authReady,
  authSession,
  canViewAdminShell,
  canViewAudit,
  canViewInstances,
  canViewTenancy,
  logout
} from './auth'

const route = useRoute()
const usePublicLayout = computed(() => route.meta.publicLayout === true)
</script>

<template>
  <RouterView v-if="usePublicLayout" />

  <section v-else-if="!authReady" class="app-loading-shell">
    <div class="content-band app-loading-card">
      <p class="eyebrow">Authentication</p>
      <h2>Loading access policy</h2>
      <p class="muted">Connecting to the configured Servicer authentication flow.</p>
    </div>
  </section>

  <section v-else-if="authError && !authSession?.authenticated" class="app-loading-shell">
    <div class="content-band app-loading-card">
      <p class="eyebrow">Authentication</p>
      <h2>Unable to establish a session</h2>
      <p class="error-text">{{ authError }}</p>
    </div>
  </section>

  <div v-else class="app-shell">
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
        <RouterLink v-if="canViewInstances()" to="/instances">Instances</RouterLink>
        <RouterLink v-if="canViewInstances()" to="/namespace-claims">Namespace claims</RouterLink>
        <RouterLink v-if="canViewTenancy()" to="/tenancy">Tenancy</RouterLink>
        <RouterLink v-if="canViewAudit()" to="/audit">Audit</RouterLink>
        <RouterLink v-if="canViewAdminShell()" to="/admin">Admin</RouterLink>
      </nav>
      <div class="auth-panel">
        <template v-if="authReady && authSession?.authenticated">
          <strong>{{ authSession.name }}</strong>
          <small>{{ authSession.provider || 'authenticated' }} · {{ (authSession.roles ?? []).join(', ') || 'no platform roles' }}</small>
          <button class="button secondary compact-button" @click="logout">Sign out</button>
        </template>
        <template v-else>
          <strong>Authentication required</strong>
          <small>Select a configured identity provider to continue.</small>
        </template>
      </div>
    </aside>

    <main class="main">
      <RouterView />
    </main>
  </div>
</template>
