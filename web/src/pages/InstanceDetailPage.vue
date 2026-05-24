<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { dump, load } from 'js-yaml'
import { api, type CredentialDetail, type ProductRequest } from '../api'
import { useApi } from '../composables/useApi'
import StatusPill from '../components/StatusPill.vue'
import YamlEditor from '../components/YamlEditor.vue'

type NatsStreamForm = {
  id: number
  name: string
  subjects: string
  storage: string
  retention: string
  maxAge: string
}

type NatsConsumerForm = {
  id: number
  name: string
  stream: string
  filterSubjects: string
  ackPolicy: string
}

type NatsCredentialForm = {
  id: number
  name: string
  username: string
  publish: string
  subscribe: string
  allowResponses: boolean
}

const props = defineProps<{ name: string }>()
const { data, loading, error, reload } = useApi(() => api.instance(props.name), {
  refreshMs: 3000,
  retainOnSilentError: true
})
const catalog = useApi(api.catalog)
const nextNatsRowId = ref(1)

const endpointRows = computed(() => Object.entries(data.value?.endpoints || {}))
const eventRows = computed(() => data.value?.events || [])
const productPlans = computed(
  () => catalog.data.value?.find((entry) => entry.name === data.value?.productClass)?.plans || []
)
const actionNames = computed(() => new Set(data.value?.availableActions.map((action) => action.name) || []))
const supportsScale = computed(() => actionNames.value.has('scale'))
const supportsFailover = computed(() => actionNames.value.has('failover'))
const supportsQuota = computed(() => actionNames.value.has('update-quota'))
const supportsGrantAccess = computed(() => actionNames.value.has('grant-access'))
const kubeconfigActions = computed(() =>
  (data.value?.recentActions ?? []).filter((a) => !!a.kubeconfigDownloadUrl)
)
const KUBECONFIG_PAGE_SIZE = 3
const kubeconfigPage = ref(0)
const kubeconfigPageCount = computed(() =>
  Math.ceil(kubeconfigActions.value.length / KUBECONFIG_PAGE_SIZE)
)
const paginatedKubeconfigActions = computed(() => {
  const start = kubeconfigPage.value * KUBECONFIG_PAGE_SIZE
  return kubeconfigActions.value.slice(start, start + KUBECONFIG_PAGE_SIZE)
})

const PAGE_SIZE = 5
const actionsPage = ref(0)
const actionsPageCount = computed(() => Math.ceil((data.value?.recentActions.length ?? 0) / PAGE_SIZE))
const paginatedActions = computed(() => {
  const start = actionsPage.value * PAGE_SIZE
  return (data.value?.recentActions ?? []).slice(start, start + PAGE_SIZE)
})
const endpointsPage = ref(0)
const endpointsPageCount = computed(() => Math.ceil(endpointRows.value.length / PAGE_SIZE))
const paginatedEndpoints = computed(() => {
  const start = endpointsPage.value * PAGE_SIZE
  return endpointRows.value.slice(start, start + PAGE_SIZE)
})
const eventsPage = ref(0)
const eventsPageCount = computed(() => Math.ceil(eventRows.value.length / PAGE_SIZE))
const paginatedEvents = computed(() => {
  const start = eventsPage.value * PAGE_SIZE
  return eventRows.value.slice(start, start + PAGE_SIZE)
})
const supportsDeleteStream = computed(() => actionNames.value.has('delete-stream'))
const supportsDeleteConsumer = computed(() => actionNames.value.has('delete-consumer'))
const supportsPurgeStream = computed(() => actionNames.value.has('purge-stream'))
const supportsBackup = computed(() => actionNames.value.has('backup'))
const currentBackup = computed(() => {
  const params = data.value?.desired?.parameters
  if (!params?.backup || typeof params.backup !== 'object') return null
  return params.backup as Record<string, unknown>
})
const currentBackupObjectStore = computed(() => {
  const b = currentBackup.value
  if (!b?.objectStore || typeof b.objectStore !== 'object') return null
  return b.objectStore as Record<string, unknown>
})
const natsStreamOptions = computed(() =>
  Array.from(
    new Set(
      parameterForm.natsStreams
        .map((stream) => stream.name.trim())
        .filter((name) => name.length > 0)
    )
  )
)

const updateForm = reactive({
  servicePlan: '',
  version: ''
})
const parameterForm = reactive({
  replicas: 3,
  databaseName: '',
  cpu: '2',
  memory: '4Gi',
  pods: '20',
  defaultDenyIngress: true,
  memoryProfile: 'medium',
  memoryLimit: '512Mi',
  persistence: 'persistent',
  storageClass: '',
  storageSize: '10Gi',
  maxMemoryPolicy: 'allkeys-lru',
  backupProfile: 'daily-7d',
  backupCredentialsSecret: '',
  backupEndpoint: '',
  backupBucket: '',
  backupPath: '',
  backupRegion: '',
  backupSchedule: '',
  backupRetention: '30d',
  maxPayload: '1MiB',
  primaryCluster: '',
  standbyClusters: '',
  maxReplicationLagSeconds: 30,
  serviceType: 'ClusterIP',
  externalDnsHostname: '',
  natsStreams: [] as NatsStreamForm[],
  natsConsumers: [] as NatsConsumerForm[],
  natsAppCredentials: [] as NatsCredentialForm[]
})
function defaultNamespaceAccessUrl() {
  if (window.location.hostname === 'localhost' || window.location.hostname === '127.0.0.1') {
    return 'https://localhost:8443'
  }
  return window.location.origin
}

const actionForm = reactive({
  reason: '',
  replicas: 3,
  candidateCluster: '',
  cpu: '2',
  memory: '4Gi',
  pods: '20',
  subject: '',
  defaultUrl: defaultNamespaceAccessUrl(),
  stream: '',
  consumer: '',
  credentialName: ''
})
const updating = ref(false)
const deleting = ref(false)
const editOpen = ref(false)
const deleteOpen = ref(false)
const yamlOpen = ref(false)
const yamlContent = ref('')
const yamlError = ref<string | null>(null)
const yamlSaving = ref(false)
const deleteConfirm = ref('')
const actionSubmitting = ref<string | null>(null)
const actionDownloading = ref<string | null>(null)
const credentialLoading = ref<string | null>(null)
const credentialDetail = ref<CredentialDetail | null>(null)
const credentialsOpen = ref(false)
const credentialError = ref<string | null>(null)
const backupConfigOpen = ref(false)
const showCapabilities = ref(false)
const formMessage = ref<string | null>(null)
const formError = ref<string | null>(null)

function allocateNatsRowId() {
  const id = nextNatsRowId.value
  nextNatsRowId.value += 1
  return id
}

function createNatsStream(values: Partial<Omit<NatsStreamForm, 'id'>> = {}): NatsStreamForm {
  return {
    id: allocateNatsRowId(),
    name: values.name || '',
    subjects: values.subjects || '',
    storage: values.storage || 'file',
    retention: values.retention || 'limits',
    maxAge: values.maxAge || '168h'
  }
}

function createNatsConsumer(values: Partial<Omit<NatsConsumerForm, 'id'>> = {}): NatsConsumerForm {
  return {
    id: allocateNatsRowId(),
    name: values.name || '',
    stream: values.stream || '',
    filterSubjects: values.filterSubjects || '',
    ackPolicy: values.ackPolicy || 'explicit'
  }
}

