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
import { describe, test } from 'node:test'

import {
  buildImageRequest,
  IMAGE_FORM_DEFAULTS,
  imageFormSchema,
  imageSize,
} from '../lib/image-form'
import type { ImageModel, MediaAsset } from '../types'

const model: ImageModel = {
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
}
const reference: MediaAsset = {
  id: 'reference',
  kind: 'reference',
  name: 'reference.png',
  mime_type: 'image/png',
  width: 1024,
  height: 1024,
  bytes: 100,
  created_at: 1,
  expires_at: 2,
}

describe('image generation request contract', () => {
  test('JPEG and WebP retain explicit zero compression while PNG omits compression', () => {
    const values = {
      ...IMAGE_FORM_DEFAULTS,
      model: model.name,
      prompt: ' A paper boat ',
      compression: 0,
    }
    const webp = buildImageRequest(
      { ...values, outputFormat: 'webp' },
      model,
      [reference],
      'request-key'
    )
    assert.equal(webp.output_compression, 0)
    assert.equal(webp.prompt, 'A paper boat')
    assert.deepEqual(webp.references, ['reference'])
    assert.equal(
      buildImageRequest(values, model, [], 'request-key').output_compression,
      undefined
    )
  })
  test('models without advanced settings omit all unsupported upstream fields', () => {
    const legacy = {
      ...model,
      capabilities: {
        ...model.capabilities,
        advanced: false,
        custom_size: false,
      },
    }
    const result = buildImageRequest(
      { ...IMAGE_FORM_DEFAULTS, ratio: '1024x1024' },
      legacy,
      [],
      'request-key'
    )
    assert.equal(result.size, '1024x1024')
    assert.equal('background' in result, false)
    assert.equal('moderation' in result, false)
    assert.equal(result.output_format, undefined)
    assert.equal('mask' in result, false)
  })
  test('4K panoramas stay within the server dimension limit and custom sizes are preserved', () => {
    assert.equal(
      imageSize(
        { ...IMAGE_FORM_DEFAULTS, ratio: '21:9', resolution: '4K' },
        model
      ),
      '3840x1648'
    )
    assert.equal(
      imageSize(
        { ...IMAGE_FORM_DEFAULTS, ratio: 'custom', width: 768, height: 1280 },
        model
      ),
      '768x1280'
    )
    assert.equal(
      imageSize({ ...IMAGE_FORM_DEFAULTS, ratio: 'auto' }, model),
      'auto'
    )
  })
  test('empty prompts, invalid counts and oversized dimensions cannot be submitted', () => {
    const valid = {
      ...IMAGE_FORM_DEFAULTS,
      model: model.name,
      prompt: 'A paper boat',
    }
    assert.equal(imageFormSchema.safeParse(valid).success, true)
    for (const patch of [
      { prompt: ' ' },
      { n: 0 },
      { n: 129 },
      { n: Number.NaN },
      { width: 3841 },
      { compression: 101 },
    ]) {
      assert.equal(
        imageFormSchema.safeParse({ ...valid, ...patch }).success,
        false
      )
    }
  })
})
