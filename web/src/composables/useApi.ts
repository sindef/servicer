import { onBeforeUnmount, onMounted, ref } from 'vue'

interface UseApiOptions {
  refreshMs?: number
  retainOnSilentError?: boolean
}

export function useApi<T>(loader: () => Promise<T>, options: UseApiOptions = {}) {
  const data = ref<T | null>(null)
  const loading = ref(true)
  const error = ref<string | null>(null)
  let refreshTimer: number | undefined
  let inFlight = false

  async function reload(silent = false) {
    if (inFlight) {
      return
    }
    inFlight = true
    if (!silent) {
      loading.value = true
    }
    if (!silent) {
      error.value = null
    }
    try {
      data.value = await loader()
      error.value = null
    } catch (err) {
      if (!silent || !options.retainOnSilentError || data.value === null) {
        error.value = err instanceof Error ? err.message : 'Request failed'
      }
    } finally {
      loading.value = false
      inFlight = false
    }
  }

  onMounted(() => {
    void reload()
    if (options.refreshMs && options.refreshMs > 0) {
      refreshTimer = window.setInterval(() => void reload(true), options.refreshMs)
    }
  })

  onBeforeUnmount(() => {
    if (refreshTimer) {
      window.clearInterval(refreshTimer)
    }
  })

  return { data, loading, error, reload }
}
