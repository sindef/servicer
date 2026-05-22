<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
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

const providers = useApi(api.admin.authProviders)
const users = useApi(api.admin.users)
const groups = useApi(api.admin.groups)
const bindings = useApi(api.admin.roleBindings)
const tenants = useApi(api.tenants)

const providerRows = computed(() => providers.data.value ?? [])
const userRows = computed(() => users.data.value ?? [])
const groupRows = computed(() => groups.data.value ?? [])
const bindingRows = computed(() => bindings.data.value ?? [])
const tenantRows = computed(() => tenants.data.value ?? [])

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
  if (!error.value) resetProviderForm()
}

async function deleteProvider(name: string) {
  if (!window.confirm(`Delete auth provider ${name}?`)) return
  await runWrite(() => api.admin.deleteAuthProvider(name), providers.reload)
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
  if (!error.value) resetUserForm()
}

async function deleteUser(name: string) {
  if (!window.confirm(`Delete user ${name}?`)) return
  await runWrite(() => api.admin.deleteUser(name), users.reload)
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
  if (!error.value) resetGroupForm()
}

async function deleteGroup(name: string) {
  if (!window.confirm(`Delete group ${name}?`)) return
  await runWrite(() => api.admin.deleteGroup(name), groups.reload)
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
  if (!error.value) resetBindingForm()
}

async function deleteBinding(name: string) {
  if (!window.confirm(`Delete role binding ${name}?`)) return
  await runWrite(() => api.admin.deleteRoleBinding(name), bindings.reload)
}

resetBindingForm()
</script>

