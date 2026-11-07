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
import { afterEach, test } from 'node:test'

import { api } from '@/lib/api'

import { getImageBlob } from '../api'

const previousAdapter = api.defaults.adapter
afterEach(() => {
  api.defaults.adapter = previousAdapter
})

test('cancelling an image preview does not cancel an immediately reopened preview', async () => {
  const image = new Blob(['image'], { type: 'image/png' })
  let started: (() => void) | undefined
  const firstStarted = new Promise<void>((resolve) => {
    started = resolve
  })
  api.defaults.adapter = (config) => {
    if (config.signal?.aborted) return Promise.reject(new Error('aborted'))
    if (!config.signal) {
      throw new Error('The image fetch must retain its cancellation signal')
    }
    if (config.signal === first.signal) {
      started?.()
      return new Promise((_resolve, reject) =>
        config.signal?.addEventListener?.('abort', () =>
          reject(new Error('aborted'))
        )
      )
    }
    return Promise.resolve({
      data: image,
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    })
  }
  const first = new AbortController()
  const cancelled = getImageBlob('reopened-preview', true, first.signal)
  const rejection = assert.rejects(cancelled)
  await firstStarted
  first.abort()
  const reopened = getImageBlob(
    'reopened-preview',
    true,
    new AbortController().signal
  )
  await rejection
  assert.equal(await reopened, image)
})
