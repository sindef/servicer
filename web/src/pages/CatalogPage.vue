<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { api, type CatalogEntry, type RepositorySummary } from '../api'
import { useApi } from '../composables/useApi'
import StatusPill from '../components/StatusPill.vue'

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

type VmNetworkForm = {
  id: number
  name: string
  networkType: 'pod' | 'multus'
  bindingMethod: 'masquerade' | 'bridge' | 'sriov'
  multusNetworkName: string
  model: string
}

type VmDiskForm = {
  id: number
  name: string
  image: string
  size: string
  storageClass: string
  bus: string
}

const { data, loading, error } = useApi(api.catalog)
const projects = useApi(api.projects)
const clusters = useApi(api.admin.clusters)
const nextNatsRowId = ref(1)

const projectRepositories = ref<RepositorySummary[]>([])
const repositoriesLoading = ref(false)

const requestForm = reactive({
  name: '',
  projectName: '',
  serviceClass: '',
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
  vmImage: '',
  vmWorkloadType: 'vm',
  vmPoolReplicas: 1,
  vmRunStrategy: 'Always',
  vmNetworks: [] as VmNetworkForm[],
  vmDisks: [] as VmDiskForm[],
  primaryCluster: '',
  standbyClusters: [] as string[],
  maxReplicationLagSeconds: 30,
  serviceType: 'ClusterIP',
  externalDnsHostname: '',
  natsStreams: [] as NatsStreamForm[],
  natsConsumers: [] as NatsConsumerForm[],
  natsAppCredentials: [] as NatsCredentialForm[],
  // argo-application
  argoRepoRef: '',
  argoRepoURL: '',
  argoPath: '',
  argoTargetRevision: 'HEAD',
  argoTargetNamespace: '',
  argoSyncPolicy: 'manual',
  argoCreateNamespace: false,
  argoHelmReleaseName: '',
  argoHelmValuesYAML: ''
})
const submitting = ref(false)
const submitError = ref<string | null>(null)
const submitMessage = ref<string | null>(null)
const expandedPlans = ref<Record<string, boolean>>({})
const requestOpen = ref(false)
const showCapabilities = ref(false)
const showBackup = ref(false)

const selectedEntry = computed(() => data.value?.find((entry) => entry.name === requestForm.serviceClass))
const selectedPlan = computed(() => selectedEntry.value?.plans.find((plan) => plan.name === requestForm.servicePlan))
const projectRows = computed(() => projects.data.value || [])
const selectedProject = computed(() => projectRows.value.find((project) => project.name === requestForm.projectName))
const clusterRows = computed(() => clusters.data.value ?? [])
const availableStandbyClusters = computed(() =>
  clusterRows.value.filter((c) => c.name !== parameterForm.primaryCluster)
)
const natsStreamOptions = computed(() =>
  Array.from(
    new Set(
      parameterForm.natsStreams
        .map((stream) => stream.name.trim())
        .filter((name) => name.length > 0)
    )
  )
)

watch(
  () => parameterForm.primaryCluster,
  (newPrimary) => {
    parameterForm.standbyClusters = parameterForm.standbyClusters.filter((c) => c !== newPrimary)
  }
)

watch(
  () => requestForm.projectName,
  async (project) => {
    if (requestForm.serviceClass !== 'argo-application' || !project) return
    repositoriesLoading.value = true
    try {
      projectRepositories.value = await api.repositories.list(project)
    } catch {
      projectRepositories.value = []
    } finally {
      repositoriesLoading.value = false
    }
  }
)

watch(
  () => requestForm.serviceClass,
  async (serviceClass) => {
    if (serviceClass !== 'argo-application' || !requestForm.projectName) return
    repositoriesLoading.value = true
    try {
      projectRepositories.value = await api.repositories.list(requestForm.projectName)
    } catch {
      projectRepositories.value = []
    } finally {
      repositoriesLoading.value = false
    }
  }
)

function logoText(serviceClass: string) {
  switch (serviceClass) {
    case 'postgresql':
      return 'Pg'
    case 'mysql':
      return 'My'
    case 'namespace':
      return 'Ns'
    case 'valkey':
      return 'Vk'
    case 'nats':
      return 'N'
    case 'yugabyte':
      return 'Yb'
    case 'argo-application':
      return 'Ar'
    case 'virtual-machine':
      return 'Vm'
    default:
      return serviceClass.slice(0, 2).toUpperCase()
  }
}

function visiblePlans(entry: CatalogEntry) {
  return expandedPlans.value[entry.name] ? entry.plans : entry.plans.slice(0, 2)
}

function extraPlanCount(entry: CatalogEntry) {
  return Math.max(0, entry.plans.length - 2)
}

function togglePlans(serviceClass: string) {
  expandedPlans.value = {
    ...expandedPlans.value,
    [serviceClass]: !expandedPlans.value[serviceClass]
  }
}

