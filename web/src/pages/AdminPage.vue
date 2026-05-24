<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import {
  api,
  type TenantSummary,
  type ProjectSummary,
  type ClusterTargetSummary,
  type ServiceClassAdminSummary,
  type RepositorySummary
} from '../api'
import { useApi } from '../composables/useApi'
import StatusPill from '../components/StatusPill.vue'
import AuthAdminPanel from '../components/AuthAdminPanel.vue'

// ── tabs ──────────────────────────────────────────────────────────────────────

type Tab = 'tenants' | 'projects' | 'clusters' | 'catalog' | 'repositories' | 'auth'
const activeTab = ref<Tab>('tenants')

// ── data ──────────────────────────────────────────────────────────────────────

const tenants = useApi(api.tenants)
const projects = useApi(api.projects)
const clusters = useApi(api.admin.clusters)
const serviceClasses = useApi(api.admin.serviceClasses)

const tenantRows = computed(() => tenants.data.value ?? [])
const projectRows = computed(() => projects.data.value ?? [])
const clusterRows = computed(() => clusters.data.value ?? [])
const serviceClassRows = computed(() => serviceClasses.data.value ?? [])

const knownClasses = computed(() =>
  serviceClassRows.value.length
    ? serviceClassRows.value.map((sc) => sc.name)
    : ['namespace', 'postgresql', 'mysql', 'valkey', 'nats']
)

// ── shared error/success ──────────────────────────────────────────────────────

const opError = ref<string | null>(null)
const opSuccess = ref<string | null>(null)
const opLoading = ref(false)

function clearOp() {
  opError.value = null
  opSuccess.value = null
}

async function runOp(fn: () => Promise<{ name: string; message: string }>) {
  opLoading.value = true
  opError.value = null
  opSuccess.value = null
  try {
    const res = await fn()
    opSuccess.value = res.message
  } catch (err) {
    opError.value = err instanceof Error ? err.message : 'Request failed'
  } finally {
    opLoading.value = false
  }
}

// ── Tenant modal ──────────────────────────────────────────────────────────────

const tenantModal = ref(false)
const editTenant = ref<TenantSummary | null>(null)

const tenantForm = reactive({
  name: '',
  displayName: '',
  owners: '',
  allowedServiceClasses: [] as string[]
})

function openNewTenant() {
  tenantForm.name = ''
  tenantForm.displayName = ''
  tenantForm.owners = ''
  tenantForm.allowedServiceClasses = []
  editTenant.value = null
  clearOp()
  tenantModal.value = true
}

function openEditTenant(t: TenantSummary) {
  tenantForm.name = t.name
  tenantForm.displayName = t.displayName
  tenantForm.owners = t.owners.join(', ')
  tenantForm.allowedServiceClasses = [...t.allowedServiceClasses]
  editTenant.value = t
  clearOp()
  tenantModal.value = true
}

function toggleServiceClass(name: string) {
  const idx = tenantForm.allowedServiceClasses.indexOf(name)
  if (idx === -1) {
    tenantForm.allowedServiceClasses.push(name)
  } else {
    tenantForm.allowedServiceClasses.splice(idx, 1)
  }
}

async function submitTenant() {
  const owners = tenantForm.owners
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)

  if (editTenant.value) {
    await runOp(() =>
      api.admin.updateTenant(editTenant.value!.name, {
        displayName: tenantForm.displayName,
        owners,
        allowedServiceClasses: tenantForm.allowedServiceClasses
      })
    )
  } else {
    await runOp(() =>
      api.admin.createTenant({
        name: tenantForm.name,
        displayName: tenantForm.displayName,
        owners,
        allowedServiceClasses: tenantForm.allowedServiceClasses
      })
    )
  }
  if (!opError.value) {
    tenantModal.value = false
    void tenants.reload()
  }
}

// ── Project modal ─────────────────────────────────────────────────────────────

const projectModal = ref(false)
const editProject = ref<ProjectSummary | null>(null)

const projectForm = reactive({
  name: '',
  displayName: '',
  tenantName: '',
  environment: 'production',
  clusterName: '',
  namespaceMode: 'dedicated',
  maxServices: ''
})

function openNewProject() {
  projectForm.name = ''
  projectForm.displayName = ''
  projectForm.tenantName = tenantRows.value[0]?.name ?? ''
  projectForm.environment = 'production'
  projectForm.clusterName = ''
  projectForm.namespaceMode = 'dedicated'
  projectForm.maxServices = ''
  editProject.value = null
  clearOp()
  projectModal.value = true
}

