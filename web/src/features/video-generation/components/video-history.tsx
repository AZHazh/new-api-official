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
  Delete02Icon,
  Download01Icon,
  HistoryIcon,
  RefreshIcon,
  RepeatIcon,
  Video01Icon,
  ViewOffIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import { toIntlLocale } from '@/i18n/languages'

import { videoDownloadUrl } from '../lib/video-logic'
import {
  videoTaskStatusLabel,
  videoTaskStatusVariant,
} from '../lib/video-task-presentation'
import type { VideoTask } from '../types'

type VideoHistoryProps = {
  tasks: VideoTask[]
  isLoading: boolean
  isFetching: boolean
  mobileOpen: boolean
  onMobileOpenChange: (open: boolean) => void
  onRefresh: () => void
  onPreview: (task: VideoTask) => void
  onReuse: (task: VideoTask) => void
  onHide: (taskId: string) => void
  onClear: () => void
}

function ClearHistoryDialog(props: { disabled: boolean; onClear: () => void }) {
  const { t } = useTranslation()
  return (
    <AlertDialog>
      <Tooltip>
        <TooltipTrigger
          render={
            <AlertDialogTrigger
              render={
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-sm'
                  disabled={props.disabled}
                  aria-label={t('Clear local history')}
                />
              }
            />
          }
        >
          <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
        </TooltipTrigger>
        <TooltipContent>{t('Clear local history')}</TooltipContent>
      </Tooltip>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('Clear local history?')}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(
              'This hides the video tasks currently shown from the last 7 days on this browser. New tasks will still appear.'
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
          <AlertDialogAction variant='destructive' onClick={props.onClear}>
            {t('Clear history')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function HistoryAction(props: {
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
            size='icon-xs'
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

function HistoryList(props: VideoHistoryProps) {
  const { t, i18n } = useTranslation()
  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(toIntlLocale(i18n.resolvedLanguage), {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      }),
    [i18n.resolvedLanguage]
  )

  if (props.isLoading) {
    return (
      <div className='flex flex-col gap-2 p-3'>
        {Array.from({ length: 6 }, (_, index) => (
          <Skeleton key={index} className='h-24 w-full rounded-lg' />
        ))}
      </div>
    )
  }
  if (props.tasks.length === 0) {
    return (
      <Empty className='min-h-72 border-0 px-4'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={HistoryIcon} strokeWidth={1.8} />
          </EmptyMedia>
          <EmptyTitle>{t('No video history')}</EmptyTitle>
          <EmptyDescription>
            {t('No tasks in the last 7 days.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className='flex flex-col gap-2 p-3' data-video-history-list>
      {props.tasks.map((task) => {
        const canPreview =
          task.status === 'succeeded' && Boolean(task.contentUrl)
        return (
          <article
            key={task.id}
            className='bg-card overflow-hidden rounded-lg border'
            data-video-history-item
          >
            <button
              type='button'
              disabled={!canPreview}
              className='hover:bg-muted/40 focus-visible:ring-ring/50 flex w-full min-w-0 items-start gap-3 p-3 text-left transition-colors outline-none focus-visible:ring-3 disabled:cursor-default'
              onClick={() => props.onPreview(task)}
            >
              <span className='bg-muted flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-lg'>
                {task.thumbnailUrl ? (
                  <img
                    src={task.thumbnailUrl}
                    alt=''
                    className='size-full object-cover'
                  />
                ) : (
                  <HugeiconsIcon
                    icon={Video01Icon}
                    strokeWidth={1.8}
                    className='text-muted-foreground size-5'
                  />
                )}
              </span>
              <span className='min-w-0 flex-1'>
                <span className='flex items-center justify-between gap-2'>
                  <span className='truncate text-xs font-medium'>
                    {task.modelName}
                  </span>
                  <Badge
                    variant={videoTaskStatusVariant(task.status)}
                    className='shrink-0'
                  >
                    {videoTaskStatusLabel(task.status, t)}
                  </Badge>
                </span>
                <span className='text-muted-foreground mt-1 line-clamp-2 text-xs leading-relaxed'>
                  {task.failureReason || task.prompt || t('Video generation')}
                </span>
                <span className='text-muted-foreground mt-1 block text-[11px]'>
                  {dateFormatter.format(task.createdAt)}
                </span>
              </span>
            </button>
            <div className='bg-muted/20 flex items-center justify-end gap-0.5 border-t px-2 py-1'>
              {canPreview && task.contentUrl && (
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <Button
                        variant='ghost'
                        size='icon-xs'
                        render={
                          <a
                            href={videoDownloadUrl(task.contentUrl)}
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
              <HistoryAction
                label={t('Reuse generation parameters')}
                icon={RepeatIcon}
                onClick={() => props.onReuse(task)}
              />
              <HistoryAction
                label={t('Hide from local history')}
                icon={ViewOffIcon}
                onClick={() => props.onHide(task.id)}
              />
            </div>
          </article>
        )
      })}
    </div>
  )
}

function HistoryToolbar(props: VideoHistoryProps) {
  const { t } = useTranslation()
  return (
    <div className='flex items-center gap-1'>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              type='button'
              variant='ghost'
              size='icon-sm'
              disabled={props.isFetching}
              onClick={props.onRefresh}
              aria-label={t('Refresh video tasks')}
            />
          }
        >
          <HugeiconsIcon
            icon={RefreshIcon}
            strokeWidth={2}
            className={cn(props.isFetching && 'animate-spin')}
          />
        </TooltipTrigger>
        <TooltipContent>{t('Refresh video tasks')}</TooltipContent>
      </Tooltip>
      <ClearHistoryDialog
        disabled={props.tasks.length === 0}
        onClear={props.onClear}
      />
    </div>
  )
}

export function VideoHistory(props: VideoHistoryProps) {
  const { t } = useTranslation()
  return (
    <>
      <aside className='bg-background hidden w-80 shrink-0 flex-col border-l xl:flex'>
        <div className='flex min-h-14 items-center justify-between gap-3 border-b px-4 py-2.5'>
          <div>
            <h2 className='text-sm font-semibold'>{t('Generation history')}</h2>
            <p className='text-muted-foreground text-xs'>{t('Last 7 days')}</p>
          </div>
          <HistoryToolbar {...props} />
        </div>
        <div className='min-h-0 flex-1 overflow-y-auto'>
          <HistoryList {...props} />
        </div>
      </aside>

      <Sheet open={props.mobileOpen} onOpenChange={props.onMobileOpenChange}>
        <SheetContent className='w-full max-w-full gap-0 sm:max-w-md xl:hidden'>
          <SheetHeader className='flex-row items-center justify-between gap-3 border-b pr-12'>
            <div>
              <SheetTitle>{t('Generation history')}</SheetTitle>
              <SheetDescription>{t('Last 7 days')}</SheetDescription>
            </div>
            <HistoryToolbar {...props} />
          </SheetHeader>
          <div className='min-h-0 flex-1 overflow-y-auto'>
            <HistoryList {...props} />
          </div>
        </SheetContent>
      </Sheet>
    </>
  )
}
