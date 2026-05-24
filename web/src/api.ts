import { authHeaders } from './auth'

export interface OverviewResponse {
  tenants: number
  projects: number
  instances: number
  readyInstances: number
  pendingActions: number
  health: {
    ready: number
    provisioning: number
    failed: number
    other: number
  }
  recentActions: ActionSummary[]
}

export interface TenantSummary {
  name: string
  displayName: string
  phase: string
  allowedServiceClasses: string[]
  projectCount: number
  instanceCount: number
  owners: string[]
}

export interface ProjectSummary {
  name: string
  displayName: string
  tenantName: string
  environment: string
  phase: string
  clusterName: string
  namespaceMode: string
  instanceCount: number
}

export interface NamespaceClaimSummary {
  name: string
  projectName: string
  phase: string
  clusterName?: string
  namespace?: string
  health?: string
}

export interface NamespaceClaimDetail {
  name: string
  displayName?: string
  projectName: string
  phase: string
  clusterName?: string
  namespace?: string
  health?: string
  deletionPolicy?: string
  quotas?: Record<string, string>
  labels?: Record<string, string>
  artifact: {
    revision?: string
    path?: string
    count?: number
  }
  delivery: {
    syncPhase?: string
    applicationName?: string
    message?: string
  }
  conditions: Array<{ type: string; status: string; reason: string; message: string }>
}

export interface NamespaceClaimRequest {
  name: string
  projectName: string
  displayName?: string
  quotas?: Record<string, string>
  labels?: Record<string, string>
  deletionPolicy?: string
}

export interface CatalogEntry {
  name: string
  displayName: string
  category: string
  driver: string
  published: boolean
  description: string
  capabilities: string[]
  plans: CatalogPlan[]
  actions: ActionSpec[]
}

export interface CatalogPlan {
  name: string
  displayName: string
  tier: string
  topology: string
  defaultVersion: string
  published: boolean
}

export interface ActionSpec {
  name: string
  displayName: string
  requiresApproval: boolean
  disruptive: boolean
}

export interface InstanceSummary {
  name: string
  displayName: string
  projectName: string
  tenantName: string
  productClass: string
  productName: string
  planName: string
  planDisplay: string
  phase: string
  health: string
  clusterName: string
  namespace: string
  syncPhase: string
  endpoints?: Record<string, string>
}

export interface InstanceDetail extends InstanceSummary {
  runtime: {
    driver: string
    apiVersion?: string
    kind?: string
    name?: string
    namespace?: string
  }
  desired: ProductRequest
  delivery: {
    syncPhase?: string
    applicationName?: string
    message?: string
    argoStatus?: string
    runtimeStatus?: string
  }
  artifact: {
    revision?: string
    path?: string
    count?: number
  }
  credentials: Array<{ name: string; namespace: string; revealUrl?: string }>
  conditions: Array<{ type: string; status: string; reason: string; message: string }>
  topology?: {
    mode?: string
    primaryCluster?: string
    trafficEndpoint?: string
    failoverReadiness?: string
    message?: string
    standbys?: Array<{ clusterName: string; ready: boolean; message?: string }>
  }
  messaging?: {
    jetStream: boolean
    streams?: Array<{ name: string; subjects?: string[]; storage?: string; retention?: string }>
    consumers?: Array<{ name: string; stream: string; filterSubjects?: string[]; ackPolicy?: string }>
    appCredentials?: Array<{ name: string; username: string; publish?: string[]; subscribe?: string[]; allowResponses?: boolean }>
  }
  availableActions: ActionSpec[]
  recentActions: ActionSummary[]
  events: AuditEventSummary[]
}

export interface ActionSummary {
  name: string
  targetName: string
  action: string
  phase: string
  result?: string
  startedAt?: string
  completedAt?: string
  kubeconfigDownloadUrl?: string
}

export interface CredentialDetail {
  name: string
  namespace: string
  data: Record<string, string>
}

