/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from 'node:assert/strict'
import { after, afterEach, test } from 'node:test'

import { Window } from 'happy-dom'

const dom = new Window({ url: 'https://model.example/' })
for (const key of [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'MutationObserver',
  'ResizeObserver',
  'getComputedStyle',
  'localStorage',
] as const) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: dom[key],
  })
}
Object.defineProperty(globalThis, 'IS_REACT_ACT_ENVIRONMENT', {
  configurable: true,
  value: true,
})
const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider, notifyManager } =
  await import('@tanstack/react-query')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { DesktopSyncAuthorization } = await import('../index')
const { api } = await import('@/lib/api')
const { useAuthStore } = await import('@/stores/auth-store')
const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
  fallbackLng: false,
})
notifyManager.setScheduler((callback) => queueMicrotask(callback))
const originalAdapter = api.defaults.adapter
const originalAssign = dom.location.assign.bind(dom.location)
const roots: ReturnType<typeof createRoot>[] = []
const clients: InstanceType<typeof QueryClient>[] = []
const callback = 'https://director.example/auth/callback'
const search = {
  client_id: 'director-web',
  redirect_uri: callback,
  state: 's'.repeat(43),
  code_challenge: 'c'.repeat(43),
  code_challenge_method: 'S256',
  mode: 'callback' as const,
}

afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) root.unmount()
  })
  for (const client of clients.splice(0)) client.clear()
  document.body.innerHTML = ''
  api.defaults.adapter = originalAdapter
  dom.location.assign = originalAssign
  useAuthStore.getState().auth.reset('idle')
  dom.sessionStorage.clear()
})
after(() => {
  notifyManager.setScheduler((fn) => setTimeout(fn, 0))
  dom.close()
})

async function renderConsent() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  })
  clients.push(client)
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  roots.push(root)
  await act(async () =>
    root.render(
      <I18nextProvider i18n={i18n}>
        <QueryClientProvider client={client}>
          <DesktopSyncAuthorization search={search} />
        </QueryClientProvider>
      </I18nextProvider>
    )
  )
  return container
}

function consentButton(container: ParentNode, name: string): HTMLButtonElement {
  const button = [
    ...container.querySelectorAll<HTMLButtonElement>('button'),
  ].find((element) => element.textContent === name)
  assert.ok(button, `Accessible action ${name} exists`)
  return button
}

test('pending destination verification disables consent and hides cancel', async () => {
  let complete: ((success: boolean) => void) | undefined
  api.defaults.adapter = (config) =>
    new Promise((resolve) => {
      complete = (success) =>
        resolve({
          config,
          status: 200,
          statusText: 'OK',
          headers: {},
          data: {
            success,
            data: { client_id: 'director-web', redirect_uri: callback },
          },
        })
    })
  const container = await renderConsent()
  assert.equal(
    consentButton(container, 'Authorize and continue').disabled,
    true
  )
  assert.ok(container.querySelector('[role="status"]'))
  assert.equal(container.textContent?.includes('Cancel and return'), false)
  await act(async () => complete?.(true))
  assert.equal(
    consentButton(container, 'Authorize and continue').disabled,
    false
  )
  assert.equal(consentButton(container, 'Cancel and return').disabled, false)
})

test('rejected destination displays an alert and never offers cancel navigation', async () => {
  api.defaults.adapter = async (config) => ({
    config,
    status: 200,
    statusText: 'OK',
    headers: {},
    data: { success: false },
  })
  const container = await renderConsent()
  assert.ok(container.querySelector('[role="alert"]'))
  assert.equal(
    consentButton(container, 'Authorize and continue').disabled,
    true
  )
  assert.equal(container.textContent?.includes('Cancel and return'), false)
})

test('cancel returns the original state and access_denied without issuing a code', async () => {
  let destination = ''
  dom.location.assign = (value) => {
    destination = String(value)
  }
  api.defaults.adapter = async (config) => {
    assert.equal(config.method, 'get')
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: {
        success: true,
        data: { client_id: 'director-web', redirect_uri: callback },
      },
    }
  }
  const container = await renderConsent()
  await act(async () => consentButton(container, 'Cancel and return').click())
  const result = new URL(destination)
  assert.equal(result.origin + result.pathname, callback)
  assert.equal(result.searchParams.get('state'), search.state)
  assert.equal(result.searchParams.get('error'), 'access_denied')
  assert.equal(result.searchParams.has('code'), false)
})

test('failed issuance restores consent and cancel actions after the loading state', async () => {
  let complete: (() => void) | undefined
  api.defaults.adapter = (config) => {
    if (config.method === 'get') {
      return Promise.resolve({
        config,
        status: 200,
        statusText: 'OK',
        headers: {},
        data: {
          success: true,
          data: { client_id: 'director-web', redirect_uri: callback },
        },
      })
    }
    assert.equal(config.url, '/api/desktop-sync/issue')
    assert.equal(JSON.parse(config.data).client_id, 'director-web')
    return new Promise((resolve) => {
      complete = () =>
        resolve({
          config,
          status: 200,
          statusText: 'OK',
          headers: {},
          data: { success: false },
        })
    })
  }
  const container = await renderConsent()
  await act(async () =>
    consentButton(container, 'Authorize and continue').click()
  )
  assert.equal(consentButton(container, 'Authorizing...').disabled, true)
  assert.equal(consentButton(container, 'Cancel and return').disabled, true)
  await act(async () => complete?.())
  assert.equal(
    consentButton(container, 'Authorize and continue').disabled,
    false
  )
  assert.equal(consentButton(container, 'Cancel and return').disabled, false)
})
