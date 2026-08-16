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

export type VideoMediaKind = 'image' | 'video' | 'audio'

export type VideoFieldWidget =
  | 'select'
  | 'slider'
  | 'number'
  | 'boolean'
  | 'text'

export type VideoFieldOption = {
  label: string
  value: string | number
}

export type VideoCapabilityField = {
  key: string
  label: string
  description?: string
  widget: VideoFieldWidget
  required: boolean
  defaultValue?: string | number | boolean
  options?: VideoFieldOption[]
  min?: number
  max?: number
  step?: number
  pattern?: string
}

export type VideoMediaRequirement = {
  id: string
  kind: VideoMediaKind
  key: string
  role?: string
  label: string
  description?: string
  required: boolean
  min: number
  max: number
  maxSizeBytes: number
  accept: string
}

export type VideoGenerationModel = {
  id: string
  modelName: string
  displayName: string
  description?: string
  family?: string
  category?: string
  tags: string[]
  createdAt?: number
  recentCallCount: number
  featured: boolean
  isNew: boolean
  available: boolean
  promptSupported: boolean
  promptRequired: boolean
  promptMaxLength?: number
  fields: VideoCapabilityField[]
  media: VideoMediaRequirement[]
  priceUsd?: number
  groupRatios: Record<string, number>
  groups: string[]
  defaultGroup?: string
  requirements: string[]
}

export type VideoModelsCatalog = {
  models: VideoGenerationModel[]
  groups: string[]
  defaultGroup?: string
  estimateIsMaximum: boolean
}

export type VideoAssetStatus = 'queued' | 'uploading' | 'uploaded' | 'failed'

export type VideoAsset = {
  id: string
  kind: VideoMediaKind
  requirementId: string
  file: File
  previewUrl: string
  status: VideoAssetStatus
  progress: number
  remoteUrl?: string
  error?: string
}

export type VideoAssetReference = {
  assetId: string
  token: string
}

export type VideoFormValues = Record<string, string | number | boolean>

export type VideoGenerationPayload = {
  model: string
  group?: string
  prompt?: string
  images?: string[]
  seconds?: string | number
  metadata?: Record<string, unknown>
  [key: string]: unknown
}

export type VideoSubmissionResult = {
  taskId?: string
  status?: string
  raw: unknown
}

export type VideoTaskStatus =
  | 'pending'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'unknown'

export type VideoTask = {
  id: string
  taskId: string
  modelName: string
  prompt?: string
  parameters: VideoFormValues
  status: VideoTaskStatus
  progress: number
  progressText?: string
  failureReason?: string
  contentUrl?: string
  contentUrls: string[]
  outputIndex?: number
  thumbnailUrl?: string
  createdAt: number
  finishedAt?: number
  expiresAt?: number
}

export type VideoTaskPage = {
  items: VideoTask[]
  total: number
}

export type AssetValidationCode =
  | 'unsupported_type'
  | 'file_too_large'
  | 'too_many_files'

export type AssetValidationFailure = {
  file: File
  code: AssetValidationCode
  max?: number
}

export type AssetSelectionResult = {
  accepted: File[]
  rejected: AssetValidationFailure[]
}

export type GenerationValidationIssue = {
  code:
    | 'prompt_required'
    | 'field_required'
    | 'field_invalid'
    | 'media_required'
    | 'upload_pending'
    | 'upload_failed'
    | 'input_required'
  field?: string
  kind?: VideoMediaKind
  min?: number
}