export interface ProductRequest {
  name: string
  projectName: string
  serviceClass: string
  servicePlan: string
  version?: string
  parameters?: Record<string, unknown>
}

export interface ActionSubmitRequest {
  action: string
  parameters?: Record<string, unknown>
  reason?: string
}

export interface AuditEventSummary {
  time?: string
  type: string
  subject: string
  action?: string
  actor?: string
  phase?: string
  reason?: string
  message?: string
  involved?: string
}

// Admin types

export interface ClusterTargetSummary {
  name: string
  displayName: string
  region: string
  phase: string
  reachable: boolean
  k8sVersion: string
  ingressDomain: string
  capabilities?: Record<string, string>
}

export interface ServiceClassAdminSummary {
  name: string
  displayName: string
  driver: string
  category: string
  published: boolean
  registered: boolean
  defaultParameters?: Record<string, unknown>
}

export interface RepositorySummary {
  name: string
  displayName: string
  scope: 'tenant' | 'project'
  tenantName?: string
  projectName?: string
  url: string
  authType: 'none' | 'http' | 'ssh'
}

export interface CreateRepositoryRequest {
  name: string
  displayName: string
  scope?: 'tenant' | 'project'
  tenantName?: string
  projectName?: string
  url: string
  authType: 'none' | 'http' | 'ssh'
  username?: string
  password?: string
  sshKey?: string
}

export interface CreateTenantRequest {
  name: string
  displayName: string
  owners: string[]
  allowedServiceClasses: string[]
  quotaProfileRef?: string
}

export interface UpdateTenantRequest {
  displayName: string
  owners: string[]
  allowedServiceClasses: string[]
}

export interface CreateProjectRequest {
  name: string
  displayName: string
  tenantName: string
  environment: string
  clusterName?: string
  namespaceMode: string
  maxServices?: number
}

export interface UpdateProjectRequest {
  displayName: string
  clusterName: string
  namespaceMode: string
  maxServices?: number | null
}

export interface CreateClusterRequest {
  name: string
  displayName: string
  region: string
  ingressDomain?: string
  capabilities?: Record<string, string>
  connectionSecretName: string
  connectionSecretNamespace: string
}

export interface UpdateClusterRequest {
  displayName: string
  region: string
  ingressDomain?: string
  capabilities?: Record<string, string>
}

export interface UpdateServiceClassRequest {
  published: boolean
  defaultParameters?: Record<string, unknown>
}

export interface AuthProviderSummary {
  name: string
  displayName: string
  type: 'local' | 'oidc' | 'ldap'
  enabled: boolean
  default: boolean
  phase?: string
  oidcIssuerUrl?: string
  oidcClientId?: string
  oidcScopes?: string[]
  oidcUsernameClaim?: string
  oidcEmailClaim?: string
  oidcRolesClaim?: string
  oidcGroupsClaim?: string
  oidcRedirectPath?: string
  oidcEndSessionUrl?: string
  ldapUrl?: string
  ldapUserBaseDn?: string
  ldapUserFilter?: string
  ldapUsernameAttribute?: string
  ldapEmailAttribute?: string
  ldapGroupBaseDn?: string
  ldapGroupFilter?: string
  ldapGroupNameAttribute?: string
  ldapStartTls?: boolean
  insecureSkipVerify?: boolean
  secretConfigured?: boolean
}

export interface AuthProviderRequest {
  name: string
  displayName: string
  type: 'local' | 'oidc' | 'ldap'
  enabled: boolean
  default: boolean
  oidcIssuerUrl?: string
  oidcClientId?: string
  oidcClientSecret?: string
  oidcScopes?: string[]
  oidcUsernameClaim?: string
  oidcEmailClaim?: string
  oidcRolesClaim?: string
  oidcGroupsClaim?: string
  oidcRedirectPath?: string
  oidcEndSessionUrl?: string
  ldapUrl?: string
  ldapBindUsername?: string
  ldapBindPassword?: string
  ldapUserBaseDn?: string
  ldapUserFilter?: string
  ldapUsernameAttribute?: string
  ldapEmailAttribute?: string
  ldapGroupBaseDn?: string
  ldapGroupFilter?: string
  ldapGroupNameAttribute?: string
  ldapStartTls?: boolean
  insecureSkipVerify?: boolean
}