function openEditProject(p: ProjectSummary) {
  projectForm.name = p.name
  projectForm.displayName = p.displayName
  projectForm.tenantName = p.tenantName
  projectForm.environment = p.environment || 'production'
  projectForm.clusterName = p.clusterName ?? ''
  projectForm.namespaceMode = p.namespaceMode || 'dedicated'
  projectForm.maxServices = ''
  editProject.value = p
  clearOp()
  projectModal.value = true
}

async function submitProject() {
  const maxServices = projectForm.maxServices ? parseInt(projectForm.maxServices) : undefined

  if (editProject.value) {
    await runOp(() =>
      api.admin.updateProject(editProject.value!.name, {
        displayName: projectForm.displayName,
        clusterName: projectForm.clusterName,
        namespaceMode: projectForm.namespaceMode,
        maxServices: maxServices ?? null
      })
    )
  } else {
    await runOp(() =>
      api.admin.createProject({
        name: projectForm.name,
        displayName: projectForm.displayName,
        tenantName: projectForm.tenantName,
        environment: projectForm.environment,
        clusterName: projectForm.clusterName || undefined,
        namespaceMode: projectForm.namespaceMode,
        maxServices
      })
    )
  }
  if (!opError.value) {
    projectModal.value = false
    void projects.reload()
  }
}

// ── Cluster modal ─────────────────────────────────────────────────────────────

const clusterModal = ref(false)
const editCluster = ref<ClusterTargetSummary | null>(null)

const clusterForm = reactive({
  name: '',
  displayName: '',
  region: '',
  ingressDomain: '',
  connectionSecretName: '',
  connectionSecretNamespace: 'servicer-system',
  capabilitiesRaw: ''
})

function openNewCluster() {
  clusterForm.name = ''
  clusterForm.displayName = ''
  clusterForm.region = ''
  clusterForm.ingressDomain = ''
  clusterForm.connectionSecretName = ''
  clusterForm.connectionSecretNamespace = 'servicer-system'
  clusterForm.capabilitiesRaw = ''
  editCluster.value = null
  clearOp()
  clusterModal.value = true
}

function openEditCluster(c: ClusterTargetSummary) {
  clusterForm.name = c.name
  clusterForm.displayName = c.displayName
  clusterForm.region = c.region
  clusterForm.ingressDomain = c.ingressDomain
  clusterForm.connectionSecretName = ''
  clusterForm.connectionSecretNamespace = 'servicer-system'
  clusterForm.capabilitiesRaw = c.capabilities
    ? Object.entries(c.capabilities)
        .map(([k, v]) => `${k}=${v}`)
        .join('\n')
    : ''
  editCluster.value = c
  clearOp()
  clusterModal.value = true
}

function parseCapabilities(raw: string): Record<string, string> | undefined {
  const pairs = raw
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
  if (!pairs.length) return undefined
  const result: Record<string, string> = {}
  for (const pair of pairs) {
    const eq = pair.indexOf('=')
    if (eq === -1) continue
    result[pair.slice(0, eq).trim()] = pair.slice(eq + 1).trim()
  }
  return Object.keys(result).length ? result : undefined
}

async function submitCluster() {
  const capabilities = parseCapabilities(clusterForm.capabilitiesRaw)

  if (editCluster.value) {
    await runOp(() =>
      api.admin.updateCluster(editCluster.value!.name, {
        displayName: clusterForm.displayName,
        region: clusterForm.region,
        ingressDomain: clusterForm.ingressDomain || undefined,
        capabilities
      })
    )
  } else {
    await runOp(() =>
      api.admin.createCluster({
        name: clusterForm.name,
        displayName: clusterForm.displayName,
        region: clusterForm.region,
        ingressDomain: clusterForm.ingressDomain || undefined,
        capabilities,
        connectionSecretName: clusterForm.connectionSecretName,
        connectionSecretNamespace: clusterForm.connectionSecretNamespace
      })
    )
  }
  if (!opError.value) {
    clusterModal.value = false
    void clusters.reload()
  }
}

// ── Catalog / service class ───────────────────────────────────────────────────

const editServiceClass = ref<ServiceClassAdminSummary | null>(null)
const scDefaultsRaw = ref('')
const scDefaultsError = ref<string | null>(null)

function openEditServiceClass(sc: ServiceClassAdminSummary) {
  editServiceClass.value = sc
  scDefaultsRaw.value = sc.defaultParameters
    ? JSON.stringify(sc.defaultParameters, null, 2)
    : '{}'
  scDefaultsError.value = null
  clearOp()
}

async function togglePublished(sc: ServiceClassAdminSummary) {
  clearOp()
  await runOp(() =>
    api.admin.updateServiceClass(sc.name, { published: !sc.published })
  )
  if (!opError.value) void serviceClasses.reload()
}

async function registerServiceClass(sc: ServiceClassAdminSummary) {
  clearOp()
  await runOp(() => api.admin.registerServiceClass(sc.name))
  if (!opError.value) void serviceClasses.reload()
}

