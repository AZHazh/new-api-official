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

import {
  DEFAULT_MAX_UPLOAD_BYTES,
  MEDIA_ACCEPT,
  MEDIA_DEFAULT_KEYS,
  MEDIA_LABEL_KEYS,
} from '../constants'
import type {
  AssetSelectionResult,
  GenerationValidationIssue,
  VideoAsset,
  VideoAssetReference,
  VideoCapabilityField,
  VideoFieldOption,
  VideoFieldWidget,
  VideoFormValues,
  VideoGenerationModel,
  VideoGenerationPayload,
  VideoMediaKind,
  VideoMediaRequirement,
  VideoTask,
  VideoTaskStatus,
} from '../types'

type UnknownRecord = Record<string, unknown>

const MEDIA_REFERENCE_LABELS: Record<VideoMediaKind, string> = {
  image: 'Image',
  video: 'Video',
  audio: 'Audio',
}

function isRecord(value: unknown): value is UnknownRecord {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function firstRecord(...values: unknown[]): UnknownRecord {
  for (const value of values) {
    if (isRecord(value)) return value
  }
  return {}
}

function firstString(...values: unknown[]): string | undefined {
  for (const value of values) {
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return undefined
}

function finiteNumber(...values: unknown[]): number | undefined {
  for (const value of values) {
    const number = typeof value === 'number' ? value : Number(value)
    if (Number.isFinite(number)) return number
  }
  return undefined
}

function positiveNumber(...values: unknown[]): number | undefined {
  const number = finiteNumber(...values)
  return number !== undefined && number > 0 ? number : undefined
}

function stringList(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.flatMap((item) => {
      if (typeof item === 'string' && item.trim()) return [item.trim()]
      if (isRecord(item)) {
        const text = firstString(item.value, item.name, item.id, item.label)
        return text ? [text] : []
      }
      return []
    })
  }
  if (typeof value === 'string') {
    return value
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean)
  }
  return []
}

function normalizeOption(value: unknown): VideoFieldOption | null {
  if (typeof value === 'string' || typeof value === 'number') {
    return { label: String(value), value }
  }
  if (!isRecord(value)) return null
  const optionValue = value.value ?? value.id ?? value.name
  if (typeof optionValue !== 'string' && typeof optionValue !== 'number') {
    return null
  }
  return {
    label: firstString(value.label, value.name) ?? String(optionValue),
    value: optionValue,
  }
}

function normalizeWidget(
  value: unknown,
  options: VideoFieldOption[]
): VideoFieldWidget {
  const widget = firstString(value)?.toLowerCase()
  if (widget === 'switch' || widget === 'checkbox' || widget === 'boolean') {
    return 'boolean'
  }
  if (widget === 'range' || widget === 'slider') return 'slider'
  if (widget === 'number' || widget === 'integer') return 'number'
  if (widget === 'select' || widget === 'enum' || options.length > 0) {
    return 'select'
  }
  return 'text'
}

function normalizeField(raw: unknown): VideoCapabilityField | null {
  if (!isRecord(raw)) return null
  const key = firstString(raw.path, raw.key, raw.name)
  if (!key || key === 'prompt' || key === 'images') return null
  let optionValues: unknown[] = []
  if (Array.isArray(raw.options)) {
    optionValues = raw.options
  } else if (Array.isArray(raw.enum)) {
    optionValues = raw.enum
  }
  const options = optionValues
    .map(normalizeOption)
    .filter((option): option is VideoFieldOption => option !== null)
  const defaultValue = raw.default_value ?? raw.default ?? raw.value
  return {
    key,
    label: firstString(raw.label, raw.title, raw.name) ?? key,
    description: firstString(raw.description, raw.help),
    widget: normalizeWidget(raw.widget ?? raw.type, options),
    required: raw.required === true,
    defaultValue:
      typeof defaultValue === 'string' ||
      typeof defaultValue === 'number' ||
      typeof defaultValue === 'boolean'
        ? defaultValue
        : undefined,
    options: options.length > 0 ? options : undefined,
    min: finiteNumber(raw.min, raw.minimum),
    max: finiteNumber(raw.max, raw.maximum),
    step: positiveNumber(raw.step),
    pattern: firstString(raw.pattern),
  }
}

