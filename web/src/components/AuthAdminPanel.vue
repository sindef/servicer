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

type AuthSection = 'users' | 'groups' | 'bindings' | 'providers'
const authSections = ['users', 'groups', 'bindings', 'providers'] as const

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
const platformRoleOptions = [
  { name: 'platform-admin', description: 'Full platform administration.' },
  { name: 'catalog-admin', description: 'Manage catalog publication and defaults.' },
  { name: 'cluster-admin', description: 'Manage cluster targets.' },
  { name: 'auditor', description: 'Read audit events.' }
]
const tenantRoleOptions = [
  { name: 'tenant-admin', description: 'Manage tenant-scoped projects, repositories, and access.' },
  { name: 'tenant-operator', description: 'Operate tenant products and repositories.' },
  { name: 'service-consumer', description: 'View and request assigned tenant products.' }
]
const bindingRoleOptions = computed(() =>
  bindingForm.scope === 'platform' ? platformRoleOptions : tenantRoleOptions
)

const activeSection = ref<AuthSection>('users')
const modalSection = ref<AuthSection | null>(null)
const modalMode = ref<'create' | 'edit'>('create')
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

function sectionCount(section: AuthSection) {
  switch (section) {
    case 'providers':
      return providerRows.value.length
    case 'users':
      return userRows.value.length
    case 'groups':
      return groupRows.value.length
    case 'bindings':
      return bindingRows.value.length
  }
}

function modalTitle(section: AuthSection) {
  const noun =
    section === 'providers'
      ? 'provider'
      : section === 'users'
        ? 'user'
        : section === 'groups'
          ? 'group'
          : 'role binding'
  return `${modalMode.value === 'create' ? 'New' : 'Edit'} ${noun}`
}

const sectionSearchPlaceholder = computed(() => {
  switch (activeSection.value) {
    case 'providers':
      return 'Search providers'
    case 'users':
      return 'Search users'
    case 'groups':
      return 'Search groups'
    case 'bindings':
      return 'Search bindings'
  }
})

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
const selectedProviderNames = ref<string[]>([])
const selectedUserNames = ref<string[]>([])
const selectedGroupNames = ref<string[]>([])
const selectedBindingNames = ref<string[]>([])

const selectedProvider = computed(() => providerRows.value.find((item) => item.name === selectedProviderName.value) ?? null)
const selectedUser = computed(() => userRows.value.find((item) => item.name === selectedUserName.value) ?? null)
const selectedGroup = computed(() => groupRows.value.find((item) => item.name === selectedGroupName.value) ?? null)
const selectedBinding = computed(() => bindingRows.value.find((item) => item.name === selectedBindingName.value) ?? null)
const activeSelectedCount = computed(() => selectionFor(activeSection.value).length)

function checkboxValue(event: Event) {
  return (event.target as HTMLInputElement).checked
}

function selectionFor(section: AuthSection) {
  switch (section) {
    case 'providers':
      return selectedProviderNames.value
    case 'users':
      return selectedUserNames.value
    case 'groups':
      return selectedGroupNames.value
    case 'bindings':
      return selectedBindingNames.value
  }
}

function setSelection(section: AuthSection, names: string[]) {
  const uniqueNames = [...new Set(names)]
  switch (section) {
    case 'providers':
      selectedProviderNames.value = uniqueNames
      break
    case 'users':
      selectedUserNames.value = uniqueNames
      break
    case 'groups':
      selectedGroupNames.value = uniqueNames
      break
    case 'bindings':
      selectedBindingNames.value = uniqueNames
      break
  }
}

function visibleNamesFor(section: AuthSection) {
  switch (section) {
    case 'providers':
      return filteredProviders.value.map((item) => item.name)
    case 'users':
      return filteredUsers.value.map((item) => item.name)
    case 'groups':
      return filteredGroups.value.map((item) => item.name)
    case 'bindings':
      return filteredBindings.value.map((item) => item.name)
  }
}

function isSelected(section: AuthSection, name: string) {
  return selectionFor(section).includes(name)
}

function toggleSelected(section: AuthSection, name: string, checked: boolean) {
  const current = selectionFor(section)
  setSelection(section, checked ? [...current, name] : current.filter((item) => item !== name))
}

function toggleVisibleSelection(section: AuthSection, checked: boolean) {
  const visibleNames = visibleNamesFor(section)
  if (!checked) {
    setSelection(section, selectionFor(section).filter((name) => !visibleNames.includes(name)))
    return
  }
  setSelection(section, [...selectionFor(section), ...visibleNames])
}

