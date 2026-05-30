export const DEFAULT_PROFILE_LABEL_KEY = 'platform.servicer.io/profile'

export type NatsStreamForm = {
  id: number
  name: string
  subjects: string
  storage: string
  retention: string
  maxAge: string
}

export type NatsConsumerForm = {
  id: number
  name: string
  stream: string
  filterSubjects: string
  ackPolicy: string
}

export type NatsCredentialForm = {
  id: number
  name: string
  username: string
  publish: string
  subscribe: string
  allowResponses: boolean
}

export type VmNetworkForm = {
  id: number
  name: string
  networkType: 'pod' | 'multus'
  bindingMethod: 'masquerade' | 'bridge' | 'sriov'
  multusNetworkName: string
  model: string
}

export type VmDiskForm = {
  id: number
  name: string
  image: string
  size: string
  storageClass: string
  bus: string
}

export type ProductParameterForm = {
  replicas: number
  databaseName: string
  cpu: string
  memory: string
  pods: string
  defaultDenyIngress: boolean
  memoryProfile: string
  memoryLimit: string
  persistence: string
  storageClass: string
  storageSize: string
  maxMemoryPolicy: string
  backupProfile: string
  backupCredentialsSecret: string
  backupEndpoint: string
  backupBucket: string
  backupPath: string
  backupRegion: string
  backupSchedule: string
  backupRetention: string
  maxPayload: string
  vmImage: string
  vmWorkloadType: string
  vmPoolReplicas: number
  vmRunStrategy: string
  vmNetworks: VmNetworkForm[]
  vmDisks: VmDiskForm[]
  primaryCluster: string
  standbyClusters: string[] | string
  maxReplicationLagSeconds: number
  serviceType: string
  externalDnsHostname: string
  natsStreams: NatsStreamForm[]
  natsConsumers: NatsConsumerForm[]
  natsAppCredentials: NatsCredentialForm[]
  argoSourceType: string
  argoRepoRef: string
  argoRepoURL: string
  argoPath: string
  argoTargetRevision: string
  argoTargetNamespace: string
  argoSyncPolicy: string
  argoCreateNamespace: boolean
  argoHelmReleaseName: string
  argoHelmValuesYAML: string
}