function mediaKind(value: unknown): VideoMediaKind | null {
  const kind = firstString(value)?.toLowerCase()
  if (kind === 'image' || kind === 'video' || kind === 'audio') return kind
  return null
}

function mediaRequirementLabel(
  kind: VideoMediaKind,
  role: string | undefined
): string {
  switch (role?.toLowerCase()) {
    case 'start':
    case 'start_frame':
    case 'first_frame':
      return 'Start frame'
    case 'end':
    case 'end_frame':
    case 'last_frame':
      return 'End frame'
    case 'driver':
      if (kind === 'video') return 'Driver video'
      if (kind === 'audio') return 'Driver audio'
      return 'Driver image'
    case 'start_end':
      return 'Start and end frames'
    case 'element':
      return 'Element images'
    case 'source':
      if (kind === 'image') return 'Source image'
      if (kind === 'video') return 'Source video'
      return 'Source audio'
    default:
      return MEDIA_LABEL_KEYS[kind]
  }
}

function normalizeMediaRequirement(raw: unknown): VideoMediaRequirement | null {
  if (!isRecord(raw)) return null
  const kind = mediaKind(raw.kind ?? raw.type ?? raw.media_type)
  if (!kind) return null
  const required =
    raw.required === true ||
    positiveNumber(raw.min, raw.min_count, raw.min_items) !== undefined
  const min = Math.max(
    0,
    finiteNumber(raw.min, raw.min_count, raw.min_items) ?? (required ? 1 : 0)
  )
  const max = Math.max(
    min,
    finiteNumber(raw.max, raw.max_count, raw.max_items) ?? 1
  )
  const maxSizeMb = positiveNumber(raw.max_size_mb, raw.maxSizeMb)
  const maxSizeBytes = positiveNumber(raw.max_size_bytes, raw.maxSizeBytes)
  const key =
    firstString(raw.field, raw.key, raw.path, raw.target) ??
    MEDIA_DEFAULT_KEYS[kind]
  const role = firstString(raw.role)
  return {
    id: `${kind}:${key}:${role ?? 'default'}`,
    kind,
    key,
    role,
    label:
      firstString(raw.label, raw.title) ?? mediaRequirementLabel(kind, role),
    description: firstString(raw.description, raw.help),
    required,
    min,
    max,
    maxSizeBytes:
      maxSizeBytes ??
      (maxSizeMb ? maxSizeMb * 1024 * 1024 : DEFAULT_MAX_UPLOAD_BYTES),
    accept: firstString(raw.accept) ?? MEDIA_ACCEPT[kind],
  }
}

function appendCapabilityFields(
  fields: VideoCapabilityField[],
  source: UnknownRecord
): VideoCapabilityField[] {
  const result = [...fields]
  const knownKeys = new Set(result.map((field) => field.key))
  const defaults = firstRecord(source.defaults)
  const candidates: Array<[string, string, unknown, unknown]> = [
    [
      'metadata.ratio',
      'Aspect ratio',
      source.aspect_ratios ?? source.ratios,
      defaults.aspect_ratio ?? source.default_ratio,
    ],
    [
      'metadata.resolution',
      'Resolution',
      source.resolutions,
      defaults.resolution ?? source.default_resolution,
    ],
    [
      'seconds',
      'Duration',
      source.durations ?? source.seconds,
      defaults.duration ?? source.default_duration ?? source.default_seconds,
    ],
  ]
  for (const [key, label, optionsValue, defaultValue] of candidates) {
    if (knownKeys.has(key)) continue
    const options = Array.isArray(optionsValue)
      ? optionsValue
          .map(normalizeOption)
          .filter((option): option is VideoFieldOption => option !== null)
      : []
    if (options.length === 0) continue
    result.push({
      key,
      label,
      widget: 'select',
      required: false,
      options,
      defaultValue: defaultValue as string | number | undefined,
    })
    knownKeys.add(key)
  }
  const toggles: Array<[string, string, unknown, unknown]> = [
    [
      'metadata.generate_audio',
      'Generate audio',
      source.generate_audio ?? source.supports_audio,
      defaults.generate_audio ?? source.default_generate_audio,
    ],
    [
      'metadata.return_last_frame',
      'Return last frame',
      source.return_last_frame ?? source.supports_last_frame,
      defaults.return_last_frame ?? source.default_return_last_frame,
    ],
  ]
  for (const [key, label, supported, defaultValue] of toggles) {
    if (knownKeys.has(key) || supported !== true) continue
    result.push({
      key,
      label,
      widget: 'boolean',
      required: false,
      defaultValue: defaultValue === true,
    })
  }
  return result
}

