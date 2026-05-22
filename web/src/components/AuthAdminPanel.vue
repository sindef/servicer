<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import {
  api,
  type AuthProviderSummary,
  type AuthProviderRequest,
  type UserSummary,
  type UserRequest,
  type GroupSummary,
  type GroupRequest,
  type RoleBindingSummary,
  type RoleBindingRequest
} from '../api'
import { useApi } from '../composables/useApi'

type AuthSection = 'providers' | 'users' | 'groups' | 'bindings'

const providers = useApi(api.admin.authProviders)
const users = useApi(api.admin.users)
const groups = useApi(api.admin.groups)
const bindings = useApi(api.admin.roleBindings)
const tenants = useApi(api.tenants)

const providerRows = computed(() => (providers.data.value ?? []).slice().sort((a, b) => a.displayName.localeCompare(b.displayName)))
const userRows = computed(() => (users.data.value ?? []).slice().sort((a, b) => (a.displayName || a.name).localeCompare(b.displayName || b.name)))
const groupRows = computed(() => (groups.data.value ?? []).slice().sort((a, b) => (a.displayName || a.name).localeCompare(b.displayName || b.name)))
const bindingRows = computed(() => (bindings.data.value ?? []).slice().sort((a, b) => (a.displayName || a.name).localeCompare(b.displayName || b.name)))
const tenantRows = computed(() => tenants.data.value ?? [])

const activeSection = ref<AuthSection>('providers')
const search = ref('')
const error = ref<string | null>(null)
const success = ref<string | null>(null)
const busy = ref(false)

function clearStatus() {
  error.value = null
  success.value = null
}

async function runWrite(fn: () => Promise<{ name: string; message: string }>, reload?: () => Promise<void>) {
  busy.value = true
  clearStatus()
  try {
    const res = await fn()
    success.value = res.message
    await reload?.()
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Request failed'
  } finally {
    busy.value = false
  }
}

const metrics = computed(() => [
  { label: 'Providers', value: providerRows.value.length, note: `${providerRows.value.filter((item) => item.enabled).length} enabled` },
  { label: 'Users', value: userRows.value.length, note: `${userRows.value.filter((item) => item.localAuthEnabled).length} local accounts` },
  { label: 'Groups', value: groupRows.value.length, note: `${groupRows.value.filter((item) => (item.externalGroups?.length ?? 0) > 0).length} mapped externally` },
  { label: 'Bindings', value: bindingRows.value.length, note: `${bindingRows.value.filter((item) => item.scope === 'tenant').length} tenant scoped` }
])

function matchesSearch(haystack: Array<string | undefined>) {
  const term = search.value.trim().toLowerCase()
  if (!term) return true
  return haystack.some((value) => (value ?? '').toLowerCase().includes(term))
}

const filteredProviders = computed(() =>
  providerRows.value.filter((provider) =>
    matchesSearch([provider.name, provider.displayName, provider.type, provider.enabled ? 'enabled' : 'disabled'])
  )
)
const filteredUsers = computed(() =>
  userRows.value.filter((user) =>
    matchesSearch([user.name, user.displayName, user.email, ...(user.externalIdentities ?? []).map((item) => item.provider), ...(user.externalIdentities ?? []).map((item) => item.subject)])
  )
)
const filteredGroups = computed(() =>
  groupRows.value.filter((group) =>
    matchesSearch([group.name, group.displayName, ...(group.members ?? []), ...(group.externalGroups ?? []).map((item) => item.provider), ...(group.externalGroups ?? []).map((item) => item.name)])
  )
)
const filteredBindings = computed(() =>
  bindingRows.value.filter((binding) =>
    matchesSearch([binding.name, binding.displayName, binding.scope, binding.tenantName, ...binding.roles, ...binding.subjects.map((item) => item.name)])
  )
)

const selectedProviderName = ref<string | null>(null)
const selectedUserName = ref<string | null>(null)
const selectedGroupName = ref<string | null>(null)
const selectedBindingName = ref<string | null>(null)

const selectedProvider = computed(() => providerRows.value.find((item) => item.name === selectedProviderName.value) ?? null)
const selectedUser = computed(() => userRows.value.find((item) => item.name === selectedUserName.value) ?? null)
const selectedGroup = computed(() => groupRows.value.find((item) => item.name === selectedGroupName.value) ?? null)
const selectedBinding = computed(() => bindingRows.value.find((item) => item.name === selectedBindingName.value) ?? null)

