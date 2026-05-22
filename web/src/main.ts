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
import { authSession, initializeAuth } from './auth'
import './styles.css'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: LoginPage, meta: { publicLayout: true } },
    { path: '/', name: 'overview', component: OverviewPage },
    { path: '/catalog', name: 'catalog', component: CatalogPage },
    { path: '/instances', name: 'instances', component: InstancesPage },
    { path: '/instances/:name', name: 'instance-detail', component: InstanceDetailPage, props: true },
    { path: '/namespace-claims', name: 'namespace-claims', component: NamespaceClaimsPage },
    { path: '/namespace-claims/:name', name: 'namespace-claim-detail', component: NamespaceClaimDetailPage, props: true },
    { path: '/tenancy', name: 'tenancy', component: TenancyPage },
    { path: '/audit', name: 'audit', component: AuditPage },
    { path: '/admin', name: 'admin', component: AdminPage }
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
  return true
})

createApp(App).use(router).mount('#app')
