<script setup lang="ts">
import { ref, computed } from 'vue'
import { api } from '../api'
import { useApi } from '../composables/useApi'
import StatusPill from '../components/StatusPill.vue'

const ACTIONS_PAGE_SIZE = 10

const { data, loading, error } = useApi(api.overview, { refreshMs: 5000, retainOnSilentError: true })
const actionsPage = ref(1)

const pagedActions = computed(() => {
  const all = data.value?.recentActions ?? []
  const start = (actionsPage.value - 1) * ACTIONS_PAGE_SIZE
  return all.slice(start, start + ACTIONS_PAGE_SIZE)
})
const actionsPageCount = computed(() =>
  Math.max(1, Math.ceil((data.value?.recentActions.length ?? 0) / ACTIONS_PAGE_SIZE))
)
</script>

<template>
  <section class="page-heading">
    <div>
      <p class="eyebrow">Overview</p>
      <h1>Platform posture</h1>
    </div>
  </section>

  <p v-if="loading" class="muted">Loading platform summary...</p>
  <p v-else-if="error" class="error-text">{{ error }}</p>

  <template v-else-if="data">
    <section class="metric-grid">
      <div class="metric">
        <span>Tenants</span>
        <strong>{{ data.tenants }}</strong>
      </div>
      <div class="metric">
        <span>Projects</span>
        <strong>{{ data.projects }}</strong>
      </div>
      <div class="metric">
        <span>Instances</span>
        <strong>{{ data.instances }}</strong>
      </div>
      <div class="metric">
        <span>Pending actions</span>
        <strong>{{ data.pendingActions }}</strong>
      </div>
    </section>

    <div class="bands-stack">
      <section class="content-band">
        <div>
          <h2>Health</h2>
          <p class="muted">Product-oriented readiness across all service instances.</p>
        </div>
        <div class="health-row">
          <span><StatusPill value="Ready" /> {{ data.health.ready }}</span>
          <span><StatusPill value="Provisioning" /> {{ data.health.provisioning }}</span>
          <span><StatusPill value="Failed" /> {{ data.health.failed }}</span>
          <span><StatusPill value="Other" /> {{ data.health.other }}</span>
        </div>
      </section>

      <section class="content-band">
        <div>
          <h2>Recent actions</h2>
          <p class="muted">Latest day-2 activity across managed products.</p>
        </div>
        <table v-if="data.recentActions.length" class="data-table">
          <thead>
            <tr>
              <th>Action</th>
              <th>Target</th>
              <th>Phase</th>
              <th>Result</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="action in pagedActions" :key="action.name">
              <td>{{ action.action }}</td>
              <td>{{ action.targetName }}</td>
              <td><StatusPill :value="action.phase" /></td>
              <td>{{ action.result || 'No result yet' }}</td>
            </tr>
          </tbody>
        </table>
        <p v-else class="muted">No actions have been submitted yet.</p>
        <div v-if="actionsPageCount > 1" class="pagination-bar">
          <button class="button secondary" :disabled="actionsPage <= 1" @click="actionsPage--">← Prev</button>
          <span class="muted">Page {{ actionsPage }} of {{ actionsPageCount }}</span>
          <button class="button secondary" :disabled="actionsPage >= actionsPageCount" @click="actionsPage++">Next →</button>
        </div>
      </section>
    </div>
  </template>
</template>