const editingProvider = ref<string | null>(null)
const providerForm = reactive({
  name: '',
  displayName: '',
  type: 'local' as 'local' | 'oidc' | 'ldap',
  enabled: true,
  default: false,
  oidcIssuerUrl: '',
  oidcClientId: '',
  oidcClientSecret: '',
  oidcScopes: 'openid profile email offline_access',
  oidcUsernameClaim: 'preferred_username',
  oidcEmailClaim: 'email',
  oidcRolesClaim: 'roles',
  oidcGroupsClaim: 'groups',
  oidcRedirectPath: '/api/auth/callback',
  oidcEndSessionUrl: '',
  ldapUrl: '',
  ldapBindUsername: '',
  ldapBindPassword: '',
  ldapUserBaseDn: '',
  ldapUserFilter: '(uid=%s)',
  ldapUsernameAttribute: 'uid',
  ldapEmailAttribute: 'mail',
  ldapGroupBaseDn: '',
  ldapGroupFilter: '(member=%s)',
  ldapGroupNameAttribute: 'cn',
  ldapStartTls: true,
  insecureSkipVerify: false
})

function resetProviderForm() {
  editingProvider.value = null
  Object.assign(providerForm, {
    name: '',
    displayName: '',
    type: 'local',
    enabled: true,
    default: false,
    oidcIssuerUrl: '',
    oidcClientId: '',
    oidcClientSecret: '',
    oidcScopes: 'openid profile email offline_access',
    oidcUsernameClaim: 'preferred_username',
    oidcEmailClaim: 'email',
    oidcRolesClaim: 'roles',
    oidcGroupsClaim: 'groups',
    oidcRedirectPath: '/api/auth/callback',
    oidcEndSessionUrl: '',
    ldapUrl: '',
    ldapBindUsername: '',
    ldapBindPassword: '',
    ldapUserBaseDn: '',
    ldapUserFilter: '(uid=%s)',
    ldapUsernameAttribute: 'uid',
    ldapEmailAttribute: 'mail',
    ldapGroupBaseDn: '',
    ldapGroupFilter: '(member=%s)',
    ldapGroupNameAttribute: 'cn',
    ldapStartTls: true,
    insecureSkipVerify: false
  })
}

function editProvider(provider: AuthProviderSummary) {
  activeSection.value = 'providers'
  selectedProviderName.value = provider.name
  editingProvider.value = provider.name
  Object.assign(providerForm, {
    name: provider.name,
    displayName: provider.displayName,
    type: provider.type,
    enabled: provider.enabled,
    default: provider.default,
    oidcIssuerUrl: provider.oidcIssuerUrl ?? '',
    oidcClientId: provider.oidcClientId ?? '',
    oidcClientSecret: '',
    oidcScopes: (provider.oidcScopes ?? ['openid', 'profile', 'email', 'offline_access']).join(' '),
    oidcUsernameClaim: provider.oidcUsernameClaim ?? 'preferred_username',
    oidcEmailClaim: provider.oidcEmailClaim ?? 'email',
    oidcRolesClaim: provider.oidcRolesClaim ?? 'roles',
    oidcGroupsClaim: provider.oidcGroupsClaim ?? 'groups',
    oidcRedirectPath: provider.oidcRedirectPath ?? '/api/auth/callback',
    oidcEndSessionUrl: provider.oidcEndSessionUrl ?? '',
    ldapUrl: provider.ldapUrl ?? '',
    ldapBindUsername: '',
    ldapBindPassword: '',
    ldapUserBaseDn: provider.ldapUserBaseDn ?? '',
    ldapUserFilter: provider.ldapUserFilter ?? '(uid=%s)',
    ldapUsernameAttribute: provider.ldapUsernameAttribute ?? 'uid',
    ldapEmailAttribute: provider.ldapEmailAttribute ?? 'mail',
    ldapGroupBaseDn: provider.ldapGroupBaseDn ?? '',
    ldapGroupFilter: provider.ldapGroupFilter ?? '(member=%s)',
    ldapGroupNameAttribute: provider.ldapGroupNameAttribute ?? 'cn',
    ldapStartTls: provider.ldapStartTls ?? true,
    insecureSkipVerify: provider.insecureSkipVerify ?? false
  })
}

