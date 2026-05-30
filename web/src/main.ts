import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import {
  authState,
  authSession,
  canViewAdminShell,
  canViewAudit,
  canViewInstances,
  canViewTenancy,
  initializeAuth
} from './auth'
import './styles.css'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('./pages/LoginPage.vue'), meta: { publicLayout: true } },
    { path: '/', name: 'overview', component: () => import('./pages/OverviewPage.vue') },
    { path: '/catalog', name: 'catalog', component: () => import('./pages/CatalogPage.vue') },
    { path: '/instances', name: 'instances', component: () => import('./pages/InstancesPage.vue'), meta: { requireCapability: 'instances' } },
    { path: '/instances/:name', name: 'instance-detail', component: () => import('./pages/InstanceDetailPage.vue'), props: true, meta: { requireCapability: 'instances' } },
    { path: '/namespace-claims', name: 'namespace-claims', component: () => import('./pages/NamespaceClaimsPage.vue'), meta: { requireCapability: 'instances' } },
    { path: '/namespace-claims/:name', name: 'namespace-claim-detail', component: () => import('./pages/NamespaceClaimDetailPage.vue'), props: true, meta: { requireCapability: 'instances' } },
    { path: '/tenancy', name: 'tenancy', component: () => import('./pages/TenancyPage.vue'), meta: { requireCapability: 'tenancy' } },
    { path: '/audit', name: 'audit', component: () => import('./pages/AuditPage.vue'), meta: { requireCapability: 'audit' } },
    { path: '/admin', name: 'admin', component: () => import('./pages/AdminPage.vue'), meta: { requireCapability: 'admin' } }
  ]
})

router.beforeEach((to) => {
  if (authState.value === 'loading') {
    return true
  }
  const authenticated = authSession.value?.authenticated === true
  if (to.name === 'login') {
    if (authenticated) {
      const returnTo = typeof to.query.returnTo === 'string' ? to.query.returnTo : '/'
      return returnTo.startsWith('/') ? returnTo : '/'
    }
    return true
  }
  if (!authenticated) {
    return {
      name: 'login',
      query: {
        returnTo: to.fullPath
      }
    }
  }
  if (!routeAllowed(to.meta.requireCapability)) {
    return '/'
  }
  return true
})

createApp(App).use(router).mount('#app')
void initializeAuth()

function routeAllowed(capability: unknown) {
  switch (capability) {
    case 'instances':
      return canViewInstances()
    case 'tenancy':
      return canViewTenancy()
    case 'audit':
      return canViewAudit()
    case 'admin':
      return canViewAdminShell()
    default:
      return true
  }
}
