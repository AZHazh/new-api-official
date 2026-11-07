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

import type {
  ImageCatalog,
  ImageGeneration,
  ImageGenerationRequest,
  MediaAsset,
  MediaPage,
} from './types'

type Envelope<T> = { success: boolean; message?: string; data: T }

function unwrap<T>(response: Envelope<T>): T {
  if (!response.success) throw new Error(response.message || 'Request failed')
  return response.data
}

export async function getImageCatalog(): Promise<ImageCatalog> {
  const response = await api.get<Envelope<ImageCatalog>>(
    '/api/multimedia/models'
  )
  return unwrap(response.data)
}

export async function getImageGenerations(
  page: number
): Promise<MediaPage<ImageGeneration>> {
  const response = await api.get<Envelope<MediaPage<ImageGeneration>>>(
    '/api/multimedia/generations',
    { params: { p: page, page_size: 12 }, disableDuplicate: true }
  )
  return unwrap(response.data)
}

export async function submitImageGeneration(
  body: ImageGenerationRequest
): Promise<ImageGeneration> {
  const headers = await getFreshAuthHeaders()
  const response = await api.post<Envelope<ImageGeneration>>(
    '/api/multimedia/generations',
    body,
    { headers }
  )
  return unwrap(response.data)
}

export async function uploadImage(file: File): Promise<MediaAsset> {
  const body = new FormData()
  body.append('file', file, file.name)
  const response = await api.post<Envelope<MediaAsset>>(
    '/api/multimedia/assets',
    body,
    {
      headers: await getFreshAuthHeaders(),
    }
  )
  return unwrap(response.data)
}

export async function getMediaAssets(
  page: number,
  kind: string
): Promise<MediaPage<MediaAsset>> {
  const response = await api.get<Envelope<MediaPage<MediaAsset>>>(
    '/api/multimedia/assets',
    {
      params: { p: page, page_size: 18, kind: kind || undefined },
      disableDuplicate: true,
    }
  )
  return unwrap(response.data)
}

export async function getImageBlob(
  id: string,
  thumbnail: boolean,
  signal?: AbortSignal
): Promise<Blob> {
  const response = await api.get<Blob>(
    `/api/multimedia/assets/${encodeURIComponent(id)}/content`,
    {
      responseType: 'blob',
      disableDuplicate: true,
      params: thumbnail ? { thumbnail: true } : undefined,
      signal,
      skipErrorHandler: true,
    }
  )
  return response.data
}

export async function removeMediaAssets(ids: string[]): Promise<void> {
  const response = await api.post<Envelope<unknown>>(
    '/api/multimedia/assets/remove',
    { ids }
  )
  unwrap(response.data)
}

export async function downloadMediaAssets(ids: string[]): Promise<Blob> {
  const response = await api.post<Blob>(
    '/api/multimedia/assets/download',
    { ids },
    { responseType: 'blob' }
  )
  return response.data
}

export function saveImageBlob(blob: Blob, name: string): void {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = name
  document.body.append(link)
  link.click()
  link.remove()
  // The download consumes the object URL after this event handler returns.
  window.setTimeout(() => URL.revokeObjectURL(url), 60_000)
}