function createNatsCredential(values: Partial<Omit<NatsCredentialForm, 'id'>> = {}): NatsCredentialForm {
  return {
    id: allocateNatsRowId(),
    name: values.name || '',
    username: values.username || '',
    publish: values.publish || '',
    subscribe: values.subscribe || '',
    allowResponses: values.allowResponses ?? true
  }
}

function csvList(value: string) {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function addNatsStream() {
  parameterForm.natsStreams.push(createNatsStream())
}

function removeNatsStream(id: number) {
  parameterForm.natsStreams = parameterForm.natsStreams.filter((stream) => stream.id !== id)
}

function addNatsConsumer() {
  parameterForm.natsConsumers.push(createNatsConsumer())
}

function removeNatsConsumer(id: number) {
  parameterForm.natsConsumers = parameterForm.natsConsumers.filter((consumer) => consumer.id !== id)
}

function addNatsCredential() {
  parameterForm.natsAppCredentials.push(createNatsCredential())
}

function removeNatsCredential(id: number) {
  parameterForm.natsAppCredentials = parameterForm.natsAppCredentials.filter((credential) => credential.id !== id)
}

function conditionTone(type: string, status: string) {
  const normalizedType = type.trim().toLowerCase()
  const normalizedStatus = status.trim().toLowerCase()
  if (normalizedStatus !== 'true' && normalizedStatus !== 'false') {
    return undefined
  }
  const invertedConditions = new Set(['failed', 'degraded'])
  if (invertedConditions.has(normalizedType)) {
    return normalizedStatus === 'false' ? 'good' : 'bad'
  }
  return normalizedStatus === 'true' ? 'good' : 'bad'
}

function credentialRows(value: string) {
  const lineCount = value.split('\n').length
  const estimatedWrappedLines = Math.ceil(value.length / 72)
  return Math.min(6, Math.max(2, Math.max(lineCount, estimatedWrappedLines)))
}

watch(
  data,
  (value) => {
    if (!value) {
      return
    }
    updateForm.servicePlan = value.planName
    updateForm.version = value.desired.version || ''
    applyParameters(value.desired.parameters || {})
  },
  { immediate: true }
)

function numberParam(parameters: Record<string, unknown>, key: string, fallback: number) {
  const value = parameters[key]
  return typeof value === 'number' ? value : fallback
}

function stringParam(parameters: Record<string, unknown>, key: string, fallback = '') {
  const value = parameters[key]
  return typeof value === 'string' ? value : fallback
}

function boolParam(parameters: Record<string, unknown>, key: string, fallback: boolean) {
  const value = parameters[key]
  return typeof value === 'boolean' ? value : fallback
}

function applyParameters(parameters: Record<string, unknown>) {
  parameterForm.replicas = numberParam(parameters, data.value?.productClass === 'postgresql' ? 'instances' : 'replicas', 3)
  parameterForm.databaseName = stringParam(parameters, 'databaseName', '')
  parameterForm.cpu = stringParam(parameters, 'cpu', '2')
  parameterForm.memory = stringParam(parameters, 'memory', '4Gi')
  parameterForm.pods = stringParam(parameters, 'pods', '20')
  parameterForm.defaultDenyIngress = boolParam(parameters, 'defaultDenyIngress', true)
  parameterForm.memoryProfile = stringParam(parameters, 'memoryProfile', 'medium')
  parameterForm.memoryLimit = stringParam(parameters, 'memoryLimit', '512Mi')
  parameterForm.persistence = stringParam(parameters, 'persistence', 'persistent')
  parameterForm.storageClass = stringParam(parameters, 'storageClass', '')
  parameterForm.storageSize = stringParam(parameters, 'storageSize', '10Gi')
  parameterForm.maxMemoryPolicy = stringParam(parameters, 'maxMemoryPolicy', 'allkeys-lru')
  parameterForm.backupProfile = stringParam(parameters, 'backupProfile', 'daily-7d')
  const backup = parameters.backup && typeof parameters.backup === 'object'
    ? (parameters.backup as Record<string, unknown>)
    : {}
  const objectStore = backup.objectStore && typeof backup.objectStore === 'object'
    ? (backup.objectStore as Record<string, unknown>)
    : {}
  parameterForm.backupCredentialsSecret = stringParam(objectStore, 'credentialsSecret', '')
  parameterForm.backupEndpoint = stringParam(objectStore, 'endpointUrl', '')
  parameterForm.backupBucket = stringParam(objectStore, 'bucket', '')
  parameterForm.backupPath = stringParam(objectStore, 'path', '')
  parameterForm.backupRegion = stringParam(objectStore, 'region', '')
  parameterForm.backupSchedule = stringParam(backup, 'schedule', '')
  parameterForm.backupRetention = stringParam(backup, 'retention', '30d')
  parameterForm.maxPayload = stringParam(parameters, 'maxPayload', '1MiB')
  parameterForm.primaryCluster = stringParam(parameters, 'primaryCluster', '')
  parameterForm.maxReplicationLagSeconds = numberParam(parameters, 'maxReplicationLagSeconds', 30)
  const standbys = parameters.standbyClusters
  parameterForm.standbyClusters = Array.isArray(standbys) ? standbys.join(', ') : ''
  parameterForm.natsStreams = Array.isArray(parameters.streams)
    ? parameters.streams.map((entry) => {
        const stream = entry as Record<string, unknown>
        return createNatsStream({
          name: stringParam(stream, 'name', ''),
          subjects: Array.isArray(stream.subjects) ? stream.subjects.join(', ') : '',
          storage: stringParam(stream, 'storage', 'file'),
          retention: stringParam(stream, 'retention', 'limits'),
          maxAge: stringParam(stream, 'maxAge', '168h')
        })
      })
    : []
  parameterForm.natsConsumers = Array.isArray(parameters.consumers)
    ? parameters.consumers.map((entry) => {
        const consumer = entry as Record<string, unknown>
        return createNatsConsumer({
          name: stringParam(consumer, 'name', ''),
          stream: stringParam(consumer, 'stream', ''),
          filterSubjects: Array.isArray(consumer.filterSubjects) ? consumer.filterSubjects.join(', ') : '',
          ackPolicy: stringParam(consumer, 'ackPolicy', 'explicit')
        })
      })
    : []
  parameterForm.natsAppCredentials = Array.isArray(parameters.appCredentials)
    ? parameters.appCredentials.map((entry) => {
        const credential = entry as Record<string, unknown>
        const permissions =
          credential.permissions && typeof credential.permissions === 'object'
            ? (credential.permissions as Record<string, unknown>)
            : {}
        return createNatsCredential({
          name: stringParam(credential, 'name', ''),
          username: stringParam(credential, 'username', ''),
          publish: Array.isArray(permissions.publish) ? permissions.publish.join(', ') : '',
          subscribe: Array.isArray(permissions.subscribe) ? permissions.subscribe.join(', ') : '',
          allowResponses: boolParam(permissions, 'allowResponses', true)
        })
      })
    : []
}

function compactParams(values: Record<string, unknown>) {
  return Object.fromEntries(
    Object.entries(values).filter(([, value]) => {
      if (Array.isArray(value)) {
        return value.length > 0
      }
      return value !== '' && value !== undefined && value !== null
    })
  )
}

function buildUpdateParameters() {
  switch (data.value?.productClass) {
    case 'namespace':
      return {
        cpu: parameterForm.cpu,
        memory: parameterForm.memory,
        pods: parameterForm.pods,
        defaultDenyIngress: parameterForm.defaultDenyIngress,
        labels: { 'platform.mnorris.dev/profile': 'workload' }
      }
    case 'postgresql': {
      const backupObjectStore = parameterForm.backupBucket && parameterForm.backupCredentialsSecret
        ? compactParams({
            endpointUrl: parameterForm.backupEndpoint,
            bucket: parameterForm.backupBucket,
            path: parameterForm.backupPath,
            region: parameterForm.backupRegion,
            credentialsSecret: parameterForm.backupCredentialsSecret
          })
        : undefined
      const backup = backupObjectStore
        ? compactParams({
            objectStore: backupObjectStore,
            schedule: parameterForm.backupSchedule,
            retention: parameterForm.backupRetention
          })
        : undefined
      return compactParams({
        instances: parameterForm.replicas,
        databaseName: parameterForm.databaseName,
        storageClass: parameterForm.storageClass,
        storageSize: parameterForm.storageSize,
        backup,
        serviceType: parameterForm.serviceType,
        externalDnsHostname: (parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort') ? parameterForm.externalDnsHostname : undefined
      })
    }
    case 'mysql':
      return compactParams({
        replicas: parameterForm.replicas,
        databaseName: parameterForm.databaseName,
        cpu: parameterForm.cpu,
        memory: parameterForm.memory,
        storageClass: parameterForm.storageClass,
        storageSize: parameterForm.storageSize,
        backupProfile: parameterForm.backupProfile,
        replicationMode: updateForm.servicePlan === 'mysql-galera' ? 'galera' : updateForm.servicePlan === 'mysql-active-passive' ? 'active-passive' : 'single-primary',
        primaryCluster: parameterForm.primaryCluster,
        standbyClusters: parameterForm.standbyClusters
          .split(',')
          .map((cluster) => cluster.trim())
          .filter(Boolean),
        serviceType: parameterForm.serviceType,
        externalDnsHostname: (parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort') ? parameterForm.externalDnsHostname : undefined
      })
    case 'nats':
      return compactParams({
        replicas: parameterForm.replicas,
        jetstream: data.value.planName === 'nats-jetstream' || data.value.planName === 'nats-geo',
        streams: parameterForm.natsStreams
          .map((stream) =>
            compactParams({
              name: stream.name.trim(),
              subjects: csvList(stream.subjects),
              storage: stream.storage,
              retention: stream.retention,
              maxAge: stream.maxAge
            })
          )
          .filter((stream) => typeof stream.name === 'string' && stream.name.length > 0),
        consumers: parameterForm.natsConsumers
          .map((consumer) =>
            compactParams({
              name: consumer.name.trim(),
              stream: consumer.stream.trim(),
              filterSubjects: csvList(consumer.filterSubjects),
              ackPolicy: consumer.ackPolicy
            })
          )
          .filter(
            (consumer) =>
              typeof consumer.name === 'string' &&
              consumer.name.length > 0 &&
              typeof consumer.stream === 'string' &&
              consumer.stream.length > 0
          ),
        appCredentials: parameterForm.natsAppCredentials
          .map((credential) =>
            compactParams({
              name: credential.name.trim(),
              username: credential.username.trim(),
              permissions: compactParams({
                publish: csvList(credential.publish),
                subscribe: csvList(credential.subscribe),
                allowResponses: credential.allowResponses ? true : undefined
              })
            })
          )
          .filter((credential) => typeof credential.name === 'string' && credential.name.length > 0),
        storageClass: parameterForm.storageClass,
        storageSize: parameterForm.storageSize,
        maxPayload: parameterForm.maxPayload,
        memoryLimit: parameterForm.memoryLimit,
        serviceType: parameterForm.serviceType,
        externalDnsHostname: (parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort') ? parameterForm.externalDnsHostname : undefined
      })
    case 'valkey':
      return compactParams({
        replicas: parameterForm.replicas,
        memoryProfile: parameterForm.memoryProfile,
        memoryLimit: parameterForm.memoryLimit,
        persistence: parameterForm.persistence,
        storageClass: parameterForm.storageClass,
        storageSize: parameterForm.storageSize,
        maxMemoryPolicy: parameterForm.maxMemoryPolicy,
        primaryCluster: parameterForm.primaryCluster,
        standbyClusters: parameterForm.standbyClusters
          .split(',')
          .map((cluster) => cluster.trim())
          .filter(Boolean),
        maxReplicationLagSeconds: parameterForm.maxReplicationLagSeconds,
        serviceType: parameterForm.serviceType,
        externalDnsHostname: (parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort') ? parameterForm.externalDnsHostname : undefined
      })
    case 'yugabyte':
      return compactParams({
        tserverReplicas: parameterForm.replicas,
        masterReplicas: parameterForm.replicas,
        databaseName: parameterForm.databaseName,
        cpu: parameterForm.cpu,
        memory: parameterForm.memory,
        storageSize: parameterForm.storageSize,
        storageClass: parameterForm.storageClass,
        backupProfile: parameterForm.backupProfile,
        primaryCluster: parameterForm.primaryCluster,
        standbyClusters: parameterForm.standbyClusters
          .split(',')
          .map((cluster) => cluster.trim())
          .filter(Boolean),
        serviceType: parameterForm.serviceType,
        externalDnsHostname: (parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort') ? parameterForm.externalDnsHostname : undefined
      })
    default:
      return undefined
  }
}

function openYamlEditor() {
  if (!data.value) return
  yamlContent.value = dump(data.value.desired)
  yamlError.value = null
  yamlOpen.value = true
}

async function saveYaml() {
  yamlError.value = null
  yamlSaving.value = true
  try {
    const parsed = load(yamlContent.value) as ProductRequest
    if (!parsed || typeof parsed !== 'object') throw new Error('Invalid YAML')
    await api.updateRequest(data.value!.name, parsed)
    yamlOpen.value = false
    await reload()
  } catch (err) {
    yamlError.value = err instanceof Error ? err.message : 'Save failed'
  } finally {
    yamlSaving.value = false
  }
}

async function submitUpdate() {
  if (!data.value) {
    return
  }
  updating.value = true
  formError.value = null
  formMessage.value = null
  try {
    const response = await api.updateRequest(data.value.name, {
      name: data.value.name,
      projectName: data.value.projectName,
      serviceClass: data.value.productClass,
      servicePlan: updateForm.servicePlan,
      version: updateForm.version || undefined,
      parameters: buildUpdateParameters()
    })
    formMessage.value = response.message
    editOpen.value = false
    await reload()
  } catch (err) {
    formError.value = err instanceof Error ? err.message : 'Update failed'
  } finally {
    updating.value = false
  }
}

async function saveBackupConfig() {
  if (!data.value) return
  updating.value = true
  formError.value = null
  try {
    const response = await api.updateRequest(data.value.name, {
      name: data.value.name,
      projectName: data.value.projectName,
      serviceClass: data.value.productClass,
      servicePlan: updateForm.servicePlan,
      version: updateForm.version || undefined,
      parameters: buildUpdateParameters()
    })
    formMessage.value = response.message
    backupConfigOpen.value = false
    await reload()
  } catch (err) {
    formError.value = err instanceof Error ? err.message : 'Save failed'
  } finally {
    updating.value = false
  }
}

async function deleteInstance() {
  if (!data.value || deleteConfirm.value !== data.value.name) {
    return
  }
  deleting.value = true
  formError.value = null
  try {
    await api.deleteRequest(data.value.name)
    window.location.href = '/instances'
  } catch (err) {
    formError.value = err instanceof Error ? err.message : 'Delete failed'
  } finally {
    deleting.value = false
  }
}

function parametersFor(action: string) {
  if (action === 'scale') {
    return { replicas: actionForm.replicas }
  }
  if (action === 'failover') {
    return { candidateCluster: actionForm.candidateCluster }
  }
  if (action === 'update-quota') {
    return { cpu: actionForm.cpu, memory: actionForm.memory, pods: actionForm.pods }
  }
  if (action === 'grant-access') {
    return { subject: actionForm.subject, defaultUrl: actionForm.defaultUrl }
  }
  if (action === 'delete-stream' || action === 'purge-stream') {
    return { stream: actionForm.stream }
  }
  if (action === 'delete-consumer') {
    return { stream: actionForm.stream, consumer: actionForm.consumer }
  }
  if (action === 'rotate-credentials' && actionForm.credentialName.trim()) {
    return { credentialName: actionForm.credentialName.trim() }
  }
  return undefined
}

async function submitAction(action: string) {
  if (!data.value) {
    return
  }
  actionSubmitting.value = action
  formError.value = null
  formMessage.value = null
  try {
    const response = await api.submitAction(data.value.name, {
      action,
      reason: actionForm.reason,
      parameters: parametersFor(action)
    })
    formMessage.value = response.message
    actionForm.reason = ''
    await reload()
  } catch (err) {
    formError.value = err instanceof Error ? err.message : 'Action submission failed'
  } finally {
    actionSubmitting.value = null
  }
}

async function downloadKubeconfig(actionName: string, url: string) {
  if (!data.value) {
    return
  }
  actionDownloading.value = actionName
  formError.value = null
  try {
    const blob = await api.downloadKubeconfig(url)
    const objectUrl = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = objectUrl
    anchor.download = `${data.value.name}-${actionName}.kubeconfig`
    document.body.appendChild(anchor)
    anchor.click()
    anchor.remove()
    URL.revokeObjectURL(objectUrl)
  } catch (err) {
    formError.value = err instanceof Error ? err.message : 'Kubeconfig download failed'
  } finally {
    actionDownloading.value = null
  }
}

async function revealCredential(name: string, url?: string) {
  if (!data.value) {
    return
  }
  const revealUrl =
    url ||
    `/api/instances/${encodeURIComponent(data.value.name)}/credentials/${encodeURIComponent(data.value.namespace || data.value.runtime.namespace || '')}/${encodeURIComponent(name)}`
  credentialLoading.value = name
  credentialError.value = null
  try {
    credentialDetail.value = await api.credential(revealUrl)
    credentialsOpen.value = true
  } catch (err) {
    credentialError.value = err instanceof Error ? err.message : 'Credential retrieval failed'
  } finally {
    credentialLoading.value = null
  }
}
</script>

<template>
  <section class="page-heading">
    <div>
      <p class="eyebrow">Instance detail</p>
      <h1>{{ props.name }}</h1>
    </div>
    <div class="form-actions">
      <button class="button secondary" @click="openYamlEditor">Edit YAML</button>
      <button class="button secondary" @click="editOpen = true; showCapabilities = false">Edit</button>
      <button class="button danger" @click="deleteOpen = true">Delete</button>
    </div>
  </section>

  <p v-if="loading" class="muted">Loading instance...</p>
  <p v-else-if="error" class="error-text">{{ error }}</p>

  <template v-else-if="data">
    <section class="detail-summary">
      <div class="summary-card summary-card-product">
        <span>Product</span>
        <strong>{{ data.productName }}</strong>
      </div>
      <div class="summary-card summary-card-phase">
        <span>Phase</span>
        <StatusPill :value="data.phase" />
      </div>
      <div class="summary-card summary-card-sync">
        <span>Sync</span>
        <StatusPill :value="data.syncPhase" />
      </div>
      <div class="summary-card summary-card-runtime">
        <span>Runtime</span>
        <StatusPill :value="data.delivery.runtimeStatus || 'Unknown'" />
      </div>
      <div class="summary-card summary-card-cluster">
        <span>Cluster</span>
        <strong>{{ data.clusterName || 'Unplaced' }}</strong>
      </div>
    </section>

    <section class="instance-workspace">
      <div class="instance-main-column">
        <section class="content-band band-health">
          <div>
            <h2>Health</h2>
            <p class="muted">{{ data.health || 'Runtime health has not been observed yet.' }}</p>
          </div>
          <dl class="definition-grid">
            <div><dt>Project</dt><dd>{{ data.projectName }}</dd></div>
            <div><dt>Tenant</dt><dd>{{ data.tenantName || 'Unknown' }}</dd></div>
            <div><dt>Plan</dt><dd>{{ data.planDisplay }}</dd></div>
            <div><dt>Namespace</dt><dd>{{ data.namespace || 'Not assigned' }}</dd></div>
          </dl>
        </section>

        <section class="three-column operational-grid">
          <div class="content-band band-delivery">
            <h2>Delivery</h2>
            <dl class="definition-grid compact-defs">
              <div><dt>Argo</dt><dd><StatusPill :value="data.delivery.argoStatus || 'Unknown'" /></dd></div>
              <div><dt>Runtime</dt><dd><StatusPill :value="data.delivery.runtimeStatus || 'Unknown'" /></dd></div>
              <div><dt>Application</dt><dd>{{ data.delivery.applicationName || 'Not registered' }}</dd></div>
              <div><dt>Artifacts</dt><dd>{{ data.artifact.count || 0 }}</dd></div>
            </dl>
          </div>

          <div class="content-band band-runtime">
            <h2>Runtime</h2>
            <dl class="definition-grid compact-defs">
              <div><dt>Driver</dt><dd>{{ data.runtime.driver || 'Unknown' }}</dd></div>
              <div><dt>Kind</dt><dd>{{ data.runtime.kind || 'Unknown' }}</dd></div>
              <div><dt>Name</dt><dd>{{ data.runtime.name || 'Unknown' }}</dd></div>
              <div><dt>Namespace</dt><dd>{{ data.runtime.namespace || data.namespace || 'Unknown' }}</dd></div>
            </dl>
          </div>

          <div class="content-band band-config">
            <h2>Desired config</h2>
            <dl class="definition-grid compact-defs">
              <div><dt>Plan</dt><dd>{{ data.planDisplay }}</dd></div>
              <div><dt>Version</dt><dd>{{ data.desired.version || 'default' }}</dd></div>
              <div><dt>Params</dt><dd>{{ Object.keys(data.desired.parameters || {}).length }}</dd></div>
              <div><dt>Project</dt><dd>{{ data.projectName }}</dd></div>
            </dl>
          </div>
        </section>

        <section v-if="data.topology" class="content-band band-topology">
          <div>
            <h2>Topology</h2>
            <p class="muted">{{ data.topology.message || data.topology.mode }}</p>
          </div>
          <dl class="definition-grid">
            <div><dt>Mode</dt><dd>{{ data.topology.mode || 'Unknown' }}</dd></div>
            <div><dt>Primary</dt><dd>{{ data.topology.primaryCluster || 'Unknown' }}</dd></div>
            <div><dt>Failover</dt><dd>{{ data.topology.failoverReadiness || 'Unknown' }}</dd></div>
            <div><dt>Traffic endpoint</dt><dd>{{ data.topology.trafficEndpoint || 'Not configured' }}</dd></div>
          </dl>
        </section>

        <section v-if="data.messaging" class="content-band band-topology">
          <div>
            <h2>Messaging resources</h2>
            <p class="muted">
              JetStream {{ data.messaging.jetStream ? 'enabled' : 'disabled' }} with {{ data.messaging.streams?.length || 0 }} stream(s),
              {{ data.messaging.consumers?.length || 0 }} consumer(s), and {{ data.messaging.appCredentials?.length || 0 }} app credential(s).
            </p>
          </div>
          <dl class="definition-grid">
            <div><dt>Streams</dt><dd>{{ data.messaging.streams?.map((stream) => stream.name).join(', ') || 'None' }}</dd></div>
            <div><dt>Consumers</dt><dd>{{ data.messaging.consumers?.map((consumer) => `${consumer.stream}/${consumer.name}`).join(', ') || 'None' }}</dd></div>
            <div><dt>App users</dt><dd>{{ data.messaging.appCredentials?.map((credential) => credential.username).join(', ') || 'None' }}</dd></div>
            <div><dt>Subjects</dt><dd>{{ data.messaging.streams?.flatMap((stream) => stream.subjects || []).join(', ') || 'None' }}</dd></div>
          </dl>
        </section>

        <section class="two-column support-grid">
          <div class="content-band band-endpoints">
            <h2>Endpoints</h2>
            <ul v-if="endpointRows.length" class="plain-list">
              <li v-for="[name, address] in paginatedEndpoints" :key="name">
                <strong>{{ name }}</strong>
                <span>{{ address }}</span>
              </li>
              <li v-if="endpointsPageCount > 1" class="pagination-row">
                <button class="button secondary compact-button" :disabled="endpointsPage === 0" @click="endpointsPage--">‹</button>
                <span class="pagination-label">{{ endpointsPage + 1 }} / {{ endpointsPageCount }}</span>
                <button class="button secondary compact-button" :disabled="endpointsPage >= endpointsPageCount - 1" @click="endpointsPage++">›</button>
              </li>
            </ul>
            <p v-else class="muted">No live endpoints published yet.</p>
          </div>

          <div class="content-band band-credentials">
            <h2>Credentials</h2>
            <ul v-if="data.credentials.length || kubeconfigActions.length" class="plain-list">
              <li v-for="credential in data.credentials" :key="`${credential.namespace}/${credential.name}`">
                <div>
                  <strong>{{ credential.name }}</strong>
                  <span>{{ credential.namespace }}</span>
                </div>
                <button
                  class="button secondary compact-button"
                  type="button"
                  :disabled="credentialLoading === credential.name"
                  @click="revealCredential(credential.name, credential.revealUrl)"
                >
                  {{ credentialLoading === credential.name ? 'Loading...' : 'Reveal' }}
                </button>
              </li>
              <li v-for="action in paginatedKubeconfigActions" :key="action.name">
                <div>
                  <strong>kubeconfig</strong>
                  <span>{{ action.result || action.phase }}</span>
                </div>
                <button
                  class="button secondary compact-button"
                  type="button"
                  :disabled="actionDownloading === action.name"
                  @click="downloadKubeconfig(action.name, action.kubeconfigDownloadUrl!)"
                >
                  {{ actionDownloading === action.name ? 'Downloading...' : 'Download' }}
                </button>
              </li>
              <li v-if="kubeconfigPageCount > 1" class="pagination-row">
                <button class="button secondary compact-button" :disabled="kubeconfigPage === 0" @click="kubeconfigPage--">‹</button>
                <span class="pagination-label">{{ kubeconfigPage + 1 }} / {{ kubeconfigPageCount }}</span>
                <button class="button secondary compact-button" :disabled="kubeconfigPage >= kubeconfigPageCount - 1" @click="kubeconfigPage++">›</button>
              </li>
            </ul>
            <p v-else class="muted">No credential Secrets published for this instance.</p>
            <p v-if="credentialError" class="error-text">{{ credentialError }}</p>
          </div>
        </section>

        <section class="content-band band-recent-actions">
            <h2>Recent actions</h2>
            <ul v-if="data.recentActions.length" class="plain-list">
              <li v-for="action in paginatedActions" :key="action.name">
                <div>
                  <strong>{{ action.action }}</strong>
                  <span>{{ action.phase }} {{ action.result ? `- ${action.result}` : '' }}</span>
                </div>
              </li>
              <li v-if="actionsPageCount > 1" class="pagination-row">
                <button class="button secondary compact-button" :disabled="actionsPage === 0" @click="actionsPage--">‹</button>
                <span class="pagination-label">{{ actionsPage + 1 }} / {{ actionsPageCount }}</span>
                <button class="button secondary compact-button" :disabled="actionsPage >= actionsPageCount - 1" @click="actionsPage++">›</button>
              </li>
            </ul>
            <p v-else class="muted">No recent actions.</p>
        </section>

        <section v-if="data.productClass === 'postgresql' && supportsBackup" class="content-band band-actions">
          <div class="band-header">
            <h2>Backup</h2>
            <div class="band-header-actions">
              <button class="button secondary small" @click="backupConfigOpen = true">Configure</button>
              <button class="button secondary small" @click="submitAction('backup')" :disabled="!!actionSubmitting">
                {{ actionSubmitting === 'backup' ? 'Queuing...' : 'Backup now' }}
              </button>
            </div>
          </div>
          <template v-if="currentBackupObjectStore">
            <dl class="info-list">
              <dt>Bucket</dt>
              <dd>{{ currentBackupObjectStore.bucket }}</dd>
              <template v-if="currentBackupObjectStore.endpointUrl">
                <dt>Endpoint</dt>
                <dd>{{ currentBackupObjectStore.endpointUrl }}</dd>
              </template>
              <template v-if="currentBackupObjectStore.path">
                <dt>Path</dt>
                <dd>{{ currentBackupObjectStore.path }}</dd>
              </template>
              <template v-if="currentBackupObjectStore.region">
                <dt>Region</dt>
                <dd>{{ currentBackupObjectStore.region }}</dd>
              </template>
              <dt>Credentials secret</dt>
              <dd>{{ currentBackupObjectStore.credentialsSecret }}</dd>
              <dt>Schedule</dt>
              <dd>{{ currentBackup?.schedule || 'On demand only' }}</dd>
              <dt>Retention</dt>
              <dd>{{ currentBackup?.retention || 'Not set' }}</dd>
            </dl>
          </template>
          <p v-else class="muted">No backup configuration. Click Configure to set up S3 backups using CNPG's barman object store.</p>
        </section>

        <section class="content-band band-actions">
          <h2>Actions</h2>
          <p class="muted action-help">
            Configure fields used by selected actions. Hidden fields are not sent.
          </p>
          <div class="form-grid action-controls">
            <label>
              Reason
              <input v-model="actionForm.reason" placeholder="Operational context for audit" />
            </label>
            <label v-if="supportsScale">
              Desired replicas
              <input v-model.number="actionForm.replicas" min="1" type="number" />
            </label>
            <label v-if="supportsFailover">
              Failover candidate
              <input v-model="actionForm.candidateCluster" placeholder="standby cluster" />
            </label>
            <label v-if="supportsQuota">
              CPU request quota
              <input v-model="actionForm.cpu" placeholder="2" />
            </label>
            <label v-if="supportsQuota">
              Memory request quota
              <input v-model="actionForm.memory" placeholder="4Gi" />
            </label>
            <label v-if="supportsQuota">
              Pod quota
              <input v-model="actionForm.pods" placeholder="20" />
            </label>
            <label v-if="supportsGrantAccess">
              Access subject
              <input v-model="actionForm.subject" placeholder="user@company.tld" />
            </label>
            <label v-if="supportsGrantAccess">
              Platform proxy URL
              <input v-model="actionForm.defaultUrl" placeholder="https://servicer.company.tld" />
            </label>
            <label v-if="supportsDeleteStream || supportsPurgeStream || supportsDeleteConsumer">
              Stream name
              <input v-model="actionForm.stream" placeholder="ORDERS" />
            </label>
            <label v-if="supportsDeleteConsumer">
              Consumer name
              <input v-model="actionForm.consumer" placeholder="DISPATCH" />
            </label>
            <label v-if="data.productClass === 'nats' && actionNames.has('rotate-credentials')">
              Credential name
              <input v-model="actionForm.credentialName" placeholder="orders-api (leave empty for admin)" />
            </label>
          </div>
          <p v-if="supportsQuota" class="muted action-help">
            Namespace quota updates Kubernetes ResourceQuota hard limits for requested CPU, requested memory, and pods.
          </p>
          <p v-if="supportsGrantAccess" class="muted action-help">
            Grant access creates namespace-scoped read access and writes a platform-proxy kubeconfig Secret for the subject.
          </p>
          <p v-if="data.productClass === 'nats'" class="muted action-help">
            NATS stream actions use the named stream, consumer deletes use the stream and consumer pair, and credential rotation can target one app credential or the admin user when left blank.
          </p>
          <div v-if="data.availableActions.length" class="action-button-grid">
            <button
              v-for="action in data.availableActions"
              :key="action.name"
              class="button secondary"
              type="button"
              :disabled="actionSubmitting !== null"
              @click="submitAction(action.name)"
            >
              {{ actionSubmitting === action.name ? 'Submitting...' : action.displayName }}
              <small>{{ action.requiresApproval ? 'Approval required' : 'Self-service' }}</small>
            </button>
          </div>
          <p v-else class="muted">No actions available.</p>
          <p v-if="formMessage" class="success-text">{{ formMessage }}</p>
          <p v-if="formError" class="error-text">{{ formError }}</p>
        </section>
      </div>

      <aside class="instance-side-column">
        <section class="content-band band-sidebar">
          <h2>Conditions</h2>
          <table class="data-table compact">
            <thead>
              <tr>
                <th>Type</th>
                <th>Status</th>
                <th>Reason</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="condition in data.conditions" :key="condition.type">
                <td>{{ condition.type }}</td>
                <td><StatusPill :value="condition.status" :tone-override="conditionTone(condition.type, condition.status)" /></td>
                <td>{{ condition.reason }}</td>
              </tr>
            </tbody>
          </table>
        </section>

        <section class="content-band band-sidebar">
          <h2>Recent events</h2>
          <table class="data-table compact">
            <thead>
              <tr>
                <th>Time</th>
                <th>Message</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="event in paginatedEvents" :key="`${event.type}-${event.subject}-${event.time}`">
                <td>{{ event.time || 'Unknown' }}</td>
                <td>{{ event.message || event.reason || event.phase || 'Observed' }}</td>
              </tr>
              <tr v-if="eventRows.length === 0">
                <td colspan="2" class="muted">No audit or runtime events observed.</td>
              </tr>
              <tr v-if="eventsPageCount > 1">
                <td colspan="2">
                  <div class="pagination-row" style="justify-content: center; display: flex; align-items: center; gap: 8px; padding: 4px 0">
                    <button class="button secondary compact-button" :disabled="eventsPage === 0" @click="eventsPage--">‹</button>
                    <span class="pagination-label">{{ eventsPage + 1 }} / {{ eventsPageCount }}</span>
                    <button class="button secondary compact-button" :disabled="eventsPage >= eventsPageCount - 1" @click="eventsPage++">›</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </section>
      </aside>
    </section>
  </template>

  <Teleport to="body">
    <div v-if="credentialsOpen && credentialDetail" class="modal-backdrop">
      <div class="modal-panel">
        <div class="modal-head">
          <div>
            <p class="eyebrow">Credential secret</p>
            <h2>{{ credentialDetail.name }}</h2>
            <p class="muted">{{ credentialDetail.namespace }}</p>
          </div>
          <button class="button secondary icon-button" type="button" @click="credentialsOpen = false">x</button>
        </div>
        <div class="credential-values">
          <div v-for="(value, key) in credentialDetail.data" :key="key" class="credential-value">
            <span>{{ key }}</span>
            <textarea :value="value" readonly :rows="credentialRows(value)" />
          </div>
        </div>
      </div>
    </div>
  </Teleport>

  <Teleport to="body">
    <div v-if="editOpen && data" class="modal-backdrop">
      <form class="modal-panel" @submit.prevent="submitUpdate">
        <div class="modal-head">
          <div>
            <p class="eyebrow">Edit instance</p>
            <h2>{{ data.name }}</h2>
          </div>
          <button class="button secondary icon-button" type="button" @click="editOpen = false">x</button>
        </div>

        <div class="form-grid modal-form-grid">
          <label>
            Plan
            <select v-model="updateForm.servicePlan" required>
              <option v-for="plan in productPlans" :key="plan.name" :value="plan.name">{{ plan.displayName }}</option>
            </select>
          </label>
          <label>
            Version
            <input v-model="updateForm.version" placeholder="default" />
          </label>
        </div>

        <section class="modal-section">
          <h3 style="cursor: pointer; user-select: none; display: flex; align-items: center; justify-content: space-between" @click="showCapabilities = !showCapabilities">
            Capabilities
            <span class="collapsible-chevron" style="font-weight: normal">{{ showCapabilities ? '▾' : '▸' }}</span>
          </h3>
          <div v-show="showCapabilities">
          <div v-if="data.productClass === 'namespace'" class="form-grid modal-form-grid">
            <label>CPU quota<input v-model="parameterForm.cpu" /></label>
            <label>Memory quota<input v-model="parameterForm.memory" /></label>
            <label>Pod quota<input v-model="parameterForm.pods" /></label>
            <label class="checkbox-label"><input v-model="parameterForm.defaultDenyIngress" type="checkbox" />Default deny ingress</label>
          </div>
          <div v-else-if="data.productClass === 'postgresql'" class="form-grid modal-form-grid">
            <label>Nodes<input v-model.number="parameterForm.replicas" min="1" type="number" /></label>
            <label>Database name<input v-model="parameterForm.databaseName" placeholder="defaults to instance name" /></label>
            <label>Storage size<input v-model="parameterForm.storageSize" /></label>
            <label>StorageClass<input v-model="parameterForm.storageClass" placeholder="default" /></label>
            <label>Service type<select v-model="parameterForm.serviceType"><option value="ClusterIP">ClusterIP</option><option value="NodePort">NodePort</option><option value="LoadBalancer">LoadBalancer</option></select></label>
            <label v-if="parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort'">External DNS hostname<input v-model="parameterForm.externalDnsHostname" placeholder="service.apps.company.tld" /></label>
          </div>
          <div v-else-if="data.productClass === 'mysql'" class="form-grid modal-form-grid">
            <label>Nodes<input v-model.number="parameterForm.replicas" min="1" type="number" /></label>
            <label>Database name<input v-model="parameterForm.databaseName" placeholder="defaults to instance name" /></label>
            <label>CPU<input v-model="parameterForm.cpu" placeholder="1" /></label>
            <label>Memory<input v-model="parameterForm.memory" placeholder="2Gi" /></label>
            <label>Storage size<input v-model="parameterForm.storageSize" /></label>
            <label>StorageClass<input v-model="parameterForm.storageClass" placeholder="default" /></label>
            <label>Backup profile<input v-model="parameterForm.backupProfile" /></label>
            <label>Primary cluster<input v-model="parameterForm.primaryCluster" placeholder="project default" /></label>
            <label>Standby clusters<input v-model="parameterForm.standbyClusters" placeholder="cluster-a, cluster-b" /></label>
            <label>Service type<select v-model="parameterForm.serviceType"><option value="ClusterIP">ClusterIP</option><option value="NodePort">NodePort</option><option value="LoadBalancer">LoadBalancer</option></select></label>
            <label v-if="parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort'">External DNS hostname<input v-model="parameterForm.externalDnsHostname" placeholder="mysql.apps.company.tld" /></label>
          </div>
          <div v-else-if="data.productClass === 'nats'" class="form-grid modal-form-grid">
            <label>Nodes<input v-model.number="parameterForm.replicas" min="1" type="number" /></label>
            <label>Memory<input v-model="parameterForm.memoryLimit" /></label>
            <label>StorageClass<input v-model="parameterForm.storageClass" placeholder="default" /></label>
            <label>Storage size<input v-model="parameterForm.storageSize" /></label>
            <label>Max payload<input v-model="parameterForm.maxPayload" /></label>
            <label>Service type<select v-model="parameterForm.serviceType"><option value="ClusterIP">ClusterIP</option><option value="NodePort">NodePort</option><option value="LoadBalancer">LoadBalancer</option></select></label>
            <label v-if="parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort'">External DNS hostname<input v-model="parameterForm.externalDnsHostname" placeholder="service.apps.company.tld" /></label>
            <div v-if="updateForm.servicePlan === 'nats-jetstream' || updateForm.servicePlan === 'nats-geo'" class="nested-resource-editor" style="grid-column: span 2">
              <div class="resource-editor-head">
                <div>
                  <h4>Streams</h4>
                  <p class="muted">Define JetStream streams and the subjects they retain.</p>
                </div>
                <button class="button secondary compact-button" type="button" @click="addNatsStream">Add stream</button>
              </div>
              <div v-if="parameterForm.natsStreams.length" class="resource-editor-list">
                <article v-for="stream in parameterForm.natsStreams" :key="stream.id" class="resource-editor-card">
                  <div class="resource-editor-head">
                    <strong>{{ stream.name || 'New stream' }}</strong>
                    <button class="button secondary compact-button" type="button" @click="removeNatsStream(stream.id)">Remove</button>
                  </div>
                  <div class="resource-editor-grid">
                    <label>Name<input v-model="stream.name" placeholder="ORDERS" /></label>
                    <label>Subjects<input v-model="stream.subjects" placeholder="orders.>, billing.events" /></label>
                    <label>Storage<select v-model="stream.storage"><option value="file">file</option><option value="memory">memory</option></select></label>
                    <label>Retention<select v-model="stream.retention"><option value="limits">limits</option><option value="interest">interest</option><option value="workqueue">workqueue</option></select></label>
                    <label>Max age<input v-model="stream.maxAge" placeholder="168h" /></label>
                  </div>
                </article>
              </div>
              <p v-else class="muted">No streams defined yet.</p>
            </div>
            <div v-if="updateForm.servicePlan === 'nats-jetstream' || updateForm.servicePlan === 'nats-geo'" class="nested-resource-editor" style="grid-column: span 2">
              <div class="resource-editor-head">
                <div>
                  <h4>Consumers</h4>
                  <p class="muted">Attach durable consumers to declared streams.</p>
                </div>
                <button class="button secondary compact-button" type="button" @click="addNatsConsumer">Add consumer</button>
              </div>
              <div v-if="parameterForm.natsConsumers.length" class="resource-editor-list">
                <article v-for="consumer in parameterForm.natsConsumers" :key="consumer.id" class="resource-editor-card">
                  <div class="resource-editor-head">
                    <strong>{{ consumer.name || 'New consumer' }}</strong>
                    <button class="button secondary compact-button" type="button" @click="removeNatsConsumer(consumer.id)">Remove</button>
                  </div>
                  <div class="resource-editor-grid">
                    <label>Name<input v-model="consumer.name" placeholder="DISPATCH" /></label>
                    <label>
                      Stream
                      <select v-model="consumer.stream">
                        <option disabled value="">Select stream</option>
                        <option v-for="streamName in natsStreamOptions" :key="streamName" :value="streamName">
                          {{ streamName }}
                        </option>
                      </select>
                    </label>
                    <label>Filter subjects<input v-model="consumer.filterSubjects" placeholder="orders.created, orders.updated" /></label>
                    <label>Ack policy<select v-model="consumer.ackPolicy"><option value="explicit">explicit</option><option value="all">all</option><option value="none">none</option></select></label>
                  </div>
                </article>
              </div>
              <p v-if="!natsStreamOptions.length" class="muted">Add a stream first so consumers can target it from the dropdown.</p>
              <p v-if="!parameterForm.natsConsumers.length" class="muted">No consumers defined yet.</p>
            </div>
            <div class="nested-resource-editor" style="grid-column: span 2">
              <div class="resource-editor-head">
                <div>
                  <h4>App credentials</h4>
                  <p class="muted">Create named users with publish and subscribe permissions.</p>
                </div>
                <button class="button secondary compact-button" type="button" @click="addNatsCredential">Add app credential</button>
              </div>
              <div v-if="parameterForm.natsAppCredentials.length" class="resource-editor-list">
                <article v-for="credential in parameterForm.natsAppCredentials" :key="credential.id" class="resource-editor-card">
                  <div class="resource-editor-head">
                    <strong>{{ credential.name || 'New credential' }}</strong>
                    <button class="button secondary compact-button" type="button" @click="removeNatsCredential(credential.id)">Remove</button>
                  </div>
                  <div class="resource-editor-grid">
                    <label>Name<input v-model="credential.name" placeholder="orders-api" /></label>
                    <label>Username<input v-model="credential.username" placeholder="defaults to name" /></label>
                    <label>Publish subjects<input v-model="credential.publish" placeholder="orders.created, orders.updated" /></label>
                    <label>Subscribe subjects<input v-model="credential.subscribe" placeholder="orders.>, _INBOX.>" /></label>
                    <label class="checkbox-label resource-inline-toggle"><input v-model="credential.allowResponses" type="checkbox" />Allow responses</label>
                  </div>
                </article>
              </div>
              <p v-else class="muted">No app credentials defined yet.</p>
            </div>
          </div>
          <div v-else-if="data.productClass === 'valkey'" class="form-grid modal-form-grid">
            <label>Nodes<input v-model.number="parameterForm.replicas" min="1" type="number" /></label>
            <label>Memory profile<select v-model="parameterForm.memoryProfile"><option value="small">small</option><option value="medium">medium</option><option value="large">large</option></select></label>
            <label>Memory<input v-model="parameterForm.memoryLimit" /></label>
            <label>Persistence<select v-model="parameterForm.persistence"><option value="none">none</option><option value="persistent">persistent</option></select></label>
            <label>StorageClass<input v-model="parameterForm.storageClass" placeholder="default" /></label>
            <label>Storage size<input v-model="parameterForm.storageSize" /></label>
            <label>Primary cluster<input v-model="parameterForm.primaryCluster" placeholder="project default" /></label>
            <label>Standby clusters<input v-model="parameterForm.standbyClusters" /></label>
            <label>Max failover lag<input v-model.number="parameterForm.maxReplicationLagSeconds" min="1" type="number" /></label>
            <label>Service type<select v-model="parameterForm.serviceType"><option value="ClusterIP">ClusterIP</option><option value="NodePort">NodePort</option><option value="LoadBalancer">LoadBalancer</option></select></label>
            <label v-if="parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort'">External DNS hostname<input v-model="parameterForm.externalDnsHostname" placeholder="service.apps.company.tld" /></label>
          </div>
          <div v-else-if="data.productClass === 'yugabyte'" class="form-grid modal-form-grid">
            <label>TServer / Master nodes<input v-model.number="parameterForm.replicas" min="1" type="number" /></label>
            <label>Database name<input v-model="parameterForm.databaseName" placeholder="defaults to instance name" /></label>
            <label>CPU per role<input v-model="parameterForm.cpu" placeholder="500m" /></label>
            <label>Memory per role<input v-model="parameterForm.memory" placeholder="1Gi" /></label>
            <label>Storage size<input v-model="parameterForm.storageSize" /></label>
            <label>StorageClass<input v-model="parameterForm.storageClass" placeholder="default" /></label>
            <label>Backup profile<input v-model="parameterForm.backupProfile" /></label>
            <label>Primary cluster<input v-model="parameterForm.primaryCluster" placeholder="project default" /></label>
            <label>Standby clusters<input v-model="parameterForm.standbyClusters" placeholder="cluster-a, cluster-b" /></label>
            <label>Service type<select v-model="parameterForm.serviceType"><option value="ClusterIP">ClusterIP</option><option value="NodePort">NodePort</option><option value="LoadBalancer">LoadBalancer</option></select></label>
            <label v-if="parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort'">External DNS hostname<input v-model="parameterForm.externalDnsHostname" placeholder="service.apps.company.tld" /></label>
          </div>
          </div>
        </section>

        <div class="form-actions">
          <button class="button primary" type="submit" :disabled="updating">
            {{ updating ? 'Updating...' : 'Save changes' }}
          </button>
          <button class="button secondary" type="button" :disabled="updating" @click="editOpen = false">Cancel</button>
          <span v-if="formError" class="error-text">{{ formError }}</span>
        </div>
      </form>
    </div>
  </Teleport>

  <Teleport to="body">
    <div v-if="deleteOpen && data" class="modal-backdrop">
      <form class="modal-panel delete-modal" @submit.prevent="deleteInstance">
        <div class="modal-head">
          <div>
            <p class="eyebrow">Delete instance</p>
            <h2>{{ data.name }}</h2>
          </div>
          <button class="button secondary icon-button" type="button" @click="deleteOpen = false">x</button>
        </div>
        <p class="muted">
          Delete request removes Servicer instance object. Type <strong>{{ data.name }}</strong> to confirm.
        </p>
        <label>
          Instance name
          <input v-model="deleteConfirm" :placeholder="data.name" />
        </label>
        <div class="form-actions">
          <button class="button danger" type="submit" :disabled="deleting || deleteConfirm !== data.name">
            {{ deleting ? 'Deleting...' : 'Delete instance' }}
          </button>
          <button class="button secondary" type="button" :disabled="deleting" @click="deleteOpen = false">Cancel</button>
          <span v-if="formError" class="error-text">{{ formError }}</span>
        </div>
      </form>
    </div>
  </Teleport>

  <Teleport to="body">
    <div v-if="yamlOpen" class="modal-backdrop">
      <div class="modal-panel" style="width: min(920px, 100%)">
        <div class="modal-head">
          <h2>Edit YAML</h2>
          <button class="button secondary icon-button" type="button" @click="yamlOpen = false">x</button>
        </div>
        <YamlEditor v-model="yamlContent" />
        <div class="form-actions" style="margin-top: 12px">
          <button class="button primary" :disabled="yamlSaving" @click="saveYaml">
            {{ yamlSaving ? 'Saving...' : 'Save' }}
          </button>
          <button class="button secondary" :disabled="yamlSaving" @click="yamlOpen = false">Cancel</button>
          <span v-if="yamlError" class="error-text">{{ yamlError }}</span>
        </div>
      </div>
    </div>
  </Teleport>

  <Teleport to="body">
    <div v-if="backupConfigOpen && data" class="modal-backdrop">
      <form class="modal-panel" style="width: min(700px, 100%)" @submit.prevent="saveBackupConfig">
        <div class="modal-head">
          <h2>Configure Backups</h2>
          <button class="button secondary icon-button" type="button" @click="backupConfigOpen = false">x</button>
        </div>
        <p class="muted" style="margin-bottom: 12px">
          Backups use CNPG's barman object store integration with S3-compatible storage.
          A Kubernetes Secret with <code>ACCESS_KEY_ID</code> and <code>ACCESS_SECRET_KEY</code> keys
          must already exist in the instance namespace. Leave bucket or secret empty to disable backup.
        </p>
        <div class="form-grid modal-form-grid">
          <label>
            Credentials secret
            <input v-model="parameterForm.backupCredentialsSecret" placeholder="my-s3-creds" />
          </label>
          <label>
            S3 bucket
            <input v-model="parameterForm.backupBucket" placeholder="my-backup-bucket" />
          </label>
          <label>
            S3 endpoint URL
            <input v-model="parameterForm.backupEndpoint" placeholder="https://s3.amazonaws.com" />
          </label>
          <label>
            Path prefix
            <input v-model="parameterForm.backupPath" placeholder="/postgresql/instance-name" />
          </label>
          <label>
            Region
            <input v-model="parameterForm.backupRegion" placeholder="us-east-1" />
          </label>
          <label>
            Schedule (cron)
            <input v-model="parameterForm.backupSchedule" placeholder="0 2 * * *" />
          </label>
          <label>
            Retention
            <select v-model="parameterForm.backupRetention">
              <option value="7d">7 days</option>
              <option value="14d">14 days</option>
              <option value="30d">30 days</option>
              <option value="90d">90 days</option>
            </select>
          </label>
        </div>
        <div class="form-actions">
          <button class="button primary" type="submit" :disabled="updating">{{ updating ? 'Saving...' : 'Save' }}</button>
          <button class="button secondary" type="button" @click="backupConfigOpen = false">Cancel</button>
          <span v-if="formError" class="error-text">{{ formError }}</span>
        </div>
      </form>
    </div>
  </Teleport>
</template>
