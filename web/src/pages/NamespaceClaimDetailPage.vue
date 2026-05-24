<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { useApi } from '../composables/useApi'
import StatusPill from '../components/StatusPill.vue'

const props = defineProps<{ name: string }>()
const router = useRouter()
const { data, loading, error, reload } = useApi(() => api.namespaceClaims.detail(props.name), {
  refreshMs: 3000,
  retainOnSilentError: true
})

const editOpen = ref(false)
const deleteOpen = ref(false)
const updating = ref(false)
const deleting = ref(false)
const formError = ref<string | null>(null)
const formMessage = ref<string | null>(null)
const deleteConfirm = ref('')

const form = reactive({
  name: '',
  projectName: '',
  displayName: '',
  deletionPolicy: 'delete',
  quotasText: '',
  labelsText: ''
})

watch(
  data,
  (claim) => {
    if (!claim) return
    form.name = claim.name
    form.projectName = claim.projectName
    form.displayName = claim.displayName || ''
    form.deletionPolicy = claim.deletionPolicy || 'delete'
    form.quotasText = formatMap(claim.quotas)
    form.labelsText = formatMap(claim.labels)
  },
  { immediate: true }
)

function formatMap(value?: Record<string, string>) {
  return Object.entries(value || {})
    .map(([key, entry]) => `${key}=${entry}`)
    .join('\n')
}

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

async function save() {
  updating.value = true
  formError.value = null
  formMessage.value = null
  try {
    const response = await api.namespaceClaims.update(props.name, {
      name: form.name,
      projectName: form.projectName,
      displayName: form.displayName || undefined,
      deletionPolicy: form.deletionPolicy,
      quotas: parseMapInput(form.quotasText),
      labels: parseMapInput(form.labelsText)
    })
    formMessage.value = response.message
    editOpen.value = false
    await reload()
  } catch (err) {
    formError.value = err instanceof Error ? err.message : 'Failed to update namespace claim'
  } finally {
    updating.value = false
  }
}

