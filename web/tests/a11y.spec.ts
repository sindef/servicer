import { expect, test, type Page, type Route } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'

function jsonResponse(body: unknown) {
  return {
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(body)
  }
}

async function mockApi(page: Page) {
  await page.route('**/api/**', async (route: Route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path === '/api/auth/config') {
      await route.fulfill(
        jsonResponse({
          mode: 'multi',
          providers: [{ name: 'local', displayName: 'Local', type: 'local', default: true }]
        })
      )
      return
    }
    if (path === '/api/auth/session') {
      await route.fulfill(
        jsonResponse({
          mode: 'multi',
          name: 'Alice Admin',
          email: 'alice@example.com',
          provider: 'local',
          roles: ['platform-admin'],
          groups: ['platform-admins'],
          tenants: [],
          authenticated: true
        })
      )
      return
    }
    if (path === '/api/catalog') {
      await route.fulfill(
        jsonResponse([
          {
            name: 'postgresql',
            displayName: 'PostgreSQL',
            category: 'database',
            driver: 'cnpg',
            published: true,
            description: 'Postgres',
            capabilities: ['ha'],
            plans: [{ name: 'postgresql-standard', displayName: 'Standard', tier: 'prod', topology: 'single-cluster', defaultVersion: '16', published: true }],
            actions: []
          }
        ])
      )
      return
    }
    if (path === '/api/projects') {
      await route.fulfill(
        jsonResponse([
          {
            name: 'acme-prod',
            displayName: 'Acme Production',
            tenantName: 'acme',
            environment: 'production',
            phase: 'Ready',
            clusterName: 'cluster-a',
            namespaceMode: 'dedicated',
            instanceCount: 2
          }
        ])
      )
      return
    }
    if (path === '/api/admin/clusters') {
      await route.fulfill(jsonResponse([]))
      return
    }
    if (path === '/api/instances/session-cache') {
      await route.fulfill(
        jsonResponse({
          name: 'session-cache',
          displayName: 'Session Cache',
          projectName: 'acme-prod',
          tenantName: 'acme',
          productClass: 'valkey',
          productName: 'Valkey',
          planName: 'valkey-standard',
          planDisplay: 'Standard',
          phase: 'Ready',
          health: 'Healthy',
          clusterName: 'cluster-a',
          namespace: 'acme-prod-session-cache',
          syncPhase: 'Synced',
          runtime: { driver: 'valkey', kind: 'StatefulSet', name: 'session-cache', namespace: 'acme-prod-session-cache' },
          desired: { name: 'session-cache', projectName: 'acme-prod', serviceClass: 'valkey', servicePlan: 'valkey-standard', parameters: { replicas: 3 } },
          delivery: { syncPhase: 'Synced', runtimeStatus: 'Ready', argoStatus: 'Healthy', applicationName: 'session-cache' },
          artifact: { revision: 'abc', path: 'clusters/cluster-a/session-cache', count: 3 },
          credentials: [{ name: 'session-cache-auth', namespace: 'acme-prod-session-cache', revealUrl: '/api/instances/session-cache/credentials/acme-prod-session-cache/session-cache-auth' }],
          conditions: [{ type: 'Ready', status: 'True', reason: 'Reconciled', message: 'ready' }],
          availableActions: [{ name: 'scale', displayName: 'Scale', requiresApproval: false, disruptive: false }],
          recentActions: [{ name: 'scale-1', targetName: 'session-cache', action: 'scale', phase: 'Succeeded' }],
          events: []
        })
      )
      return
    }
    if (path === '/api/instances/session-cache/credentials/acme-prod-session-cache/session-cache-auth') {
      await route.fulfill(
        jsonResponse({
          name: 'session-cache-auth',
          namespace: 'acme-prod-session-cache',
          data: { password: 'super-secret-password' }
        })
      )
      return
    }
    if (path === '/api/admin/auth/providers') {
      await route.fulfill(jsonResponse([{ name: 'local', displayName: 'Local', type: 'local', enabled: true, default: true }]))
      return
    }
    if (path === '/api/admin/auth/users') {
      await route.fulfill(jsonResponse([{ name: 'alice', displayName: 'Alice', email: 'alice@example.com', localAuthEnabled: true, externalIdentities: [] }]))
      return
    }
    if (path === '/api/admin/auth/groups') {
      await route.fulfill(jsonResponse([{ name: 'platform-admins', displayName: 'Platform Admins', members: ['alice'], externalGroups: [] }]))
      return
    }
    if (path === '/api/admin/auth/roles') {
      await route.fulfill(jsonResponse([{ name: 'platform-admin', displayName: 'Platform Admin', description: 'admin', scope: 'platform', builtIn: true, permissions: ['platform-admin'] }]))
      return
    }
    if (path === '/api/admin/auth/rolebindings') {
      await route.fulfill(jsonResponse([{ name: 'platform-admin-binding', displayName: 'Platform Admin Binding', scope: 'platform', subjects: [{ kind: 'User', name: 'alice' }], roles: ['platform-admin'] }]))
      return
    }
    if (path === '/api/tenants') {
      await route.fulfill(jsonResponse([{ name: 'acme', displayName: 'Acme', phase: 'Ready', allowedServiceClasses: ['postgresql'], projectCount: 1, instanceCount: 2, owners: ['alice@example.com'] }]))
      return
    }
    if (path === '/api/admin/projects' || path === '/api/admin/tenants' || path === '/api/admin/auth/users' || path === '/api/admin/auth/groups' || path === '/api/admin/auth/roles' || path === '/api/admin/auth/rolebindings') {
      await route.fulfill(jsonResponse({ name: 'ok', message: 'ok' }))
      return
    }
    await route.fulfill(jsonResponse({}))
  })
}

async function expectNoA11yViolations(page: Page, includeSelector = 'body') {
  const results = await new AxeBuilder({ page }).include(includeSelector).analyze()
  expect(results.violations).toEqual([])
}

test('catalog request dialog is keyboard accessible', async ({ page }) => {
  await mockApi(page)
  await page.goto('/catalog')
  await page.getByRole('button', { name: /standard/i }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.keyboard.press('Tab')
  await expectNoA11yViolations(page, '[role="dialog"]')
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toBeHidden()
})

test('instance detail credential/delete/yaml dialogs are accessible', async ({ page }) => {
  await mockApi(page)
  await page.goto('/instances/session-cache')
  await page.getByRole('button', { name: /reveal/i }).first().click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.getByLabel(/i understand/i).check()
  await page.getByRole('button', { name: /reveal now/i }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await expectNoA11yViolations(page, '[role="dialog"]')
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: /delete/i }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: /edit yaml/i }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await expectNoA11yViolations(page, '[role="dialog"]')
})

test('auth admin modal has dialog semantics and keyboard close', async ({ page }) => {
  await mockApi(page)
  await page.goto('/admin')
  await page.getByRole('button', { name: /^auth$/i }).click()
  await page.getByRole('button', { name: /providers/i }).click()
  await page.getByRole('button', { name: /new provider/i }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await expectNoA11yViolations(page, '[role="dialog"]')
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toBeHidden()
})