function addField(
  fields: VideoCapabilityField[],
  knownKeys: Set<string>,
  field: VideoCapabilityField
): void {
  if (knownKeys.has(field.key)) return
  fields.push(field)
  knownKeys.add(field.key)
}

function generationCapabilityFields(
  fields: VideoCapabilityField[],
  capability: UnknownRecord
): VideoCapabilityField[] {
  const result = [...fields]
  const knownKeys = new Set(result.map((field) => field.key))
  const defaults = firstRecord(capability.defaults)
  const selectField = (
    key: string,
    label: string,
    values: unknown,
    defaultValue: unknown
  ): void => {
    const options = Array.isArray(values)
      ? values
          .map(normalizeOption)
          .filter((option): option is VideoFieldOption => option !== null)
      : []
    if (options.length === 0) return
    addField(result, knownKeys, {
      key,
      label,
      widget: 'select',
      required: false,
      options,
      defaultValue:
        typeof defaultValue === 'string' || typeof defaultValue === 'number'
          ? defaultValue
          : options[0].value,
    })
  }
  selectField(
    'metadata.ratio',
    'Aspect ratio',
    capability.aspect_ratios,
    defaults.aspect_ratio
  )
  selectField(
    'metadata.resolution',
    'Resolution',
    capability.resolutions,
    defaults.resolution
  )

  const durations = firstRecord(capability.durations)
  if (durations.supported === true) {
    const values = Array.isArray(durations.values)
      ? durations.values
          .map((value) => finiteNumber(value))
          .filter((value): value is number => value !== undefined)
      : []
    const smartValue = finiteNumber(durations.smart_value)
    const minimum = finiteNumber(durations.min) ?? 1
    const maximum = finiteNumber(durations.max) ?? minimum
    if (values.length > 0 || smartValue !== undefined) {
      const options: VideoFieldOption[] = []
      if (smartValue !== undefined) {
        options.push({ label: 'Smart', value: smartValue })
      }
      const rangeValues =
        values.length > 0
          ? values
          : Array.from(
              { length: Math.min(120, Math.max(0, maximum - minimum + 1)) },
              (_, index) => minimum + index
            )
      options.push(
        ...rangeValues.map((value) => ({ label: `${value}s`, value }))
      )
      addField(result, knownKeys, {
        key: 'seconds',
        label: 'Video duration',
        widget: 'select',
        required: false,
        options,
        defaultValue: finiteNumber(defaults.duration) ?? options[0]?.value,
      })
    } else {
      addField(result, knownKeys, {
        key: 'seconds',
        label: 'Video duration',
        widget: 'slider',
        required: false,
        min: minimum,
        max: maximum,
        step: 1,
        defaultValue: finiteNumber(defaults.duration) ?? minimum,
      })
    }
  }

  if (capability.supports_audio === true) {
    addField(result, knownKeys, {
      key: 'metadata.generate_audio',
      label: 'Generate audio',
      widget: 'boolean',
      required: false,
      defaultValue: defaults.generate_audio === true,
    })
  }
  if (capability.supports_last_frame === true) {
    addField(result, knownKeys, {
      key: 'metadata.return_last_frame',
      label: 'Return last frame',
      widget: 'boolean',
      required: false,
      defaultValue: defaults.return_last_frame === true,
    })
  }
  selectField(
    'batch_size',
    'Batch size',
    capability.batch_sizes,
    defaults.batch_size
  )

  if (Array.isArray(capability.advanced_fields)) {
    for (const rawField of capability.advanced_fields) {
      const field = normalizeField(rawField)
      if (field) addField(result, knownKeys, field)
    }
  }
  return result
}

