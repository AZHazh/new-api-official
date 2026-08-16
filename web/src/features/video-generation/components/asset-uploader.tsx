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
  AiAudioIcon,
  Cancel01Icon,
  Image01Icon,
  RefreshIcon,
  Upload01Icon,
  Video01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

import type { VideoAsset, VideoMediaRequirement } from '../types'

const MEDIA_ICONS = {
  image: Image01Icon,
  video: Video01Icon,
  audio: AiAudioIcon,
} as const

function formatBytes(bytes: number): string {
  if (bytes >= 1024 * 1024) return `${Math.round(bytes / (1024 * 1024))} MB`
  return `${Math.round(bytes / 1024)} KB`
}

function AssetPreview(props: {
  asset: VideoAsset
  compact: boolean
  disabled: boolean
  onRemove: (assetId: string) => void
  onRetry: (assetId: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div
      className={cn(
        'bg-muted/40 relative overflow-hidden rounded-lg border',
        props.compact && 'min-w-0'
      )}
    >
      <div className={cn('aspect-video bg-black/5', props.compact && 'h-12')}>
        {props.asset.kind === 'image' && (
          <img
            src={props.asset.previewUrl}
            alt={props.asset.file.name}
            className='size-full object-cover'
          />
        )}
        {props.asset.kind === 'video' && (
          <video
            src={props.asset.previewUrl}
            muted
            playsInline
            preload='metadata'
            className='size-full object-cover'
          />
        )}
        {props.asset.kind === 'audio' && (
          <div className='flex size-full items-center justify-center'>
            <HugeiconsIcon
              icon={AiAudioIcon}
              strokeWidth={1.7}
              className='text-muted-foreground size-7'
            />
          </div>
        )}
      </div>
      <div className='flex min-w-0 items-center gap-2 border-t px-2 py-1.5'>
        <span
          className='min-w-0 flex-1 truncate text-xs'
          title={props.asset.file.name}
        >
          {props.asset.file.name}
        </span>
        {props.asset.status === 'failed' && (
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-xs'
                  disabled={props.disabled}
                  onClick={() => props.onRetry(props.asset.id)}
                  aria-label={t('Retry upload')}
                />
              }
            >
              <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
            </TooltipTrigger>
            <TooltipContent>{t('Retry upload')}</TooltipContent>
          </Tooltip>
        )}
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                type='button'
                variant='ghost'
                size='icon-xs'
                disabled={props.disabled}
                onClick={() => props.onRemove(props.asset.id)}
                aria-label={t('Remove file')}
              />
            }
          >
            <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} />
          </TooltipTrigger>
          <TooltipContent>{t('Remove file')}</TooltipContent>
        </Tooltip>
      </div>
      {(props.asset.status === 'uploading' ||
        props.asset.status === 'queued') && (
        <Progress value={props.asset.progress} className='rounded-none' />
      )}
      {props.asset.status === 'failed' && (
        <p
          className='text-destructive border-t px-2 py-1.5 text-xs'
          title={props.asset.error}
        >
          {t('Upload failed')}
        </p>
      )}
    </div>
  )
}