async function saveServiceClassDefaults() {
  scDefaultsError.value = null
  let parsed: Record<string, unknown>
  try {
    parsed = JSON.parse(scDefaultsRaw.value)
  } catch {
    scDefaultsError.value = 'Invalid JSON'
    return
  }
  await runOp(() =>
    api.admin.updateServiceClass(editServiceClass.value!.name, {
      published: editServiceClass.value!.published,
      defaultParameters: parsed
    })
  )
  if (!opError.value) {
    editServiceClass.value = null
    void serviceClasses.reload()
  }
}

// ── Repositories ─────────────────────────────────────────────────────────────

type RepoScope = 'tenant' | 'project'

const repoScope = ref<RepoScope>('tenant')
const repoTenantFilter = ref('')
const repoProjectFilter = ref('')
const repoRows = ref<RepositorySummary[]>([])
const repoLoading = ref(false)
const repoModal = ref(false)

const repoForm = reactive({
  name: '',
  displayName: '',
  url: '',
  authType: 'none' as 'none' | 'http' | 'ssh',
  username: '',
  password: '',
  sshKey: ''
})

const repoScopeTarget = computed(() =>
  repoScope.value === 'tenant' ? repoTenantFilter.value : repoProjectFilter.value
)

const selectedRepoTenant = computed(() =>
  tenantRows.value.find((tenant) => tenant.name === repoTenantFilter.value)
)

const selectedRepoProject = computed(() =>
  projectRows.value.find((project) => project.name === repoProjectFilter.value)
)

const repoTargetLabel = computed(() => {
  if (repoScope.value === 'tenant') {
    return selectedRepoTenant.value?.displayName || repoTenantFilter.value
  }
  return selectedRepoProject.value?.displayName || repoProjectFilter.value
})

const repoTargetDetail = computed(() => {
  if (repoScope.value === 'tenant') {
    return repoTenantFilter.value ? `Tenant-wide repositories for ${repoTargetLabel.value}.` : 'Choose a tenant to manage shared repositories.'
  }
  return repoProjectFilter.value ? `Project-only repositories for ${repoTargetLabel.value}.` : 'Choose a project to manage project repositories.'
})

const repoAuthMethods = [
  { value: 'none', label: 'Public', description: 'No credentials required.' },
  { value: 'http', label: 'HTTP token', description: 'Username with password or token.' },
  { value: 'ssh', label: 'SSH key', description: 'Private deploy key.' }
] as const

function resetRepoForm() {
  repoForm.name = ''
  repoForm.displayName = ''
  repoForm.url = ''
  repoForm.authType = 'none'
  repoForm.username = ''
  repoForm.password = ''
  repoForm.sshKey = ''
}

function setRepoScope(scope: RepoScope) {
  repoScope.value = scope
  clearOp()
}

async function loadRepositories() {
  const target = repoScopeTarget.value
  if (!target) {
    repoRows.value = []
    return
  }
  repoLoading.value = true
  try {
    repoRows.value =
      repoScope.value === 'tenant'
        ? await api.repositories.listTenant(target)
        : await api.repositories.listProject(target)
  } catch (err) {
    repoRows.value = []
    opError.value = err instanceof Error ? err.message : 'Failed to load repositories'
  } finally {
    repoLoading.value = false
  }
}

watch(tenantRows, (rows) => {
  if (!repoTenantFilter.value && rows.length) {
    repoTenantFilter.value = rows[0].name
  }
}, { immediate: true })

watch(projectRows, (rows) => {
  if (!repoProjectFilter.value && rows.length) {
    repoProjectFilter.value = rows[0].name
  }
}, { immediate: true })

watch([repoScope, repoTenantFilter, repoProjectFilter], () => {
  if (activeTab.value === 'repositories') {
    clearOp()
    void loadRepositories()
  }
})

watch(activeTab, (tab) => {
  if (tab === 'repositories') {
    void loadRepositories()
  }
})

function openNewRepo() {
  if (!repoScopeTarget.value) return
  resetRepoForm()
  clearOp()
  repoModal.value = true
}

async function submitRepo() {
  const target = repoScopeTarget.value
  if (!target) return

  const body = {
    name: repoForm.name,
    displayName: repoForm.displayName,
    scope: repoScope.value,
    tenantName: repoScope.value === 'tenant' ? target : undefined,
    projectName: repoScope.value === 'project' ? target : undefined,
    url: repoForm.url,
    authType: repoForm.authType,
    username: repoForm.authType === 'http' ? repoForm.username : undefined,
    password: repoForm.authType === 'http' ? repoForm.password : undefined,
    sshKey: repoForm.authType === 'ssh' ? repoForm.sshKey : undefined
  }

  await runOp(() =>
    repoScope.value === 'tenant'
      ? api.repositories.createTenant(target, body)
      : api.repositories.createProject(target, body)
  )
  if (!opError.value) {
    repoModal.value = false
    await loadRepositories()
  }
}

