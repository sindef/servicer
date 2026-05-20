<script setup lang="ts">
import { ref, computed } from 'vue'
import { api, type AuditEventSummary } from '../api'
import StatusPill from '../components/StatusPill.vue'

const PAGE_SIZE = 25

const query = ref('')
const loading = ref(false)
const error = ref<string | null>(null)
const events = ref<AuditEventSummary[]>([])
const page = ref(1)

const pageCount = computed(() => Math.max(1, Math.ceil(events.value.length / PAGE_SIZE)))
const pageEvents = computed(() => {
  const start = (page.value - 1) * PAGE_SIZE
  return events.value.slice(start, start + PAGE_SIZE)
})

async function search() {
  loading.value = true
  error.value = null
  page.value = 1
  try {
    events.value = await api.audit(query.value)
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Audit search failed'
  } finally {
    loading.value = false
  }
}

search()
</script>

<template>
  <section class="page-heading">
    <div>
      <p class="eyebrow">Audit</p>
      <h1>Events and action trail</h1>
    </div>
  </section>

  <form class="toolbar" @submit.prevent="search">
    <input v-model="query" class="search" type="search" placeholder="Search actions, reasons, targets, actors" />
    <button class="button secondary" type="submit">Search</button>
  </form>

  <p v-if="loading" class="muted">Searching audit events...</p>
  <p v-else-if="error" class="error-text">{{ error }}</p>

  <template v-else>
    <table class="data-table">
      <thead>
        <tr>
          <th>Time</th>
          <th>Type</th>
          <th>Subject</th>
          <th>Phase</th>
          <th>Message</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="pageEvents.length === 0">
          <td colspan="5" class="muted">No events found.</td>
        </tr>
        <tr v-for="event in pageEvents" :key="`${event.type}-${event.subject}-${event.time}`">
          <td>{{ event.time || 'Unknown' }}</td>
          <td>{{ event.type }}</td>
          <td>
            <strong>{{ event.subject }}</strong>
            <small>{{ event.action || event.reason || event.involved }}</small>
          </td>
          <td><StatusPill :value="event.phase || event.reason || 'Observed'" /></td>
          <td>{{ event.message || 'No message' }}</td>
        </tr>
      </tbody>
    </table>

    <div v-if="pageCount > 1" class="pagination-bar">
      <button class="button secondary" :disabled="page <= 1" @click="page--">← Prev</button>
      <span class="muted">Page {{ page }} of {{ pageCount }} &nbsp;·&nbsp; {{ events.length }} total</span>
      <button class="button secondary" :disabled="page >= pageCount" @click="page++">Next →</button>
    </div>
  </template>
</template>
