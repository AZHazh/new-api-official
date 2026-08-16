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
  Download01Icon,
  RepeatIcon,
  ViewOffIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

import { videoDownloadUrl } from '../lib/video-logic'
import type { VideoTask } from '../types'

export function VideoPreviewDialog(props: {
  task: VideoTask | null
  onOpenChange: (open: boolean) => void
  onReuse: (task: VideoTask) => void
  onHide: (taskId: string) => void
}) {
  const { t, i18n } = useTranslation()
  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(toIntlLocale(i18n.resolvedLanguage), {
        dateStyle: 'medium',
        timeStyle: 'short',
      }),
    [i18n.resolvedLanguage]
  )
  const task = props.task

  return (
    <Dialog open={task !== null} onOpenChange={props.onOpenChange}>
      <DialogContent className='grid max-h-[calc(100svh-1rem)] grid-rows-[auto_minmax(0,1fr)_auto] gap-0 overflow-hidden p-0 sm:max-w-5xl'>
        <DialogHeader className='border-b p-4 pr-12'>
          <DialogTitle>{task?.modelName ?? t('Video preview')}</DialogTitle>
          <DialogDescription>
            {task
              ? dateFormatter.format(task.createdAt)
              : t('Generated video preview')}
          </DialogDescription>
        </DialogHeader>
        <div className='min-h-0 overflow-y-auto'>
          {task?.contentUrl && (
            <div className='flex min-h-64 items-center justify-center bg-black sm:min-h-96'>
              <video
                key={task.contentUrl}
                src={task.contentUrl}
                poster={task.thumbnailUrl}
                controls
                autoPlay
                playsInline
                preload='metadata'
                className='max-h-[68svh] w-full object-contain'
                aria-label={t('Generated video preview')}
              />
            </div>
          )}
          {task?.prompt && (
            <div className='p-4'>
              <p className='text-muted-foreground text-xs font-medium'>
                {t('Prompt')}
              </p>
              <p className='mt-1 text-sm leading-relaxed wrap-break-word'>
                {task.prompt}
              </p>
            </div>
          )}
        </div>
        <DialogFooter className='m-0'>
          {task && (
            <>
              <Button
                type='button'
                variant='ghost'
                onClick={() => props.onReuse(task)}
              >
                <HugeiconsIcon
                  icon={RepeatIcon}
                  strokeWidth={2}
                  data-icon='inline-start'
                />
                {t('Reuse parameters')}
              </Button>
              <Button
                type='button'
                variant='ghost'
                onClick={() => props.onHide(task.id)}
              >
                <HugeiconsIcon
                  icon={ViewOffIcon}
                  strokeWidth={2}
                  data-icon='inline-start'
                />
                {t('Hide')}
              </Button>
              {task.contentUrl && (
                <Button
                  render={
                    <a href={videoDownloadUrl(task.contentUrl)} download />
                  }
                  nativeButton={false}
                >
                  <HugeiconsIcon
                    icon={Download01Icon}
                    strokeWidth={2}
                    data-icon='inline-start'
                  />
                  {t('Download video')}
                </Button>
              )}
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
