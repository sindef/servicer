<script setup lang="ts">
const props = defineProps<{
  value?: string
  toneOverride?: 'good' | 'warn' | 'bad' | 'neutral'
}>()

function tone(value?: string) {
  if (props.toneOverride) {
    return props.toneOverride
  }
  const normalized = (value || '').trim().toLowerCase()
  if (
    [
      'ready',
      'synced',
      'succeeded',
      'running',
      'healthy',
      'published',
      'observed',
      'true'
    ].includes(normalized)
  ) {
    return 'good'
  }
  if (
    [
      'failed',
      'degraded',
      'blocked',
      'rejected',
      'unhealthy',
      'error',
      'false'
    ].includes(normalized)
  ) {
    return 'bad'
  }
  if (
    [
      'pendingapproval',
      'pending',
      'queued',
      'materialized',
      'provisioning',
      'awaitingsync',
      'outofsync',
      'syncing',
      'progressing'
    ].includes(normalized)
  ) {
    return 'warn'
  }
  return 'neutral'
}
</script>

<template>
  <span class="status-pill" :class="tone(props.value)">{{ props.value || 'Unknown' }}</span>
</template>