function mediaFromKind(kindValue: unknown): VideoMediaRequirement[] {
  const kind = firstString(kindValue)?.toLowerCase()
  const requirement = (
    media: VideoMediaKind,
    min = 1,
    max = 1
  ): VideoMediaRequirement => ({
    id: `${media}:${MEDIA_DEFAULT_KEYS[media]}:default`,
    kind: media,
    key: MEDIA_DEFAULT_KEYS[media],
    label: MEDIA_LABEL_KEYS[media],
    required: true,
    min,
    max,
    maxSizeBytes: DEFAULT_MAX_UPLOAD_BYTES,
    accept: MEDIA_ACCEPT[media],
  })
  switch (kind) {
    case 'image':
    case 'i2v':
    case 'reference':
    case 'r2v':
      return [requirement('image')]
    case 'start_end':
      return [requirement('image', 2, 2)]
    case 'video':
    case 'v2v':
    case 'edit':
    case 'motion':
    case 'upscale':
      return [requirement('video')]
    case 'lip_tts':
    case 'lip_video':
      return [requirement('video')]
    default:
      return []
  }
}

export function normalizeVideoModel(raw: unknown): VideoGenerationModel | null {
  if (!isRecord(raw)) return null
  const capability = firstRecord(raw.capability, raw.capabilities, raw.schema)
  const modelName = firstString(raw.model_name, raw.model, raw.name, raw.id)
  if (!modelName) return null
  let rawFields: unknown[] = []
  if (Array.isArray(capability.fields)) {
    rawFields = capability.fields
  } else if (Array.isArray(raw.fields)) {
    rawFields = raw.fields
  }
  const fields = generationCapabilityFields(
    appendCapabilityFields(
      rawFields
        .map(normalizeField)
        .filter((field): field is VideoCapabilityField => field !== null),
      { ...raw, ...capability }
    ),
    capability
  )
  let rawMedia: unknown[] = []
  if (Array.isArray(capability.media)) {
    rawMedia = capability.media
  } else if (Array.isArray(capability.media_requirements)) {
    rawMedia = capability.media_requirements
  } else if (Array.isArray(raw.media)) {
    rawMedia = raw.media
  }
  let media = rawMedia
    .map(normalizeMediaRequirement)
    .filter(
      (requirement): requirement is VideoMediaRequirement =>
        requirement !== null
    )
  if (media.length === 0) media = mediaFromKind(capability.kind ?? raw.kind)
  const groupRatiosRecord = firstRecord(raw.group_ratio, raw.group_ratios)
  const groupRatios: Record<string, number> = {}
  for (const [group, ratioValue] of Object.entries(groupRatiosRecord)) {
    const ratio = positiveNumber(ratioValue)
    if (ratio) groupRatios[group] = ratio
  }
  const groups = stringList(
    raw.groups ?? raw.enable_groups ?? capability.groups
  )
  const tags = stringList(raw.tags)
  const prompt = firstRecord(capability.prompt)
  const priceUsd =
    raw.price_available === false
      ? undefined
      : positiveNumber(
          raw.estimated_price_usd,
          raw.price_usd,
          raw.model_price,
          raw.price
        )
  return {
    id: firstString(raw.id) ?? modelName,
    modelName,
    displayName: firstString(raw.display_name, raw.title) ?? modelName,
    description: firstString(raw.description, capability.description),
    family: firstString(raw.family, capability.family),
    category: firstString(raw.category, capability.category),
    tags,
    createdAt: finiteNumber(raw.created_at, raw.created_time),
    recentCallCount: Math.max(
      0,
      finiteNumber(raw.recent_call_count, raw.popularity) ?? 0
    ),
    featured: raw.featured === true || raw.is_featured === true,
    isNew: raw.is_new === true,
    available: raw.available !== false && raw.enabled !== false,
    promptSupported: prompt.supported !== false,
    promptRequired: prompt.required === true,
    promptMaxLength: positiveNumber(prompt.max_length),
    fields,
    media,
    priceUsd,
    groupRatios,
    groups,
    defaultGroup: firstString(raw.default_group, capability.default_group),
    requirements: stringList(capability.requirements),
  }
}