export interface ExternalIdentitySummary {
  provider: string
  subject: string
}

export interface UserSummary {
  name: string
  displayName?: string
  email?: string
  localAuthEnabled: boolean
  externalIdentities?: ExternalIdentitySummary[]
}

export interface UserRequest {
  name: string
  displayName?: string
  email?: string
  localAuthEnabled: boolean
  password?: string
  externalIdentities?: ExternalIdentitySummary[]
}

export interface ExternalGroupSummary {
  provider: string
  name: string
}

export interface GroupSummary {
  name: string
  displayName?: string
  members?: string[]
  externalGroups?: ExternalGroupSummary[]
}

export interface GroupRequest {
  name: string
  displayName?: string
  members?: string[]
  externalGroups?: ExternalGroupSummary[]
}

export interface RoleBindingSubject {
  kind: 'User' | 'Group'
  name: string
}

export interface RoleBindingSummary {
  name: string
  displayName?: string
  scope: 'platform' | 'tenant'
  tenantName?: string
  subjects: RoleBindingSubject[]
  roles: string[]
}

export interface RoleBindingRequest {
  name: string
  displayName?: string
  scope: 'platform' | 'tenant'
  tenantName?: string
  subjects: RoleBindingSubject[]
  roles: string[]
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      ...authHeaders(),
      ...init.headers
    }
  })
  if (!response.ok) {
    const message = await response.text()
    throw new Error(message || `Request failed: ${response.status}`)
  }
  return response.json() as Promise<T>
}

async function download(path: string): Promise<Blob> {
  const response = await fetch(path, {
    headers: authHeaders()
  })
  if (!response.ok) {
    const message = await response.text()
    throw new Error(message || `Request failed: ${response.status}`)
  }
  return response.blob()
}

