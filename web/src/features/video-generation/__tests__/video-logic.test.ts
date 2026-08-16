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
  buildGenerationPayload,
  buildInitialValues,
  buildVideoAssetReferences,
  estimateVideoPrice,
  filterVideoHistoryTasks,
  normalizeVideoModel,
  normalizeVideoTask,
  validateGeneration,
  videoDownloadUrl,
} from '../lib/video-logic'
import type {
  VideoAsset,
  VideoGenerationModel,
  VideoMediaRequirement,
} from '../types'

function modelFrom(
  capabilities: Record<string, unknown>
): VideoGenerationModel {
  const model = normalizeVideoModel({
    id: 'seedance-test',
    name: 'seedance-test',
    estimated_price_usd: 0.25,
    price_available: true,
    capabilities,
  })
  assert.ok(model)
  return model
}

function uploadedAsset(
  requirement: VideoMediaRequirement,
  remoteUrl: string
): VideoAsset {
  return {
    id: `${requirement.id}:${remoteUrl}`,
    kind: requirement.kind,
    requirementId: requirement.id,
    file: new File(['test'], 'test.png', { type: 'image/png' }),
    previewUrl: 'blob:test',
    status: 'uploaded',
    progress: 100,
    remoteUrl,
  }
}

describe('video generation contract', () => {
  test('requires a prompt only when the selected model marks it required', () => {
    const model = modelFrom({
      prompt: { supported: true, required: true, max_length: 2_000 },
    })

    const issue = validateGeneration(model, buildInitialValues(model), [])

    assert.equal(model.promptRequired, true)
    assert.equal(model.promptMaxLength, 2_000)
    assert.deepEqual(issue, { code: 'prompt_required' })
  })

  test('uses nested capability defaults for generated controls', () => {
    const model = modelFrom({
      prompt: { supported: true, required: false },
      aspect_ratios: ['16:9', '9:16'],
      resolutions: ['480p', '720p'],
      durations: { supported: true, min: 3, max: 10, values: [5, 10] },
      supports_audio: true,
      supports_last_frame: true,
      advanced_fields: [
        {
          name: 'Seed',
          path: 'metadata.seed',
          widget: 'number',
          required: false,
          minimum: -1,
          maximum: 2_147_483_647,
          step: 1,
          default_value: -1,
        },
        {
          name: 'Safety tolerance',
          path: 'metadata.safety_tolerance',
          widget: 'number',
          required: false,
          minimum: 0,
          maximum: 4,
          step: 1,
          default_value: 2,
        },
      ],
      defaults: {
        aspect_ratio: '9:16',
        resolution: '720p',
        duration: 10,
        generate_audio: true,
        return_last_frame: true,
      },
    })

    const values = buildInitialValues(model)

    assert.equal(values['metadata.ratio'], '9:16')
    assert.equal(values['metadata.resolution'], '720p')
    assert.equal(values.seconds, 10)
    assert.equal(values['metadata.generate_audio'], true)
    assert.equal(values['metadata.return_last_frame'], true)
    assert.equal(values['metadata.seed'], -1)
    assert.equal(values['metadata.safety_tolerance'], 2)
    assert.equal(
      model.fields.find((field) => field.key === 'metadata.seed')?.min,
      -1
    )
    assert.deepEqual(
      model.fields.find((field) => field.key === 'metadata.safety_tolerance'),
      {
        key: 'metadata.safety_tolerance',
        label: 'Safety tolerance',
        widget: 'number',
        required: false,
        defaultValue: 2,
        options: undefined,
        min: 0,
        max: 4,
        step: 1,
        pattern: undefined,
        description: undefined,
      }
    )
  })

  test('normalizes media field, role, and item bounds independently', () => {
    const model = modelFrom({
      media: [
        {
          type: 'image',
          field: 'image_urls',
          role: 'start_frame',
          min_items: 1,
          max_items: 1,
        },
        {
          type: 'image',
          field: 'end_url',
          role: 'end_frame',
          min_items: 0,
          max_items: 1,
        },
      ],
    })

    assert.deepEqual(
      model.media.map((requirement) => ({
        key: requirement.key,
        role: requirement.role,
        min: requirement.min,
        max: requirement.max,
        label: requirement.label,
      })),
      [
        {
          key: 'image_urls',
          role: 'start_frame',
          min: 1,
          max: 1,
          label: 'Start frame',
        },
        {
          key: 'end_url',
          role: 'end_frame',
          min: 0,
          max: 1,
          label: 'End frame',
        },
      ]
    )
  })

  test('builds typed nested URL objects for metadata content', () => {
    const model = modelFrom({
      media: [
        {
          type: 'image',
          field: 'metadata.content',
          role: 'reference',
          min_items: 1,
          max_items: 2,
        },
      ],
    })
    const requirement = model.media[0]
    assert.ok(requirement)

    const payload = buildGenerationPayload(
      model,
      { prompt: 'A moving camera' },
      [uploadedAsset(requirement, 'https://cdn.example/frame.png')],
      'default'
    )

    assert.deepEqual(payload.metadata, {
      content: [
        {
          type: 'image_url',
          image_url: { url: 'https://cdn.example/frame.png' },
        },
      ],
    })
  })

  test('keeps singular URL fields as strings', () => {
    const model = modelFrom({
      media: [
        {
          type: 'video',
          field: 'metadata.video_url',
          role: 'source',
          min_items: 1,
          max_items: 1,
        },
      ],
    })
    const requirement = model.media[0]
    assert.ok(requirement)

    const payload = buildGenerationPayload(
      model,
      { prompt: '' },
      [uploadedAsset(requirement, 'https://cdn.example/source.mp4')],
      undefined
    )

    assert.deepEqual(payload.metadata, {
      video_url: 'https://cdn.example/source.mp4',
    })
  })

  test('builds Hailuo multi media fields as metadata content', () => {
    const model = modelFrom({
      media: [
        {
          type: 'image',
          field: 'metadata.content',
          role: 'reference',
          min_items: 0,
          max_items: 9,
        },
        {
          type: 'video',
          field: 'metadata.content',
          role: 'reference',
          min_items: 0,
          max_items: 3,
        },
      ],
    })
    const imageRequirement = model.media[0]
    const videoRequirement = model.media[1]
    assert.ok(imageRequirement)
    assert.ok(videoRequirement)

    const payload = buildGenerationPayload(
      model,
      { prompt: 'replace @Video 1 with @Image 1' },
      [
        uploadedAsset(imageRequirement, 'https://cdn.example/image.png'),
        uploadedAsset(videoRequirement, 'https://cdn.example/video.mp4'),
      ],
      'default'
    )

    assert.deepEqual(payload.metadata, {
      content: [
        {
          type: 'image_url',
          image_url: { url: 'https://cdn.example/image.png' },
        },
        {
          type: 'video_url',
          video_url: { url: 'https://cdn.example/video.mp4' },
        },
      ],
    })
  })

  test('builds spaced media reference tokens', () => {
    const requirement: VideoMediaRequirement = {
      id: 'video:metadata.content:reference',
      kind: 'video',
      key: 'metadata.content',
      role: 'reference',
      label: 'Reference video',
      required: false,
      min: 0,
      max: 3,
      maxSizeBytes: 50 * 1024 * 1024,
      accept: 'video/*',
    }
    const references = buildVideoAssetReferences([
      uploadedAsset(requirement, 'https://cdn.example/video.mp4'),
    ])

    assert.equal(references[0]?.token, '@Video 1')
  })

  test('binds start and end frame assets to separate payload fields', () => {
    const model = modelFrom({
      media: [
        {
          type: 'image',
          field: 'image_urls',
          role: 'start_frame',
          min_items: 1,
          max_items: 1,
        },
        {
          type: 'image',
          field: 'end_url',
          role: 'end_frame',
          min_items: 0,
          max_items: 1,
        },
      ],
    })
    const startFrame = model.media[0]
    const endFrame = model.media[1]
    assert.ok(startFrame)
    assert.ok(endFrame)

    const payload = buildGenerationPayload(
      model,
      { prompt: 'Transition between two scenes' },
      [
        uploadedAsset(startFrame, 'https://cdn.example/start.png'),
        uploadedAsset(endFrame, 'https://cdn.example/end.png'),
      ],
      'default'
    )

    assert.deepEqual(payload.images, undefined)
    assert.deepEqual(payload.image_urls, ['https://cdn.example/start.png'])
    assert.equal(payload.end_url, 'https://cdn.example/end.png')
    assert.equal('request_count' in payload, false)
  })

  test('multiplies the fixed estimate only by the requested generation count', () => {
    const model = modelFrom({})

    assert.equal(estimateVideoPrice(model, 1), 0.25)
    assert.equal(estimateVideoPrice(model, 4), 1)
    assert.equal(estimateVideoPrice(model, 2, 4), 2)
  })

  test('rejects out-of-range numbers and values outside configured options', () => {
    const model = modelFrom({
      advanced_fields: [
        {
          name: 'Safety tolerance',
          path: 'metadata.safety_tolerance',
          widget: 'number',
          minimum: 0,
          maximum: 4,
          step: 1,
        },
        {
          name: 'Mode',
          path: 'metadata.mode',
          widget: 'select',
          options: ['standard', 'fast'],
        },
      ],
    })

    assert.deepEqual(
      validateGeneration(
        model,
        { 'metadata.safety_tolerance': 5, 'metadata.mode': 'standard' },
        []
      ),
      { code: 'field_invalid', field: 'Safety tolerance' }
    )
    assert.deepEqual(
      validateGeneration(
        model,
        { 'metadata.safety_tolerance': 2, 'metadata.mode': 'unsupported' },
        []
      ),
      { code: 'field_invalid', field: 'Mode' }
    )
  })

  test('normalizes task status, percentage progress, and playback fallback', () => {
    const running = normalizeVideoTask({
      id: 7,
      task_id: 'task-running',
      status: 'IN_PROGRESS',
      progress: 0.42,
      created_at: 1_700_000_000,
      properties: { origin_model_name: 'seedance-test' },
    })
    const completed = normalizeVideoTask({
      id: 8,
      task_id: 'task-complete',
      status: 'SUCCESS',
      progress: 0,
      created_at: 1_700_000_000,
    })

    assert.ok(running)
    assert.equal(running.status, 'running')
    assert.equal(running.progress, 42)
    assert.equal(running.createdAt, 1_700_000_000_000)
    assert.ok(completed)
    assert.equal(completed.status, 'succeeded')
    assert.equal(completed.progress, 100)
    assert.equal(completed.contentUrl, '/v1/videos/task-complete/content')
    assert.deepEqual(completed.contentUrls, [
      '/v1/videos/task-complete/content',
    ])
  })

  test('preserves every proxied URL returned by a multi-result task', () => {
    const task = normalizeVideoTask({
      id: 9,
      task_id: 'task-multi',
      status: 'SUCCESS',
      data: JSON.stringify({
        video_urls: [
          '/v1/videos/task-multi/content?index=0',
          '/v1/videos/task-multi/content?index=1',
        ],
      }),
    })

    assert.ok(task)
    assert.deepEqual(task.contentUrls, [
      '/v1/videos/task-multi/content?index=0',
      '/v1/videos/task-multi/content?index=1',
    ])
  })

  test('never exposes an upstream result URL in video history', () => {
    const task = normalizeVideoTask({
      id: 10,
      task_id: 'task-signed-url',
      status: 'SUCCESS',
      result_url: 'https://upstream.example/signed/video.mp4?secret=value',
    })

    assert.ok(task)
    assert.equal(task.contentUrl, '/v1/videos/task-signed-url/content')
    assert.deepEqual(task.contentUrls, ['/v1/videos/task-signed-url/content'])
  })

  test('builds a same-origin attachment URL without dropping output index', () => {
    assert.equal(
      videoDownloadUrl('/v1/videos/task/content?index=2'),
      '/v1/videos/task/content?index=2&download=1'
    )
  })

  test('filters history by seven-day cutoff, local hides, and clear time', () => {
    const now = 2_000_000_000_000
    const task = (id: string, createdAt: number) => ({
      id,
      taskId: id,
      modelName: 'seedance-test',
      parameters: {},
      status: 'succeeded' as const,
      progress: 100,
      contentUrls: [],
      createdAt,
    })
    const tasks = [
      task('old', now - 8 * 24 * 60 * 60 * 1_000),
      task('cleared', now - 2_000),
      task('hidden', now - 1_000),
      task('visible', now),
    ]

    assert.deepEqual(
      filterVideoHistoryTasks(
        tasks,
        new Set(['hidden']),
        now,
        7,
        now - 1_500
      ).map((item) => item.id),
      ['visible']
    )
  })
})