function providerRequest(): AuthProviderRequest {
  return {
    name: providerForm.name,
    displayName: providerForm.displayName,
    type: providerForm.type,
    enabled: providerForm.enabled,
    default: providerForm.default,
    oidcIssuerUrl: providerForm.oidcIssuerUrl || undefined,
    oidcClientId: providerForm.oidcClientId || undefined,
    oidcClientSecret: providerForm.oidcClientSecret || undefined,
    oidcScopes: providerForm.oidcScopes.split(/\s+/).filter(Boolean),
    oidcUsernameClaim: providerForm.oidcUsernameClaim || undefined,
    oidcEmailClaim: providerForm.oidcEmailClaim || undefined,
    oidcRolesClaim: providerForm.oidcRolesClaim || undefined,
    oidcGroupsClaim: providerForm.oidcGroupsClaim || undefined,
    oidcRedirectPath: providerForm.oidcRedirectPath || undefined,
    oidcEndSessionUrl: providerForm.oidcEndSessionUrl || undefined,
    ldapUrl: providerForm.ldapUrl || undefined,
    ldapBindUsername: providerForm.ldapBindUsername || undefined,
    ldapBindPassword: providerForm.ldapBindPassword || undefined,
    ldapUserBaseDn: providerForm.ldapUserBaseDn || undefined,
    ldapUserFilter: providerForm.ldapUserFilter || undefined,
    ldapUsernameAttribute: providerForm.ldapUsernameAttribute || undefined,
    ldapEmailAttribute: providerForm.ldapEmailAttribute || undefined,
    ldapGroupBaseDn: providerForm.ldapGroupBaseDn || undefined,
    ldapGroupFilter: providerForm.ldapGroupFilter || undefined,
    ldapGroupNameAttribute: providerForm.ldapGroupNameAttribute || undefined,
    ldapStartTls: providerForm.ldapStartTls,
    insecureSkipVerify: providerForm.insecureSkipVerify
  }
}

async function submitProvider() {
  const body = providerRequest()
  if (editingProvider.value) {
    await runWrite(() => api.admin.updateAuthProvider(editingProvider.value!, body), providers.reload)
  } else {
    await runWrite(() => api.admin.createAuthProvider(body), providers.reload)
  }
  if (!error.value) {
    selectedProviderName.value = body.name
    editProvider({ ...body, phase: '', secretConfigured: true })
  }
}

async function deleteProvider(name: string) {
  if (!window.confirm(`Delete authentication provider ${name}?`)) return
  await runWrite(() => api.admin.deleteAuthProvider(name), providers.reload)
  if (!error.value && selectedProviderName.value === name) {
    selectedProviderName.value = null
    resetProviderForm()
  }
}

const editingUser = ref<string | null>(null)
const userForm = reactive({
  name: '',
  displayName: '',
  email: '',
  localAuthEnabled: true,
  password: '',
  externalIdentitiesRaw: ''
})

function resetUserForm() {
  editingUser.value = null
  Object.assign(userForm, {
    name: '',
    displayName: '',
    email: '',
    localAuthEnabled: true,
    password: '',
    externalIdentitiesRaw: ''
  })
}

function editUser(user: UserSummary) {
  activeSection.value = 'users'
  selectedUserName.value = user.name
  editingUser.value = user.name
  Object.assign(userForm, {
    name: user.name,
    displayName: user.displayName ?? '',
    email: user.email ?? '',
    localAuthEnabled: user.localAuthEnabled,
    password: '',
    externalIdentitiesRaw: (user.externalIdentities ?? []).map((identity) => `${identity.provider}:${identity.subject}`).join('\n')
  })
}

function parseExternalIdentityLines(raw: string) {
  return raw
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [provider, ...rest] = line.split(':')
      return { provider: provider.trim(), subject: rest.join(':').trim() }
    })
    .filter((identity) => identity.provider && identity.subject)
}

async function submitUser() {
  const body: UserRequest = {
    name: userForm.name,
    displayName: userForm.displayName || undefined,
    email: userForm.email || undefined,
    localAuthEnabled: userForm.localAuthEnabled,
    password: userForm.password || undefined,
    externalIdentities: parseExternalIdentityLines(userForm.externalIdentitiesRaw)
  }
  if (editingUser.value) {
    await runWrite(() => api.admin.updateUser(editingUser.value!, body), users.reload)
  } else {
    await runWrite(() => api.admin.createUser(body), users.reload)
  }
  if (!error.value) {
    selectedUserName.value = body.name
    editUser({
      name: body.name,
      displayName: body.displayName,
      email: body.email,
      localAuthEnabled: body.localAuthEnabled,
      externalIdentities: body.externalIdentities
    })
  }
}

