<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { api, type RepositorySummary } from '../api'
import { useApi } from '../composables/useApi'
import StatusPill from '../components/StatusPill.vue'

const tenants = useApi(api.tenants)
const projects = useApi(api.projects)
const tenantRows = computed(() => tenants.data.value || [])
const projectRows = computed(() => projects.data.value || [])

function projectsForTenant(tenantName: string) {
  return projectRows.value.filter((p) => p.tenantName === tenantName)
}

// ── Per-project repositories ──────────────────────────────────────────────────

const expandedRepos = ref<Record<string, boolean>>({})
const projectRepos = ref<Record<string, RepositorySummary[]>>({})
const repoLoading = ref<Record<string, boolean>>({})
const repoModal = ref<string | null>(null)  // project name whose modal is open
const repoOpError = ref<string | null>(null)
const repoOpSuccess = ref<string | null>(null)
const repoSubmitting = ref(false)

const repoForm = reactive({
  name: '',
  displayName: '',
  url: '',
  authType: 'none' as 'none' | 'http' | 'ssh',
  username: '',
  password: '',
  sshKey: ''
})

async function toggleRepos(projectName: string) {
  if (expandedRepos.value[projectName]) {
    expandedRepos.value = { ...expandedRepos.value, [projectName]: false }
    return
  }
  expandedRepos.value = { ...expandedRepos.value, [projectName]: true }
  await loadRepos(projectName)
}

async function loadRepos(projectName: string) {
  repoLoading.value = { ...repoLoading.value, [projectName]: true }
  try {
    projectRepos.value = {
      ...projectRepos.value,
      [projectName]: await api.repositories.list(projectName)
    }
  } catch {
    projectRepos.value = { ...projectRepos.value, [projectName]: [] }
  } finally {
    repoLoading.value = { ...repoLoading.value, [projectName]: false }
  }
}

function openAddRepo(projectName: string) {
  repoForm.name = ''
  repoForm.displayName = ''
  repoForm.url = ''
  repoForm.authType = 'none'
  repoForm.username = ''
  repoForm.password = ''
  repoForm.sshKey = ''
  repoOpError.value = null
  repoOpSuccess.value = null
  repoModal.value = projectName
}

async function submitRepo() {
  const project = repoModal.value!
  repoSubmitting.value = true
  repoOpError.value = null
  repoOpSuccess.value = null
  try {
    const res = await api.repositories.create(project, {
      name: repoForm.name,
      displayName: repoForm.displayName,
      projectName: project,
      url: repoForm.url,
      authType: repoForm.authType,
      username: repoForm.authType === 'http' ? repoForm.username : undefined,
      password: repoForm.authType === 'http' ? repoForm.password : undefined,
      sshKey: repoForm.authType === 'ssh' ? repoForm.sshKey : undefined
    })
    repoOpSuccess.value = res.message
    repoModal.value = null
    await loadRepos(project)
  } catch (err) {
    repoOpError.value = err instanceof Error ? err.message : 'Failed to add repository'
  } finally {
    repoSubmitting.value = false
  }
}

async function removeRepo(projectName: string, repo: RepositorySummary) {
  if (!confirm(`Remove repository "${repo.displayName || repo.name}"?`)) return
  try {
    await api.repositories.delete(projectName, repo.name)
    await loadRepos(projectName)
  } catch (err) {
    alert(err instanceof Error ? err.message : 'Failed to remove repository')
  }
}
</script>

