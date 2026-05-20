import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import OverviewPage from './pages/OverviewPage.vue'
import CatalogPage from './pages/CatalogPage.vue'
import InstancesPage from './pages/InstancesPage.vue'
import InstanceDetailPage from './pages/InstanceDetailPage.vue'
import TenancyPage from './pages/TenancyPage.vue'
import AuditPage from './pages/AuditPage.vue'
import AdminPage from './pages/AdminPage.vue'
import './styles.css'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'overview', component: OverviewPage },
    { path: '/catalog', name: 'catalog', component: CatalogPage },
    { path: '/instances', name: 'instances', component: InstancesPage },
    { path: '/instances/:name', name: 'instance-detail', component: InstanceDetailPage, props: true },
    { path: '/tenancy', name: 'tenancy', component: TenancyPage },
    { path: '/audit', name: 'audit', component: AuditPage },
    { path: '/admin', name: 'admin', component: AdminPage }
  ]
})

createApp(App).use(router).mount('#app')
