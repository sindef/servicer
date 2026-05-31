<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '../api'
import { useApi } from '../composables/useApi'
import StatusPill from '../components/StatusPill.vue'

const filter = ref('')
const pageSize = 50
const offset = ref(0)

const { data, loading, error, reload } = useApi(
  (signal?: AbortSignal) =>
    api.instances(
      {
        q: filter.value.trim(),
        limit: pageSize,
        offset: offset.value
      },
      { signal }
    ),
  { refreshMs: 4000, retainOnSilentError: true }
)

const rows = computed(() => data.value || [])
const canPreviousPage = computed(() => offset.value > 0)
const canNextPage = computed(() => rows.value.length === pageSize)

watch(filter, () => {
  offset.value = 0
  void reload()
})

watch(offset, () => {
  void reload()
})

function previousPage() {
  if (!canPreviousPage.value) {
    return
  }
  offset.value = Math.max(0, offset.value - pageSize)
}

function nextPage() {
  if (!canNextPage.value) {
    return
  }
  offset.value += pageSize
}
</script>

<template>
  <section class="page-heading">
    <div>
      <p class="eyebrow">Instances</p>
      <h1>Managed services</h1>
    </div>
  </section>

  <div class="toolbar">
    <input v-model="filter" class="search" type="search" placeholder="Filter by name, product, project, or phase" />
    <div class="toolbar-actions">
      <button class="button secondary" :disabled="!canPreviousPage || loading" @click="previousPage">Previous</button>
      <button class="button secondary" :disabled="!canNextPage || loading" @click="nextPage">Next</button>
    </div>
  </div>

  <p v-if="loading" class="muted">Loading instances...</p>
  <p v-else-if="error" class="error-text">{{ error }}</p>
  <p v-if="!loading && !error" class="muted">Showing {{ rows.length }} instance{{ rows.length === 1 ? '' : 's' }} (offset {{ offset }})</p>

  <table v-if="!loading && !error" class="data-table">
    <thead>
      <tr>
        <th>Name</th>
        <th>Product</th>
        <th>Project</th>
        <th>Phase</th>
        <th>Sync</th>
        <th>Cluster</th>
      </tr>
    </thead>
    <tbody>
      <tr v-for="instance in rows" :key="instance.name">
        <td>
          <RouterLink class="table-link" :to="`/instances/${instance.name}`">{{ instance.name }}</RouterLink>
          <small>{{ instance.health || 'No health summary yet' }}</small>
        </td>
        <td>{{ instance.productName }}</td>
        <td>{{ instance.projectName }}</td>
        <td><StatusPill :value="instance.phase" /></td>
        <td><StatusPill :value="instance.syncPhase" /></td>
        <td>{{ instance.clusterName || 'Unplaced' }}</td>
      </tr>
    </tbody>
  </table>
</template>