<template>
  <section class="page-heading">
    <div>
      <p class="eyebrow">Tenancy</p>
      <h1>Tenants and projects</h1>
    </div>
  </section>

  <p v-if="tenants.loading.value" class="muted">Loading tenants...</p>
  <p v-else-if="tenants.error.value" class="error-text">{{ tenants.error.value }}</p>

  <div v-else class="tenant-tree">
    <div v-for="tenant in tenantRows" :key="tenant.name" class="tenant-block">
      <div class="tenant-block-header">
        <div class="tenant-block-title">
          <strong>{{ tenant.displayName }}</strong>
          <small>{{ tenant.name }}</small>
        </div>
        <StatusPill :value="tenant.phase" />
        <div class="tag-row">
          <span v-for="cls in tenant.allowedServiceClasses" :key="cls">{{ cls }}</span>
        </div>
        <span class="muted" style="font-size: 13px; white-space: nowrap">{{ tenant.instanceCount }} instance{{ tenant.instanceCount === 1 ? '' : 's' }}</span>
      </div>

      <div class="tenant-projects">
        <p v-if="projects.loading.value" class="muted" style="padding: 10px 14px; font-size: 13px">Loading projects…</p>
        <p v-else-if="!projectsForTenant(tenant.name).length" class="muted" style="padding: 10px 14px; font-size: 13px">No projects in this tenant.</p>
        <table v-else class="data-table tenant-projects-table">
          <thead>
            <tr>
              <th>Project</th>
              <th>Environment</th>
              <th>Phase</th>
              <th>Cluster</th>
              <th>Instances</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="project in projectsForTenant(tenant.name)" :key="project.name">
              <td><strong>{{ project.displayName }}</strong><small>{{ project.name }}</small></td>
              <td>{{ project.environment || '—' }}</td>
              <td><StatusPill :value="project.phase" /></td>
              <td>{{ project.clusterName || 'Unplaced' }}</td>
              <td>{{ project.instanceCount }}</td>
              <td>
                <button
                  class="button text"
                  style="font-size: 12px"
                  @click="toggleRepos(project.name)"
                >
                  {{ expandedRepos[project.name] ? 'Hide repos' : 'Repos' }}
                </button>
              </td>
            </tr>
            <!-- Inline repo panel per project -->
            <template v-for="project in projectsForTenant(tenant.name)" :key="project.name + '-repos'">
              <tr v-if="expandedRepos[project.name]">
                <td colspan="6" style="padding: 0; background: var(--surface-alt, #0d1117)">
                  <div style="padding: 12px 16px 14px">
                    <div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px">
                      <span style="font-size: 12px; font-weight: 700; text-transform: uppercase; color: var(--muted-strong)">Repositories — {{ project.displayName }}</span>
                      <button class="button primary" style="padding: 4px 10px; font-size: 12px" @click="openAddRepo(project.name)">+ Add</button>
                    </div>
                    <p v-if="repoLoading[project.name]" class="muted" style="font-size: 13px">Loading...</p>
                    <p v-else-if="!projectRepos[project.name]?.length" class="muted" style="font-size: 13px">No repositories registered. Add one to enable Managed Application deployments.</p>
                    <table v-else style="width: 100%; border-collapse: collapse; font-size: 13px">
                      <thead>
                        <tr style="color: var(--muted-strong); text-align: left">
                          <th style="padding: 4px 8px; font-weight: 600">Name</th>
                          <th style="padding: 4px 8px; font-weight: 600">URL</th>
                          <th style="padding: 4px 8px; font-weight: 600">Auth</th>
                          <th style="padding: 4px 8px"></th>
                        </tr>
                      </thead>
                      <tbody>
                        <tr v-for="repo in projectRepos[project.name]" :key="repo.name">
                          <td style="padding: 4px 8px">{{ repo.displayName || repo.name }}</td>
                          <td style="padding: 4px 8px; font-family: var(--mono); color: var(--muted-strong)">{{ repo.url }}</td>
                          <td style="padding: 4px 8px">{{ repo.authType }}</td>
                          <td style="padding: 4px 8px">
                            <button class="button text danger" style="font-size: 12px" @click="removeRepo(project.name, repo)">Remove</button>
                          </td>
                        </tr>
                      </tbody>
                    </table>
                  </div>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </div>

    <p v-if="tenantRows.length === 0" class="muted">No tenants found.</p>
  </div>

  <!-- Add repository modal -->
  <div v-if="repoModal" class="modal-backdrop">
    <div class="modal-panel">
      <div class="modal-head">
        <h2>Add repository — {{ repoModal }}</h2>
        <button class="button text" @click="repoModal = null">✕</button>
      </div>
      <div class="modal-section">
        <div class="form-grid modal-form-grid">
          <label>
            Name (slug)
            <input v-model="repoForm.name" placeholder="my-app-repo" />
          </label>
          <label>
            Display name
            <input v-model="repoForm.displayName" placeholder="My App Repo" />
          </label>
          <label style="grid-column: span 2">
            Repository URL
            <input v-model="repoForm.url" placeholder="https://github.com/org/repo.git" />
          </label>
          <label style="grid-column: span 2">
            Auth type
            <select v-model="repoForm.authType">
              <option value="none">None (public)</option>
              <option value="http">HTTP (username / password or token)</option>
              <option value="ssh">SSH key</option>
            </select>
          </label>
          <template v-if="repoForm.authType === 'http'">
            <label>
              Username
              <input v-model="repoForm.username" autocomplete="new-password" />
            </label>
            <label>
              Password / token
              <input v-model="repoForm.password" type="password" autocomplete="new-password" />
            </label>
          </template>
          <template v-if="repoForm.authType === 'ssh'">
            <label style="grid-column: span 2">
              SSH private key
              <textarea
                v-model="repoForm.sshKey"
                rows="6"
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                style="font-family: var(--mono); font-size: 12px; resize: vertical"
              />
            </label>
          </template>
        </div>
      </div>
      <div class="form-actions">
        <button class="button primary" :disabled="repoSubmitting" @click="submitRepo">
          {{ repoSubmitting ? 'Saving...' : 'Add repository' }}
        </button>
        <button class="button secondary" @click="repoModal = null">Cancel</button>
        <span v-if="repoOpError" class="error-text">{{ repoOpError }}</span>
      </div>
    </div>
  </div>
</template>
