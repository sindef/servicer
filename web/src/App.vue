<script setup lang="ts">
import { authConfig, authError, authReady, authSession, beginLogin, logout } from './auth'
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
        <template v-if="authReady && authConfig?.mode === 'oidc' && authSession?.authenticated">
          <strong>{{ authSession.name }}</strong>
          <small>{{ authSession.roles.join(', ') || 'Authenticated user' }}</small>
          <button class="button secondary compact-button" @click="logout">Sign out</button>
        </template>
        <template v-else-if="authReady && authConfig?.mode === 'oidc'">
          <strong>Sign in required</strong>
          <small>Use your identity provider account to access Servicer.</small>
          <button class="button primary compact-button" @click="beginLogin()">Sign in</button>
        </template>
        <template v-else>
          <strong>Demo mode</strong>
          <small>{{ authSession?.name || 'demo@servicer.local' }}</small>
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
        <button v-if="authConfig?.mode === 'oidc'" class="button primary" @click="beginLogin()">Try sign in again</button>
      </section>
      <section v-else-if="authConfig?.mode === 'oidc' && !authSession?.authenticated" class="content-band auth-splash">
        <p class="eyebrow">Authentication</p>
        <h1>Sign in to Servicer</h1>
        <p class="muted">Your control plane is configured for OpenID Connect. Use browser sign-in to establish a session.</p>
        <div class="form-actions">
          <button class="button primary" @click="beginLogin()">Continue to login</button>
        </div>
      </section>
      <RouterView v-else />
    </main>
  </div>
</template>