async function deleteUser(name: string) {
  if (!window.confirm(`Delete user ${name}?`)) return
  await runWrite(() => api.admin.deleteUser(name), users.reload)
  if (!error.value && selectedUserName.value === name) {
    selectedUserName.value = null
    resetUserForm()
  }
}

const editingGroup = ref<string | null>(null)
const groupForm = reactive({
  name: '',
  displayName: '',
  membersRaw: '',
  externalGroupsRaw: ''
})

function resetGroupForm() {
  editingGroup.value = null
  Object.assign(groupForm, { name: '', displayName: '', membersRaw: '', externalGroupsRaw: '' })
}

function editGroup(group: GroupSummary) {
  activeSection.value = 'groups'
  selectedGroupName.value = group.name
  editingGroup.value = group.name
  Object.assign(groupForm, {
    name: group.name,
    displayName: group.displayName ?? '',
    membersRaw: (group.members ?? []).join('\n'),
    externalGroupsRaw: (group.externalGroups ?? []).map((external) => `${external.provider}:${external.name}`).join('\n')
  })
}

function parseExternalGroups(raw: string) {
  return raw
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [provider, ...rest] = line.split(':')
      return { provider: provider.trim(), name: rest.join(':').trim() }
    })
    .filter((group) => group.provider && group.name)
}

async function submitGroup() {
  const body: GroupRequest = {
    name: groupForm.name,
    displayName: groupForm.displayName || undefined,
    members: groupForm.membersRaw.split('\n').map((line) => line.trim()).filter(Boolean),
    externalGroups: parseExternalGroups(groupForm.externalGroupsRaw)
  }
  if (editingGroup.value) {
    await runWrite(() => api.admin.updateGroup(editingGroup.value!, body), groups.reload)
  } else {
    await runWrite(() => api.admin.createGroup(body), groups.reload)
  }
  if (!error.value) {
    selectedGroupName.value = body.name
    editGroup({
      name: body.name,
      displayName: body.displayName,
      members: body.members,
      externalGroups: body.externalGroups
    })
  }
}

async function deleteGroup(name: string) {
  if (!window.confirm(`Delete group ${name}?`)) return
  await runWrite(() => api.admin.deleteGroup(name), groups.reload)
  if (!error.value && selectedGroupName.value === name) {
    selectedGroupName.value = null
    resetGroupForm()
  }
}

const editingBinding = ref<string | null>(null)
const bindingForm = reactive({
  name: '',
  displayName: '',
  scope: 'tenant' as 'platform' | 'tenant',
  tenantName: '',
  subjectsRaw: 'User:',
  rolesRaw: 'tenant-operator'
})

function resetBindingForm() {
  editingBinding.value = null
  Object.assign(bindingForm, {
    name: '',
    displayName: '',
    scope: 'tenant',
    tenantName: tenantRows.value[0]?.name ?? '',
    subjectsRaw: 'User:',
    rolesRaw: 'tenant-operator'
  })
}

function editBinding(binding: RoleBindingSummary) {
  activeSection.value = 'bindings'
  selectedBindingName.value = binding.name
  editingBinding.value = binding.name
  Object.assign(bindingForm, {
    name: binding.name,
    displayName: binding.displayName ?? '',
    scope: binding.scope,
    tenantName: binding.tenantName ?? '',
    subjectsRaw: binding.subjects.map((subject) => `${subject.kind}:${subject.name}`).join('\n'),
    rolesRaw: binding.roles.join(', ')
  })
}

function parseBindingSubjects(raw: string) {
  return raw
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [kind, ...rest] = line.split(':')
      return { kind: (kind.trim() || 'User') as 'User' | 'Group', name: rest.join(':').trim() }
    })
    .filter((subject) => subject.name)
}

async function submitBinding() {
  const body: RoleBindingRequest = {
    name: bindingForm.name,
    displayName: bindingForm.displayName || undefined,
    scope: bindingForm.scope,
    tenantName: bindingForm.scope === 'tenant' ? bindingForm.tenantName || undefined : undefined,
    subjects: parseBindingSubjects(bindingForm.subjectsRaw),
    roles: bindingForm.rolesRaw.split(',').map((role) => role.trim()).filter(Boolean)
  }
  if (editingBinding.value) {
    await runWrite(() => api.admin.updateRoleBinding(editingBinding.value!, body), bindings.reload)
  } else {
    await runWrite(() => api.admin.createRoleBinding(body), bindings.reload)
  }
  if (!error.value) {
    selectedBindingName.value = body.name
    editBinding({
      name: body.name,
      displayName: body.displayName,
      scope: body.scope,
      tenantName: body.tenantName,
      subjects: body.subjects,
      roles: body.roles
    })
  }
}