export function buildInitialValues(
  model: VideoGenerationModel
): VideoFormValues {
  const values: VideoFormValues = { prompt: '' }
  for (const field of model.fields) {
    if (field.defaultValue !== undefined) {
      values[field.key] = field.defaultValue
      continue
    }
    if (field.widget === 'boolean') values[field.key] = false
    else if (field.options?.[0]) values[field.key] = field.options[0].value
    else if (field.min !== undefined) values[field.key] = field.min
    else values[field.key] = ''
  }
  return values
}

function acceptedMime(file: File, requirement: VideoMediaRequirement): boolean {
  const acceptParts = requirement.accept
    .split(',')
    .map((part) => part.trim().toLowerCase())
  const filename = file.name.toLowerCase()
  const mime = file.type.toLowerCase()
  return acceptParts.some((part) => {
    if (part.startsWith('.')) return filename.endsWith(part)
    if (part.endsWith('/*')) return mime.startsWith(part.slice(0, -1))
    return mime === part
  })
}

export function validateAssetSelection(
  files: File[],
  currentCount: number,
  requirement: VideoMediaRequirement
): AssetSelectionResult {
  const accepted: File[] = []
  const rejected: AssetSelectionResult['rejected'] = []
  let remaining = Math.max(0, requirement.max - currentCount)
  for (const file of files) {
    if (!acceptedMime(file, requirement)) {
      rejected.push({ file, code: 'unsupported_type' })
      continue
    }
    if (file.size <= 0 || file.size > requirement.maxSizeBytes) {
      rejected.push({
        file,
        code: 'file_too_large',
        max: requirement.maxSizeBytes,
      })
      continue
    }
    if (remaining === 0) {
      rejected.push({ file, code: 'too_many_files', max: requirement.max })
      continue
    }
    accepted.push(file)
    remaining -= 1
  }
  return { accepted, rejected }
}

function isBlank(value: unknown): boolean {
  return (
    value === undefined ||
    value === null ||
    (typeof value === 'string' && value.trim() === '')
  )
}

