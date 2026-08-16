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

import type { VideoMediaKind } from './types'

export const VIDEO_MODELS_QUERY_KEY = ['video-generation', 'models'] as const
export const VIDEO_TASKS_QUERY_KEY = ['video-generation', 'tasks'] as const

export const MAX_VIDEO_REQUEST_COUNT = 4
export const DEFAULT_MAX_UPLOAD_BYTES = 50 * 1024 * 1024
export const VIDEO_HISTORY_DAYS = 7
export const VIDEO_HIDDEN_TASKS_STORAGE_KEY = 'video-generation:hidden-tasks:v1'

export const MEDIA_ACCEPT: Record<VideoMediaKind, string> = {
  image: 'image/jpeg,image/png,image/webp,.jpg,.jpeg,.png,.webp',
  video:
    'video/mp4,video/quicktime,video/x-msvideo,video/x-matroska,.mp4,.mov,.avi,.mkv',
  audio: 'audio/mpeg,audio/wav,audio/flac,.mp3,.wav,.flac',
}

export const MEDIA_DEFAULT_KEYS: Record<VideoMediaKind, string> = {
  image: 'images',
  video: 'metadata.video_urls',
  audio: 'metadata.audio_urls',
}

export const MEDIA_LABEL_KEYS: Record<VideoMediaKind, string> = {
  image: 'Reference images',
  video: 'Reference videos',
  audio: 'Reference audio',
}

export const TERMINAL_TASK_STATUSES = new Set(['succeeded', 'failed'])
