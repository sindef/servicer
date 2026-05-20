<script setup lang="ts">
import { computed } from 'vue'
import { api } from '../api'
import { useApi } from '../composables/useApi'
import StatusPill from '../components/StatusPill.vue'

const tenants = useApi(api.tenants)
const projects = useApi(api.projects)
const tenantRows = computed(() => tenants.data.value || [])
const projectRows = computed(() => projects.data.value || [])

function projectsForTenant(tenantName: string) {
  return projectRows.value.filter((p) => p.tenantName === tenantName)
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
            </tr>
          </thead>
          <tbody>
            <tr v-for="project in projectsForTenant(tenant.name)" :key="project.name">
              <td><strong>{{ project.displayName }}</strong><small>{{ project.name }}</small></td>
              <td>{{ project.environment || '—' }}</td>
              <td><StatusPill :value="project.phase" /></td>
              <td>{{ project.clusterName || 'Unplaced' }}</td>
              <td>{{ project.instanceCount }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <p v-if="tenantRows.length === 0" class="muted">No tenants found.</p>
  </div>
</template>