export function validateGeneration(
  model: VideoGenerationModel,
  values: VideoFormValues,
  assets: VideoAsset[]
): GenerationValidationIssue | null {
  if (model.promptRequired && isBlank(values.prompt)) {
    return { code: 'prompt_required' }
  }
  if (
    model.promptMaxLength !== undefined &&
    String(values.prompt ?? '').length > model.promptMaxLength
  ) {
    return { code: 'field_invalid', field: 'Prompt' }
  }
  for (const field of model.fields) {
    const value = values[field.key]
    if (field.required && isBlank(value)) {
      return { code: 'field_required', field: field.label }
    }
    if (isBlank(value)) continue
    if (
      field.options &&
      !field.options.some((option) => option.value === value)
    ) {
      return { code: 'field_invalid', field: field.label }
    }
    if (field.widget === 'number' || field.widget === 'slider') {
      const number = Number(value)
      if (!Number.isFinite(number)) {
        return { code: 'field_invalid', field: field.label }
      }
      if (field.min !== undefined && number < field.min) {
        return { code: 'field_invalid', field: field.label }
      }
      if (field.max !== undefined && number > field.max) {
        return { code: 'field_invalid', field: field.label }
      }
      if (field.step !== undefined) {
        const offset = number - (field.min ?? 0)
        const steps = offset / field.step
        if (Math.abs(steps - Math.round(steps)) > 1e-9) {
          return { code: 'field_invalid', field: field.label }
        }
      }
    }
    if (field.pattern) {
      try {
        if (!new RegExp(field.pattern).test(String(value))) {
          return { code: 'field_invalid', field: field.label }
        }
      } catch {
        return { code: 'field_invalid', field: field.label }
      }
    }
  }
  for (const requirement of model.media) {
    const matching = assets.filter(
      (asset) => asset.requirementId === requirement.id
    )
    if (matching.length < requirement.min) {
      return {
        code: 'media_required',
        field: requirement.label,
        kind: requirement.kind,
        min: requirement.min,
      }
    }
    if (matching.some((asset) => asset.status === 'failed')) {
      return {
        code: 'upload_failed',
        field: requirement.label,
        kind: requirement.kind,
      }
    }
    if (
      matching.some((asset) => asset.status !== 'uploaded' || !asset.remoteUrl)
    ) {
      return {
        code: 'upload_pending',
        field: requirement.label,
        kind: requirement.kind,
      }
    }
  }
  const uploadedAssets = assets.filter(
    (asset) => asset.status === 'uploaded' && asset.remoteUrl
  )
  if (
    model.requirements.includes('at_least_one_media') &&
    uploadedAssets.length === 0
  ) {
    return { code: 'input_required' }
  }
  if (
    model.requirements.includes('prompt_or_media_or_local_task') &&
    isBlank(values.prompt) &&
    uploadedAssets.length === 0 &&
    isBlank(values['metadata.extend_from_task_id'])
  ) {
    return { code: 'input_required' }
  }
  return null
}

export function buildVideoAssetReferences(
  assets: VideoAsset[]
): VideoAssetReference[] {
  const counts: Record<VideoMediaKind, number> = {
    image: 0,
    video: 0,
    audio: 0,
  }
  return assets.map((asset) => {
    counts[asset.kind] += 1
    const label = MEDIA_REFERENCE_LABELS[asset.kind]
    return {
      assetId: asset.id,
      token: `@${label} ${counts[asset.kind]}`,
    }
  })
}

export function insertPromptText(
  prompt: string,
  insertion: string,
  selectionStart = prompt.length,
  selectionEnd = selectionStart
): { value: string; cursor: number } {
  const start = Math.max(0, Math.min(prompt.length, selectionStart))
  const end = Math.max(start, Math.min(prompt.length, selectionEnd))
  const before = prompt.slice(0, start)
  const after = prompt.slice(end)
  const prefix = before.length > 0 && !/\s$/.test(before) ? ' ' : ''
  const suffix = after.length > 0 && !/^\s/.test(after) ? ' ' : ''
  const text = `${prefix}${insertion}${suffix}`
  return {
    value: `${before}${text}${after}`,
    cursor: before.length + text.length,
  }
}

function setPath(
  target: Record<string, unknown>,
  path: string,
  value: unknown
): void {
  const keys = path.split('.').filter(Boolean)
  if (keys.length === 0) return
  let cursor = target
  for (let index = 0; index < keys.length - 1; index += 1) {
    const key = keys[index]
    const next = cursor[key]
    if (!isRecord(next)) cursor[key] = {}
    cursor = cursor[key] as Record<string, unknown>
  }
  const finalKey = keys.at(-1)
  if (finalKey) cursor[finalKey] = value
}