function allVisibleSelected(section: AuthSection) {
  const visibleNames = visibleNamesFor(section)
  const selectedNames = selectionFor(section)
  return visibleNames.length > 0 && visibleNames.every((name) => selectedNames.includes(name))
}

function clearSelected(section: AuthSection) {
  setSelection(section, [])
}

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

function openEditProvider(provider: AuthProviderSummary) {
  clearStatus()
  editProvider(provider)
  modalMode.value = 'edit'
  modalSection.value = 'providers'
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

function openEditUser(user: UserSummary) {
  clearStatus()
  editUser(user)
  modalMode.value = 'edit'
  modalSection.value = 'users'
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
  members: [] as string[],
  externalGroupsRaw: ''
})
const groupMemberSearch = ref('')
const filteredGroupMemberOptions = computed(() => {
  const term = groupMemberSearch.value.trim().toLowerCase()
  return userRows.value.filter((user) => {
    if (!term) return true
    return [user.name, user.displayName, user.email].some((value) => (value ?? '').toLowerCase().includes(term))
  })
})

function resetGroupForm() {
  editingGroup.value = null
  groupMemberSearch.value = ''
  Object.assign(groupForm, { name: '', displayName: '', members: [], externalGroupsRaw: '' })
}

function editGroup(group: GroupSummary) {
  activeSection.value = 'groups'
  selectedGroupName.value = group.name
  editingGroup.value = group.name
  Object.assign(groupForm, {
    name: group.name,
    displayName: group.displayName ?? '',
    members: [...(group.members ?? [])],
    externalGroupsRaw: (group.externalGroups ?? []).map((external) => `${external.provider}:${external.name}`).join('\n')
  })
}

function openEditGroup(group: GroupSummary) {
  clearStatus()
  editGroup(group)
  modalMode.value = 'edit'
  modalSection.value = 'groups'
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
    members: groupForm.members,
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
  subjects: [] as RoleBindingRequest['subjects'],
  roles: ['tenant-operator'] as string[]
})
const bindingSubjectSearch = ref('')
const bindingSubjectOptions = computed(() => {
  const users = userRows.value.map((user) => ({
    kind: 'User' as const,
    name: user.name,
    label: user.displayName || user.name,
    detail: user.email || user.name
  }))
  const groupOptions = groupRows.value.map((group) => ({
    kind: 'Group' as const,
    name: group.name,
    label: group.displayName || group.name,
    detail: `${group.members?.length || 0} members`
  }))
  const term = bindingSubjectSearch.value.trim().toLowerCase()
  return [...users, ...groupOptions].filter((subject) => {
    if (!term) return true
    return [subject.kind, subject.name, subject.label, subject.detail].some((value) => value.toLowerCase().includes(term))
  })
})

function resetBindingForm() {
  editingBinding.value = null
  bindingSubjectSearch.value = ''
  Object.assign(bindingForm, {
    name: '',
    displayName: '',
    scope: 'tenant',
    tenantName: tenantRows.value[0]?.name ?? '',
    subjects: [],
    roles: ['tenant-operator']
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
    subjects: binding.subjects.map((subject) => ({ ...subject })),
    roles: [...binding.roles]
  })
}

function openEditBinding(binding: RoleBindingSummary) {
  clearStatus()
  editBinding(binding)
  modalMode.value = 'edit'
  modalSection.value = 'bindings'
}