async function deleteRepo(repo: RepositorySummary) {
  if (!confirm(`Remove repository "${repo.displayName || repo.name}"?`)) return
  const scope = repo.scope || repoScope.value
  const target = scope === 'tenant'
    ? (repo.tenantName || repoTenantFilter.value)
    : (repo.projectName || repoProjectFilter.value)
  if (!target) return
  clearOp()
  await runOp(() =>
    scope === 'tenant'
      ? api.repositories.deleteTenant(target, repo.name)
      : api.repositories.deleteProject(target, repo.name)
  )
  if (!opError.value) await loadRepositories()
}

// ── Delete confirm ────────────────────────────────────────────────────────────

const deleteConfirm = ref<{ type: 'tenant' | 'project' | 'cluster'; name: string; displayName: string } | null>(null)

function confirmDelete(type: 'tenant' | 'project' | 'cluster', name: string, displayName: string) {
  deleteConfirm.value = { type, name, displayName }
}

async function executeDelete() {
  if (!deleteConfirm.value) return
  const { type, name } = deleteConfirm.value
  deleteConfirm.value = null
  clearOp()
  if (type === 'tenant') {
    await runOp(() => api.admin.deleteTenant(name))
    if (!opError.value) void tenants.reload()
  } else if (type === 'project') {
    await runOp(() => api.admin.deleteProject(name))
    if (!opError.value) void projects.reload()
  } else {
    await runOp(() => api.admin.deleteCluster(name))
    if (!opError.value) void clusters.reload()
  }
}
</script>

