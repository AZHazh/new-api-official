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
  buildVideoSample,
  getVideoApiProtocol,
  type VideoSampleContext,
} from '../../lib/video-api-samples'

const SAMPLE_CONTEXT: VideoSampleContext = {
  baseUrl: 'https://new-api.example',
  apiKeyEnv: 'NEW_API_KEY',
  modelName: 'seedance-2.0-mini-t2v',
  endpointPath: '/v1/videos',
}

describe('video API samples', () => {
  test('uses the main videos submit and local query endpoints for video output', () => {
    const sample = buildVideoSample('typescript', SAMPLE_CONTEXT)

    assert.equal(getVideoApiProtocol(SAMPLE_CONTEXT.modelName), 'video-output')
    assert.match(sample, /\/v1\/videos'/)
    assert.match(sample, /\/v1\/videos\/\$\{taskId\}/)
    assert.doesNotMatch(sample, /chat\/completions/)
  })

  test('uses the legacy workflow and result_text for every Context IR SKU', () => {
    const models = [
      'minmax-h3-context-ir-text',
      'minmax-h3-context-ir-image',
      'minmax-h3-context-ir-multimodal',
    ]

    for (const modelName of models) {
      const sample = buildVideoSample('python', {
        ...SAMPLE_CONTEXT,
        modelName,
      })

      assert.equal(getVideoApiProtocol(modelName), 'video-prompt-enhancer')
      assert.match(sample, /\/v1\/video\/generations/)
      assert.match(sample, /task\.get\("data", \{\}\)\.get\("task_id"\)/)
      assert.match(sample, /result_text/)
      assert.doesNotMatch(sample, /\/v1\/videos/)
    }
  })

  test('uses the dedicated Midjourney Video submit and query endpoints', () => {
    const sample = buildVideoSample('typescript', {
      ...SAMPLE_CONTEXT,
      modelName: 'midjourney-video',
    })

    assert.equal(getVideoApiProtocol('midjourney-video'), 'midjourney-video')
    assert.match(sample, /\/v1\/midjourney\/generations\/video/)
    assert.match(sample, /\/v1\/midjourney\/tasks\/\$\{taskId\}/)
    assert.match(sample, /task\.data\?\.\[0\]\?\.task_id/)
    assert.match(sample, /"batch_size":1/)
  })
})