async function deleteBinding(name: string) {
  if (!window.confirm(`Delete role binding ${name}?`)) return
  await runWrite(() => api.admin.deleteRoleBinding(name), bindings.reload)
  if (!error.value && selectedBindingName.value === name) {
    selectedBindingName.value = null
    resetBindingForm()
  }
}

function sectionMeta(section: AuthSection) {
  switch (section) {
    case 'providers':
      return {
        title: 'Auth providers',
        description: 'Manage local, OIDC, and LDAP identity sources exposed at sign-in.',
        action: 'New provider'
      }
    case 'users':
      return {
        title: 'Users',
        description: 'Create local operators and map enterprise identities to Servicer users.',
        action: 'New user'
      }
    case 'groups':
      return {
        title: 'Groups',
        description: 'Aggregate local users and upstream directory groups for reusable access grants.',
        action: 'New group'
      }
    case 'bindings':
      return {
        title: 'Role bindings',
        description: 'Grant platform-wide or tenant-scoped access to users and groups.',
        action: 'New binding'
      }
  }
}

function openNewCurrentSection() {
  clearStatus()
  if (activeSection.value === 'providers') {
    resetProviderForm()
  } else if (activeSection.value === 'users') {
    resetUserForm()
  } else if (activeSection.value === 'groups') {
    resetGroupForm()
  } else {
    resetBindingForm()
  }
}

watch(activeSection, () => {
  clearStatus()
  search.value = ''
})

watch(providerRows, (rows) => {
  if (selectedProviderName.value && !rows.some((item) => item.name === selectedProviderName.value)) {
    selectedProviderName.value = null
  }
})

watch(userRows, (rows) => {
  if (selectedUserName.value && !rows.some((item) => item.name === selectedUserName.value)) {
    selectedUserName.value = null
  }
})

watch(groupRows, (rows) => {
  if (selectedGroupName.value && !rows.some((item) => item.name === selectedGroupName.value)) {
    selectedGroupName.value = null
  }
})

watch(bindingRows, (rows) => {
  if (selectedBindingName.value && !rows.some((item) => item.name === selectedBindingName.value)) {
    selectedBindingName.value = null
  }
})

resetProviderForm()
resetUserForm()
resetGroupForm()
resetBindingForm()
</script>