async function submitBinding() {
  const body: RoleBindingRequest = {
    name: bindingForm.name,
    displayName: bindingForm.displayName || undefined,
    scope: bindingForm.scope,
    tenantName: bindingForm.scope === 'tenant' ? bindingForm.tenantName || undefined : undefined,
    subjects: bindingForm.subjects,
    roles: bindingForm.roles
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

function toggleStringValue(values: string[], value: string, checked: boolean) {
  const current = new Set(values)
  if (checked) {
    current.add(value)
  } else {
    current.delete(value)
  }
  values.splice(0, values.length, ...Array.from(current))
}

function subjectKey(subject: RoleBindingRequest['subjects'][number]) {
  return `${subject.kind}:${subject.name}`
}

function toggleBindingSubject(subject: RoleBindingRequest['subjects'][number], checked: boolean) {
  const key = subjectKey(subject)
  if (checked) {
    if (!bindingForm.subjects.some((item) => subjectKey(item) === key)) {
      bindingForm.subjects.push({ ...subject })
    }
    return
  }
  const index = bindingForm.subjects.findIndex((item) => subjectKey(item) === key)
  if (index !== -1) {
    bindingForm.subjects.splice(index, 1)
  }
}

function bindingSubjectSelected(subject: RoleBindingRequest['subjects'][number]) {
  const key = subjectKey(subject)
  return bindingForm.subjects.some((item) => subjectKey(item) === key)
}

watch(() => bindingForm.scope, (scope) => {
  const allowed = new Set((scope === 'platform' ? platformRoleOptions : tenantRoleOptions).map((role) => role.name))
  bindingForm.roles = bindingForm.roles.filter((role) => allowed.has(role))
  if (!bindingForm.roles.length) {
    bindingForm.roles = [scope === 'platform' ? 'platform-admin' : 'tenant-operator']
  }
})

async function deleteBinding(name: string) {
  if (!window.confirm(`Delete role binding ${name}?`)) return
  await runWrite(() => api.admin.deleteRoleBinding(name), bindings.reload)
  if (!error.value && selectedBindingName.value === name) {
    selectedBindingName.value = null
    resetBindingForm()
  }
}

async function deleteSelected(section: AuthSection) {
  const names = [...selectionFor(section)]
  if (!names.length) return
  const label = sectionItemLabel(section, names.length)
  if (!window.confirm(`Remove ${names.length} ${label}?`)) return

  busy.value = true
  clearStatus()
  try {
    for (const name of names) {
      if (section === 'providers') {
        await api.admin.deleteAuthProvider(name)
      } else if (section === 'users') {
        await api.admin.deleteUser(name)
      } else if (section === 'groups') {
        await api.admin.deleteGroup(name)
      } else {
        await api.admin.deleteRoleBinding(name)
      }
    }
    clearSelected(section)
    if (section === 'providers') {
      if (selectedProviderName.value && names.includes(selectedProviderName.value)) {
        selectedProviderName.value = null
        resetProviderForm()
      }
      await providers.reload()
    } else if (section === 'users') {
      if (selectedUserName.value && names.includes(selectedUserName.value)) {
        selectedUserName.value = null
        resetUserForm()
      }
      await users.reload()
    } else if (section === 'groups') {
      if (selectedGroupName.value && names.includes(selectedGroupName.value)) {
        selectedGroupName.value = null
        resetGroupForm()
      }
      await groups.reload()
    } else {
      if (selectedBindingName.value && names.includes(selectedBindingName.value)) {
        selectedBindingName.value = null
        resetBindingForm()
      }
      await bindings.reload()
    }
    success.value = `Removed ${names.length} ${label}.`
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Bulk remove failed'
  } finally {
    busy.value = false
  }
}

function sectionItemLabel(section: AuthSection, count: number) {
  const plural = count !== 1
  switch (section) {
    case 'providers':
      return plural ? 'auth providers' : 'auth provider'
    case 'users':
      return plural ? 'users' : 'user'
    case 'groups':
      return plural ? 'groups' : 'group'
    case 'bindings':
      return plural ? 'role bindings' : 'role binding'
  }
}

function sectionMeta(section: AuthSection) {
  switch (section) {
    case 'providers':
      return {
        title: 'Auth providers',
        description: 'Local, OIDC, and LDAP sign-in methods.',
        action: 'New provider'
      }
    case 'users':
      return {
        title: 'Users',
        description: 'People who can sign in.',
        action: 'New user'
      }
    case 'groups':
      return {
        title: 'Groups',
        description: 'Reusable collections of users.',
        action: 'New group'
      }
    case 'bindings':
      return {
        title: 'Role bindings',
        description: 'Roles assigned to users and groups.',
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
  modalMode.value = 'create'
  modalSection.value = activeSection.value
}

function closeModal() {
  modalSection.value = null
}

async function submitCreateProvider() {
  await submitProvider()
  if (!error.value) closeModal()
}

async function submitCreateUser() {
  await submitUser()
  if (!error.value) closeModal()
}

async function submitCreateGroup() {
  await submitGroup()
  if (!error.value) closeModal()
}

async function submitCreateBinding() {
  await submitBinding()
  if (!error.value) closeModal()
}

async function submitEditProvider() {
  await submitProvider()
  if (!error.value) closeModal()
}

async function submitEditUser() {
  await submitUser()
  if (!error.value) closeModal()
}

async function submitEditGroup() {
  await submitGroup()
  if (!error.value) closeModal()
}

async function submitEditBinding() {
  await submitBinding()
  if (!error.value) closeModal()
}

watch(activeSection, () => {
  clearStatus()
  search.value = ''
  closeModal()
})

watch(providerRows, (rows) => {
  if (selectedProviderName.value && !rows.some((item) => item.name === selectedProviderName.value)) {
    selectedProviderName.value = null
  }
  selectedProviderNames.value = selectedProviderNames.value.filter((name) => rows.some((item) => item.name === name))
})

watch(userRows, (rows) => {
  if (selectedUserName.value && !rows.some((item) => item.name === selectedUserName.value)) {
    selectedUserName.value = null
  }
  selectedUserNames.value = selectedUserNames.value.filter((name) => rows.some((item) => item.name === name))
})

watch(groupRows, (rows) => {
  if (selectedGroupName.value && !rows.some((item) => item.name === selectedGroupName.value)) {
    selectedGroupName.value = null
  }
  selectedGroupNames.value = selectedGroupNames.value.filter((name) => rows.some((item) => item.name === name))
})

watch(bindingRows, (rows) => {
  if (selectedBindingName.value && !rows.some((item) => item.name === selectedBindingName.value)) {
    selectedBindingName.value = null
  }
  selectedBindingNames.value = selectedBindingNames.value.filter((name) => rows.some((item) => item.name === name))
})

resetProviderForm()
resetUserForm()
resetGroupForm()
resetBindingForm()
</script>

<template>
  <div class="stack-gap">
    <section class="auth-admin-header">
      <p class="eyebrow">Auth</p>
      <h2>Authentication</h2>
    </section>

    <section class="content-band">
      <div class="auth-toolbar">
        <div class="tab-strip auth-subtabs">
          <button
            v-for="section in authSections"
            :key="section"
            class="tab-btn"
            :class="{ active: activeSection === section }"
            @click="activeSection = section"
          >
            <span>{{ sectionMeta(section).title }}</span>
            <span class="tab-count">{{ sectionCount(section) }}</span>
          </button>
        </div>
      </div>

      <p v-if="error" class="error-text" style="margin-bottom: 12px">{{ error }}</p>
      <p v-if="success" class="success-text" style="margin-bottom: 12px">{{ success }}</p>

      <div class="auth-list-pane">
        <div class="auth-pane-header">
          <div>
            <h3>{{ sectionMeta(activeSection).title }}</h3>
          </div>
          <div class="auth-pane-controls">
            <input
              v-model="search"
              class="auth-search"
              type="search"
              :placeholder="sectionSearchPlaceholder"
            />
            <button class="button primary" :disabled="busy" @click="openNewCurrentSection">
              {{ sectionMeta(activeSection).action }}
            </button>
          </div>
        </div>

        <div v-if="activeSelectedCount" class="auth-bulk-bar">
          <strong>{{ activeSelectedCount }} selected</strong>
          <button class="button secondary compact-button" :disabled="busy" @click="clearSelected(activeSection)">Clear</button>
          <button class="button secondary compact-button auth-remove-action" :disabled="busy" @click="deleteSelected(activeSection)">
            Remove selected
          </button>
        </div>

        <table v-if="activeSection === 'providers'" class="data-table auth-table">
            <thead>
              <tr>
                <th class="auth-select-col">
                  <input
                    aria-label="Select all visible providers"
                    class="auth-row-checkbox"
                    type="checkbox"
                    :checked="allVisibleSelected('providers')"
                    @change="toggleVisibleSelection('providers', checkboxValue($event))"
                  />
                </th>
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
              >
                <td class="auth-select-col">
                  <input
                    :aria-label="`Select provider ${provider.displayName || provider.name}`"
                    class="auth-row-checkbox"
                    type="checkbox"
                    :checked="isSelected('providers', provider.name)"
                    @click.stop
                    @change.stop="toggleSelected('providers', provider.name, checkboxValue($event))"
                  />
                </td>
                <td><strong>{{ provider.displayName }}</strong><small>{{ provider.name }}</small></td>
                <td>{{ provider.type }}</td>
                <td>
                  <span class="status-pill" :class="provider.enabled ? 'good' : 'neutral'">
                    {{ provider.enabled ? 'Enabled' : 'Disabled' }}
                  </span>
                </td>
                <td class="table-actions auth-table-actions">
                  <button class="button secondary compact-button" @click="openEditProvider(provider)">Edit</button>
                  <button class="button secondary compact-button auth-remove-action" @click="deleteProvider(provider.name)">Remove</button>
                </td>
              </tr>
            </tbody>
        </table>

        <table v-else-if="activeSection === 'users'" class="data-table auth-table">
            <thead>
              <tr>
                <th class="auth-select-col">
                  <input
                    aria-label="Select all visible users"
                    class="auth-row-checkbox"
                    type="checkbox"
                    :checked="allVisibleSelected('users')"
                    @change="toggleVisibleSelection('users', checkboxValue($event))"
                  />
                </th>
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
              >
                <td class="auth-select-col">
                  <input
                    :aria-label="`Select user ${user.displayName || user.name}`"
                    class="auth-row-checkbox"
                    type="checkbox"
                    :checked="isSelected('users', user.name)"
                    @click.stop
                    @change.stop="toggleSelected('users', user.name, checkboxValue($event))"
                  />
                </td>
                <td><strong>{{ user.displayName || user.name }}</strong><small>{{ user.email || user.name }}</small></td>
                <td>{{ user.localAuthEnabled ? 'Yes' : 'No' }}</td>
                <td>{{ user.externalIdentities?.length || 0 }}</td>
                <td class="table-actions auth-table-actions">
                  <button class="button secondary compact-button" @click="openEditUser(user)">Edit</button>
                  <button class="button secondary compact-button auth-remove-action" @click="deleteUser(user.name)">Remove</button>
                </td>
              </tr>
            </tbody>
        </table>

        <table v-else-if="activeSection === 'groups'" class="data-table auth-table">
            <thead>
              <tr>
                <th class="auth-select-col">
                  <input
                    aria-label="Select all visible groups"
                    class="auth-row-checkbox"
                    type="checkbox"
                    :checked="allVisibleSelected('groups')"
                    @change="toggleVisibleSelection('groups', checkboxValue($event))"
                  />
                </th>
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
              >
                <td class="auth-select-col">
                  <input
                    :aria-label="`Select group ${group.displayName || group.name}`"
                    class="auth-row-checkbox"
                    type="checkbox"
                    :checked="isSelected('groups', group.name)"
                    @click.stop
                    @change.stop="toggleSelected('groups', group.name, checkboxValue($event))"
                  />
                </td>
                <td><strong>{{ group.displayName || group.name }}</strong><small>{{ group.name }}</small></td>
                <td>{{ group.members?.length || 0 }}</td>
                <td>{{ group.externalGroups?.length || 0 }}</td>
                <td class="table-actions auth-table-actions">
                  <button class="button secondary compact-button" @click="openEditGroup(group)">Edit</button>
                  <button class="button secondary compact-button auth-remove-action" @click="deleteGroup(group.name)">Remove</button>
                </td>
              </tr>
            </tbody>
        </table>

        <table v-else class="data-table auth-table">
            <thead>
              <tr>
                <th class="auth-select-col">
                  <input
                    aria-label="Select all visible role bindings"
                    class="auth-row-checkbox"
                    type="checkbox"
                    :checked="allVisibleSelected('bindings')"
                    @change="toggleVisibleSelection('bindings', checkboxValue($event))"
                  />
                </th>
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
              >
                <td class="auth-select-col">
                  <input
                    :aria-label="`Select role binding ${binding.displayName || binding.name}`"
                    class="auth-row-checkbox"
                    type="checkbox"
                    :checked="isSelected('bindings', binding.name)"
                    @click.stop
                    @change.stop="toggleSelected('bindings', binding.name, checkboxValue($event))"
                  />
                </td>
                <td><strong>{{ binding.displayName || binding.name }}</strong><small>{{ binding.name }}</small></td>
                <td>{{ binding.scope }}<span v-if="binding.tenantName"> · {{ binding.tenantName }}</span></td>
                <td>{{ binding.roles.join(', ') }}</td>
                <td class="table-actions auth-table-actions">
                  <button class="button secondary compact-button" @click="openEditBinding(binding)">Edit</button>
                  <button class="button secondary compact-button auth-remove-action" @click="deleteBinding(binding.name)">Remove</button>
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
    </section>

    <div v-if="modalSection" class="modal-backdrop">
      <div class="modal-panel auth-modal-panel">
        <div class="modal-head">
          <div>
            <p class="eyebrow">{{ modalMode === 'create' ? 'Create' : 'Edit' }}</p>
            <h2>{{ modalTitle(modalSection) }}</h2>
            <p class="muted">{{ sectionMeta(modalSection).description }}</p>
          </div>
          <button class="button secondary" :disabled="busy" @click="closeModal">Close</button>
        </div>

        <template v-if="modalSection === 'providers'">
          <div class="auth-modal-body">
            <section class="auth-modal-section">
              <div class="auth-section-heading">
                <h3>Sign-in method</h3>
                <p>Choose how users authenticate with this provider.</p>
              </div>
              <div class="auth-method-picker">
                <button
                  v-for="type in (['local', 'oidc', 'ldap'] as const)"
                  :key="type"
                  class="auth-method-option"
                  :class="{ active: providerForm.type === type }"
                  :disabled="modalMode === 'edit'"
                  type="button"
                  @click="providerForm.type = type"
                >
                  <strong>{{ type.toUpperCase() }}</strong>
                  <span v-if="type === 'local'">Servicer password users</span>
                  <span v-else-if="type === 'oidc'">Browser SSO redirect</span>
                  <span v-else>Directory bind</span>
                </button>
              </div>
            </section>

            <section class="auth-modal-section">
              <div class="auth-section-heading">
                <h3>Provider details</h3>
              </div>
              <div class="auth-modal-grid">
                <label>
                  <span>Provider name</span>
                  <input v-model="providerForm.name" :disabled="modalMode === 'edit'" placeholder="corp-sso" />
                </label>
                <label>
                  <span>Display name</span>
                  <input v-model="providerForm.displayName" placeholder="Corporate SSO" />
                </label>
              </div>

              <div class="auth-switch-grid">
                <label class="auth-switch">
                  <input v-model="providerForm.enabled" type="checkbox" />
                  <span class="auth-switch-control"></span>
                  <span>
                    <strong>Enabled</strong>
                    <small>Show on login.</small>
                  </span>
                </label>
                <label class="auth-switch">
                  <input v-model="providerForm.default" type="checkbox" />
                  <span class="auth-switch-control"></span>
                  <span>
                    <strong>Default provider</strong>
                    <small>Preselect on login.</small>
                  </span>
                </label>
              </div>
            </section>

            <section v-if="providerForm.type === 'oidc'" class="auth-modal-section">
              <div class="auth-section-heading">
                <h3>OIDC settings</h3>
                <p>Client secret is stored in Kubernetes when saved.</p>
              </div>
              <div class="auth-modal-grid">
                <label><span>Issuer URL</span><input v-model="providerForm.oidcIssuerUrl" placeholder="https://issuer.company.tld" /></label>
                <label><span>Client ID</span><input v-model="providerForm.oidcClientId" placeholder="servicer-web" /></label>
                <label class="auth-full-width"><span>Client secret</span><input v-model="providerForm.oidcClientSecret" type="password" placeholder="Stored in Kubernetes Secret on save" /></label>
                <label><span>Scopes</span><input v-model="providerForm.oidcScopes" placeholder="openid profile email offline_access" /></label>
                <label><span>Redirect path</span><input v-model="providerForm.oidcRedirectPath" placeholder="/api/auth/callback" /></label>
                <label><span>Username claim</span><input v-model="providerForm.oidcUsernameClaim" placeholder="preferred_username" /></label>
                <label><span>Email claim</span><input v-model="providerForm.oidcEmailClaim" placeholder="email" /></label>
                <label><span>Roles claim</span><input v-model="providerForm.oidcRolesClaim" placeholder="roles" /></label>
                <label><span>Groups claim</span><input v-model="providerForm.oidcGroupsClaim" placeholder="groups" /></label>
                <label class="auth-full-width"><span>End-session URL</span><input v-model="providerForm.oidcEndSessionUrl" placeholder="Optional provider logout endpoint" /></label>
              </div>
            </section>

            <section v-if="providerForm.type === 'ldap'" class="auth-modal-section">
              <div class="auth-section-heading">
                <h3>LDAP settings</h3>
                <p>Bind password is stored in Kubernetes when saved.</p>
              </div>
              <div class="auth-modal-grid">
                <label><span>LDAP URL</span><input v-model="providerForm.ldapUrl" placeholder="ldaps://ldap.company.tld:636" /></label>
                <label><span>Bind DN</span><input v-model="providerForm.ldapBindUsername" placeholder="cn=svc,dc=company,dc=tld" /></label>
                <label class="auth-full-width"><span>Bind password</span><input v-model="providerForm.ldapBindPassword" type="password" placeholder="Stored in Kubernetes Secret on save" /></label>
                <label><span>User base DN</span><input v-model="providerForm.ldapUserBaseDn" placeholder="ou=people,dc=company,dc=tld" /></label>
                <label><span>User filter</span><input v-model="providerForm.ldapUserFilter" placeholder="(uid=%s)" /></label>
                <label><span>Username attribute</span><input v-model="providerForm.ldapUsernameAttribute" placeholder="uid" /></label>
                <label><span>Email attribute</span><input v-model="providerForm.ldapEmailAttribute" placeholder="mail" /></label>
                <label><span>Group base DN</span><input v-model="providerForm.ldapGroupBaseDn" placeholder="ou=groups,dc=company,dc=tld" /></label>
                <label><span>Group filter</span><input v-model="providerForm.ldapGroupFilter" placeholder="(member=%s)" /></label>
                <label><span>Group name attribute</span><input v-model="providerForm.ldapGroupNameAttribute" placeholder="cn" /></label>
              </div>
              <div class="auth-switch-grid">
                <label class="auth-switch">
                  <input v-model="providerForm.ldapStartTls" type="checkbox" />
                  <span class="auth-switch-control"></span>
                  <span>
                    <strong>StartTLS</strong>
                    <small>Upgrade before bind.</small>
                  </span>
                </label>
                <label class="auth-switch">
                  <input v-model="providerForm.insecureSkipVerify" type="checkbox" />
                  <span class="auth-switch-control"></span>
                  <span>
                    <strong>Insecure TLS</strong>
                    <small>Skip certificate verification.</small>
                  </span>
                </label>
              </div>
            </section>
          </div>

          <div class="auth-modal-actions">
            <button class="button reset" :disabled="busy" @click="resetProviderForm">Reset</button>
            <div class="auth-action-spacer"></div>
            <button class="button secondary" :disabled="busy" @click="closeModal">Cancel</button>
            <button class="button primary" :disabled="busy" @click="modalMode === 'create' ? submitCreateProvider() : submitEditProvider()">
              {{ modalMode === 'create' ? 'Create provider' : 'Save provider' }}
            </button>
          </div>
        </template>

        <template v-else-if="modalSection === 'users'">
          <div class="auth-modal-body">
            <section class="auth-modal-section">
              <div class="auth-section-heading">
                <h3>User details</h3>
                <p>Name is the stable login identity in Servicer.</p>
              </div>
              <div class="auth-modal-grid">
                <label><span>Username</span><input v-model="userForm.name" :disabled="modalMode === 'edit'" placeholder="alice" /></label>
                <label><span>Display name</span><input v-model="userForm.displayName" placeholder="Alice Johnson" /></label>
                <label class="auth-full-width"><span>Email</span><input v-model="userForm.email" placeholder="user@company.tld" /></label>
              </div>
            </section>

            <section class="auth-modal-section">
              <div class="auth-section-heading">
                <h3>Local sign-in</h3>
              </div>
              <label class="auth-switch">
                <input v-model="userForm.localAuthEnabled" type="checkbox" />
                <span class="auth-switch-control"></span>
                <span>
                  <strong>Local password</strong>
                  <small>Allow username/password login for this user.</small>
                </span>
              </label>
              <label v-if="userForm.localAuthEnabled" class="auth-stacked-field">
                <span>{{ modalMode === 'edit' ? 'Rotate password' : 'Initial password' }}</span>
                <input v-model="userForm.password" type="password" :placeholder="modalMode === 'edit' ? 'Enter a new password to rotate credentials' : 'Password'" />
              </label>
            </section>

            <section class="auth-modal-section">
              <div class="auth-section-heading">
                <h3>External identities</h3>
                <p>Use one line per upstream identity: <code>provider:subject</code>.</p>
              </div>
              <label class="auth-stacked-field">
                <span>Mappings</span>
                <textarea v-model="userForm.externalIdentitiesRaw" class="defaults-textarea" placeholder="oidc:00u1234567&#10;ldap:uid=alice,ou=people,dc=company,dc=tld" />
              </label>
              <div class="auth-example-box">
                Provider must match an AuthProvider name. Subject is the value returned by that provider.
              </div>
            </section>
          </div>

          <div class="auth-modal-actions">
            <button class="button reset" :disabled="busy" @click="resetUserForm">Reset</button>
            <div class="auth-action-spacer"></div>
            <button class="button secondary" :disabled="busy" @click="closeModal">Cancel</button>
            <button class="button primary" :disabled="busy" @click="modalMode === 'create' ? submitCreateUser() : submitEditUser()">
              {{ modalMode === 'create' ? 'Create user' : 'Save user' }}
            </button>
          </div>
        </template>

        <template v-else-if="modalSection === 'groups'">
          <div class="auth-modal-body">
            <section class="auth-modal-section">
              <div class="auth-section-heading">
                <h3>Group details</h3>
              </div>
              <div class="auth-modal-grid">
                <label><span>Group name</span><input v-model="groupForm.name" :disabled="modalMode === 'edit'" placeholder="platform-operators" /></label>
                <label><span>Display name</span><input v-model="groupForm.displayName" placeholder="Platform Operators" /></label>
              </div>
            </section>

            <section class="auth-modal-section">
              <div class="auth-section-heading">
                <h3>Members</h3>
                <p>Select existing Servicer users.</p>
              </div>
              <input v-model="groupMemberSearch" class="auth-search auth-picker-search" type="search" placeholder="Search users" />
              <div class="auth-choice-grid">
                <label v-for="user in filteredGroupMemberOptions" :key="user.name" class="auth-choice">
                  <input
                    type="checkbox"
                    :checked="groupForm.members.includes(user.name)"
                    @change="toggleStringValue(groupForm.members, user.name, checkboxValue($event))"
                  />
                  <span>
                    <strong>{{ user.displayName || user.name }}</strong>
                    <small>{{ user.email || user.name }}</small>
                  </span>
                </label>
              </div>
              <div v-if="groupForm.members.length" class="auth-chip-row">
                <button
                  v-for="member in groupForm.members"
                  :key="member"
                  class="auth-chip"
                  type="button"
                  @click="toggleStringValue(groupForm.members, member, false)"
                >
                  {{ member }} ×
                </button>
              </div>
            </section>

            <section class="auth-modal-section">
              <div class="auth-section-heading">
                <h3>External group mappings</h3>
                <p>Use one mapping per line: <code>provider:group</code>.</p>
              </div>
              <label class="auth-stacked-field">
                <span>Mappings</span>
                <textarea v-model="groupForm.externalGroupsRaw" class="defaults-textarea" placeholder="oidc:platform-admins&#10;ldap:cn=platform-admins,ou=groups,dc=company,dc=tld" />
              </label>
            </section>
          </div>

          <div class="auth-modal-actions">
            <button class="button reset" :disabled="busy" @click="resetGroupForm">Reset</button>
            <div class="auth-action-spacer"></div>
            <button class="button secondary" :disabled="busy" @click="closeModal">Cancel</button>
            <button class="button primary" :disabled="busy" @click="modalMode === 'create' ? submitCreateGroup() : submitEditGroup()">
              {{ modalMode === 'create' ? 'Create group' : 'Save group' }}
            </button>
          </div>
        </template>

        <template v-else>
          <div class="auth-modal-body">
            <section class="auth-modal-section">
              <div class="auth-section-heading">
                <h3>Binding details</h3>
              </div>
              <div class="auth-modal-grid">
                <label><span>Binding name</span><input v-model="bindingForm.name" :disabled="modalMode === 'edit'" placeholder="tenant-operators" /></label>
                <label><span>Display name</span><input v-model="bindingForm.displayName" placeholder="Tenant operators" /></label>
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
              </div>
            </section>

            <section class="auth-modal-section">
              <div class="auth-section-heading">
                <h3>Subjects</h3>
                <p>Select existing users or groups.</p>
              </div>
              <input v-model="bindingSubjectSearch" class="auth-search auth-picker-search" type="search" placeholder="Search users and groups" />
              <div class="auth-choice-grid">
                <label v-for="subject in bindingSubjectOptions" :key="subject.kind + ':' + subject.name" class="auth-choice">
                  <input
                    type="checkbox"
                    :checked="bindingSubjectSelected(subject)"
                    @change="toggleBindingSubject(subject, checkboxValue($event))"
                  />
                  <span>
                    <strong>{{ subject.label }}</strong>
                    <small>{{ subject.kind }} · {{ subject.detail }}</small>
                  </span>
                </label>
              </div>
              <div v-if="bindingForm.subjects.length" class="auth-chip-row">
                <button
                  v-for="subject in bindingForm.subjects"
                  :key="subject.kind + ':' + subject.name"
                  class="auth-chip"
                  type="button"
                  @click="toggleBindingSubject(subject, false)"
                >
                  {{ subject.kind }}:{{ subject.name }} ×
                </button>
              </div>
            </section>

            <section class="auth-modal-section">
              <div class="auth-section-heading">
                <h3>Roles</h3>
                <p>Roles are built in; assign them with role bindings.</p>
              </div>
              <div class="auth-choice-grid">
                <label v-for="role in bindingRoleOptions" :key="role.name" class="auth-choice">
                  <input
                    type="checkbox"
                    :checked="bindingForm.roles.includes(role.name)"
                    @change="toggleStringValue(bindingForm.roles, role.name, checkboxValue($event))"
                  />
                  <span>
                    <strong>{{ role.name }}</strong>
                    <small>{{ role.description }}</small>
                  </span>
                </label>
              </div>
            </section>
          </div>

          <div class="auth-modal-actions">
            <button class="button reset" :disabled="busy" @click="resetBindingForm">Reset</button>
            <div class="auth-action-spacer"></div>
            <button class="button secondary" :disabled="busy" @click="closeModal">Cancel</button>
            <button class="button primary" :disabled="busy" @click="modalMode === 'create' ? submitCreateBinding() : submitEditBinding()">
              {{ modalMode === 'create' ? 'Create binding' : 'Save binding' }}
            </button>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>
