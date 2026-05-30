import { describe, expect, it } from 'vitest'
import { buildProductParameters, type ProductParameterForm } from './parameters'

function baseForm(): ProductParameterForm {
  return {
    replicas: 3,
    databaseName: 'appdb',
    cpu: '2',
    memory: '4Gi',
    pods: '20',
    defaultDenyIngress: true,
    memoryProfile: 'medium',
    memoryLimit: '512Mi',
    persistence: 'persistent',
    storageClass: 'fast',
    storageSize: '20Gi',
    maxMemoryPolicy: 'allkeys-lru',
    backupProfile: 'daily-7d',
    backupCredentialsSecret: 'backup-creds',
    backupEndpoint: 'https://s3.example.com',
    backupBucket: 'db-backups',
    backupPath: 'prod/appdb',
    backupRegion: 'us-east-1',
    backupSchedule: '0 2 * * *',
    backupRetention: '30d',
    maxPayload: '1MiB',
    vmImage: 'quay.io/containerdisks/ubuntu:22.04',
    vmWorkloadType: 'vm',
    vmPoolReplicas: 1,
    vmRunStrategy: 'Always',
    vmNetworks: [{ id: 1, name: 'default', networkType: 'pod', bindingMethod: 'masquerade', multusNetworkName: '', model: 'virtio' }],
    vmDisks: [{ id: 1, name: 'rootdisk', image: 'quay.io/containerdisks/ubuntu:22.04', size: '20Gi', storageClass: 'fast', bus: 'virtio' }],
    primaryCluster: 'cluster-a',
    standbyClusters: ['cluster-b'],
    maxReplicationLagSeconds: 30,
    serviceType: 'ClusterIP',
    externalDnsHostname: '',
    natsStreams: [{ id: 1, name: 'ORDERS', subjects: 'orders.>', storage: 'file', retention: 'limits', maxAge: '168h' }],
    natsConsumers: [{ id: 1, name: 'DISPATCH', stream: 'ORDERS', filterSubjects: 'orders.created', ackPolicy: 'explicit' }],
    natsAppCredentials: [{ id: 1, name: 'orders-api', username: 'orders-api', publish: 'orders.created', subscribe: 'orders.>', allowResponses: true }],
    argoSourceType: 'manifests',
    argoRepoRef: 'platform-catalog',
    argoRepoURL: '',
    argoPath: 'apps/orders',
    argoTargetRevision: 'v1.2.3',
    argoTargetNamespace: 'orders',
    argoSyncPolicy: 'manual',
    argoCreateNamespace: false,
    argoHelmReleaseName: '',
    argoHelmValuesYAML: ''
  }
}

describe('buildProductParameters parity', () => {
  const cases = [
    { serviceClass: 'mysql', servicePlan: 'mysql-galera' },
    { serviceClass: 'valkey', servicePlan: 'valkey-standard' },
    { serviceClass: 'nats', servicePlan: 'nats-jetstream' },
    { serviceClass: 'postgresql', servicePlan: 'postgresql-standard' },
    { serviceClass: 'virtual-machine', servicePlan: 'kubevirt-standard' },
    { serviceClass: 'namespace', servicePlan: 'namespace-default' },
    { serviceClass: 'argo-application', servicePlan: 'argo-application-standard' }
  ]

  for (const testCase of cases) {
    it(`matches create/edit output for ${testCase.serviceClass}`, () => {
      const form = baseForm()
      const createParams = buildProductParameters({
        serviceClass: testCase.serviceClass,
        servicePlan: testCase.servicePlan,
        form,
        includeMultiRegionOnlyFields: true,
        selectedPlanTopology: 'multi-region'
      })
      const editParams = buildProductParameters({
        serviceClass: testCase.serviceClass,
        servicePlan: testCase.servicePlan,
        form
      })
      expect(editParams).toEqual(createParams)
    })
  }
})

describe('buildProductParameters plan behavior', () => {
  it('uses updated nats plan for jetstream flag', () => {
    const form = baseForm()
    const parameters = buildProductParameters({
      serviceClass: 'nats',
      servicePlan: 'nats-jetstream',
      form
    }) as Record<string, unknown>
    expect(parameters.jetstream).toBe(true)
  })
})
