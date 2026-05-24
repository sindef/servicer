<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { api } from '../api'
import { useApi } from '../composables/useApi'
import StatusPill from '../components/StatusPill.vue'

const claims = useApi(api.namespaceClaims.list, { refreshMs: 4000, retainOnSilentError: true })
const projects = useApi(api.projects)
const filter = ref('')
const requestOpen = ref(false)
const submitting = ref(false)
const submitError = ref<string | null>(null)
const submitMessage = ref<string | null>(null)

const form = reactive({
  name: '',
  projectName: '',
  displayName: '',
  deletionPolicy: 'delete',
  quotasText: '',
  labelsText: ''
})

const filtered = computed(() => {
  const rows = claims.data.value || []
  const needle = filter.value.trim().toLowerCase()
  if (!needle) return rows
  return rows.filter((claim) =>
    [claim.name, claim.projectName, claim.phase, claim.namespace || ''].some((value) =>
      value.toLowerCase().includes(needle)
    )
  )
})

function parseMapInput(value: string) {
  const result: Record<string, string> = {}
  for (const line of value.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const separator = trimmed.includes('=') ? '=' : ':'
    const index = trimmed.indexOf(separator)
    if (index <= 0) {
      throw new Error(`Expected key${separator}value entry, got "${trimmed}"`)
    }
    const key = trimmed.slice(0, index).trim()
    const mapValue = trimmed.slice(index + 1).trim()
    if (!key || !mapValue) {
      throw new Error(`Expected key${separator}value entry, got "${trimmed}"`)
    }
    result[key] = mapValue
  }
  return Object.keys(result).length ? result : undefined
}

function openRequest() {
  form.name = ''
  form.projectName = projects.data.value?.[0]?.name || ''
  form.displayName = ''
  form.deletionPolicy = 'delete'
  form.quotasText = ''
  form.labelsText = ''
  submitError.value = null
  submitMessage.value = null
  requestOpen.value = true
}

async function submit() {
  submitting.value = true
  submitError.value = null
  submitMessage.value = null
  try {
    const response = await api.namespaceClaims.create({
      name: form.name,
      projectName: form.projectName,
      displayName: form.displayName || undefined,
      deletionPolicy: form.deletionPolicy,
      quotas: parseMapInput(form.quotasText),
      labels: parseMapInput(form.labelsText)
    })
    submitMessage.value = response.message
    requestOpen.value = false
    await claims.reload()
  } catch (err) {
    submitError.value = err instanceof Error ? err.message : 'Failed to submit namespace claim'
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <section class="page-heading">
    <div>
      <p class="eyebrow">Namespace claims</p>
      <h1>Dedicated namespace requests</h1>
    </div>
    <button class="button primary" @click="openRequest">New claim</button>
  </section>

  <div class="toolbar">
    <input
      v-model="filter"
      class="search"
      type="search"
      placeholder="Filter by name, project, phase, or namespace"
    />
    <span v-if="submitMessage" class="success-text">{{ submitMessage }}</span>
  </div>

  <p v-if="claims.loading.value" class="muted">Loading namespace claims...</p>
  <p v-else-if="claims.error.value" class="error-text">{{ claims.error.value }}</p>

  <table v-else class="data-table">
    <thead>
      <tr>
        <th>Name</th>
        <th>Project</th>
        <th>Phase</th>
        <th>Namespace</th>
        <th>Cluster</th>
      </tr>
    </thead>
    <tbody>
      <tr v-for="claim in filtered" :key="claim.name">
        <td>
          <RouterLink class="table-link" :to="`/namespace-claims/${claim.name}`">{{ claim.name }}</RouterLink>
          <small>{{ claim.health || 'No health summary yet' }}</small>
        </td>
        <td>{{ claim.projectName }}</td>
        <td><StatusPill :value="claim.phase" /></td>
        <td>{{ claim.namespace || 'Pending' }}</td>
        <td>{{ claim.clusterName || 'Unplaced' }}</td>
      </tr>
      <tr v-if="filtered.length === 0">
        <td colspan="5" class="muted">No namespace claims match the current filter.</td>
      </tr>
    </tbody>
  </table>

  <div v-if="requestOpen" class="modal-backdrop">
    <div class="modal-panel">
      <div class="modal-head">
        <div>
          <h2>Create namespace claim</h2>
          <p class="muted">Request a dedicated namespace with explicit project ownership and quota intent.</p>
        </div>
        <button class="button text" @click="requestOpen = false">Close</button>
      </div>

      <div class="modal-section">
        <div class="form-grid modal-form-grid">
          <label>
            Name
            <input v-model="form.name" placeholder="orders-team-space" />
          </label>
          <label>
            Project
            <select v-model="form.projectName">
              <option disabled value="">Select project</option>
              <option v-for="project in projects.data.value || []" :key="project.name" :value="project.name">
                {{ project.displayName }}
              </option>
            </select>
          </label>
          <label>
            Display name
            <input v-model="form.displayName" placeholder="Orders Team Space" />
          </label>
          <label>
            Deletion policy
            <select v-model="form.deletionPolicy">
              <option value="delete">Delete</option>
              <option value="orphan">Orphan</option>
              <option value="snapshot">Snapshot</option>
            </select>
          </label>
          <label style="grid-column: span 2">
            Quotas
            <textarea
              v-model="form.quotasText"
              rows="5"
              placeholder="limits.memory=8Gi&#10;requests.cpu=2"
            />
          </label>
          <label style="grid-column: span 2">
            Labels
            <textarea
              v-model="form.labelsText"
              rows="5"
              placeholder="owner=orders&#10;compliance=tier-1"
            />
          </label>
        </div>
        <p class="muted">Use one `key=value` pair per line for quotas and labels.</p>
        <p v-if="submitError" class="error-text">{{ submitError }}</p>
      </div>

      <div class="form-actions">
        <button class="button secondary" :disabled="submitting" @click="requestOpen = false">Cancel</button>
        <button class="button primary" :disabled="submitting" @click="submit">
          {{ submitting ? 'Submitting...' : 'Submit claim' }}
        </button>
      </div>
    </div>
  </div>
</template>