function chooseProduct(serviceClass: string, servicePlan: string, version = '') {
  requestForm.serviceClass = serviceClass
  requestForm.servicePlan = servicePlan
  requestForm.version = version
  applyPlanDefaults(serviceClass, servicePlan)
  showCapabilities.value = false
  showBackup.value = false
  requestOpen.value = true
  submitError.value = null
  submitMessage.value = null
}

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

function createVmNetwork(values: Partial<Omit<VmNetworkForm, 'id'>> = {}): VmNetworkForm {
  return {
    id: allocateNatsRowId(),
    name: values.name || 'default',
    networkType: values.networkType || 'pod',
    bindingMethod: values.bindingMethod || 'masquerade',
    multusNetworkName: values.multusNetworkName || '',
    model: values.model || 'virtio'
  }
}

function createVmDisk(values: Partial<Omit<VmDiskForm, 'id'>> = {}): VmDiskForm {
  return {
    id: allocateNatsRowId(),
    name: values.name || 'rootdisk',
    image: values.image || '',
    size: values.size || '20Gi',
    storageClass: values.storageClass || '',
    bus: values.bus || 'virtio'
  }
}

function resetNatsEditors() {
  parameterForm.natsStreams = []
  parameterForm.natsConsumers = []
  parameterForm.natsAppCredentials = []
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

function addVmNetwork() {
  parameterForm.vmNetworks.push(createVmNetwork({ name: `net-${parameterForm.vmNetworks.length + 1}` }))
}

function removeVmNetwork(id: number) {
  parameterForm.vmNetworks = parameterForm.vmNetworks.filter((network) => network.id !== id)
}

function addVmDisk() {
  parameterForm.vmDisks.push(createVmDisk({ name: `disk-${parameterForm.vmDisks.length + 1}`, image: parameterForm.vmImage }))
}

function removeVmDisk(id: number) {
  parameterForm.vmDisks = parameterForm.vmDisks.filter((disk) => disk.id !== id)
}

function onArgoRepoRefChange(repoName: string) {
  const repo = projectRepositories.value.find((r) => r.name === repoName)
  parameterForm.argoRepoURL = repo ? repo.url : ''
}

function csvList(value: string) {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function natsStreamParameters() {
  return parameterForm.natsStreams
    .map((stream) =>
      compactParams({
        name: stream.name.trim(),
        subjects: csvList(stream.subjects),
        storage: stream.storage,
        retention: stream.retention,
        maxAge: stream.maxAge
      })
    )
    .filter((stream) => typeof stream.name === 'string' && stream.name.length > 0)
}

function natsConsumerParameters() {
  return parameterForm.natsConsumers
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
    )
}

function natsCredentialParameters() {
  return parameterForm.natsAppCredentials
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
    .filter((credential) => typeof credential.name === 'string' && credential.name.length > 0)
}

function applyPlanDefaults(serviceClass: string, servicePlan: string) {
  if (serviceClass === 'namespace') {
    parameterForm.cpu = '2'
    parameterForm.memory = '4Gi'
    parameterForm.pods = '20'
    parameterForm.defaultDenyIngress = true
  }
  if (serviceClass === 'postgresql') {
    parameterForm.replicas = 3
    parameterForm.databaseName = ''
    parameterForm.storageSize = '100Gi'
    parameterForm.backupCredentialsSecret = ''
    parameterForm.backupEndpoint = ''
    parameterForm.backupBucket = ''
    parameterForm.backupPath = ''
    parameterForm.backupRegion = ''
    parameterForm.backupSchedule = ''
    parameterForm.backupRetention = '30d'
    parameterForm.serviceType = 'ClusterIP'
    parameterForm.externalDnsHostname = ''
  }
  if (serviceClass === 'mysql') {
    parameterForm.replicas = servicePlan === 'mysql-active-passive' ? 1 : 3
    parameterForm.databaseName = ''
    parameterForm.cpu = '1'
    parameterForm.memory = '2Gi'
    parameterForm.storageSize = '100Gi'
    parameterForm.backupProfile = 'daily-7d'
    parameterForm.primaryCluster = ''
    parameterForm.standbyClusters = []
    parameterForm.serviceType = 'ClusterIP'
    parameterForm.externalDnsHostname = ''
  }
  if (serviceClass === 'nats') {
    const isMulti = servicePlan === 'nats-jetstream' || servicePlan === 'nats-geo'
    parameterForm.replicas = isMulti ? 3 : 1
    parameterForm.memoryLimit = '512Mi'
    parameterForm.storageSize = isMulti ? '20Gi' : ''
    parameterForm.maxPayload = '1MiB'
    parameterForm.standbyClusters = []
    parameterForm.serviceType = 'ClusterIP'
    parameterForm.externalDnsHostname = ''
    resetNatsEditors()
  }
  if (serviceClass === 'valkey') {
    parameterForm.replicas = servicePlan === 'valkey-dev' ? 1 : 3
    parameterForm.memoryProfile = servicePlan === 'valkey-dev' ? 'small' : 'medium'
    parameterForm.persistence = servicePlan === 'valkey-dev' ? 'none' : 'persistent'
    parameterForm.storageSize = servicePlan === 'valkey-dev' ? '' : '10Gi'
    parameterForm.memoryLimit = '512Mi'
    parameterForm.maxMemoryPolicy = 'allkeys-lru'
    parameterForm.maxReplicationLagSeconds = 30
    parameterForm.serviceType = 'ClusterIP'
    parameterForm.externalDnsHostname = ''
  }
  if (serviceClass === 'yugabyte') {
    parameterForm.replicas = servicePlan === 'yugabyte-dev' ? 1 : 3
    parameterForm.databaseName = ''
    parameterForm.cpu = servicePlan === 'yugabyte-dev' ? '500m' : '1'
    parameterForm.memory = servicePlan === 'yugabyte-dev' ? '1Gi' : '2Gi'
    parameterForm.storageSize = servicePlan === 'yugabyte-dev' ? '10Gi' : '100Gi'
    parameterForm.backupProfile = servicePlan === 'yugabyte-dev' ? '' : 'daily-7d'
    parameterForm.primaryCluster = ''
    parameterForm.standbyClusters = []
    parameterForm.serviceType = 'ClusterIP'
    parameterForm.externalDnsHostname = ''
  }
  if (serviceClass === 'argo-application') {
    parameterForm.argoRepoRef = ''
    parameterForm.argoRepoURL = ''
    parameterForm.argoPath = ''
    parameterForm.argoTargetRevision = 'HEAD'
    parameterForm.argoTargetNamespace = ''
    parameterForm.argoSyncPolicy = 'manual'
    parameterForm.argoCreateNamespace = false
    parameterForm.argoHelmReleaseName = ''
    parameterForm.argoHelmValuesYAML = ''
  }
  if (serviceClass === 'virtual-machine') {
    parameterForm.cpu = '2'
    parameterForm.memory = '4Gi'
    parameterForm.storageClass = ''
    parameterForm.storageSize = '20Gi'
    parameterForm.vmImage = 'quay.io/containerdisks/ubuntu:22.04'
    parameterForm.vmWorkloadType = 'vm'
    parameterForm.vmPoolReplicas = 1
    parameterForm.vmRunStrategy = 'Always'
    parameterForm.vmNetworks = [createVmNetwork({ name: 'default' })]
    parameterForm.vmDisks = [createVmDisk({ name: 'rootdisk', image: parameterForm.vmImage, size: '20Gi' })]
  }
}

function buildParameters() {
  switch (requestForm.serviceClass) {
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
        replicationMode: requestForm.servicePlan === 'mysql-galera' ? 'galera' : requestForm.servicePlan === 'mysql-active-passive' ? 'active-passive' : 'single-primary',
        primaryCluster: selectedPlan.value?.topology === 'multi-region' ? parameterForm.primaryCluster : undefined,
        standbyClusters: selectedPlan.value?.topology === 'multi-region' ? parameterForm.standbyClusters : undefined,
        serviceType: parameterForm.serviceType,
        externalDnsHostname: (parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort') ? parameterForm.externalDnsHostname : undefined
      })
    case 'nats':
      return compactParams({
        replicas: parameterForm.replicas,
        jetstream: requestForm.servicePlan === 'nats-jetstream' || requestForm.servicePlan === 'nats-geo',
        streams: natsStreamParameters(),
        consumers: natsConsumerParameters(),
        appCredentials: natsCredentialParameters(),
        storageClass: parameterForm.storageClass,
        storageSize: parameterForm.storageSize,
        maxPayload: parameterForm.maxPayload,
        memoryLimit: parameterForm.memoryLimit,
        standbyClusters: parameterForm.standbyClusters,
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
        standbyClusters: parameterForm.standbyClusters,
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
        standbyClusters: parameterForm.standbyClusters,
        serviceType: parameterForm.serviceType,
        externalDnsHostname: (parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort') ? parameterForm.externalDnsHostname : undefined
      })
    case 'argo-application':
      return compactParams({
        repoURL: parameterForm.argoRepoURL || parameterForm.argoRepoRef,
        path: parameterForm.argoPath,
        targetRevision: parameterForm.argoTargetRevision,
        targetNamespace: parameterForm.argoTargetNamespace,
        syncPolicy: parameterForm.argoSyncPolicy,
        createNamespace: parameterForm.argoCreateNamespace || undefined,
        helmReleaseName: parameterForm.argoHelmReleaseName,
        helmValuesYAML: parameterForm.argoHelmValuesYAML,
        repoRef: parameterForm.argoRepoRef
      })
    case 'virtual-machine':
      return compactParams({
        image: parameterForm.vmImage,
        workloadType: parameterForm.vmWorkloadType,
        poolReplicas: parameterForm.vmWorkloadType === 'vmp' ? parameterForm.vmPoolReplicas : undefined,
        cpu: parameterForm.cpu,
        memory: parameterForm.memory,
        runStrategy: parameterForm.vmRunStrategy,
        storageClass: parameterForm.storageClass,
        storageSize: parameterForm.storageSize,
        networks: parameterForm.vmNetworks
          .map((network) =>
            compactParams({
              name: network.name.trim(),
              type: network.networkType,
              bindingMethod: network.bindingMethod,
              multusNetworkName: network.networkType === 'multus' ? network.multusNetworkName.trim() : undefined,
              model: network.model.trim()
            })
          )
          .filter((network) => typeof network.name === 'string' && network.name.length > 0),
        disks: parameterForm.vmDisks
          .map((disk) =>
            compactParams({
              name: disk.name.trim(),
              image: disk.image.trim(),
              size: disk.size.trim(),
              storageClass: disk.storageClass.trim(),
              bus: disk.bus.trim()
            })
          )
          .filter(
            (disk) =>
              typeof disk.name === 'string' &&
              disk.name.length > 0 &&
              typeof disk.image === 'string' &&
              disk.image.length > 0 &&
              typeof disk.size === 'string' &&
              disk.size.length > 0
          )
      })
    default:
      return undefined
  }
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

function closeRequest() {
  if (submitting.value) {
    return
  }
  requestOpen.value = false
}

async function submitRequest() {
  submitting.value = true
  submitError.value = null
  submitMessage.value = null
  try {
    const parameters = buildParameters()
    const response = await api.createRequest({
      name: requestForm.name,
      projectName: requestForm.projectName,
      serviceClass: requestForm.serviceClass,
      servicePlan: requestForm.servicePlan,
      version: requestForm.version || undefined,
      parameters
    })
    submitMessage.value = response.message
    requestForm.name = ''
    requestOpen.value = false
  } catch (err) {
    submitError.value = err instanceof Error ? err.message : 'Product request failed'
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <section class="page-heading">
    <div>
      <p class="eyebrow">Catalog</p>
      <h1>Requestable products</h1>
    </div>
  </section>

  <p v-if="loading" class="muted">Loading catalog...</p>
  <p v-else-if="error" class="error-text">{{ error }}</p>

  <section v-else-if="data" class="catalog-list">
    <div class="catalog-grid">
      <article v-for="entry in data" :key="entry.name" class="catalog-card">
        <div class="catalog-card-head">
          <div class="product-logo" :class="`product-logo-${entry.name}`">
            <span>{{ logoText(entry.name) }}</span>
          </div>
          <StatusPill :value="entry.published ? 'Published' : 'Draft'" />
        </div>

        <div class="catalog-card-body">
          <h2>{{ entry.displayName }}</h2>
          <p>{{ entry.description }}</p>
          <div class="catalog-meta">
            <span>{{ entry.category || 'service' }}</span>
            <span>{{ entry.driver }}</span>
          </div>
        </div>

        <div class="catalog-card-foot">
          <div class="plan-strip">
            <button
              v-for="plan in visiblePlans(entry)"
              :key="plan.name"
              class="plan-chip"
              type="button"
              @click="chooseProduct(entry.name, plan.name, plan.defaultVersion)"
            >
              <span>
                <strong>{{ plan.displayName }}</strong>
                <small>{{ plan.topology || 'standard' }}</small>
              </span>
              <em>Request</em>
            </button>
            <button
              v-if="extraPlanCount(entry) > 0"
              class="plan-chip plan-chip-more"
              type="button"
              @click="togglePlans(entry.name)"
            >
              <span>
                <strong>{{ expandedPlans[entry.name] ? 'Show fewer' : `+${extraPlanCount(entry)} plans` }}</strong>
                <small>{{ expandedPlans[entry.name] ? 'collapse list' : 'view all options' }}</small>
              </span>
            </button>
          </div>
        </div>
      </article>
    </div>
  </section>

  <Teleport to="body">
    <div v-if="requestOpen" class="modal-backdrop">
      <form class="modal-panel" @submit.prevent="submitRequest">
        <div class="modal-head">
          <div>
            <p class="eyebrow">Product request</p>
            <h2>{{ selectedEntry?.displayName || 'New product' }}</h2>
          </div>
          <button class="button secondary icon-button" type="button" @click="closeRequest">x</button>
        </div>

        <div class="form-grid modal-form-grid">
          <label>
            Name
            <input
              v-model="requestForm.name"
              pattern="([a-z]|[a-z][a-z0-9-]*[a-z0-9])"
              required
              title="Use lowercase letters, numbers, and hyphens. Start with a letter."
              placeholder="orders-cache"
            />
          </label>
          <label>
            Project
            <select v-model="requestForm.projectName" required>
              <option disabled value="">Select project</option>
              <option v-for="project in projectRows" :key="project.name" :value="project.name">
                {{ project.displayName }}
              </option>
            </select>
          </label>
          <label>
            Product
            <select v-model="requestForm.serviceClass" required @change="requestForm.servicePlan = ''">
              <option disabled value="">Select product</option>
              <option v-for="entry in data || []" :key="entry.name" :value="entry.name">
                {{ entry.displayName }}
              </option>
            </select>
          </label>
          <label>
            Plan
            <select
              v-model="requestForm.servicePlan"
              required
              @change="applyPlanDefaults(requestForm.serviceClass, requestForm.servicePlan)"
            >
              <option disabled value="">Select plan</option>
              <option v-for="plan in selectedEntry?.plans || []" :key="plan.name" :value="plan.name">
                {{ plan.displayName }}
              </option>
            </select>
          </label>
          <label>
            Version
            <input v-model="requestForm.version" :placeholder="selectedPlan?.defaultVersion || 'default'" />
          </label>
        </div>

        <section v-if="requestForm.serviceClass" class="modal-section">
          <div style="cursor: pointer; user-select: none; display: flex; align-items: flex-start; justify-content: space-between; gap: 8px" @click="showCapabilities = !showCapabilities">
            <div>
              <h3>Capabilities</h3>
              <p class="muted">
                Placement follows selected project
                <strong>{{ selectedProject?.clusterName || 'pending placement' }}</strong>.
              </p>
            </div>
            <span class="collapsible-chevron">{{ showCapabilities ? '▾' : '▸' }}</span>
          </div>

          <div v-show="showCapabilities">
          <div v-if="requestForm.serviceClass === 'namespace'" class="form-grid modal-form-grid">
            <label>
              CPU quota
              <input v-model="parameterForm.cpu" placeholder="2" />
            </label>
            <label>
              Memory quota
              <input v-model="parameterForm.memory" placeholder="4Gi" />
            </label>
            <label>
              Pod quota
              <input v-model="parameterForm.pods" placeholder="20" />
            </label>
            <label class="checkbox-label">
              <input v-model="parameterForm.defaultDenyIngress" type="checkbox" />
              Default deny ingress
            </label>
          </div>

          <div v-else-if="requestForm.serviceClass === 'postgresql'" class="form-grid modal-form-grid">
            <label>
              Nodes
              <input v-model.number="parameterForm.replicas" min="1" type="number" />
            </label>
            <label>
              Database name
              <input v-model="parameterForm.databaseName" placeholder="defaults to instance name" />
            </label>
            <label>
              Storage size
              <input v-model="parameterForm.storageSize" placeholder="100Gi" />
            </label>
            <label>
              StorageClass
              <input v-model="parameterForm.storageClass" placeholder="default" />
            </label>
            <label>
              Service type
              <select v-model="parameterForm.serviceType">
                <option value="ClusterIP">ClusterIP</option>
                <option value="NodePort">NodePort</option>
                <option value="LoadBalancer">LoadBalancer</option>
              </select>
            </label>
            <label v-if="parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort'">
              External DNS hostname
              <input v-model="parameterForm.externalDnsHostname" placeholder="service.apps.company.tld" />
            </label>
            <div class="form-section-heading" style="grid-column: 1 / -1; flex-direction: row; align-items: center; justify-content: space-between; cursor: pointer; user-select: none" @click.stop="showBackup = !showBackup">
              <div>
                <span>Backup (optional)</span>
                <small class="muted" style="display: block; margin-top: 2px">Requires a pre-existing K8s Secret with <code>ACCESS_KEY_ID</code> and <code>ACCESS_SECRET_KEY</code> keys.</small>
              </div>
              <span class="collapsible-chevron">{{ showBackup ? '▾' : '▸' }}</span>
            </div>
            <template v-if="showBackup">
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
            </template>
          </div>

          <div v-else-if="requestForm.serviceClass === 'mysql'" class="form-grid modal-form-grid">
            <label>
              Nodes
              <input v-model.number="parameterForm.replicas" min="1" type="number" />
            </label>
            <label>
              Database name
              <input v-model="parameterForm.databaseName" placeholder="defaults to instance name" />
            </label>
            <label>
              CPU
              <input v-model="parameterForm.cpu" placeholder="1" />
            </label>
            <label>
              Memory
              <input v-model="parameterForm.memory" placeholder="2Gi" />
            </label>
            <label>
              Storage size
              <input v-model="parameterForm.storageSize" placeholder="100Gi" />
            </label>
            <label>
              StorageClass
              <input v-model="parameterForm.storageClass" placeholder="default" />
            </label>
            <label>
              Backup profile
              <input v-model="parameterForm.backupProfile" placeholder="daily-7d" />
            </label>
            <label>
              Service type
              <select v-model="parameterForm.serviceType">
                <option value="ClusterIP">ClusterIP</option>
                <option value="NodePort">NodePort</option>
                <option value="LoadBalancer">LoadBalancer</option>
              </select>
            </label>
            <label v-if="parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort'">
              External DNS hostname
              <input v-model="parameterForm.externalDnsHostname" placeholder="mysql.apps.company.tld" />
            </label>
            <div v-if="selectedPlan?.topology === 'multi-region'" style="grid-column: span 2">
              <label>
                Primary cluster
                <select v-model="parameterForm.primaryCluster">
                  <option value="">Use project cluster</option>
                  <option v-for="cluster in clusterRows" :key="cluster.name" :value="cluster.name">{{ cluster.displayName }}</option>
                </select>
              </label>
              <div class="inline-checkbox-list">
                <label v-for="cluster in availableStandbyClusters" :key="cluster.name" class="checkbox-label">
                  <input type="checkbox" :value="cluster.name" v-model="parameterForm.standbyClusters" />
                  {{ cluster.displayName }}
                </label>
              </div>
            </div>
          </div>

          <div v-else-if="requestForm.serviceClass === 'nats'" class="form-grid modal-form-grid">
            <label>
              Nodes
              <input v-model.number="parameterForm.replicas" min="1" type="number" />
            </label>
            <label>
              Memory
              <input v-model="parameterForm.memoryLimit" placeholder="512Mi" />
            </label>
            <label>
              StorageClass
              <input v-model="parameterForm.storageClass" placeholder="default" />
            </label>
            <label>
              Storage size
              <input v-model="parameterForm.storageSize" placeholder="20Gi" />
            </label>
            <label>
              Max payload
              <input v-model="parameterForm.maxPayload" placeholder="1MiB" />
            </label>
            <label>
              Service type
              <select v-model="parameterForm.serviceType">
                <option value="ClusterIP">ClusterIP</option>
                <option value="NodePort">NodePort</option>
                <option value="LoadBalancer">LoadBalancer</option>
              </select>
            </label>
            <label v-if="parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort'">
              External DNS hostname
              <input v-model="parameterForm.externalDnsHostname" placeholder="service.apps.company.tld" />
            </label>
            <div v-if="selectedPlan?.topology === 'multi-region'" style="grid-column: span 2">
              <p style="color: var(--muted-strong); font-size: 12px; font-weight: 800; text-transform: uppercase; margin: 0 0 8px">Standby clusters</p>
              <div class="tag-row">
                <label
                  v-for="cluster in clusterRows"
                  :key="cluster.name"
                  class="checkbox-label form-grid"
                  style="padding: 5px 10px; border: 1px solid var(--border); border-radius: 6px; cursor: pointer"
                >
                  <input type="checkbox" :value="cluster.name" v-model="parameterForm.standbyClusters" />
                  {{ cluster.displayName || cluster.name }}
                </label>
                <span v-if="clusterRows.length === 0" class="muted" style="font-size: 13px">No clusters available</span>
              </div>
            </div>
            <div v-if="requestForm.servicePlan === 'nats-jetstream' || requestForm.servicePlan === 'nats-geo'" class="nested-resource-editor" style="grid-column: span 2">
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

            <div v-if="requestForm.servicePlan === 'nats-jetstream' || requestForm.servicePlan === 'nats-geo'" class="nested-resource-editor" style="grid-column: span 2">
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
                  <p class="muted">Create named users with product-scoped publish and subscribe permissions.</p>
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

          <div v-else-if="requestForm.serviceClass === 'valkey'" class="form-grid modal-form-grid">
            <label>
              Nodes
              <input v-model.number="parameterForm.replicas" min="1" type="number" />
            </label>
            <label>
              Memory profile
              <select v-model="parameterForm.memoryProfile">
                <option value="small">small</option>
                <option value="medium">medium</option>
                <option value="large">large</option>
              </select>
            </label>
            <label>
              Memory
              <input v-model="parameterForm.memoryLimit" placeholder="512Mi" />
            </label>
            <label>
              Persistence
              <select v-model="parameterForm.persistence">
                <option value="none">none</option>
                <option value="persistent">persistent</option>
              </select>
            </label>
            <label>
              StorageClass
              <input v-model="parameterForm.storageClass" placeholder="default" />
            </label>
            <label>
              Storage size
              <input v-model="parameterForm.storageSize" placeholder="10Gi" />
            </label>
            <label>
              Service type
              <select v-model="parameterForm.serviceType">
                <option value="ClusterIP">ClusterIP</option>
                <option value="NodePort">NodePort</option>
                <option value="LoadBalancer">LoadBalancer</option>
              </select>
            </label>
            <label v-if="parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort'">
              External DNS hostname
              <input v-model="parameterForm.externalDnsHostname" placeholder="service.apps.company.tld" />
            </label>
            <label>
              Max failover lag
              <input v-model.number="parameterForm.maxReplicationLagSeconds" min="1" type="number" />
            </label>
            <label>
              Primary cluster
              <select v-model="parameterForm.primaryCluster">
                <option value="">Project default</option>
                <option v-for="cluster in clusterRows" :key="cluster.name" :value="cluster.name">
                  {{ cluster.displayName || cluster.name }}
                </option>
              </select>
            </label>
            <div style="grid-column: span 2">
              <p style="color: var(--muted-strong); font-size: 12px; font-weight: 800; text-transform: uppercase; margin: 0 0 8px">Standby clusters</p>
              <div class="tag-row">
                <label
                  v-for="cluster in availableStandbyClusters"
                  :key="cluster.name"
                  class="checkbox-label form-grid"
                  style="padding: 5px 10px; border: 1px solid var(--border); border-radius: 6px; cursor: pointer"
                >
                  <input type="checkbox" :value="cluster.name" v-model="parameterForm.standbyClusters" />
                  {{ cluster.displayName || cluster.name }}
                </label>
                <span v-if="availableStandbyClusters.length === 0" class="muted" style="font-size: 13px">No other clusters available</span>
              </div>
            </div>
          </div>

          <div v-else-if="requestForm.serviceClass === 'yugabyte'" class="form-grid modal-form-grid">
            <label>
              TServer / Master nodes
              <input v-model.number="parameterForm.replicas" min="1" type="number" />
            </label>
            <label>
              Database name
              <input v-model="parameterForm.databaseName" placeholder="defaults to instance name" />
            </label>
            <label>
              CPU per role
              <input v-model="parameterForm.cpu" placeholder="500m" />
            </label>
            <label>
              Memory per role
              <input v-model="parameterForm.memory" placeholder="1Gi" />
            </label>
            <label>
              Storage size
              <input v-model="parameterForm.storageSize" placeholder="10Gi" />
            </label>
            <label>
              StorageClass
              <input v-model="parameterForm.storageClass" placeholder="default" />
            </label>
            <label>
              Backup profile
              <input v-model="parameterForm.backupProfile" placeholder="daily-7d" />
            </label>
            <label>
              Service type
              <select v-model="parameterForm.serviceType">
                <option value="ClusterIP">ClusterIP</option>
                <option value="NodePort">NodePort</option>
                <option value="LoadBalancer">LoadBalancer</option>
              </select>
            </label>
            <label v-if="parameterForm.serviceType === 'LoadBalancer' || parameterForm.serviceType === 'NodePort'">
              External DNS hostname
              <input v-model="parameterForm.externalDnsHostname" placeholder="service.apps.company.tld" />
            </label>
            <label>
              Primary cluster
              <select v-model="parameterForm.primaryCluster">
                <option value="">Project default</option>
                <option v-for="cluster in clusterRows" :key="cluster.name" :value="cluster.name">
                  {{ cluster.displayName || cluster.name }}
                </option>
              </select>
            </label>
            <div style="grid-column: span 2">
              <p style="color: var(--muted-strong); font-size: 12px; font-weight: 800; text-transform: uppercase; margin: 0 0 8px">Standby clusters</p>
              <div class="tag-row">
                <label
                  v-for="cluster in availableStandbyClusters"
                  :key="cluster.name"
                  class="checkbox-label form-grid"
                  style="padding: 5px 10px; border: 1px solid var(--border); border-radius: 6px; cursor: pointer"
                >
                  <input type="checkbox" :value="cluster.name" v-model="parameterForm.standbyClusters" />
                  {{ cluster.displayName || cluster.name }}
                </label>
                <span v-if="availableStandbyClusters.length === 0" class="muted" style="font-size: 13px">No other clusters available</span>
              </div>
            </div>
          </div>
          <div v-else-if="requestForm.serviceClass === 'virtual-machine'" class="form-grid modal-form-grid">
            <label>
              Guest image
              <input v-model="parameterForm.vmImage" placeholder="quay.io/containerdisks/ubuntu:22.04" />
            </label>
            <label>
              Workload type
              <select v-model="parameterForm.vmWorkloadType">
                <option value="vm">VirtualMachine</option>
                <option value="vmp">VirtualMachinePool</option>
              </select>
            </label>
            <label v-if="parameterForm.vmWorkloadType === 'vmp'">
              Pool replicas
              <input v-model.number="parameterForm.vmPoolReplicas" min="1" type="number" />
            </label>
            <label>
              Run strategy
              <select v-model="parameterForm.vmRunStrategy">
                <option value="Always">Always</option>
                <option value="RerunOnFailure">RerunOnFailure</option>
                <option value="Manual">Manual</option>
                <option value="Halted">Halted</option>
              </select>
            </label>
            <label>
              Guest CPU
              <input v-model="parameterForm.cpu" placeholder="2" />
            </label>
            <label>
              Guest memory
              <input v-model="parameterForm.memory" placeholder="4Gi" />
            </label>

            <div class="nested-resource-editor" style="grid-column: span 2">
              <div class="resource-editor-head">
                <div>
                  <h4>Networks</h4>
                  <p class="muted">Configure VM interfaces and network attachments.</p>
                </div>
                <button class="button secondary compact-button" type="button" @click="addVmNetwork">Add network</button>
              </div>
              <div v-if="parameterForm.vmNetworks.length" class="resource-editor-list">
                <article v-for="network in parameterForm.vmNetworks" :key="network.id" class="resource-editor-card">
                  <div class="resource-editor-head">
                    <strong>{{ network.name || 'New network' }}</strong>
                    <button class="button secondary compact-button" type="button" @click="removeVmNetwork(network.id)">Remove</button>
                  </div>
                  <div class="resource-editor-grid">
                    <label>Name<input v-model="network.name" placeholder="default" /></label>
                    <label>
                      Type
                      <select v-model="network.networkType">
                        <option value="pod">pod</option>
                        <option value="multus">multus</option>
                      </select>
                    </label>
                    <label>
                      Binding
                      <select v-model="network.bindingMethod">
                        <option value="masquerade">masquerade</option>
                        <option value="bridge">bridge</option>
                        <option value="sriov">sriov</option>
                      </select>
                    </label>
                    <label>Model<input v-model="network.model" placeholder="virtio" /></label>
                    <label v-if="network.networkType === 'multus'">Multus network name<input v-model="network.multusNetworkName" placeholder="default/vlan-net" /></label>
                  </div>
                </article>
              </div>
              <p v-else class="muted">No networks defined yet.</p>
            </div>

            <div class="nested-resource-editor" style="grid-column: span 2">
              <div class="resource-editor-head">
                <div>
                  <h4>Disks</h4>
                  <p class="muted">Configure DataVolume-backed VM disks.</p>
                </div>
                <button class="button secondary compact-button" type="button" @click="addVmDisk">Add disk</button>
              </div>
              <div v-if="parameterForm.vmDisks.length" class="resource-editor-list">
                <article v-for="disk in parameterForm.vmDisks" :key="disk.id" class="resource-editor-card">
                  <div class="resource-editor-head">
                    <strong>{{ disk.name || 'New disk' }}</strong>
                    <button class="button secondary compact-button" type="button" @click="removeVmDisk(disk.id)">Remove</button>
                  </div>
                  <div class="resource-editor-grid">
                    <label>Name<input v-model="disk.name" placeholder="rootdisk" /></label>
                    <label>Image<input v-model="disk.image" placeholder="quay.io/containerdisks/ubuntu:22.04" /></label>
                    <label>Size<input v-model="disk.size" placeholder="20Gi" /></label>
                    <label>StorageClass<input v-model="disk.storageClass" placeholder="default" /></label>
                    <label>
                      Bus
                      <select v-model="disk.bus">
                        <option value="virtio">virtio</option>
                        <option value="sata">sata</option>
                        <option value="scsi">scsi</option>
                      </select>
                    </label>
                  </div>
                </article>
              </div>
              <p v-else class="muted">No disks defined yet.</p>
            </div>
          </div>
          <div v-else-if="requestForm.serviceClass === 'argo-application'" class="form-grid modal-form-grid">
            <label style="grid-column: span 2">
              Repository
              <select
                v-if="projectRepositories.length > 0"
                v-model="parameterForm.argoRepoRef"
                @change="onArgoRepoRefChange(parameterForm.argoRepoRef)"
              >
                <option value="">Select a repository</option>
                <option v-for="repo in projectRepositories" :key="repo.name" :value="repo.name">
                  {{ repo.displayName || repo.name }} — {{ repo.url }}
                </option>
              </select>
              <span v-else-if="repositoriesLoading" class="muted" style="font-size: 13px">Loading repositories...</span>
              <span v-else class="muted" style="font-size: 13px">
                No repositories registered for this project.
                <a href="#" @click.prevent="() => {}">Add one in Admin → Repositories.</a>
              </span>
            </label>
            <label style="grid-column: span 2" v-if="!parameterForm.argoRepoRef">
              Repository URL (manual)
              <input v-model="parameterForm.argoRepoURL" placeholder="https://github.com/org/repo.git" />
            </label>
            <label>
              Path
              <input v-model="parameterForm.argoPath" placeholder="charts/my-app" />
            </label>
            <label>
              Target revision
              <input v-model="parameterForm.argoTargetRevision" placeholder="HEAD" />
            </label>
            <label>
              Target namespace
              <input v-model="parameterForm.argoTargetNamespace" placeholder="my-namespace" />
            </label>
            <label>
              Sync policy
              <select v-model="parameterForm.argoSyncPolicy">
                <option value="manual">Manual</option>
                <option value="auto">Automatic</option>
              </select>
            </label>
            <label class="checkbox-label" style="align-self: center; margin-top: 8px">
              <input type="checkbox" v-model="parameterForm.argoCreateNamespace" />
              Auto-create namespace
            </label>
            <label>
              Helm release name <span class="muted" style="font-size: 11px">(optional)</span>
              <input v-model="parameterForm.argoHelmReleaseName" placeholder="leave blank to use instance name" />
            </label>
            <label style="grid-column: span 2">
              Helm values override <span class="muted" style="font-size: 11px">(optional YAML)</span>
              <textarea
                v-model="parameterForm.argoHelmValuesYAML"
                rows="4"
                placeholder="key: value&#10;nested:&#10;  key: value"
                style="font-family: var(--mono); font-size: 12px; resize: vertical"
              />
            </label>
          </div>
          </div>
        </section>

        <div class="form-actions">
          <button class="button primary" type="submit" :disabled="submitting">
            {{ submitting ? 'Submitting...' : 'Submit request' }}
          </button>
          <button class="button secondary" type="button" :disabled="submitting" @click="closeRequest">Cancel</button>
          <span v-if="submitError" class="error-text">{{ submitError }}</span>
        </div>
      </form>
    </div>
  </Teleport>
</template>