async function remove() {
  if (deleteConfirm.value !== props.name) {
    formError.value = 'Type the claim name to confirm deletion.'
    return
  }
  deleting.value = true
  formError.value = null
  try {
    await api.namespaceClaims.delete(props.name)
    await router.push('/namespace-claims')
  } catch (err) {
    formError.value = err instanceof Error ? err.message : 'Failed to delete namespace claim'
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <section class="page-heading">
    <div>
      <p class="eyebrow">Namespace claim</p>
      <h1>{{ data?.displayName || name }}</h1>
      <p class="muted">{{ data?.name }}</p>
    </div>
    <div class="form-actions">
      <button class="button secondary" @click="editOpen = true">Edit</button>
      <button class="button danger" @click="deleteOpen = true">Delete</button>
    </div>
  </section>

  <p v-if="loading" class="muted">Loading namespace claim...</p>
  <p v-else-if="error" class="error-text">{{ error }}</p>

  <div v-else-if="data" class="bands-stack">
    <div class="claims-summary-grid">
      <article class="metric">
        <span>Phase</span>
        <div style="margin-top: 10px"><StatusPill :value="data.phase" /></div>
      </article>
      <article class="metric">
        <span>Project</span>
        <strong style="font-size: 20px">{{ data.projectName }}</strong>
      </article>
      <article class="metric">
        <span>Namespace</span>
        <strong style="font-size: 20px">{{ data.namespace || 'Pending' }}</strong>
      </article>
      <article class="metric">
        <span>Cluster</span>
        <strong style="font-size: 20px">{{ data.clusterName || 'Unplaced' }}</strong>
      </article>
    </div>

    <section class="content-band band-config">
      <div class="band-header">
        <h2>Desired contract</h2>
        <span class="muted">Deletion: {{ data.deletionPolicy || 'delete' }}</span>
      </div>
      <div class="claims-two-column">
        <div>
          <h3>Quota intent</h3>
          <ul v-if="Object.keys(data.quotas || {}).length" class="plain-list claims-map-list">
            <li v-for="(value, key) in data.quotas" :key="key"><code>{{ key }}</code><span>{{ value }}</span></li>
          </ul>
          <p v-else class="muted">No quota overrides declared.</p>
        </div>
        <div>
          <h3>Namespace labels</h3>
          <ul v-if="Object.keys(data.labels || {}).length" class="plain-list claims-map-list">
            <li v-for="(value, key) in data.labels" :key="key"><code>{{ key }}</code><span>{{ value }}</span></li>
          </ul>
          <p v-else class="muted">No labels declared.</p>
        </div>
      </div>
    </section>

    <section class="content-band band-delivery">
      <div class="band-header">
        <h2>Delivery</h2>
        <StatusPill :value="data.delivery.syncPhase || 'Pending'" />
      </div>
      <div class="claims-two-column">
        <div>
          <h3>Artifact</h3>
          <p class="muted">Revision: {{ data.artifact.revision || 'Unavailable' }}</p>
          <p class="muted">Path: {{ data.artifact.path || 'Unavailable' }}</p>
          <p class="muted">Files: {{ data.artifact.count || 0 }}</p>
        </div>
        <div>
          <h3>Sync handoff</h3>
          <p class="muted">Application: {{ data.delivery.applicationName || 'Not assigned' }}</p>
          <p class="muted">{{ data.delivery.message || 'No delivery message yet.' }}</p>
        </div>
      </div>
    </section>

    <section class="content-band band-health">
      <div class="band-header">
        <h2>Conditions</h2>
        <span class="muted">{{ data.health || 'No health summary yet' }}</span>
      </div>
      <table v-if="data.conditions.length" class="data-table compact">
        <thead>
          <tr>
            <th>Type</th>
            <th>Status</th>
            <th>Reason</th>
            <th>Message</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="condition in data.conditions" :key="`${condition.type}-${condition.reason}`">
            <td>{{ condition.type }}</td>
            <td><StatusPill :value="condition.status" /></td>
            <td>{{ condition.reason }}</td>
            <td>{{ condition.message }}</td>
          </tr>
        </tbody>
      </table>
      <p v-else class="muted">No status conditions reported yet.</p>
    </section>

    <p v-if="formMessage" class="success-text">{{ formMessage }}</p>
    <p v-if="formError" class="error-text">{{ formError }}</p>
  </div>

  <div v-if="editOpen" class="modal-backdrop">
    <div class="modal-panel">
      <div class="modal-head">
        <div>
          <h2>Edit namespace claim</h2>
          <p class="muted">Adjust ownership-safe request fields without dropping the claim identity.</p>
        </div>
        <button class="button text" @click="editOpen = false">Close</button>
      </div>
      <div class="modal-section">
        <div class="form-grid modal-form-grid">
          <label>
            Name
            <input v-model="form.name" disabled />
          </label>
          <label>
            Project
            <input v-model="form.projectName" disabled />
          </label>
          <label>
            Display name
            <input v-model="form.displayName" />
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
            <textarea v-model="form.quotasText" rows="5" />
          </label>
          <label style="grid-column: span 2">
            Labels
            <textarea v-model="form.labelsText" rows="5" />
          </label>
        </div>
        <p class="muted">Use one `key=value` pair per line.</p>
      </div>
      <div class="form-actions">
        <button class="button secondary" :disabled="updating" @click="editOpen = false">Cancel</button>
        <button class="button primary" :disabled="updating" @click="save">
          {{ updating ? 'Saving...' : 'Save changes' }}
        </button>
      </div>
    </div>
  </div>

  <div v-if="deleteOpen" class="modal-backdrop">
    <div class="modal-panel delete-modal">
      <div class="modal-head">
        <div>
          <h2>Delete namespace claim</h2>
          <p class="muted">This removes the claim request and lets the controller apply its deletion policy.</p>
        </div>
        <button class="button text" @click="deleteOpen = false">Close</button>
      </div>
      <div class="modal-section">
        <label>
          Type <strong>{{ name }}</strong> to confirm
          <input v-model="deleteConfirm" />
        </label>
      </div>
      <div class="form-actions">
        <button class="button secondary" :disabled="deleting" @click="deleteOpen = false">Cancel</button>
        <button class="button danger" :disabled="deleting" @click="remove">
          {{ deleting ? 'Deleting...' : 'Delete claim' }}
        </button>
      </div>
    </div>
  </div>
</template>
