import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import OverviewPage from './pages/OverviewPage.vue'
import CatalogPage from './pages/CatalogPage.vue'
import InstancesPage from './pages/InstancesPage.vue'
import InstanceDetailPage from './pages/InstanceDetailPage.vue'
import NamespaceClaimsPage from './pages/NamespaceClaimsPage.vue'
import NamespaceClaimDetailPage from './pages/NamespaceClaimDetailPage.vue'
import TenancyPage from './pages/TenancyPage.vue'
import AuditPage from './pages/AuditPage.vue'
import AdminPage from './pages/AdminPage.vue'
import LoginPage from './pages/LoginPage.vue'
import {
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
    { path: '/login', name: 'login', component: LoginPage, meta: { publicLayout: true } },
    { path: '/', name: 'overview', component: OverviewPage },
    { path: '/catalog', name: 'catalog', component: CatalogPage },
    { path: '/instances', name: 'instances', component: InstancesPage, meta: { requireCapability: 'instances' } },
    { path: '/instances/:name', name: 'instance-detail', component: InstanceDetailPage, props: true, meta: { requireCapability: 'instances' } },
    { path: '/namespace-claims', name: 'namespace-claims', component: NamespaceClaimsPage, meta: { requireCapability: 'instances' } },
    { path: '/namespace-claims/:name', name: 'namespace-claim-detail', component: NamespaceClaimDetailPage, props: true, meta: { requireCapability: 'instances' } },
    { path: '/tenancy', name: 'tenancy', component: TenancyPage, meta: { requireCapability: 'tenancy' } },
    { path: '/audit', name: 'audit', component: AuditPage, meta: { requireCapability: 'audit' } },
    { path: '/admin', name: 'admin', component: AdminPage, meta: { requireCapability: 'admin' } }
  ]
})

await initializeAuth()

router.beforeEach((to) => {
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
