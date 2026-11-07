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
import { after, afterEach, beforeEach, test } from 'node:test'

import { Window } from 'happy-dom'

const dom = new Window({ url: 'http://localhost/' })
for (const key of [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'ResizeObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
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
const { act, useState } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { ImageGeneratorForm } =
  await import('../components/image-generator-form')
const { useImageGenerator } = await import('../hooks/use-image-generator')
const { MultimediaNavigation } = await import('../index')
const { AssetGallery } = await import('../components/asset-gallery')
const { GenerationHistory } = await import('../components/generation-history')
const { api } = await import('@/lib/api')
const { useAuthStore } = await import('@/stores/auth-store')
const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
  fallbackLng: false,
})
const catalog = {
  models: [
    {
      name: 'gpt-image-2',
      price_usd: null,
      capabilities: {
        max_references: 9,
        max_count: 10,
        custom_size: true,
        advanced: true,
        sizes: ['auto', '1024x1024'],
        qualities: ['auto', 'low', 'medium', 'high'],
      },
    },
  ],
  max_upload_bytes: 33554432,
  retention_days: 30,
}
const originalAdapter = api.defaults.adapter
const roots: ReturnType<typeof createRoot>[] = []
const clients: InstanceType<typeof QueryClient>[] = []
beforeEach(() => {
  useAuthStore.getState().auth.setBundle({
    access_token: 'test-access-token',
    token_type: 'Bearer',
    access_expires_at: 4102444800,
    user: { id: 1, username: 'image-owner', role: 1 },
    session: {
      sid: 'test-session',
      current: true,
      login_method: 'password',
      ip: '127.0.0.1',
      user_agent: 'test',
      created_at: 1,
      last_active_at: 1,
      expires_at: 4102444800,
    },
  })
})
afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) root.unmount()
  })
  for (const client of clients.splice(0)) client.clear()
  document.body.innerHTML = ''
  api.defaults.adapter = originalAdapter
  useAuthStore.getState().auth.reset('idle')
})
after(() => dom.close())

function GeneratorHarness() {
  const generator = useImageGenerator()
  return <ImageGeneratorForm generator={generator} />
}
function NavigationHarness() {
  const [view, setView] = useState<'generate' | 'assets'>('generate')
  return <MultimediaNavigation view={view} onChange={setView} />
}
async function render(
  content: React.ReactNode,
  setup?: (client: InstanceType<typeof QueryClient>) => void
) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  })
  client.setQueryData(['multimedia', 'models'], catalog)
  setup?.(client)
  clients.push(client)
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  roots.push(root)
  await act(async () =>
    root.render(
      <I18nextProvider i18n={i18n}>
        <QueryClientProvider client={client}>{content}</QueryClientProvider>
      </I18nextProvider>
    )
  )
  return container
}
function button(container: ParentNode, text: string): HTMLButtonElement {
  const element = [
    ...container.querySelectorAll<HTMLButtonElement>('button'),
  ].find((node) => node.textContent === text)
  assert.ok(element, `Button ${text} exists`)
  return element
}

test('floating navigation switches the visible destination and accessible current state', async () => {
  const container = await render(<NavigationHarness />)
  assert.equal(
    button(container, 'Image generation').getAttribute('aria-current'),
    'page'
  )
  await act(async () => button(container, 'My assets').click())
  assert.equal(
    button(container, 'My assets').getAttribute('aria-current'),
    'page'
  )
  assert.equal(
    button(container, 'Image generation').hasAttribute('aria-current'),
    false
  )
})

test('more settings omit background, moderation and mask and show compression only for JPEG or WebP', async () => {
  const container = await render(<GeneratorHarness />)
  for (const id of ['image-background', 'image-moderation', 'image-mask']) {
    assert.equal(container.querySelector(`#${id}`), null)
  }
  assert.ok(container.querySelector('#image-quality'))
  assert.equal(container.querySelector('#image-compression'), null)
  const format = container.querySelector<HTMLSelectElement>('#image-format')
  assert.ok(format)
  await act(async () => {
    format.value = 'jpeg'
    format.dispatchEvent(new Event('change', { bubbles: true }))
  })
  assert.ok(container.querySelector('#image-compression'))
  await act(async () => {
    format.value = 'png'
    format.dispatchEvent(new Event('change', { bubbles: true }))
  })
  assert.equal(container.querySelector('#image-compression'), null)
})

