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
import { z } from 'zod'

import type { ImageGenerationRequest, ImageModel, MediaAsset } from '../types'

export const imageFormSchema = z.object({
  model: z.string().min(1, 'Select an image model'),
  prompt: z
    .string()
    .trim()
    .min(1, 'Describe the image you want to create')
    .max(32000),
  ratio: z.string(),
  resolution: z.enum(['1K', '2K', '4K']),
  width: z.number().int().min(256).max(3840),
  height: z.number().int().min(256).max(3840),
  n: z.number().int().min(1).max(128),
  quality: z.string(),
  outputFormat: z.enum(['png', 'jpeg', 'webp']),
  compression: z.number().int().min(0).max(100),
})

export type ImageFormValues = z.infer<typeof imageFormSchema>

export const IMAGE_FORM_DEFAULTS: ImageFormValues = {
  model: '',
  prompt: '',
  ratio: '1:1',
  resolution: '1K',
  width: 1024,
  height: 1024,
  n: 1,
  quality: 'auto',
  outputFormat: 'png',
  compression: 100,
}

export const IMAGE_RATIOS = [
  'auto',
  '1:1',
  '16:9',
  '9:16',
  '4:3',
  '3:4',
  '3:2',
  '2:3',
  '4:5',
  '5:4',
  '21:9',
  'custom',
] as const

export function imageSize(values: ImageFormValues, model: ImageModel): string {
  if (!model.capabilities.custom_size) return values.ratio
  if (values.ratio === 'auto') return 'auto'
  if (values.ratio === 'custom') return `${values.width}x${values.height}`
  const [width, height] = values.ratio.split(':').map(Number)
  const limit = { '1K': 1536, '2K': 3072, '4K': 3840 }[values.resolution]
  const square = { '1K': 1024, '2K': 2048, '4K': 3840 }[values.resolution]
  if (width === height) return `${square}x${square}`
  const scale = limit / Math.max(width, height)
  const outputWidth = Math.max(256, Math.round((width * scale) / 16) * 16)
  const outputHeight = Math.max(256, Math.round((height * scale) / 16) * 16)
  return `${outputWidth}x${outputHeight}`
}

export function buildImageRequest(
  values: ImageFormValues,
  model: ImageModel,
  references: MediaAsset[],
  requestKey: string
): ImageGenerationRequest {
  const request: ImageGenerationRequest = {
    model: model.name,
    prompt: values.prompt.trim(),
    n: values.n,
    size: imageSize(values, model),
    quality: values.quality,
    references: references.map((asset) => asset.id),
    request_key: requestKey,
  }
  if (model.capabilities.advanced) {
    request.output_format = values.outputFormat
    if (values.outputFormat !== 'png') {
      request.output_compression = values.compression
    }
  }
  if (model.capabilities.resolutions?.length) {
    request.resolution = values.resolution
  }
  return request
}

export function generationIsActive(status: string): boolean {
  return status === 'queued' || status === 'running'
}

export function parseImageParameters(
  parameters: string
): Partial<ImageGenerationRequest> {
  try {
    const value: unknown = JSON.parse(parameters)
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      return value as Partial<ImageGenerationRequest>
    }
  } catch {
    /* History is still readable when an old parameter payload is invalid. */
  }
  return {}
}
