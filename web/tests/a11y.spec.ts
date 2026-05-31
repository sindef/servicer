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

async function expectFocusInside(page: Page, selector: string) {
  const focusedInside = await page.evaluate((dialogSelector) => {
    const dialog = document.querySelector(dialogSelector)
    const active = document.activeElement
    return !!dialog && !!active && dialog.contains(active)
  }, selector)
  expect(focusedInside).toBe(true)
}

test('catalog request dialog traps focus, supports escape close, and restores opener focus', async ({ page }) => {
  await mockApi(page)
  await page.goto('/catalog')
  const openRequest = page.getByRole('button', { name: /standard/i })
  await openRequest.click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('textbox', { name: 'Name', exact: true })).toBeVisible()
  await expect(dialog.getByRole('combobox', { name: 'Project', exact: true })).toBeVisible()
  await expect(dialog.getByRole('combobox', { name: 'Plan', exact: true })).toBeVisible()
  await expectFocusInside(page, '[role="dialog"]')
  await page.keyboard.press('Tab')
  await expectFocusInside(page, '[role="dialog"]')
  await page.keyboard.press('Shift+Tab')
  await expectFocusInside(page, '[role="dialog"]')
  await expectNoA11yViolations(page, '[role="dialog"]')
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(openRequest).toBeFocused()
})

test('instance detail dialogs are keyboard accessible with focus return', async ({ page }) => {
  await mockApi(page)
  await page.goto('/instances/session-cache')
  const revealButton = page.getByRole('button', { name: /^reveal$/i }).first()
  await revealButton.click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expectFocusInside(page, '[role="dialog"]')
  await page.getByLabel(/i understand/i).check()
  await page.getByRole('button', { name: /reveal now/i }).click()
  await expect(dialog).toBeVisible()
  await expectNoA11yViolations(page, '[role="dialog"]')
  await page.keyboard.press('Escape')
  const deleteButton = page.getByRole('button', { name: /^delete$/i })
  await deleteButton.click()
  await expect(dialog).toBeVisible()
  await expectFocusInside(page, '[role="dialog"]')
  await page.keyboard.press('Escape')
  await expect(deleteButton).toBeFocused()
  const yamlButton = page.getByRole('button', { name: /edit yaml/i })
  await yamlButton.click()
  await expect(dialog).toBeVisible()
  await expect(page.getByRole('textbox', { name: /product request yaml editor/i })).toBeVisible()
  await expectNoA11yViolations(page, '[role="dialog"]')
  await page.keyboard.press('Escape')
  await expect(yamlButton).toBeFocused()
})

test('auth admin modal controls are labeled and keyboard closable', async ({ page }) => {
  await mockApi(page)
  await page.goto('/admin')
  await page.getByRole('button', { name: /^auth$/i }).click()
  const newUserButton = page.getByRole('button', { name: /new user/i })
  await newUserButton.click()
  let dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('textbox', { name: 'Username' })).toBeVisible()
  await expect(dialog.getByRole('textbox', { name: 'Display name' })).toBeVisible()
  await expect(dialog.getByRole('textbox', { name: 'Email' })).toBeVisible()
  await expectNoA11yViolations(page, '[role="dialog"]')
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(newUserButton).toBeFocused()

  await page.getByRole('button', { name: /providers/i }).click()
  const newProviderButton = page.getByRole('button', { name: /new provider/i })
  await newProviderButton.click()
  dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('textbox', { name: 'Provider name' })).toBeVisible()
  await expect(dialog.getByRole('button', { name: /oidc/i })).toBeVisible()
  await dialog.getByRole('button', { name: /oidc/i }).click()
  await expect(dialog.getByRole('textbox', { name: 'Issuer URL' })).toBeVisible()
  await expect(dialog.getByRole('textbox', { name: 'Client ID' })).toBeVisible()
  await expectNoA11yViolations(page, '[role="dialog"]')
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(newProviderButton).toBeFocused()
})