<template>
  <section class="page-heading">
    <div>
      <p class="eyebrow">Platform</p>
      <h1>Admin</h1>
    </div>
  </section>

  <!-- Tab strip -->
  <div class="tab-strip">
    <button
      v-for="tab in (['tenants', 'projects', 'clusters', 'catalog', 'repositories', 'auth'] as const)"
      :key="tab"
      class="tab-btn"
      :class="{ active: activeTab === tab }"
      @click="activeTab = tab; clearOp()"
    >
      {{ tab.charAt(0).toUpperCase() + tab.slice(1) }}
    </button>
  </div>

  <!-- Global op feedback -->
  <p v-if="opError" class="error-text" style="margin-bottom: 12px">{{ opError }}</p>
  <p v-if="opSuccess" class="success-text" style="margin-bottom: 12px">{{ opSuccess }}</p>

  <!-- ── TENANTS ─────────────────────────────────────────────────────────── -->
  <template v-if="activeTab === 'tenants'">
    <div class="toolbar">
      <button class="button primary" @click="openNewTenant">New tenant</button>
    </div>

    <section class="content-band">
      <p v-if="tenants.loading.value" class="muted">Loading…</p>
      <p v-else-if="tenants.error.value" class="error-text">{{ tenants.error.value }}</p>
      <table v-else class="data-table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Phase</th>
            <th>Owners</th>
            <th>Allowed classes</th>
            <th>Projects</th>
            <th>Instances</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="t in tenantRows" :key="t.name">
            <td><strong>{{ t.displayName }}</strong><small>{{ t.name }}</small></td>
            <td><StatusPill :value="t.phase" /></td>
            <td>{{ t.owners.join(', ') || '—' }}</td>
            <td>
              <div class="tag-row">
                <span v-for="cls in t.allowedServiceClasses" :key="cls">{{ cls }}</span>
              </div>
            </td>
            <td>{{ t.projectCount }}</td>
            <td>{{ t.instanceCount }}</td>
            <td>
              <div class="form-actions">
                <button class="button secondary" style="font-size:12px; min-height:30px; padding: 0 10px" @click="openEditTenant(t)">Edit</button>
                <button class="button secondary" style="font-size:12px; min-height:30px; padding: 0 10px; color: var(--danger, #f87171)" @click="confirmDelete('tenant', t.name, t.displayName)">Delete</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </section>
  </template>

  <!-- ── PROJECTS ───────────────────────────────────────────────────────── -->
  <template v-if="activeTab === 'projects'">
    <div class="toolbar">
      <button class="button primary" @click="openNewProject">New project</button>
    </div>

    <section class="content-band">
      <p v-if="projects.loading.value" class="muted">Loading…</p>
      <p v-else-if="projects.error.value" class="error-text">{{ projects.error.value }}</p>
      <table v-else class="data-table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Tenant</th>
            <th>Environment</th>
            <th>Phase</th>
            <th>Cluster</th>
            <th>NS mode</th>
            <th>Instances</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in projectRows" :key="p.name">
            <td><strong>{{ p.displayName }}</strong><small>{{ p.name }}</small></td>
            <td>{{ p.tenantName }}</td>
            <td>{{ p.environment || '—' }}</td>
            <td><StatusPill :value="p.phase" /></td>
            <td>{{ p.clusterName || 'Unplaced' }}</td>
            <td>{{ p.namespaceMode || '—' }}</td>
            <td>{{ p.instanceCount }}</td>
            <td>
              <div class="form-actions">
                <button class="button secondary" style="font-size:12px; min-height:30px; padding: 0 10px" @click="openEditProject(p)">Edit</button>
                <button class="button secondary" style="font-size:12px; min-height:30px; padding: 0 10px; color: var(--danger, #f87171)" @click="confirmDelete('project', p.name, p.displayName)">Delete</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </section>
  </template>

  <!-- ── CLUSTERS ───────────────────────────────────────────────────────── -->
  <template v-if="activeTab === 'clusters'">
    <div class="toolbar">
      <button class="button primary" @click="openNewCluster">Add cluster</button>
    </div>

    <section class="content-band">
      <p v-if="clusters.loading.value" class="muted">Loading…</p>
      <p v-else-if="clusters.error.value" class="error-text">{{ clusters.error.value }}</p>
      <table v-else class="data-table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Region</th>
            <th>Phase</th>
            <th>Reachable</th>
            <th>K8s version</th>
            <th>Ingress domain</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="clusterRows.length === 0">
            <td colspan="7" class="muted">No clusters registered.</td>
          </tr>
          <tr v-for="c in clusterRows" :key="c.name">
            <td><strong>{{ c.displayName }}</strong><small>{{ c.name }}</small></td>
            <td>{{ c.region || '—' }}</td>
            <td><StatusPill :value="c.phase || 'Unknown'" /></td>
            <td>
              <span class="status-pill" :class="c.reachable ? 'good' : 'bad'">
                {{ c.reachable ? 'Yes' : 'No' }}
              </span>
            </td>
            <td>{{ c.k8sVersion || '—' }}</td>
            <td>{{ c.ingressDomain || '—' }}</td>
            <td>
              <div class="form-actions">
                <button class="button secondary" style="font-size:12px; min-height:30px; padding: 0 10px" @click="openEditCluster(c)">Edit</button>
                <button class="button secondary" style="font-size:12px; min-height:30px; padding: 0 10px; color: var(--danger, #f87171)" @click="confirmDelete('cluster', c.name, c.displayName)">Delete</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </section>
  </template>

  <!-- ── CATALOG ────────────────────────────────────────────────────────── -->
  <template v-if="activeTab === 'catalog'">
    <p class="muted" style="margin-bottom: 16px">
      Toggle global publish state and set default parameters per service class. Per-tenant access is controlled via the Tenants tab.
    </p>

    <section class="content-band">
      <p v-if="serviceClasses.loading.value" class="muted">Loading…</p>
      <p v-else-if="serviceClasses.error.value" class="error-text">{{ serviceClasses.error.value }}</p>
      <template v-else>
        <table class="data-table">
          <thead>
            <tr>
              <th>Service class</th>
              <th>Driver</th>
              <th>Category</th>
              <th>Published</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="sc in serviceClassRows" :key="sc.name">
              <td><strong>{{ sc.displayName }}</strong><small>{{ sc.name }}</small></td>
              <td>{{ sc.driver }}</td>
              <td>{{ sc.category || '—' }}</td>
              <td>
                <span v-if="!sc.registered" class="status-pill neutral">Not registered</span>
                <span v-else class="status-pill" :class="sc.published ? 'good' : 'neutral'">
                  {{ sc.published ? 'Published' : 'Unpublished' }}
                </span>
              </td>
              <td>
                <div class="form-actions">
                  <template v-if="!sc.registered">
                    <button
                      class="button primary"
                      style="font-size:12px; min-height:30px; padding: 0 10px"
                      :disabled="opLoading"
                      @click="registerServiceClass(sc)"
                    >
                      Register
                    </button>
                  </template>
                  <template v-else>
                    <button
                      class="button secondary"
                      style="font-size:12px; min-height:30px; padding: 0 10px"
                      :disabled="opLoading"
                      @click="togglePublished(sc)"
                    >
                      {{ sc.published ? 'Unpublish' : 'Publish' }}
                    </button>
                    <button
                      class="button secondary"
                      style="font-size:12px; min-height:30px; padding: 0 10px"
                      @click="openEditServiceClass(sc)"
                    >
                      Edit defaults
                    </button>
                  </template>
                </div>
              </td>
            </tr>
          </tbody>
        </table>

        <!-- Inline defaults editor -->
        <template v-if="editServiceClass">
          <div class="admin-defaults-editor">
            <h3>Default parameters — {{ editServiceClass.displayName }}</h3>
            <p class="muted" style="font-size:13px; margin-bottom: 10px">
              JSON object applied as base defaults for every new instance of this class.
            </p>
            <textarea
              v-model="scDefaultsRaw"
              class="defaults-textarea"
              spellcheck="false"
              autocomplete="off"
            />
            <p v-if="scDefaultsError" class="error-text" style="margin-top:6px">{{ scDefaultsError }}</p>
            <div class="form-actions" style="margin-top: 12px">
              <button class="button primary" :disabled="opLoading" @click="saveServiceClassDefaults">Save defaults</button>
              <button class="button secondary" @click="editServiceClass = null; clearOp()">Cancel</button>
            </div>
          </div>
        </template>
      </template>
    </section>
  </template>

  <!-- ── REPOSITORIES ────────────────────────────────────────────────────── -->
  <template v-if="activeTab === 'repositories'">
    <section class="content-band repo-admin">
      <div class="repo-admin-head">
        <div>
          <h2>Repositories</h2>
          <p class="muted">Register Git sources at tenant or project scope.</p>
        </div>
        <button class="button primary" :disabled="!repoScopeTarget" @click="openNewRepo">New repository</button>
      </div>

      <div class="repo-toolbar">
        <div class="repo-scope-toggle" aria-label="Repository scope">
          <button :class="{ active: repoScope === 'tenant' }" @click="setRepoScope('tenant')">Tenant</button>
          <button :class="{ active: repoScope === 'project' }" @click="setRepoScope('project')">Project</button>
        </div>

        <label class="repo-target-picker">
          <span>{{ repoScope === 'tenant' ? 'Tenant' : 'Project' }}</span>
          <select v-if="repoScope === 'tenant'" v-model="repoTenantFilter">
            <option value="">Select a tenant...</option>
            <option v-for="tenant in tenantRows" :key="tenant.name" :value="tenant.name">
              {{ tenant.displayName || tenant.name }}
            </option>
          </select>
          <select v-else v-model="repoProjectFilter">
            <option value="">Select a project...</option>
            <option v-for="project in projectRows" :key="project.name" :value="project.name">
              {{ project.displayName || project.name }}
            </option>
          </select>
        </label>
      </div>

      <p class="repo-context">{{ repoTargetDetail }}</p>

      <div v-if="!repoScopeTarget" class="empty-state">
        <p>Select a {{ repoScope }} to manage repositories.</p>
      </div>
      <div v-else-if="repoLoading" class="empty-state"><p>Loading repositories...</p></div>
      <div v-else-if="repoRows.length === 0" class="empty-state repo-empty">
        <p>No repositories registered for {{ repoTargetLabel }}.</p>
        <button class="button secondary" @click="openNewRepo">Add the first repository</button>
      </div>
      <table v-else class="data-table repo-table">
        <thead>
          <tr>
            <th>Repository</th>
            <th>Scope</th>
            <th>URL</th>
            <th>Auth</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="repo in repoRows" :key="repo.name">
            <td>
              <strong>{{ repo.displayName || repo.name }}</strong>
              <small>{{ repo.name }}</small>
            </td>
            <td>
              <span class="repo-scope-badge">{{ repo.scope || repoScope }}</span>
              <small>{{ repo.tenantName || repo.projectName || repoTargetLabel }}</small>
            </td>
            <td class="repo-url-cell">{{ repo.url }}</td>
            <td>{{ repo.authType || 'none' }}</td>
            <td class="table-actions">
              <button class="button danger compact-button" @click="deleteRepo(repo)">Remove</button>
            </td>
          </tr>
        </tbody>
      </table>
    </section>
  </template>

  <template v-if="activeTab === 'auth'">
    <AuthAdminPanel />
  </template>

  <!-- ── REPOSITORY MODAL ────────────────────────────────────────────────── -->
  <div v-if="repoModal" class="modal-backdrop">
    <div class="modal-panel auth-modal-panel">
      <div class="modal-head">
        <div>
          <p class="eyebrow">Create</p>
          <h2>New repository</h2>
          <p class="muted">{{ repoScope === 'tenant' ? 'Tenant-wide Git source' : 'Project-only Git source' }} for {{ repoTargetLabel }}.</p>
        </div>
        <button class="button secondary" @click="repoModal = false">Close</button>
      </div>

      <div class="auth-modal-body">
        <section class="auth-modal-section">
          <div class="auth-section-heading">
            <h3>Repository</h3>
            <p>Use a stable slug; deployments reference repositories by this name.</p>
          </div>
          <div class="auth-modal-grid">
            <label>
              <span>Name</span>
              <input v-model="repoForm.name" placeholder="storefront-app" autocomplete="off" />
            </label>
            <label>
              <span>Display name</span>
              <input v-model="repoForm.displayName" placeholder="Storefront App" autocomplete="off" />
            </label>
            <label class="auth-full-width">
              <span>Repository URL</span>
              <input v-model="repoForm.url" placeholder="https://github.com/acme/storefront.git" autocomplete="off" />
            </label>
          </div>
        </section>

        <section class="auth-modal-section">
          <div class="auth-section-heading">
            <h3>Authentication</h3>
            <p>Choose how Servicer and Argo CD should authenticate to this Git remote.</p>
          </div>
          <div class="auth-method-picker">
            <button
              v-for="method in repoAuthMethods"
              :key="method.value"
              type="button"
              class="auth-method-option"
              :class="{ active: repoForm.authType === method.value }"
              @click="repoForm.authType = method.value"
            >
              <strong>{{ method.label }}</strong>
              <span>{{ method.description }}</span>
            </button>
          </div>

          <div v-if="repoForm.authType === 'http'" class="auth-modal-grid">
            <label>
              <span>Username</span>
              <input v-model="repoForm.username" autocomplete="new-password" placeholder="git" />
            </label>
            <label>
              <span>Password or token</span>
              <input v-model="repoForm.password" type="password" autocomplete="new-password" placeholder="ghp_..." />
            </label>
          </div>

          <label v-if="repoForm.authType === 'ssh'" class="auth-stacked-field">
            <span>SSH private key</span>
            <textarea
              v-model="repoForm.sshKey"
              rows="7"
              placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
            />
            <small class="field-help">Paste a deploy key with read access to this repository. Public host key trust is handled by the cluster Git tooling.</small>
          </label>
        </section>
      </div>

      <div class="auth-modal-actions">
        <button class="button reset" type="button" @click="resetRepoForm">Reset</button>
        <span class="auth-action-spacer"></span>
        <button class="button secondary" type="button" @click="repoModal = false">Cancel</button>
        <button class="button primary" :disabled="opLoading" @click="submitRepo">
          {{ opLoading ? 'Saving...' : 'Create repository' }}
        </button>
      </div>
      <p v-if="opError" class="error-text" style="padding: 0 22px 20px">{{ opError }}</p>
    </div>
  </div>
  <div v-if="tenantModal" class="modal-backdrop">
    <div class="modal-panel">
      <div class="modal-head">
        <h2>{{ editTenant ? 'Edit tenant' : 'New tenant' }}</h2>
        <button class="button text" @click="tenantModal = false">✕</button>
      </div>

      <div class="modal-section">
        <div class="form-grid modal-form-grid">
          <label v-if="!editTenant">
            Name (slug)
            <input v-model="tenantForm.name" type="text" placeholder="acme-corp" />
          </label>
          <label>
            Display name
            <input v-model="tenantForm.displayName" type="text" placeholder="Acme Corp" />
          </label>
          <label style="grid-column: 1 / -1">
            Owners (comma-separated user IDs)
            <input v-model="tenantForm.owners" type="text" placeholder="alice@example.com, bob@example.com" />
          </label>
        </div>
      </div>

      <div class="modal-section">
        <h3>Allowed service classes</h3>
        <div class="tag-row" style="gap: 10px">
          <label
            v-for="cls in knownClasses"
            :key="cls"
            class="checkbox-label form-grid"
            style="padding: 6px 10px; border: 1px solid var(--border); border-radius: 6px; cursor: pointer"
          >
            <input
              type="checkbox"
              :checked="tenantForm.allowedServiceClasses.includes(cls)"
              @change="toggleServiceClass(cls)"
            />
            {{ cls }}
          </label>
        </div>
      </div>

      <p v-if="opError" class="error-text">{{ opError }}</p>

      <div class="form-actions">
        <button class="button primary" :disabled="opLoading" @click="submitTenant">
          {{ editTenant ? 'Save changes' : 'Create tenant' }}
        </button>
        <button class="button secondary" @click="tenantModal = false">Cancel</button>
      </div>
    </div>
  </div>

  <!-- ── PROJECT MODAL ───────────────────────────────────────────────────── -->
  <div v-if="projectModal" class="modal-backdrop">
    <div class="modal-panel">
      <div class="modal-head">
        <h2>{{ editProject ? 'Edit project' : 'New project' }}</h2>
        <button class="button text" @click="projectModal = false">✕</button>
      </div>

      <div class="modal-section">
        <div class="form-grid modal-form-grid">
          <label v-if="!editProject">
            Name (slug)
            <input v-model="projectForm.name" type="text" placeholder="acme-production" />
          </label>
          <label>
            Display name
            <input v-model="projectForm.displayName" type="text" placeholder="Acme Production" />
          </label>

          <label v-if="!editProject">
            Tenant
            <select v-model="projectForm.tenantName">
              <option v-for="t in tenantRows" :key="t.name" :value="t.name">{{ t.displayName }}</option>
            </select>
          </label>
          <label v-if="!editProject">
            Environment
            <select v-model="projectForm.environment">
              <option value="production">production</option>
              <option value="staging">staging</option>
              <option value="development">development</option>
              <option value="sandbox">sandbox</option>
            </select>
          </label>

          <label>
            Cluster (optional)
            <select v-model="projectForm.clusterName">
              <option value="">— Unplaced —</option>
              <option v-for="c in clusterRows" :key="c.name" :value="c.name">{{ c.displayName }} ({{ c.region }})</option>
            </select>
          </label>
          <label>
            Namespace mode
            <select v-model="projectForm.namespaceMode">
              <option value="dedicated">dedicated</option>
              <option value="shared">shared</option>
              <option value="bring-your-own">bring-your-own</option>
            </select>
          </label>

          <label>
            Max services (quota)
            <input v-model="projectForm.maxServices" type="number" min="1" placeholder="Unlimited" />
          </label>
        </div>
      </div>

      <p v-if="opError" class="error-text">{{ opError }}</p>

      <div class="form-actions">
        <button class="button primary" :disabled="opLoading" @click="submitProject">
          {{ editProject ? 'Save changes' : 'Create project' }}
        </button>
        <button class="button secondary" @click="projectModal = false">Cancel</button>
      </div>
    </div>
  </div>

  <!-- ── CLUSTER MODAL ───────────────────────────────────────────────────── -->
  <div v-if="clusterModal" class="modal-backdrop">
    <div class="modal-panel">
      <div class="modal-head">
        <h2>{{ editCluster ? 'Edit cluster' : 'Add cluster' }}</h2>
        <button class="button text" @click="clusterModal = false">✕</button>
      </div>

      <div class="modal-section">
        <div class="form-grid modal-form-grid">
          <label v-if="!editCluster">
            Name (slug)
            <input v-model="clusterForm.name" type="text" placeholder="prod-eu-west-1" />
          </label>
          <label>
            Display name
            <input v-model="clusterForm.displayName" type="text" placeholder="Production EU West" />
          </label>

          <label>
            Region
            <input v-model="clusterForm.region" type="text" placeholder="eu-west-1" />
          </label>
          <label>
            Ingress domain
            <input v-model="clusterForm.ingressDomain" type="text" placeholder="apps.example.com" />
          </label>

          <template v-if="!editCluster">
            <label>
              Connection secret name
              <input v-model="clusterForm.connectionSecretName" type="text" placeholder="prod-eu-kubeconfig" />
            </label>
            <label>
              Connection secret namespace
              <input v-model="clusterForm.connectionSecretNamespace" type="text" placeholder="servicer-system" />
            </label>
          </template>

          <label style="grid-column: 1 / -1">
            Capabilities (one <code>key=value</code> per line)
            <textarea
              v-model="clusterForm.capabilitiesRaw"
              class="defaults-textarea"
              style="min-height: 80px"
              placeholder="gpu=true&#10;storage-class=fast"
            />
          </label>
        </div>
      </div>

      <p v-if="opError" class="error-text">{{ opError }}</p>

      <div class="form-actions">
        <button class="button primary" :disabled="opLoading" @click="submitCluster">
          {{ editCluster ? 'Save changes' : 'Register cluster' }}
        </button>
        <button class="button secondary" @click="clusterModal = false">Cancel</button>
      </div>
    </div>
  </div>

  <!-- ── DELETE CONFIRM MODAL ────────────────────────────────────────────── -->
  <div v-if="deleteConfirm" class="modal-backdrop">
    <div class="modal-panel" style="max-width: 420px">
      <div class="modal-head">
        <h2>Confirm deletion</h2>
        <button class="button text" @click="deleteConfirm = null">✕</button>
      </div>
      <div class="modal-section">
        <p>
          Delete <strong>{{ deleteConfirm.displayName }}</strong>?
          This action cannot be undone.
        </p>
      </div>
      <p v-if="opError" class="error-text">{{ opError }}</p>
      <div class="form-actions">
        <button class="button primary" style="background: var(--danger, #ef4444); border-color: var(--danger, #ef4444)" :disabled="opLoading" @click="executeDelete">
          {{ opLoading ? 'Deleting…' : 'Delete' }}
        </button>
        <button class="button secondary" @click="deleteConfirm = null">Cancel</button>
      </div>
    </div>
  </div>
</template>