function RequirementUploader(props: {
  requirement: VideoMediaRequirement
  assets: VideoAsset[]
  disabled: boolean
  compact: boolean
  onFiles: (requirement: VideoMediaRequirement, files: File[]) => void
  onRemove: (assetId: string) => void
  onRetry: (assetId: string) => void
}) {
  const { t } = useTranslation()
  const inputRef = useRef<HTMLInputElement>(null)
  const [dragging, setDragging] = useState(false)
  const full = props.assets.length >= props.requirement.max
  const openPicker = () => {
    if (!props.disabled && !full) inputRef.current?.click()
  }
  return (
    <section
      className={cn('flex flex-col gap-2', props.compact && 'gap-1.5')}
      aria-label={t(props.requirement.label)}
    >
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex items-center gap-2'>
          <HugeiconsIcon
            icon={MEDIA_ICONS[props.requirement.kind]}
            strokeWidth={1.8}
            className='size-4'
          />
          <h3 className='text-sm font-medium'>{t(props.requirement.label)}</h3>
          {props.requirement.required && (
            <Badge variant='secondary'>{t('Required')}</Badge>
          )}
        </div>
        <span className='text-muted-foreground text-xs tabular-nums'>
          {props.assets.length}/{props.requirement.max}
        </span>
      </div>
      {props.requirement.description && (
        <p className='text-muted-foreground text-xs leading-relaxed'>
          {props.requirement.description}
        </p>
      )}
      <input
        ref={inputRef}
        type='file'
        accept={props.requirement.accept}
        multiple={props.requirement.max > 1}
        className='hidden'
        onChange={(event) => {
          const files = [...(event.target.files ?? [])]
          event.target.value = ''
          if (files.length > 0) props.onFiles(props.requirement, files)
        }}
      />
      <button
        type='button'
        disabled={props.disabled || full}
        onClick={openPicker}
        onDragEnter={(event) => {
          event.preventDefault()
          if (!props.disabled && !full) setDragging(true)
        }}
        onDragOver={(event) => event.preventDefault()}
        onDragLeave={() => setDragging(false)}
        onDrop={(event) => {
          event.preventDefault()
          setDragging(false)
          if (!props.disabled && !full) {
            props.onFiles(props.requirement, [...event.dataTransfer.files])
          }
        }}
        className={cn(
          'hover:bg-muted/40 focus-visible:ring-ring/50 flex min-h-24 w-full flex-col items-center justify-center gap-1 rounded-lg border border-dashed px-4 py-3 text-center outline-none transition-colors focus-visible:ring-3 disabled:cursor-not-allowed disabled:opacity-50',
          props.compact &&
            'min-h-14 flex-row justify-start px-3 py-2 text-left',
          dragging && 'border-primary bg-primary/5'
        )}
      >
        <HugeiconsIcon
          icon={Upload01Icon}
          strokeWidth={1.8}
          className='text-muted-foreground size-5'
        />
        <span className='text-sm font-medium'>
          {t('Drop files here or click to upload')}
        </span>
        <span
          className={cn(
            'text-muted-foreground text-xs',
            props.compact && 'ml-auto hidden sm:inline'
          )}
        >
          {t('Up to {{size}} per file', {
            size: formatBytes(props.requirement.maxSizeBytes),
          })}
        </span>
      </button>
      {props.assets.length > 0 && (
        <div
          className={cn(
            'grid grid-cols-2 gap-2',
            props.compact && 'grid-cols-3 sm:grid-cols-4'
          )}
        >
          {props.assets.map((asset) => (
            <AssetPreview
              key={asset.id}
              asset={asset}
              compact={props.compact}
              disabled={props.disabled}
              onRemove={props.onRemove}
              onRetry={props.onRetry}
            />
          ))}
        </div>
      )}
    </section>
  )
}

export function AssetUploader(props: {
  requirements: VideoMediaRequirement[]
  assets: VideoAsset[]
  disabled: boolean
  compact?: boolean
  onFiles: (requirement: VideoMediaRequirement, files: File[]) => void
  onRemove: (assetId: string) => void
  onRetry: (assetId: string) => void
}) {
  if (props.requirements.length === 0) return null
  return (
    <div className={cn('flex flex-col gap-5', props.compact && 'gap-3')}>
      {props.requirements.map((requirement) => (
        <RequirementUploader
          key={requirement.id}
          requirement={requirement}
          assets={props.assets.filter(
            (asset) => asset.requirementId === requirement.id
          )}
          disabled={props.disabled}
          compact={props.compact === true}
          onFiles={props.onFiles}
          onRemove={props.onRemove}
          onRetry={props.onRetry}
        />
      ))}
    </div>
  )
}
