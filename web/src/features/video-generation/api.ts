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
import { api, getFreshAuthHeaders } from '@/lib/api'

import { normalizeVideoModel, normalizeVideoTask } from './lib/video-logic'
import type {
  VideoGenerationPayload,
  VideoModelsCatalog,
  VideoSubmissionResult,
  VideoTaskPage,
} from './types'

type UnknownRecord = Record<string, unknown>

function isRecord(value: unknown): value is UnknownRecord {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function unwrapResponse(value: unknown): unknown {
  if (!isRecord(value)) return value
  if (value.success === false) {
    throw new Error(
      typeof value.message === 'string' ? value.message : 'Request failed'
    )
  }
  return value.data ?? value
}

function stringValue(...values: unknown[]): string | undefined {
  for (const value of values) {
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return undefined
}

function stringArray(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.filter(
      (item): item is string =>
        typeof item === 'string' && item.trim().length > 0
    )
  }
  if (isRecord(value)) return Object.keys(value)
  return []
}

export async function optimizeVideoPrompt(
  prompt: string,
  group?: string,
  signal?: AbortSignal
): Promise<string> {
  const response = await api.post(
    '/api/video-generation/optimize',
    { prompt, ...(group ? { group } : {}) },
    { signal, skipErrorHandler: true }
  )
  const payload = unwrapResponse(response.data)
  if (!isRecord(payload) || typeof payload.prompt !== 'string') {
    throw new Error('Prompt optimizer returned no text')
  }
  return payload.prompt.trim()
}

export async function getVideoModels(
  group?: string
): Promise<VideoModelsCatalog> {
  const response = await api.get('/api/video-generation/models', {
    params: group ? { group } : undefined,
    skipErrorHandler: true,
  })
  const payload = unwrapResponse(response.data)
  const record = isRecord(payload) ? payload : {}
  let rawModels: unknown[] = []
  if (Array.isArray(payload)) {
    rawModels = payload
  } else if (Array.isArray(record.models)) {
    rawModels = record.models
  } else if (Array.isArray(record.items)) {
    rawModels = record.items
  }
  const upload = isRecord(record.upload) ? record.upload : {}
  const uploadMaxBytes = Number(upload.max_bytes)
  const maxUploadBytes =
    Number.isFinite(uploadMaxBytes) && uploadMaxBytes > 0
      ? uploadMaxBytes
      : undefined
  const catalogGroup = stringValue(record.group, group)
  const models = rawModels
    .map(normalizeVideoModel)
    .filter((model): model is NonNullable<typeof model> => model !== null)
    .filter((model) => model.available)
    .map((model) => ({
      ...model,
      defaultGroup: catalogGroup,
      groups: catalogGroup ? [catalogGroup] : model.groups,
      media: model.media.map((requirement) => ({
        ...requirement,
        maxSizeBytes: maxUploadBytes ?? requirement.maxSizeBytes,
      })),
    }))
  const responseGroups = stringArray(record.groups)
  const modelGroups = models.flatMap((model) => model.groups)
  const groups = [...new Set([...responseGroups, ...modelGroups])]
  return {
    models,
    groups,
    defaultGroup: catalogGroup,
    estimateIsMaximum: record.estimate_is_maximum === true,
  }
}

export async function uploadVideoAsset(
  file: File,
  group: string | undefined,
  onProgress: (progress: number) => void,
  signal?: AbortSignal
): Promise<string> {
  const body = new FormData()
  body.append('file', file, file.name)
  const response = await api.post('/pg/files/upload', body, {
    params: group ? { group } : undefined,
    signal,
    skipErrorHandler: true,
    onUploadProgress: (event) => {
      if (!event.total) return
      onProgress(Math.min(99, Math.round((event.loaded / event.total) * 100)))
    },
  })
  const payload = unwrapResponse(response.data)
  const record = isRecord(payload) ? payload : {}
  const nestedFile = isRecord(record.file) ? record.file : {}
  const url = stringValue(
    record.url,
    record.file_url,
    record.download_url,
    nestedFile.url
  )
  if (!url) throw new Error('Upload response did not include a file URL')
  onProgress(100)
  return url
}

export async function submitVideoGeneration(
  payload: VideoGenerationPayload
): Promise<VideoSubmissionResult> {
  // The video page can stay open longer than the short-lived dashboard access
  // token. Refresh it immediately before submitting so an expired token is
  // never used for the task-creation request.
  const headers = await getFreshAuthHeaders()
  const response = await api.post('/pg/video/generations', payload, {
    headers,
    skipErrorHandler: true,
  })
  const data = unwrapResponse(response.data)
  const record = isRecord(data) ? data : {}
  const nestedTask = isRecord(record.task) ? record.task : {}
  return {
    taskId: stringValue(
      record.task_id,
      record.id,
      nestedTask.task_id,
      nestedTask.id
    ),
    status: stringValue(record.status, nestedTask.status),
    raw: data,
  }
}

export async function getVideoTasks(): Promise<VideoTaskPage> {
  const historyStartTimestamp = Math.floor(
    (Date.now() - 7 * 24 * 60 * 60 * 1_000) / 1_000
  )
  const response = await api.get('/api/task/self', {
    params: {
      p: 1,
      page_size: 100,
      platform: 61,
      actions: 'seedance_video,seedance_midjourney_video',
      start_timestamp: historyStartTimestamp,
    },
    disableDuplicate: true,
    skipErrorHandler: true,
  })
  const payload = unwrapResponse(response.data)
  const record = isRecord(payload) ? payload : {}
  let rawItems: unknown[] = []
  if (Array.isArray(record.items)) {
    rawItems = record.items
  } else if (Array.isArray(payload)) {
    rawItems = payload
  }
  const items = rawItems.flatMap((item) => {
    const task = normalizeVideoTask(item)
    if (!task) return []
    if (task.contentUrls.length <= 1) return [task]
    return task.contentUrls.map((contentUrl, outputIndex) => ({
      ...task,
      id: `${task.id}:${outputIndex}`,
      contentUrl,
      outputIndex,
    }))
  })
  const totalValue = Number(record.total)
  return {
    items,
    total: Number.isFinite(totalValue) ? totalValue : items.length,
  }
}
