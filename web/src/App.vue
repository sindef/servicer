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
        <RouterLink to="/tenancy">Tenancy</RouterLink>
        <RouterLink to="/audit">Audit</RouterLink>
        <RouterLink to="/admin">Admin</RouterLink>
      </nav>
    </aside>

    <main class="main">
      <RouterView />
    </main>
  </div>
</template>