export function csvList(value: string) {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

export function compactParams(values: Record<string, unknown>) {
  return Object.fromEntries(
    Object.entries(values).filter(([, value]) => {
      if (Array.isArray(value)) {
        return value.length > 0
      }
      return value !== '' && value !== undefined && value !== null
    })
  )
}

export function isNatsMultiRegionPlan(plan: string) {
  return plan === 'nats-jetstream' || plan === 'nats-geo'
}

function normalizedStandbyClusters(value: string[] | string) {
  if (Array.isArray(value)) {
    return value
  }
  return value
    .split(',')
    .map((cluster) => cluster.trim())
    .filter(Boolean)
}

function profileLabelKey() {
  const configured = import.meta.env?.VITE_PROFILE_LABEL_KEY
  return typeof configured === 'string' && configured.trim().length > 0
    ? configured.trim()
    : DEFAULT_PROFILE_LABEL_KEY
}

function mysqlReplicationMode(plan: string) {
  if (plan === 'mysql-galera') {
    return 'galera'
  }
  if (plan === 'mysql-active-passive') {
    return 'active-passive'
  }
  return 'single-primary'
}

function natsStreamParameters(form: ProductParameterForm) {
  return form.natsStreams
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

function natsConsumerParameters(form: ProductParameterForm) {
  return form.natsConsumers
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

function natsCredentialParameters(form: ProductParameterForm) {
  return form.natsAppCredentials
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

export function buildProductParameters(args: {
  serviceClass: string
  servicePlan: string
  form: ProductParameterForm
  includeMultiRegionOnlyFields?: boolean
  selectedPlanTopology?: string
}) {
  const { serviceClass, servicePlan, form } = args
  const includeMultiRegionOnlyFields = args.includeMultiRegionOnlyFields ?? false
  const multiRegion = args.selectedPlanTopology === 'multi-region'
  switch (serviceClass) {
    case 'namespace':
      return {
        cpu: form.cpu,
        memory: form.memory,
        pods: form.pods,
        defaultDenyIngress: form.defaultDenyIngress,
        labels: { [profileLabelKey()]: 'workload' }
      }
    case 'postgresql': {
      const backupObjectStore = form.backupBucket && form.backupCredentialsSecret
        ? compactParams({
            endpointUrl: form.backupEndpoint,
            bucket: form.backupBucket,
            path: form.backupPath,
            region: form.backupRegion,
            credentialsSecret: form.backupCredentialsSecret
          })
        : undefined
      const backup = backupObjectStore
        ? compactParams({
            objectStore: backupObjectStore,
            schedule: form.backupSchedule,
            retention: form.backupRetention
          })
        : undefined
      return compactParams({
        instances: form.replicas,
        databaseName: form.databaseName,
        storageClass: form.storageClass,
        storageSize: form.storageSize,
        backup,
        serviceType: form.serviceType,
        externalDnsHostname: (form.serviceType === 'LoadBalancer' || form.serviceType === 'NodePort') ? form.externalDnsHostname : undefined
      })
    }
    case 'mysql':
      return compactParams({
        replicas: form.replicas,
        databaseName: form.databaseName,
        cpu: form.cpu,
        memory: form.memory,
        storageClass: form.storageClass,
        storageSize: form.storageSize,
        backupProfile: form.backupProfile,
        replicationMode: mysqlReplicationMode(servicePlan),
        primaryCluster: includeMultiRegionOnlyFields ? (multiRegion ? form.primaryCluster : undefined) : form.primaryCluster,
        standbyClusters: includeMultiRegionOnlyFields ? (multiRegion ? normalizedStandbyClusters(form.standbyClusters) : undefined) : normalizedStandbyClusters(form.standbyClusters),
        serviceType: form.serviceType,
        externalDnsHostname: (form.serviceType === 'LoadBalancer' || form.serviceType === 'NodePort') ? form.externalDnsHostname : undefined
      })
    case 'nats':
      return compactParams({
        replicas: form.replicas,
        jetstream: isNatsMultiRegionPlan(servicePlan),
        streams: natsStreamParameters(form),
        consumers: natsConsumerParameters(form),
        appCredentials: natsCredentialParameters(form),
        storageClass: form.storageClass,
        storageSize: form.storageSize,
        maxPayload: form.maxPayload,
        memoryLimit: form.memoryLimit,
        standbyClusters: normalizedStandbyClusters(form.standbyClusters),
        serviceType: form.serviceType,
        externalDnsHostname: (form.serviceType === 'LoadBalancer' || form.serviceType === 'NodePort') ? form.externalDnsHostname : undefined
      })
    case 'valkey':
      return compactParams({
        replicas: form.replicas,
        memoryProfile: form.memoryProfile,
        memoryLimit: form.memoryLimit,
        persistence: form.persistence,
        storageClass: form.storageClass,
        storageSize: form.storageSize,
        maxMemoryPolicy: form.maxMemoryPolicy,
        primaryCluster: form.primaryCluster,
        standbyClusters: normalizedStandbyClusters(form.standbyClusters),
        maxReplicationLagSeconds: form.maxReplicationLagSeconds,
        serviceType: form.serviceType,
        externalDnsHostname: (form.serviceType === 'LoadBalancer' || form.serviceType === 'NodePort') ? form.externalDnsHostname : undefined
      })
    case 'yugabyte':
      return compactParams({
        tserverReplicas: form.replicas,
        masterReplicas: form.replicas,
        databaseName: form.databaseName,
        cpu: form.cpu,
        memory: form.memory,
        storageSize: form.storageSize,
        storageClass: form.storageClass,
        backupProfile: form.backupProfile,
        primaryCluster: form.primaryCluster,
        standbyClusters: normalizedStandbyClusters(form.standbyClusters),
        serviceType: form.serviceType,
        externalDnsHostname: (form.serviceType === 'LoadBalancer' || form.serviceType === 'NodePort') ? form.externalDnsHostname : undefined
      })
    case 'argo-application':
      return compactParams({
        sourceType: form.argoSourceType,
        repoURL: form.argoRepoURL || form.argoRepoRef,
        path: form.argoPath,
        targetRevision: form.argoTargetRevision,
        targetNamespace: form.argoTargetNamespace,
        syncPolicy: form.argoSyncPolicy,
        createNamespace: form.argoCreateNamespace || undefined,
        helmReleaseName: form.argoSourceType === 'helm' ? form.argoHelmReleaseName : undefined,
        helmValuesYAML: form.argoSourceType === 'helm' ? form.argoHelmValuesYAML : undefined,
        repoRef: form.argoRepoRef
      })
    case 'virtual-machine':
      return compactParams({
        image: form.vmImage,
        workloadType: form.vmWorkloadType,
        poolReplicas: form.vmWorkloadType === 'vmp' ? form.vmPoolReplicas : undefined,
        cpu: form.cpu,
        memory: form.memory,
        runStrategy: form.vmRunStrategy,
        storageClass: form.storageClass,
        storageSize: form.storageSize,
        networks: form.vmNetworks
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
        disks: form.vmDisks
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
