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
export type MediaAsset = {
  id: string
  generation_id?: string
  kind: 'generated' | 'reference'
  name: string
  mime_type: string
  width: number
  height: number
  bytes: number
  created_at: number
  expires_at: number
}

export type ImageModel = {
  name: string
  price_usd: number | null
  capabilities: {
    max_references: number
    max_count: number
    custom_size: boolean
    advanced: boolean
    sizes: string[]
    qualities: string[]
    resolutions?: ('1K' | '2K' | '4K')[]
  }
}

export type ImageCatalog = {
  models: ImageModel[]
  max_upload_bytes: number
  retention_days: number
}

export type ImageGenerationRequest = {
  model: string
  prompt: string
  n: number
  size: string
  quality: string
  resolution?: '1K' | '2K' | '4K'
  output_format?: string
  output_compression?: number
  references: string[]
  request_key: string
}

export type ImageGeneration = {
  id: string
  model: string
  prompt: string
  parameters: string
  status:
    | 'queued'
    | 'running'
    | 'succeeded'
    | 'failed'
    | 'interrupted'
    | 'partial'
    | 'storage_failed'
  error?: string
  created_at: number
  finished_at: number
  assets: MediaAsset[]
}

export type MediaPage<T> = {
  items: T[]
  total: number
  retention_days?: number
}

export type ReferenceImage = {
  id: string
  name: string
  asset?: MediaAsset
  status: 'uploading' | 'uploaded' | 'failed'
}