<template>
  <div class="stack-gap">
    <section class="content-band auth-admin-hero">
      <div>
        <p class="eyebrow">Identity & access</p>
        <h2>Enterprise authentication workspace</h2>
        <p class="muted">
          Configure providers, manage internal identities, and grant least-privilege access across tenants.
        </p>
      </div>
      <div class="auth-metric-grid">
        <div v-for="metric in metrics" :key="metric.label" class="auth-metric-card">
          <span>{{ metric.label }}</span>
          <strong>{{ metric.value }}</strong>
          <small>{{ metric.note }}</small>
        </div>
      </div>
    </section>

    <section class="content-band">
      <div class="auth-toolbar">
        <div class="tab-strip auth-subtabs">
          <button
            v-for="section in (['providers', 'users', 'groups', 'bindings'] as const)"
            :key="section"
            class="tab-btn"
            :class="{ active: activeSection === section }"
            @click="activeSection = section"
          >
            {{ sectionMeta(section).title }}
          </button>
        </div>
        <input
          v-model="search"
          class="auth-search"
          type="search"
          :placeholder="`Search ${sectionMeta(activeSection).title.toLowerCase()}...`"
        />
      </div>

      <p v-if="error" class="error-text" style="margin-bottom: 12px">{{ error }}</p>
      <p v-if="success" class="success-text" style="margin-bottom: 12px">{{ success }}</p>

      <div class="auth-workspace">
        <div class="auth-list-pane">
          <div class="auth-pane-header">
            <div>
              <h3>{{ sectionMeta(activeSection).title }}</h3>
              <p class="muted">{{ sectionMeta(activeSection).description }}</p>
            </div>
            <button class="button primary" :disabled="busy" @click="openNewCurrentSection">
              {{ sectionMeta(activeSection).action }}
            </button>
          </div>

          <table v-if="activeSection === 'providers'" class="data-table auth-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Type</th>
                <th>State</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="provider in filteredProviders"
                :key="provider.name"
                class="clickable-row"
                :class="{ selected: selectedProviderName === provider.name }"
                @click="editProvider(provider)"
              >
                <td><strong>{{ provider.displayName }}</strong><small>{{ provider.name }}</small></td>
                <td>{{ provider.type }}</td>
                <td>
                  <span class="status-pill" :class="provider.enabled ? 'good' : 'neutral'">
                    {{ provider.enabled ? 'Enabled' : 'Disabled' }}
                  </span>
                </td>
                <td class="table-actions">
                  <button class="button text danger" @click.stop="deleteProvider(provider.name)">Remove</button>
                </td>
              </tr>
            </tbody>
          </table>

          <table v-else-if="activeSection === 'users'" class="data-table auth-table">
            <thead>
              <tr>
                <th>User</th>
                <th>Local</th>
                <th>External</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="user in filteredUsers"
                :key="user.name"
                class="clickable-row"
                :class="{ selected: selectedUserName === user.name }"
                @click="editUser(user)"
              >
                <td><strong>{{ user.displayName || user.name }}</strong><small>{{ user.email || user.name }}</small></td>
                <td>{{ user.localAuthEnabled ? 'Yes' : 'No' }}</td>
                <td>{{ user.externalIdentities?.length || 0 }}</td>
                <td class="table-actions">
                  <button class="button text danger" @click.stop="deleteUser(user.name)">Remove</button>
                </td>
              </tr>
            </tbody>
          </table>

          <table v-else-if="activeSection === 'groups'" class="data-table auth-table">
            <thead>
              <tr>
                <th>Group</th>
                <th>Members</th>
                <th>Mapped</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="group in filteredGroups"
                :key="group.name"
                class="clickable-row"
                :class="{ selected: selectedGroupName === group.name }"
                @click="editGroup(group)"
              >
                <td><strong>{{ group.displayName || group.name }}</strong><small>{{ group.name }}</small></td>
                <td>{{ group.members?.length || 0 }}</td>
                <td>{{ group.externalGroups?.length || 0 }}</td>
                <td class="table-actions">
                  <button class="button text danger" @click.stop="deleteGroup(group.name)">Remove</button>
                </td>
              </tr>
            </tbody>
          </table>

          <table v-else class="data-table auth-table">
            <thead>
              <tr>
                <th>Binding</th>
                <th>Scope</th>
                <th>Roles</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="binding in filteredBindings"
                :key="binding.name"
                class="clickable-row"
                :class="{ selected: selectedBindingName === binding.name }"
                @click="editBinding(binding)"
              >
                <td><strong>{{ binding.displayName || binding.name }}</strong><small>{{ binding.name }}</small></td>
                <td>{{ binding.scope }}<span v-if="binding.tenantName"> · {{ binding.tenantName }}</span></td>
                <td>{{ binding.roles.join(', ') }}</td>
                <td class="table-actions">
                  <button class="button text danger" @click.stop="deleteBinding(binding.name)">Remove</button>
                </td>
              </tr>
            </tbody>
          </table>

          <div
            v-if="
              (activeSection === 'providers' && filteredProviders.length === 0) ||
              (activeSection === 'users' && filteredUsers.length === 0) ||
              (activeSection === 'groups' && filteredGroups.length === 0) ||
              (activeSection === 'bindings' && filteredBindings.length === 0)
            "
            class="empty-state auth-empty"
          >
            <p>No matching {{ sectionMeta(activeSection).title.toLowerCase() }}.</p>
          </div>
        </div>

        <div class="content-band auth-detail-pane">
          <template v-if="activeSection === 'providers'">
            <div class="auth-pane-header">
              <div>
                <h3>{{ editingProvider ? 'Edit provider' : 'New provider' }}</h3>
                <p class="muted">
                  {{ selectedProvider?.type === 'oidc' ? 'Identity redirect provider' : selectedProvider?.type === 'ldap' ? 'Directory-backed provider' : 'Platform-managed credential provider' }}
                </p>
              </div>
              <button class="button secondary" @click="resetProviderForm">Reset</button>
            </div>

            <div class="form-grid modal-form-grid">
              <label>
                Provider name
                <input v-model="providerForm.name" :disabled="!!editingProvider" placeholder="corp-sso" />
              </label>
              <label>
                Display name
                <input v-model="providerForm.displayName" placeholder="Corporate SSO" />
              </label>
              <label>
                Type
                <select v-model="providerForm.type" :disabled="!!editingProvider">
                  <option value="local">local</option>
                  <option value="oidc">oidc</option>
                  <option value="ldap">ldap</option>
                </select>
              </label>
              <div class="auth-checkbox-stack">
                <label><input v-model="providerForm.enabled" type="checkbox" /> Enabled</label>
                <label><input v-model="providerForm.default" type="checkbox" /> Default provider</label>
              </div>
            </div>

            <div v-if="providerForm.type === 'oidc'" class="auth-config-card">
              <h4>OIDC settings</h4>
              <div class="form-grid modal-form-grid">
                <label><span>Issuer URL</span><input v-model="providerForm.oidcIssuerUrl" placeholder="https://issuer.example.com" /></label>
                <label><span>Client ID</span><input v-model="providerForm.oidcClientId" placeholder="servicer-web" /></label>
                <label style="grid-column: 1 / -1"><span>Client secret</span><input v-model="providerForm.oidcClientSecret" type="password" placeholder="Stored in Kubernetes Secret on save" /></label>
                <label><span>Scopes</span><input v-model="providerForm.oidcScopes" placeholder="openid profile email offline_access" /></label>
                <label><span>Redirect path</span><input v-model="providerForm.oidcRedirectPath" placeholder="/api/auth/callback" /></label>
                <label><span>Username claim</span><input v-model="providerForm.oidcUsernameClaim" placeholder="preferred_username" /></label>
                <label><span>Email claim</span><input v-model="providerForm.oidcEmailClaim" placeholder="email" /></label>
                <label><span>Roles claim</span><input v-model="providerForm.oidcRolesClaim" placeholder="roles" /></label>
                <label><span>Groups claim</span><input v-model="providerForm.oidcGroupsClaim" placeholder="groups" /></label>
                <label style="grid-column: 1 / -1"><span>End-session URL</span><input v-model="providerForm.oidcEndSessionUrl" placeholder="Optional provider logout endpoint" /></label>
              </div>
            </div>

            <div v-if="providerForm.type === 'ldap'" class="auth-config-card">
              <h4>LDAP settings</h4>
              <div class="form-grid modal-form-grid">
                <label><span>LDAP URL</span><input v-model="providerForm.ldapUrl" placeholder="ldaps://ldap.example.com:636" /></label>
                <label><span>Bind DN</span><input v-model="providerForm.ldapBindUsername" placeholder="cn=svc,dc=example,dc=com" /></label>
                <label style="grid-column: 1 / -1"><span>Bind password</span><input v-model="providerForm.ldapBindPassword" type="password" placeholder="Stored in Kubernetes Secret on save" /></label>
                <label><span>User base DN</span><input v-model="providerForm.ldapUserBaseDn" placeholder="ou=people,dc=example,dc=com" /></label>
                <label><span>User filter</span><input v-model="providerForm.ldapUserFilter" placeholder="(uid=%s)" /></label>
                <label><span>Username attribute</span><input v-model="providerForm.ldapUsernameAttribute" placeholder="uid" /></label>
                <label><span>Email attribute</span><input v-model="providerForm.ldapEmailAttribute" placeholder="mail" /></label>
                <label><span>Group base DN</span><input v-model="providerForm.ldapGroupBaseDn" placeholder="ou=groups,dc=example,dc=com" /></label>
                <label><span>Group filter</span><input v-model="providerForm.ldapGroupFilter" placeholder="(member=%s)" /></label>
                <label><span>Group name attribute</span><input v-model="providerForm.ldapGroupNameAttribute" placeholder="cn" /></label>
                <div class="auth-checkbox-stack">
                  <label><input v-model="providerForm.ldapStartTls" type="checkbox" /> StartTLS</label>
                  <label><input v-model="providerForm.insecureSkipVerify" type="checkbox" /> Insecure TLS</label>
                </div>
              </div>
            </div>

            <div class="form-actions">
              <button class="button primary" :disabled="busy" @click="submitProvider">
                {{ editingProvider ? 'Save provider' : 'Create provider' }}
              </button>
            </div>
          </template>

          <template v-else-if="activeSection === 'users'">
            <div class="auth-pane-header">
              <div>
                <h3>{{ editingUser ? 'Edit user' : 'New user' }}</h3>
                <p class="muted">Link enterprise identities to a Servicer user and optionally keep a local break-glass password.</p>
              </div>
              <button class="button secondary" @click="resetUserForm">Reset</button>
            </div>

            <div class="form-grid modal-form-grid">
              <label><span>Username</span><input v-model="userForm.name" :disabled="!!editingUser" placeholder="alice" /></label>
              <label><span>Display name</span><input v-model="userForm.displayName" placeholder="Alice Johnson" /></label>
              <label><span>Email</span><input v-model="userForm.email" placeholder="alice@example.com" /></label>
              <div class="auth-checkbox-stack">
                <label><input v-model="userForm.localAuthEnabled" type="checkbox" /> Local password enabled</label>
              </div>
              <label v-if="userForm.localAuthEnabled" style="grid-column: 1 / -1">
                <span>{{ editingUser ? 'Rotate password' : 'Initial password' }}</span>
                <input v-model="userForm.password" type="password" :placeholder="editingUser ? 'Enter a new password to rotate credentials' : 'Password'" />
              </label>
              <label style="grid-column: 1 / -1">
                <span>External identities</span>
                <textarea v-model="userForm.externalIdentitiesRaw" class="defaults-textarea" placeholder="oidc:00u1234567&#10;ldap:uid=alice,ou=people,dc=example,dc=com" />
              </label>
            </div>

            <div class="form-actions">
              <button class="button primary" :disabled="busy" @click="submitUser">
                {{ editingUser ? 'Save user' : 'Create user' }}
              </button>
            </div>
          </template>

          <template v-else-if="activeSection === 'groups'">
            <div class="auth-pane-header">
              <div>
                <h3>{{ editingGroup ? 'Edit group' : 'New group' }}</h3>
                <p class="muted">Use groups to aggregate operators and map upstream directory membership into Servicer access control.</p>
              </div>
              <button class="button secondary" @click="resetGroupForm">Reset</button>
            </div>

            <div class="form-grid modal-form-grid">
              <label><span>Group name</span><input v-model="groupForm.name" :disabled="!!editingGroup" placeholder="platform-operators" /></label>
              <label><span>Display name</span><input v-model="groupForm.displayName" placeholder="Platform Operators" /></label>
              <label style="grid-column: 1 / -1">
                <span>Members</span>
                <textarea v-model="groupForm.membersRaw" class="defaults-textarea" placeholder="alice&#10;bob" />
              </label>
              <label style="grid-column: 1 / -1">
                <span>External group mappings</span>
                <textarea v-model="groupForm.externalGroupsRaw" class="defaults-textarea" placeholder="oidc:platform-admins&#10;ldap:cn=platform-admins,ou=groups,dc=example,dc=com" />
              </label>
            </div>

            <div class="form-actions">
              <button class="button primary" :disabled="busy" @click="submitGroup">
                {{ editingGroup ? 'Save group' : 'Create group' }}
              </button>
            </div>
          </template>

          <template v-else>
            <div class="auth-pane-header">
              <div>
                <h3>{{ editingBinding ? 'Edit role binding' : 'New role binding' }}</h3>
                <p class="muted">Grant platform-level or tenant-level access to users and groups with explicit role assignment.</p>
              </div>
              <button class="button secondary" @click="resetBindingForm">Reset</button>
            </div>

            <div class="form-grid modal-form-grid">
              <label><span>Binding name</span><input v-model="bindingForm.name" :disabled="!!editingBinding" placeholder="demo-tenant-operators" /></label>
              <label><span>Display name</span><input v-model="bindingForm.displayName" placeholder="Demo tenant operators" /></label>
              <label>
                <span>Scope</span>
                <select v-model="bindingForm.scope">
                  <option value="platform">platform</option>
                  <option value="tenant">tenant</option>
                </select>
              </label>
              <label v-if="bindingForm.scope === 'tenant'">
                <span>Tenant</span>
                <select v-model="bindingForm.tenantName">
                  <option v-for="tenant in tenantRows" :key="tenant.name" :value="tenant.name">
                    {{ tenant.displayName }}
                  </option>
                </select>
              </label>
              <label style="grid-column: 1 / -1">
                <span>Subjects</span>
                <textarea v-model="bindingForm.subjectsRaw" class="defaults-textarea" placeholder="User:alice&#10;Group:platform-operators" />
              </label>
              <label style="grid-column: 1 / -1">
                <span>Roles</span>
                <input v-model="bindingForm.rolesRaw" placeholder="tenant-admin, tenant-operator" />
              </label>
            </div>

            <div class="form-actions">
              <button class="button primary" :disabled="busy" @click="submitBinding">
                {{ editingBinding ? 'Save binding' : 'Create binding' }}
              </button>
            </div>
          </template>
        </div>
      </div>
    </section>
  </div>
</template>