test('selecting an image model needs no group selection and submits only model settings', async () => {
  let submitted: Record<string, unknown> | undefined
  api.defaults.adapter = async (config) => {
    submitted = JSON.parse(config.data as string) as Record<string, unknown>
    return {
      data: { success: true, data: { id: 'img_queued' } },
      status: 202,
      statusText: 'Accepted',
      headers: {},
      config,
    }
  }
  const container = await render(<GeneratorHarness />)
  assert.equal(!!container.querySelector('#image-group'), false)
  assert.ok(container.querySelector('#image-model'))
  const prompt = container.querySelector<HTMLTextAreaElement>('#image-prompt')
  assert.ok(prompt)
  const setValue = Object.getOwnPropertyDescriptor(
    dom.HTMLTextAreaElement.prototype,
    'value'
  )?.set
  assert.ok(setValue)
  await act(async () => {
    setValue.call(prompt, 'A paper boat')
    prompt.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await act(async () =>
    container
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  )
  assert.ok(submitted)
  assert.equal(submitted.model, 'gpt-image-2')
  assert.equal('group' in submitted, false)
  assert.equal('background' in submitted, false)
  assert.equal('moderation' in submitted, false)
  assert.equal('mask' in submitted, false)
})

test('switching to Gemini shows native resolutions and hides unsupported GPT settings', async () => {
  const gemini = {
    name: 'gemini-3.1-flash-image-preview',
    price_usd: null,
    capabilities: {
      max_references: 9,
      max_count: 1,
      custom_size: false,
      advanced: false,
      sizes: ['auto', '1:1', '16:9'],
      qualities: [],
      resolutions: ['1K', '2K', '4K'],
    },
  }
  const container = await render(<GeneratorHarness />, (client) =>
    client.setQueryData(['multimedia', 'models'], {
      ...catalog,
      models: [...catalog.models, gemini],
    })
  )
  const modelSelect = container.querySelector<HTMLSelectElement>('#image-model')
  assert.ok(modelSelect)
  await act(async () => {
    modelSelect.value = gemini.name
    modelSelect.dispatchEvent(new Event('change', { bubbles: true }))
  })
  assert.equal(!!container.querySelector('#image-group'), false)
  assert.equal(!!container.querySelector('#image-mask'), false)
  assert.equal(!!container.querySelector('#image-format'), false)
  assert.equal(!!container.querySelector('#image-quality'), false)
  assert.equal(!!container.querySelector('details'), false)
  assert.ok(container.querySelector('input[type="file"]'))
  assert.equal(
    container.querySelector<HTMLInputElement>('#image-count')?.max,
    '1'
  )
  const resolution =
    container.querySelector<HTMLSelectElement>('#image-resolution')
  assert.ok(resolution)
  assert.deepEqual(
    [...resolution.options].map((option) => option.value),
    ['1K', '2K', '4K']
  )
  assert.equal(resolution.disabled, false)
  assert.equal(resolution.value, '1K')
})

test('an empty image model catalog disables generation without asking for a group', async () => {
  const container = await render(<GeneratorHarness />, (client) =>
    client.setQueryData(['multimedia', 'models'], { ...catalog, models: [] })
  )
  assert.equal(button(container, 'Generate images').disabled, true)
  assert.equal(!!container.querySelector('#image-group'), false)
  assert.equal(
    container.querySelector<HTMLSelectElement>('#image-model')?.disabled,
    true
  )
})

test('an empty prompt shows validation instead of calling generation API', async () => {
  let requests = 0
  api.defaults.adapter = async (config) => {
    requests++
    return {
      data: { success: true, data: {} },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  const container = await render(<GeneratorHarness />)
  await act(async () =>
    container
      .querySelector('form')
      ?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  )
  assert.equal(
    container.querySelector('#image-prompt')?.getAttribute('aria-invalid'),
    'true'
  )
  assert.equal(requests, 0)
})

test('an empty asset library disables selection and batch actions', async () => {
  const container = await render(
    <AssetGallery onPreview={() => {}} onReference={() => {}} />,
    (client) =>
      client.setQueryData(['multimedia', 'assets', 1, ''], {
        items: [],
        total: 0,
        retention_days: 30,
      })
  )
  assert.equal(button(container, 'Download').disabled, true)
  assert.equal(button(container, 'Delete').disabled, true)
  assert.equal(
    container.querySelector<HTMLInputElement>('input[type="checkbox"]')
      ?.disabled,
    true
  )
  assert.ok(container.textContent?.includes('No images yet.'))
})

test('selecting assets enables batch actions and cancelling deletion preserves the images', async () => {
  const asset = {
    id: 'owned-image',
    kind: 'generated',
    name: 'owned.png',
    mime_type: 'image/png',
    width: 1,
    height: 1,
    bytes: 70,
    created_at: 1,
    expires_at: 4102444800,
  }
  let removals = 0
  api.defaults.adapter = async (config) => {
    if (config.url?.endsWith('/remove')) removals++
    return {
      data: { success: true, data: {} },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  const container = await render(
    <AssetGallery onPreview={() => {}} onReference={() => {}} />,
    (client) => {
      client.setQueryData(['multimedia', 'assets', 1, ''], {
        items: [asset],
        total: 1,
        retention_days: 30,
      })
      client.setQueryData(
        ['multimedia', 'content', asset.id, false],
        new Blob(['image'], { type: 'image/png' })
      )
    }
  )
  const selectAll = container.querySelector<HTMLInputElement>(
    'input[type="checkbox"]'
  )
  assert.ok(selectAll)
  await act(async () => selectAll.click())
  assert.equal(button(container, 'Download').disabled, false)
  assert.equal(button(container, 'Delete').disabled, false)
  await act(async () => button(container, 'Delete').click())
  const dialog = document.querySelector('[role="alertdialog"]')
  assert.ok(dialog)
  assert.ok(dialog.textContent?.includes('Deleted images cannot be restored.'))
  await act(async () => button(dialog, 'Cancel').click())
  assert.equal(removals, 0)
  assert.ok(container.textContent?.includes('owned.png'))
  assert.equal(selectAll.checked, true)
})

test('the initial aspect ratio agrees with the dimensions sent for the selected model', async () => {
  const container = await render(<GeneratorHarness />)
  assert.equal(
    container.querySelector<HTMLSelectElement>('#image-ratio')?.value,
    '1:1'
  )
  assert.ok(container.textContent?.includes('1024x1024'))
})

test('assets that expire during a refresh no longer remain selected for batch actions', async () => {
  const asset = {
    id: 'expiring-image',
    kind: 'reference',
    name: 'expiring.png',
    mime_type: 'image/png',
    width: 1,
    height: 1,
    bytes: 70,
    created_at: 1,
    expires_at: 4102444800,
  }
  let refresh: (() => void) | undefined
  const container = await render(
    <AssetGallery onPreview={() => {}} onReference={() => {}} />,
    (client) => {
      client.setQueryData(['multimedia', 'assets', 1, ''], {
        items: [asset],
        total: 1,
      })
      client.setQueryData(
        ['multimedia', 'content', asset.id, false],
        new Blob(['image'], { type: 'image/png' })
      )
      refresh = () =>
        client.setQueryData(['multimedia', 'assets', 1, ''], {
          items: [],
          total: 0,
        })
    }
  )
  const selectAll = container.querySelector<HTMLInputElement>(
    'input[type="checkbox"]'
  )
  assert.ok(selectAll)
  await act(async () => selectAll.click())
  assert.equal(button(container, 'Download').disabled, false)
  await act(async () => {
    refresh?.()
    await dom.happyDOM.whenAsyncComplete()
  })
  assert.equal(button(container, 'Download').disabled, true)
  assert.equal(button(container, 'Delete').disabled, true)
})

test('queued and running works show image placeholders while failed works stay static', async () => {
  const container = await render(
    <GenerationHistory
      onReuse={() => {}}
      onPreview={() => {}}
      onReference={() => {}}
    />,
    (client) =>
      client.setQueryData(['multimedia', 'generations', 1], {
        items: [
          {
            id: 'queued-work',
            model: 'gpt-image-2',
            prompt: 'A paper boat at sunrise',
            parameters: '{}',
            status: 'queued',
            created_at: 1,
            finished_at: 0,
            assets: [],
          },
          {
            id: 'running-work',
            model: 'gpt-image-2',
            prompt: 'A cat in a garden',
            parameters: '{}',
            status: 'running',
            created_at: 1,
            finished_at: 0,
            assets: [],
          },
          {
            id: 'failed-work',
            model: 'gpt-image-2',
            prompt: 'A mountain lake',
            parameters: '{}',
            status: 'failed',
            error: 'The upstream request timed out',
            created_at: 1,
            finished_at: 2,
            assets: [],
          },
        ],
        total: 3,
      })
  )
  const cards = [...container.querySelectorAll('article')]
  assert.equal(cards.length, 3)
  const grid = cards[0].parentElement
  assert.ok(grid)
  assert.ok(grid.classList.contains('grid'))
  assert.ok(grid.classList.contains('grid-cols-2'))
  assert.ok(grid.classList.contains('lg:grid-cols-3'))
  for (const card of cards.slice(0, 2)) {
    assert.equal(card.getAttribute('aria-busy'), 'true')
    const placeholder = card.querySelector('[role="status"]')
    assert.ok(placeholder)
    assert.equal(placeholder.textContent, '')
    assert.equal(placeholder.classList.contains('animate-none'), false)
  }
  assert.equal(cards[2].getAttribute('aria-busy'), 'false')
  const failedPlaceholder = cards[2].querySelector(
    '[role="img"][aria-label="Failed"]'
  )
  assert.ok(failedPlaceholder)
  assert.ok(failedPlaceholder.classList.contains('animate-none'))
  assert.equal(container.querySelector('[role="progressbar"]'), null)
  assert.equal(
    container.textContent?.includes(
      'You can leave this page while the image is being generated.'
    ),
    false
  )
  const details = cards[2].querySelector('details')
  assert.ok(details)
  assert.equal(details.open, false)
  await act(async () => details.querySelector('summary')?.click())
  assert.equal(details.open, true)
  assert.equal(
    details.querySelector('[role="alert"]')?.textContent,
    'The upstream request timed out'
  )
})

test('completed batch images occupy separate grid cards with working preview, reuse and reference actions', async () => {
  const assets = ['first', 'second'].map((name) => ({
    id: name,
    kind: 'generated' as const,
    name: `${name}.png`,
    mime_type: 'image/png',
    width: 1024,
    height: 1024,
    bytes: 100,
    created_at: 1,
    expires_at: 4102444800,
  }))
  const previews: string[] = []
  const reused: string[] = []
  const references: string[] = []
  const container = await render(
    <GenerationHistory
      onPreview={(asset) => previews.push(asset.id)}
      onReuse={(job) => reused.push(job.id)}
      onReference={(asset) => references.push(asset.id)}
    />,
    (client) => {
      client.setQueryData(['multimedia', 'generations', 1], {
        items: [
          {
            id: 'completed-batch',
            model: 'gpt-image-2',
            prompt: 'Two paper boat variations',
            parameters: '{"n":2}',
            status: 'succeeded',
            created_at: 1,
            finished_at: 2,
            assets,
          },
        ],
        total: 1,
      })
      for (const asset of assets) {
        client.setQueryData(
          ['multimedia', 'content', asset.id, false],
          new Blob(['image'], { type: 'image/png' })
        )
      }
    }
  )
  const cards = [...container.querySelectorAll('article')]
  assert.equal(cards.length, 2)
  assert.equal(cards[0].parentElement, cards[1].parentElement)
  assert.equal(container.querySelector('[role="status"]'), null)
  assert.equal(container.querySelector('[data-slot="skeleton"]'), null)
  const preview = cards[1].querySelector<HTMLButtonElement>(
    'button[aria-label="Preview second.png"]'
  )
  assert.ok(preview)
  await act(async () => {
    preview.click()
    button(cards[1], 'Reuse settings').click()
    button(cards[1], 'Use as reference').click()
  })
  assert.deepEqual(previews, ['second'])
  assert.deepEqual(reused, ['completed-batch'])
  assert.deepEqual(references, ['second'])
})

test('submitting a generation returns paginated history to the newest works', async () => {
  function HistoryHarness() {
    const [submitted, setSubmitted] = useState<string>()
    return (
      <>
        <button type='button' onClick={() => setSubmitted('new-job')}>
          Submit new work
        </button>
        <GenerationHistory
          submittedGeneration={submitted}
          onReuse={() => {}}
          onPreview={() => {}}
          onReference={() => {}}
        />
      </>
    )
  }
  const jobs = Array.from({ length: 13 }, (_, index) => ({
    id: `job-${index}`,
    model: 'gpt-image-2',
    prompt: 'A paper boat',
    parameters: '{}',
    status: 'succeeded',
    created_at: 1,
    finished_at: 2,
    assets: [],
  }))
  const container = await render(<HistoryHarness />, (client) => {
    client.setQueryData(['multimedia', 'generations', 1], {
      items: jobs.slice(0, 12),
      total: 13,
    })
    client.setQueryData(['multimedia', 'generations', 2], {
      items: jobs.slice(12),
      total: 13,
    })
  })
  await act(async () => button(container, 'Next').click())
  assert.equal(button(container, 'Previous').disabled, false)
  await act(async () => button(container, 'Submit new work').click())
  assert.equal(button(container, 'Previous').disabled, true)
})
