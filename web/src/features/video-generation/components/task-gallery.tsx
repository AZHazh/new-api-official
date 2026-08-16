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
  AlertCircleIcon,
  Download01Icon,
  RefreshIcon,
  RepeatIcon,
  Video01Icon,
  ViewIcon,
  ViewOffIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { toIntlLocale } from '@/i18n/languages'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import { videoDownloadUrl } from '../lib/video-logic'
import {
  videoTaskStatusLabel,
  videoTaskStatusVariant,
} from '../lib/video-task-presentation'
import type { VideoTask } from '../types'

function TaskAction(props: {
  label: string
  icon: typeof RepeatIcon
  onClick: () => void
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type='button'
            variant='ghost'
            size='icon-sm'
            onClick={props.onClick}
            aria-label={props.label}
          />
        }
      >
        <HugeiconsIcon icon={props.icon} strokeWidth={2} />
      </TooltipTrigger>
      <TooltipContent>{props.label}</TooltipContent>
    </Tooltip>
  )
}

function VideoTaskCard(props: {
  task: VideoTask
  dateFormatter: Intl.DateTimeFormat
  onPreview: (task: VideoTask) => void
  onReuse: (task: VideoTask) => void
  onHide: (taskId: string) => void
}) {
  const { t } = useTranslation()
  const active =
    props.task.status === 'pending' || props.task.status === 'running'
  const canPreview =
    props.task.status === 'succeeded' && Boolean(props.task.contentUrl)
  return (
    <article
      className='bg-card overflow-hidden rounded-lg border'
      data-video-task-card
      data-status={props.task.status}
    >
      <div className='bg-muted/40 relative aspect-video'>
        {canPreview ? (
          <>
            <video
              src={props.task.contentUrl}
              poster={props.task.thumbnailUrl}
              muted
              playsInline
              preload='metadata'
              className='pointer-events-none size-full bg-black object-contain'
              aria-hidden='true'
            />
            <button
              type='button'
              className='group focus-visible:ring-ring/50 absolute inset-0 flex items-center justify-center bg-black/0 transition-colors outline-none hover:bg-black/20 focus-visible:ring-3'
              onClick={() => props.onPreview(props.task)}
              aria-label={t('Preview generated video')}
            >
              <span className='bg-background/90 flex size-10 items-center justify-center rounded-full opacity-0 shadow-sm transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100'>
                <HugeiconsIcon icon={ViewIcon} strokeWidth={2} />
              </span>
            </button>
          </>
        ) : (
          <div className='flex size-full flex-col items-center justify-center gap-2 px-5 text-center'>
            <HugeiconsIcon
              icon={
                props.task.status === 'failed' ? AlertCircleIcon : Video01Icon
              }
              strokeWidth={1.6}
              className='text-muted-foreground size-8'
            />
            <p className='text-muted-foreground text-xs'>
              {props.task.failureReason ||
                props.task.progressText ||
                videoTaskStatusLabel(props.task.status, t)}
            </p>
          </div>
        )}
        <Badge
          variant={videoTaskStatusVariant(props.task.status)}
          className='absolute top-2 left-2 shadow-sm'
        >
          {videoTaskStatusLabel(props.task.status, t)}
        </Badge>
      </div>
      {active && (
        <Progress value={props.task.progress} className='rounded-none' />
      )}
      <div className='flex min-w-0 items-center gap-2 border-t px-3 py-2.5'>
        <div className='min-w-0 flex-1'>
          <p className='truncate text-xs font-medium' title={props.task.prompt}>
            {props.task.prompt || props.task.modelName}
          </p>
          <p className='text-muted-foreground mt-0.5 truncate text-[11px]'>
            {props.task.modelName} -{' '}
            {props.dateFormatter.format(props.task.createdAt)}
          </p>
        </div>
        {canPreview && props.task.contentUrl && (
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant='ghost'
                  size='icon-sm'
                  render={
                    <a
                      href={videoDownloadUrl(props.task.contentUrl)}
                      download
                      aria-label={t('Download video')}
                    />
                  }
                  nativeButton={false}
                />
              }
            >
              <HugeiconsIcon icon={Download01Icon} strokeWidth={2} />
            </TooltipTrigger>
            <TooltipContent>{t('Download video')}</TooltipContent>
          </Tooltip>
        )}
        <TaskAction
          label={t('Reuse generation parameters')}
          icon={RepeatIcon}
          onClick={() => props.onReuse(props.task)}
        />
        <TaskAction
          label={t('Hide from local history')}
          icon={ViewOffIcon}
          onClick={() => props.onHide(props.task.id)}
        />
      </div>
    </article>
  )
}

export function TaskGallery(props: {
  tasks: VideoTask[]
  isLoading: boolean
  error: unknown
  isFetching: boolean
  onRetry: () => void
  onPreview: (task: VideoTask) => void
  onReuse: (task: VideoTask) => void
  onHide: (taskId: string) => void
}) {
  const { t, i18n } = useTranslation()
  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(toIntlLocale(i18n.resolvedLanguage), {
        dateStyle: 'short',
        timeStyle: 'short',
      }),
    [i18n.resolvedLanguage]
  )
  if (props.isLoading) {
    return (
      <div className='grid gap-3 sm:grid-cols-2 2xl:grid-cols-3'>
        {Array.from({ length: 6 }, (_, index) => (
          <Skeleton key={index} className='aspect-video rounded-lg' />
        ))}
      </div>
    )
  }
  if (props.error) {
    return (
      <Alert variant='destructive'>
        <HugeiconsIcon icon={AlertCircleIcon} strokeWidth={2} />
        <AlertTitle>{t('Failed to load video tasks')}</AlertTitle>
        <AlertDescription>{t('Please try again later.')}</AlertDescription>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={props.onRetry}
        >
          <HugeiconsIcon
            icon={RefreshIcon}
            strokeWidth={2}
            data-icon='inline-start'
          />
          {t('Retry')}
        </Button>
      </Alert>
    )
  }
  if (props.tasks.length === 0) {
    return (
      <Empty className='min-h-72 border-0'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={Video01Icon} strokeWidth={1.8} />
          </EmptyMedia>
          <EmptyTitle>{t('Your videos will appear here')}</EmptyTitle>
          <EmptyDescription>
            {t('Choose a model, describe your scene, and start generating.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  return (
    <section
      aria-labelledby='video-results-heading'
      className='flex flex-col gap-3'
    >
      <div className='flex items-center justify-between gap-3'>
        <div>
          <h2 id='video-results-heading' className='text-base font-semibold'>
            {t('Generated videos')}
          </h2>
          <p className='text-muted-foreground text-xs'>
            {t('{{count}} results from the last 7 days', {
              count: props.tasks.length,
            })}
          </p>
        </div>
        <Button
          type='button'
          variant='ghost'
          size='icon-sm'
          disabled={props.isFetching}
          onClick={props.onRetry}
          aria-label={t('Refresh video tasks')}
        >
          <HugeiconsIcon
            icon={RefreshIcon}
            strokeWidth={2}
            className={props.isFetching ? 'animate-spin' : undefined}
          />
        </Button>
      </div>
      <div className='grid gap-3 sm:grid-cols-2 2xl:grid-cols-3'>
        {props.tasks.map((task) => (
          <VideoTaskCard
            key={task.id}
            task={task}
            dateFormatter={dateFormatter}
            onPreview={props.onPreview}
            onReuse={props.onReuse}
            onHide={props.onHide}
          />
        ))}
      </div>
    </section>
  )
}