export const api = {
  overview: () => request<OverviewResponse>('/api/overview'),
  tenants: () => request<TenantSummary[]>('/api/tenants'),
  projects: () => request<ProjectSummary[]>('/api/projects'),
  namespaceClaims: {
    list: () => request<NamespaceClaimSummary[]>('/api/namespaceclaims'),
    detail: (name: string) =>
      request<NamespaceClaimDetail>(`/api/namespaceclaims/${encodeURIComponent(name)}`),
    create: (body: NamespaceClaimRequest) =>
      request<{ name: string; message: string }>('/api/namespaceclaims', {
        method: 'POST',
        body: JSON.stringify(body)
      }),
    update: (name: string, body: NamespaceClaimRequest) =>
      request<{ name: string; message: string }>(`/api/namespaceclaims/${encodeURIComponent(name)}`, {
        method: 'PUT',
        body: JSON.stringify(body)
      }),
    delete: (name: string) =>
      request<{ name: string; message: string }>(`/api/namespaceclaims/${encodeURIComponent(name)}`, {
        method: 'DELETE'
      })
  },
  catalog: () => request<CatalogEntry[]>('/api/catalog'),
  instances: () => request<InstanceSummary[]>('/api/instances'),
  instance: (name: string) => request<InstanceDetail>(`/api/instances/${encodeURIComponent(name)}`),
  audit: (query = '') => request<AuditEventSummary[]>(`/api/audit${query ? `?q=${encodeURIComponent(query)}` : ''}`),
  createRequest: (body: ProductRequest) =>
    request<{ name: string; message: string }>('/api/requests', {
      method: 'POST',
      body: JSON.stringify(body)
    }),
  updateRequest: (name: string, body: ProductRequest) =>
    request<{ name: string; message: string }>(`/api/instances/${encodeURIComponent(name)}`, {
      method: 'PUT',
      body: JSON.stringify(body)
    }),
  deleteRequest: (name: string) =>
    request<{ name: string; message: string }>(`/api/instances/${encodeURIComponent(name)}`, {
      method: 'DELETE'
    }),
  submitAction: (name: string, body: ActionSubmitRequest) =>
    request<{ name: string; message: string }>(`/api/instances/${encodeURIComponent(name)}/actions`, {
      method: 'POST',
      body: JSON.stringify(body)
    }),
  credential: (url: string) => request<CredentialDetail>(url),
  downloadKubeconfig: (url: string) => download(url),
  admin: {
    clusters: () => request<ClusterTargetSummary[]>('/api/admin/clusters'),
    createCluster: (body: CreateClusterRequest) =>
      request<{ name: string; message: string }>('/api/admin/clusters', {
        method: 'POST',
        body: JSON.stringify(body)
      }),
    updateCluster: (name: string, body: UpdateClusterRequest) =>
      request<{ name: string; message: string }>(`/api/admin/clusters/${encodeURIComponent(name)}`, {
        method: 'PUT',
        body: JSON.stringify(body)
      }),
    deleteCluster: (name: string) =>
      request<{ name: string; message: string }>(`/api/admin/clusters/${encodeURIComponent(name)}`, {
        method: 'DELETE'
      }),
    createTenant: (body: CreateTenantRequest) =>
      request<{ name: string; message: string }>('/api/admin/tenants', {
        method: 'POST',
        body: JSON.stringify(body)
      }),
    updateTenant: (name: string, body: UpdateTenantRequest) =>
      request<{ name: string; message: string }>(`/api/admin/tenants/${encodeURIComponent(name)}`, {
        method: 'PUT',
        body: JSON.stringify(body)
      }),
    deleteTenant: (name: string) =>
      request<{ name: string; message: string }>(`/api/admin/tenants/${encodeURIComponent(name)}`, {
        method: 'DELETE'
      }),
    createProject: (body: CreateProjectRequest) =>
      request<{ name: string; message: string }>('/api/admin/projects', {
        method: 'POST',
        body: JSON.stringify(body)
      }),
    updateProject: (name: string, body: UpdateProjectRequest) =>
      request<{ name: string; message: string }>(`/api/admin/projects/${encodeURIComponent(name)}`, {
        method: 'PUT',
        body: JSON.stringify(body)
      }),
    deleteProject: (name: string) =>
      request<{ name: string; message: string }>(`/api/admin/projects/${encodeURIComponent(name)}`, {
        method: 'DELETE'
      }),
    serviceClasses: () => request<ServiceClassAdminSummary[]>('/api/admin/serviceclasses'),
    registerServiceClass: (name: string) =>
      request<{ name: string; message: string }>('/api/admin/serviceclasses', {
        method: 'POST',
        body: JSON.stringify({ name })
      }),
    updateServiceClass: (name: string, body: UpdateServiceClassRequest) =>
      request<{ name: string; message: string }>(`/api/admin/serviceclasses/${encodeURIComponent(name)}`, {
        method: 'PUT',
        body: JSON.stringify(body)
      }),
    authProviders: () => request<AuthProviderSummary[]>('/api/admin/auth/providers'),
    createAuthProvider: (body: AuthProviderRequest) =>
      request<{ name: string; message: string }>('/api/admin/auth/providers', {
        method: 'POST',
        body: JSON.stringify(body)
      }),
    updateAuthProvider: (name: string, body: AuthProviderRequest) =>
      request<{ name: string; message: string }>(`/api/admin/auth/providers/${encodeURIComponent(name)}`, {
        method: 'PUT',
        body: JSON.stringify(body)
      }),
    deleteAuthProvider: (name: string) =>
      request<{ name: string; message: string }>(`/api/admin/auth/providers/${encodeURIComponent(name)}`, {
        method: 'DELETE'
      }),
    users: () => request<UserSummary[]>('/api/admin/auth/users'),
    createUser: (body: UserRequest) =>
      request<{ name: string; message: string }>('/api/admin/auth/users', {
        method: 'POST',
        body: JSON.stringify(body)
      }),
    updateUser: (name: string, body: UserRequest) =>
      request<{ name: string; message: string }>(`/api/admin/auth/users/${encodeURIComponent(name)}`, {
        method: 'PUT',
        body: JSON.stringify(body)
      }),
    deleteUser: (name: string) =>
      request<{ name: string; message: string }>(`/api/admin/auth/users/${encodeURIComponent(name)}`, {
        method: 'DELETE'
      }),
    groups: () => request<GroupSummary[]>('/api/admin/auth/groups'),
    createGroup: (body: GroupRequest) =>
      request<{ name: string; message: string }>('/api/admin/auth/groups', {
        method: 'POST',
        body: JSON.stringify(body)
      }),
    updateGroup: (name: string, body: GroupRequest) =>
      request<{ name: string; message: string }>(`/api/admin/auth/groups/${encodeURIComponent(name)}`, {
        method: 'PUT',
        body: JSON.stringify(body)
      }),
    deleteGroup: (name: string) =>
      request<{ name: string; message: string }>(`/api/admin/auth/groups/${encodeURIComponent(name)}`, {
        method: 'DELETE'
      }),
    roleBindings: () => request<RoleBindingSummary[]>('/api/admin/auth/rolebindings'),
    createRoleBinding: (body: RoleBindingRequest) =>
      request<{ name: string; message: string }>('/api/admin/auth/rolebindings', {
        method: 'POST',
        body: JSON.stringify(body)
      }),
    updateRoleBinding: (name: string, body: RoleBindingRequest) =>
      request<{ name: string; message: string }>(`/api/admin/auth/rolebindings/${encodeURIComponent(name)}`, {
        method: 'PUT',
        body: JSON.stringify(body)
      }),
    deleteRoleBinding: (name: string) =>
      request<{ name: string; message: string }>(`/api/admin/auth/rolebindings/${encodeURIComponent(name)}`, {
        method: 'DELETE'
      })
  },

  repositories: {
    list: (project: string) =>
      request<RepositorySummary[]>(`/api/projects/${encodeURIComponent(project)}/repositories`),
    listProject: (project: string) =>
      request<RepositorySummary[]>(`/api/projects/${encodeURIComponent(project)}/repositories`),
    listTenant: (tenant: string) =>
      request<RepositorySummary[]>(`/api/tenants/${encodeURIComponent(tenant)}/repositories`),
    create: (project: string, body: CreateRepositoryRequest) =>
      request<{ name: string; message: string }>(`/api/projects/${encodeURIComponent(project)}/repositories`, {
        method: 'POST',
        body: JSON.stringify(body)
      }),
    createProject: (project: string, body: CreateRepositoryRequest) =>
      request<{ name: string; message: string }>(`/api/projects/${encodeURIComponent(project)}/repositories`, {
        method: 'POST',
        body: JSON.stringify(body)
      }),
    createTenant: (tenant: string, body: CreateRepositoryRequest) =>
      request<{ name: string; message: string }>(`/api/tenants/${encodeURIComponent(tenant)}/repositories`, {
        method: 'POST',
        body: JSON.stringify(body)
      }),
    delete: (project: string, repo: string) =>
      request<{ name: string; message: string }>(`/api/projects/${encodeURIComponent(project)}/repositories/${encodeURIComponent(repo)}`, {
        method: 'DELETE'
      }),
    deleteProject: (project: string, repo: string) =>
      request<{ name: string; message: string }>(`/api/projects/${encodeURIComponent(project)}/repositories/${encodeURIComponent(repo)}`, {
        method: 'DELETE'
      }),
    deleteTenant: (tenant: string, repo: string) =>
      request<{ name: string; message: string }>(`/api/tenants/${encodeURIComponent(tenant)}/repositories/${encodeURIComponent(repo)}`, {
        method: 'DELETE'
      })
  }
}