<template>
  <div class="stack-gap">
    <p v-if="error" class="error-text">{{ error }}</p>
    <p v-if="success" class="success-text">{{ success }}</p>

    <section class="content-band">
      <h2>Auth providers</h2>
      <div class="split-admin">
        <div>
          <table class="data-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Type</th>
                <th>Enabled</th>
                <th>Default</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="provider in providerRows" :key="provider.name">
                <td><strong>{{ provider.displayName }}</strong><small>{{ provider.name }}</small></td>
                <td>{{ provider.type }}</td>
                <td>{{ provider.enabled ? 'Yes' : 'No' }}</td>
                <td>{{ provider.default ? 'Yes' : 'No' }}</td>
                <td class="form-actions">
                  <button class="button secondary" @click="editProvider(provider)">Edit</button>
                  <button class="button secondary" @click="deleteProvider(provider.name)">Delete</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="content-band">
          <h3>{{ editingProvider ? 'Edit provider' : 'New provider' }}</h3>
          <div class="form-grid modal-form-grid">
            <label>
              Name
              <input v-model="providerForm.name" :disabled="!!editingProvider" />
            </label>
            <label>
              Display name
              <input v-model="providerForm.displayName" />
            </label>
            <label>
              Type
              <select v-model="providerForm.type" :disabled="!!editingProvider">
                <option value="local">local</option>
                <option value="oidc">oidc</option>
                <option value="ldap">ldap</option>
              </select>
            </label>
            <label><input v-model="providerForm.enabled" type="checkbox" /> Enabled</label>
            <label><input v-model="providerForm.default" type="checkbox" /> Default</label>
          </div>
          <div v-if="providerForm.type === 'oidc'" class="form-grid modal-form-grid">
            <label><input v-model="providerForm.oidcIssuerUrl" placeholder="https://issuer.example.com" /></label>
            <label><input v-model="providerForm.oidcClientId" placeholder="client-id" /></label>
            <label><input v-model="providerForm.oidcClientSecret" type="password" placeholder="client secret" /></label>
            <label><input v-model="providerForm.oidcScopes" placeholder="openid profile email offline_access" /></label>
            <label><input v-model="providerForm.oidcUsernameClaim" placeholder="preferred_username" /></label>
            <label><input v-model="providerForm.oidcEmailClaim" placeholder="email" /></label>
            <label><input v-model="providerForm.oidcRolesClaim" placeholder="roles" /></label>
            <label><input v-model="providerForm.oidcGroupsClaim" placeholder="groups" /></label>
          </div>
          <div v-if="providerForm.type === 'ldap'" class="form-grid modal-form-grid">
            <label><input v-model="providerForm.ldapUrl" placeholder="ldaps://ldap.example.com:636" /></label>
            <label><input v-model="providerForm.ldapBindUsername" placeholder="cn=svc,dc=example,dc=com" /></label>
            <label><input v-model="providerForm.ldapBindPassword" type="password" placeholder="bind password" /></label>
            <label><input v-model="providerForm.ldapUserBaseDn" placeholder="ou=people,dc=example,dc=com" /></label>
            <label><input v-model="providerForm.ldapUserFilter" placeholder="(uid=%s)" /></label>
            <label><input v-model="providerForm.ldapUsernameAttribute" placeholder="uid" /></label>
            <label><input v-model="providerForm.ldapEmailAttribute" placeholder="mail" /></label>
            <label><input v-model="providerForm.ldapGroupBaseDn" placeholder="ou=groups,dc=example,dc=com" /></label>
            <label><input v-model="providerForm.ldapGroupFilter" placeholder="(member=%s)" /></label>
            <label><input v-model="providerForm.ldapGroupNameAttribute" placeholder="cn" /></label>
            <label><input v-model="providerForm.ldapStartTls" type="checkbox" /> StartTLS</label>
            <label><input v-model="providerForm.insecureSkipVerify" type="checkbox" /> Insecure TLS</label>
          </div>
          <div class="form-actions">
            <button class="button primary" :disabled="busy" @click="submitProvider">{{ editingProvider ? 'Save provider' : 'Create provider' }}</button>
            <button class="button secondary" @click="resetProviderForm">Reset</button>
          </div>
        </div>
      </div>
    </section>

    <section class="content-band">
      <h2>Users</h2>
      <div class="split-admin">
        <table class="data-table">
          <thead>
            <tr><th>Name</th><th>Email</th><th>Local</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="user in userRows" :key="user.name">
              <td><strong>{{ user.displayName || user.name }}</strong><small>{{ user.name }}</small></td>
              <td>{{ user.email || '—' }}</td>
              <td>{{ user.localAuthEnabled ? 'Yes' : 'No' }}</td>
              <td class="form-actions">
                <button class="button secondary" @click="editUser(user)">Edit</button>
                <button class="button secondary" @click="deleteUser(user.name)">Delete</button>
              </td>
            </tr>
          </tbody>
        </table>
        <div class="content-band">
          <h3>{{ editingUser ? 'Edit user' : 'New user' }}</h3>
          <div class="form-grid modal-form-grid">
            <label><input v-model="userForm.name" :disabled="!!editingUser" placeholder="alice" /></label>
            <label><input v-model="userForm.displayName" placeholder="Alice" /></label>
            <label><input v-model="userForm.email" placeholder="alice@example.com" /></label>
            <label><input v-model="userForm.localAuthEnabled" type="checkbox" /> Local auth enabled</label>
            <label v-if="userForm.localAuthEnabled" style="grid-column: 1 / -1">
              <input v-model="userForm.password" type="password" :placeholder="editingUser ? 'new password to rotate' : 'password'" />
            </label>
            <label style="grid-column: 1 / -1">
              External identities (`provider:subject`)
              <textarea v-model="userForm.externalIdentitiesRaw" class="defaults-textarea" />
            </label>
          </div>
          <div class="form-actions">
            <button class="button primary" :disabled="busy" @click="submitUser">{{ editingUser ? 'Save user' : 'Create user' }}</button>
            <button class="button secondary" @click="resetUserForm">Reset</button>
          </div>
        </div>
      </div>
    </section>

    <section class="content-band">
      <h2>Groups</h2>
      <div class="split-admin">
        <table class="data-table">
          <thead>
            <tr><th>Name</th><th>Members</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="group in groupRows" :key="group.name">
              <td><strong>{{ group.displayName || group.name }}</strong><small>{{ group.name }}</small></td>
              <td>{{ (group.members ?? []).join(', ') || '—' }}</td>
              <td class="form-actions">
                <button class="button secondary" @click="editGroup(group)">Edit</button>
                <button class="button secondary" @click="deleteGroup(group.name)">Delete</button>
              </td>
            </tr>
          </tbody>
        </table>
        <div class="content-band">
          <h3>{{ editingGroup ? 'Edit group' : 'New group' }}</h3>
          <div class="form-grid modal-form-grid">
            <label><input v-model="groupForm.name" :disabled="!!editingGroup" placeholder="acme-operators" /></label>
            <label><input v-model="groupForm.displayName" placeholder="Acme Operators" /></label>
            <label style="grid-column: 1 / -1">
              Members (one user per line)
              <textarea v-model="groupForm.membersRaw" class="defaults-textarea" />
            </label>
            <label style="grid-column: 1 / -1">
              External groups (`provider:name`)
              <textarea v-model="groupForm.externalGroupsRaw" class="defaults-textarea" />
            </label>
          </div>
          <div class="form-actions">
            <button class="button primary" :disabled="busy" @click="submitGroup">{{ editingGroup ? 'Save group' : 'Create group' }}</button>
            <button class="button secondary" @click="resetGroupForm">Reset</button>
          </div>
        </div>
      </div>
    </section>

    <section class="content-band">
      <h2>Role bindings</h2>
      <div class="split-admin">
        <table class="data-table">
          <thead>
            <tr><th>Name</th><th>Scope</th><th>Roles</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="binding in bindingRows" :key="binding.name">
              <td><strong>{{ binding.displayName || binding.name }}</strong><small>{{ binding.name }}</small></td>
              <td>{{ binding.scope }}<span v-if="binding.tenantName"> · {{ binding.tenantName }}</span></td>
              <td>{{ binding.roles.join(', ') }}</td>
              <td class="form-actions">
                <button class="button secondary" @click="editBinding(binding)">Edit</button>
                <button class="button secondary" @click="deleteBinding(binding.name)">Delete</button>
              </td>
            </tr>
          </tbody>
        </table>
        <div class="content-band">
          <h3>{{ editingBinding ? 'Edit role binding' : 'New role binding' }}</h3>
          <div class="form-grid modal-form-grid">
            <label><input v-model="bindingForm.name" :disabled="!!editingBinding" placeholder="acme-tenant-ops" /></label>
            <label><input v-model="bindingForm.displayName" placeholder="Acme tenant ops" /></label>
            <label>
              Scope
              <select v-model="bindingForm.scope">
                <option value="platform">platform</option>
                <option value="tenant">tenant</option>
              </select>
            </label>
            <label v-if="bindingForm.scope === 'tenant'">
              Tenant
              <select v-model="bindingForm.tenantName">
                <option v-for="tenant in tenantRows" :key="tenant.name" :value="tenant.name">{{ tenant.displayName }}</option>
              </select>
            </label>
            <label style="grid-column: 1 / -1">
              Subjects (`User:name` or `Group:name`)
              <textarea v-model="bindingForm.subjectsRaw" class="defaults-textarea" />
            </label>
            <label style="grid-column: 1 / -1">
              Roles (comma-separated)
              <input v-model="bindingForm.rolesRaw" />
            </label>
          </div>
          <div class="form-actions">
            <button class="button primary" :disabled="busy" @click="submitBinding">{{ editingBinding ? 'Save binding' : 'Create binding' }}</button>
            <button class="button secondary" @click="resetBindingForm">Reset</button>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>