export function buildGenerationPayload(
  model: VideoGenerationModel,
  values: VideoFormValues,
  assets: VideoAsset[],
  group: string | undefined
): VideoGenerationPayload {
  const payload: VideoGenerationPayload = { model: model.modelName }
  if (group) payload.group = group
  const prompt = String(values.prompt ?? '').trim()
  if (prompt) payload.prompt = prompt
  for (const field of model.fields) {
    const value = values[field.key]
    if (!isBlank(value)) setPath(payload, field.key, value)
  }
  // The Seedance video contract encodes duration as a string (for example
  // `"5"` or `"-1"`), while the form control stores numeric values.
  if (payload.seconds !== undefined && payload.seconds !== null) {
    payload.seconds = String(payload.seconds)
  }
  for (const requirement of model.media) {
    const urls = assets
      .filter(
        (asset) => asset.requirementId === requirement.id && asset.remoteUrl
      )
      .map((asset) => asset.remoteUrl as string)
    if (urls.length === 0) continue
    if (requirement.key === 'metadata.content') {
      const metadata = isRecord(payload.metadata) ? payload.metadata : {}
      const content = Array.isArray(metadata.content) ? metadata.content : []
      const mediaField = `${requirement.kind}_url`
      metadata.content = [
        ...content,
        ...urls.map((url) => ({
          type: mediaField,
          [mediaField]: { url },
        })),
      ]
      payload.metadata = metadata
      continue
    }
    const singular =
      requirement.max === 1 &&
      (requirement.key === 'end_url' || requirement.key.endsWith('_url'))
    setPath(payload, requirement.key, singular ? urls[0] : urls)
  }
  if (
    isRecord(payload.metadata) &&
    Object.keys(payload.metadata).length === 0
  ) {
    delete payload.metadata
  }
  return payload
}

export function estimateVideoPrice(
  model: VideoGenerationModel,
  requestCount: number,
  batchSize = 1
): number | null {
  if (
    !model.priceUsd ||
    !Number.isFinite(model.priceUsd) ||
    model.priceUsd <= 0
  ) {
    return null
  }
  const count = Math.max(1, Math.floor(requestCount))
  const normalizedBatchSize =
    Number.isFinite(batchSize) && batchSize > 0 ? Math.floor(batchSize) : 1
  const estimate = model.priceUsd * count * normalizedBatchSize
  return Number.isFinite(estimate) && estimate > 0 ? estimate : null
}

export function videoDownloadUrl(contentUrl: string): string {
  const url = new URL(contentUrl, 'https://video-content.local')
  url.searchParams.set('download', '1')
  if (url.origin === 'https://video-content.local') {
    return `${url.pathname}${url.search}${url.hash}`
  }
  return url.toString()
}

export function filterVideoHistoryTasks(
  tasks: VideoTask[],
  hiddenTaskIds: ReadonlySet<string>,
  now: number,
  historyDays: number,
  clearedBefore = 0
): VideoTask[] {
  const cutoff = now - historyDays * 24 * 60 * 60 * 1_000
  return tasks.filter(
    (task) =>
      task.createdAt >= cutoff &&
      task.createdAt > clearedBefore &&
      !hiddenTaskIds.has(task.id)
  )
}

export function normalizeTaskStatus(value: unknown): VideoTaskStatus {
  const status = firstString(value)?.toUpperCase()
  if (
    status === 'SUCCESS' ||
    status === 'SUCCEEDED' ||
    status === 'COMPLETED'
  ) {
    return 'succeeded'
  }
  if (
    status === 'FAILURE' ||
    status === 'FAILED' ||
    status === 'ERROR' ||
    status === 'CANCELLED' ||
    status === 'SUBMIT_UNKNOWN'
  ) {
    return 'failed'
  }
  if (
    status === 'IN_PROGRESS' ||
    status === 'RUNNING' ||
    status === 'PROCESSING'
  ) {
    return 'running'
  }
  if (
    status === 'NOT_START' ||
    status === 'SUBMITTING' ||
    status === 'SUBMITTED' ||
    status === 'QUEUED'
  ) {
    return 'pending'
  }
  return 'unknown'
}

