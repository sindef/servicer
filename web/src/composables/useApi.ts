import { onBeforeUnmount, onMounted, ref } from 'vue'

interface UseApiOptions {
  refreshMs?: number
  retainOnSilentError?: boolean
}

type ApiLoader<T> = (...args: any[]) => Promise<T>

export function useApi<T>(loader: ApiLoader<T>, options: UseApiOptions = {}) {
  const data = ref<T | null>(null)
  const loading = ref(true)
  const error = ref<string | null>(null)
  let refreshTimer: number | undefined
  let inFlight = false
  let activeController: AbortController | null = null

  async function reload(silent = false) {
    if (inFlight) {
      return
    }
    activeController?.abort()
    activeController = new AbortController()
    inFlight = true
    if (!silent) {
      loading.value = true
    }
    if (!silent) {
      error.value = null
    }
    try {
      data.value = await (loader.length > 0 ? loader(activeController.signal) : loader())
      error.value = null
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') {
        return
      }
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
    activeController?.abort()
    if (refreshTimer) {
      window.clearInterval(refreshTimer)
    }
  })

  return { data, loading, error, reload }
}
