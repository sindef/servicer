<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '../api'
import { useApi } from '../composables/useApi'
import StatusPill from '../components/StatusPill.vue'

const { data, loading, error } = useApi(api.instances, { refreshMs: 4000, retainOnSilentError: true })
const filter = ref('')

const filtered = computed(() => {
  const needle = filter.value.trim().toLowerCase()
  if (!data.value || !needle) return data.value || []
  return data.value.filter((instance) =>
    [instance.name, instance.productName, instance.projectName, instance.phase].some((value) =>
      value.toLowerCase().includes(needle)
    )
  )
})
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
  </div>

  <p v-if="loading" class="muted">Loading instances...</p>
  <p v-else-if="error" class="error-text">{{ error }}</p>

  <table v-else class="data-table">
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
      <tr v-for="instance in filtered" :key="instance.name">
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