export function normalizeTaskProgress(
  value: unknown,
  status: VideoTaskStatus
): number {
  if (status === 'succeeded') return 100
  const text = typeof value === 'string' ? value.replace('%', '').trim() : value
  const number = finiteNumber(text) ?? 0
  const percent = number > 0 && number <= 1 ? number * 100 : number
  return Math.min(100, Math.max(0, percent))
}

function recordFromJson(value: unknown): UnknownRecord {
  if (isRecord(value)) return value
  if (typeof value !== 'string' || !value.trim()) return {}
  try {
    const parsed: unknown = JSON.parse(value)
    return isRecord(parsed) ? parsed : {}
  } catch {
    return {}
  }
}

function arrayStrings(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.filter(
    (item): item is string => typeof item === 'string' && item.trim().length > 0
  )
}

function epochMilliseconds(...values: unknown[]): number | undefined {
  const epoch = finiteNumber(...values)
  if (!epoch || epoch <= 0) return undefined
  return epoch < 1_000_000_000_000 ? epoch * 1000 : epoch
}

function taskParameters(input: UnknownRecord): VideoFormValues {
  const values: VideoFormValues = {}
  for (const [key, value] of Object.entries(input)) {
    if (key === 'model' || key === 'group' || key === 'metadata') continue
    if (
      typeof value === 'string' ||
      typeof value === 'number' ||
      typeof value === 'boolean'
    ) {
      values[key] = value
    }
  }
  const metadata = firstRecord(input.metadata)
  for (const [key, value] of Object.entries(metadata)) {
    if (
      typeof value === 'string' ||
      typeof value === 'number' ||
      typeof value === 'boolean'
    ) {
      values[`metadata.${key}`] = value
    }
  }
  return values
}

export function normalizeVideoTask(raw: unknown): VideoTask | null {
  if (!isRecord(raw)) return null
  const taskId = firstString(raw.task_id, raw.taskId, raw.id)
  if (!taskId) return null
  const data = recordFromJson(raw.data)
  const output = firstRecord(data.output, data.result)
  const properties = firstRecord(raw.properties)
  const input = recordFromJson(properties.input ?? raw.input)
  const status = normalizeTaskStatus(raw.status ?? data.status)
  const safeContentUrls = [
    data.content_url,
    data.video_url,
    output.content_url,
    output.video_url,
    ...arrayStrings(data.video_urls),
    ...arrayStrings(output.video_urls),
    raw.content_url,
  ].flatMap((value) => {
    if (typeof value !== 'string') return []
    const url = value.trim()
    if (!url.startsWith('/v1/videos/') || !url.includes('/content')) return []
    return [url]
  })
  const contentUrls = [...new Set(safeContentUrls)]
  if (status === 'succeeded' && contentUrls.length === 0) {
    contentUrls.push(`/v1/videos/${encodeURIComponent(taskId)}/content`)
  }
  const createdAt =
    epochMilliseconds(raw.created_at, raw.submit_time, data.created_at) ??
    Date.now()
  return {
    id: String(raw.id ?? taskId),
    taskId,
    modelName:
      firstString(
        properties.origin_model_name,
        properties.upstream_model_name,
        raw.model,
        data.model,
        input.model
      ) ?? 'Video',
    prompt: firstString(raw.prompt, data.prompt, input.prompt),
    parameters: taskParameters(input),
    status,
    progress: normalizeTaskProgress(raw.progress ?? data.progress, status),
    progressText: firstString(raw.progress_message, data.progress_message),
    failureReason: firstString(
      raw.fail_reason,
      raw.error,
      data.fail_reason,
      data.error
    ),
    contentUrl: contentUrls[0],
    contentUrls,
    thumbnailUrl: firstString(
      raw.thumbnail_url,
      data.thumbnail_url,
      output.thumbnail_url
    ),
    createdAt,
    finishedAt: epochMilliseconds(raw.finish_time, data.completed_at),
    expiresAt: epochMilliseconds(raw.result_expires_at, data.expires_at),
  }
}
