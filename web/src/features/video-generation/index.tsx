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
  AiVideoIcon,
  AlertCircleIcon,
  HistoryIcon,
  RefreshIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import type { TFunction } from 'i18next'
import { type ReactNode, useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'

import { GenerationComposer } from './components/generation-composer'
import { ModelPickerDialog } from './components/model-picker'
import { TaskGallery } from './components/task-gallery'
import { VideoHistory } from './components/video-history'
import { VideoPreviewDialog } from './components/video-preview-dialog'
import { useVideoGenerationStudio } from './hooks/use-video-generation-studio'
import type { GenerationValidationIssue, VideoTask } from './types'

type VideoGenerationProps = {
  initialModelName?: string
  onModelChange?: (modelName: string) => void
}

function validationMessage(
  issue: GenerationValidationIssue | null,
  t: TFunction
): string | undefined {
  if (!issue) return undefined
  switch (issue.code) {
    case 'prompt_required':
      return t('Enter a prompt to continue.')
    case 'field_required':
      return t('{{field}} is required.', {
        field: issue.field ?? t('This field'),
      })
    case 'field_invalid':
      return t('{{field}} has an invalid format.', {
        field: issue.field ?? t('This field'),
      })
    case 'media_required':
      return t('Upload at least {{count}} file(s) for {{field}}.', {
        count: issue.min ?? 1,
        field: t(issue.field ?? 'Required media'),
      })
    case 'upload_pending':
      return t('Wait for {{field}} to finish uploading.', {
        field: t(issue.field ?? 'Required media'),
      })
    case 'upload_failed':
      return t('Retry or replace the failed files in {{field}}.', {
        field: t(issue.field ?? 'Required media'),
      })
    default:
      return t('Add a prompt or supported media to continue.')
  }
}

export function VideoGeneration(props: VideoGenerationProps) {
  const { t } = useTranslation()
  const [modelDialogOpen, setModelDialogOpen] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [previewTask, setPreviewTask] = useState<VideoTask | null>(null)
  const studio = useVideoGenerationStudio({
    initialModelName: props.initialModelName,
    onModelChange: props.onModelChange,
  })
  const issueMessage = validationMessage(studio.validationIssue, t)

  const reuseTask = useCallback(
    (task: VideoTask) => {
      studio.reuseTask(task)
      setHistoryOpen(false)
      setPreviewTask(null)
    },
    [studio]
  )
  const hideTask = useCallback(
    (taskId: string) => {
      studio.hideTask(taskId)
      setPreviewTask((current) => (current?.id === taskId ? null : current))
    },
    [studio]
  )
  const clearHistory = useCallback(() => {
    studio.clearHistory()
    setPreviewTask(null)
  }, [studio])

  let resultsContent: ReactNode
  if (studio.modelsError) {
    resultsContent = (
      <Alert variant='destructive'>
        <HugeiconsIcon icon={AlertCircleIcon} strokeWidth={2} />
        <AlertTitle>{t('Failed to load video models')}</AlertTitle>
        <AlertDescription>
          {studio.modelsError instanceof Error
            ? studio.modelsError.message
            : t('Please try again later.')}
        </AlertDescription>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => void studio.refetchModels()}
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
  } else if (studio.modelsLoading) {
    resultsContent = (
      <div className='grid gap-3 sm:grid-cols-2 2xl:grid-cols-3'>
        {Array.from({ length: 6 }, (_, index) => (
          <Skeleton key={index} className='aspect-video rounded-lg' />
        ))}
      </div>
    )
  } else if (!studio.selectedModel) {
    resultsContent = (
      <Empty className='min-h-[50svh] border-0'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={AiVideoIcon} strokeWidth={1.8} />
          </EmptyMedia>
          <EmptyTitle>{t('No video models available')}</EmptyTitle>
          <EmptyDescription>
            {t('Ask an administrator to enable a video model for your group.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    resultsContent = (
      <TaskGallery
        tasks={studio.tasks}
        isLoading={studio.tasksLoading}
        error={studio.tasksError}
        isFetching={studio.tasksFetching}
        onRetry={() => void studio.refetchTasks()}
        onPreview={setPreviewTask}
        onReuse={reuseTask}
        onHide={hideTask}
      />
    )
  }

  return (
    <div className='bg-muted/20 flex min-h-0 flex-1 overflow-hidden'>
      <section className='flex min-w-0 flex-1 flex-col'>
        <header className='bg-background flex min-h-14 shrink-0 items-center gap-3 border-b px-4 py-2.5 sm:px-5'>
          <span className='bg-muted text-muted-foreground flex size-9 shrink-0 items-center justify-center rounded-lg'>
            <HugeiconsIcon
              icon={AiVideoIcon}
              strokeWidth={1.8}
              className='size-5'
            />
          </span>
          <div className='min-w-0 flex-1'>
            <h1 className='text-sm font-semibold sm:text-base'>
              {t('Video generation')}
            </h1>
            <p className='text-muted-foreground truncate text-xs'>
              {studio.selectedModel?.displayName ??
                t('Choose a model to begin')}
            </p>
          </div>
          <Button
            type='button'
            variant='outline'
            size='icon-sm'
            className='xl:hidden'
            onClick={() => setHistoryOpen(true)}
            aria-label={t('Open generation history')}
          >
            <HugeiconsIcon icon={HistoryIcon} strokeWidth={2} />
          </Button>
        </header>

        <div className='min-h-0 flex-1 overflow-y-auto'>
          <div className='mx-auto w-full max-w-6xl p-4 sm:p-6'>
            {resultsContent}
          </div>
        </div>

        {studio.selectedModel && (
          <GenerationComposer
            studio={studio}
            validationMessage={issueMessage}
            onOpenModels={() => setModelDialogOpen(true)}
          />
        )}
      </section>

      <VideoHistory
        tasks={studio.tasks}
        isLoading={studio.tasksLoading}
        isFetching={studio.tasksFetching}
        mobileOpen={historyOpen}
        onMobileOpenChange={setHistoryOpen}
        onRefresh={() => void studio.refetchTasks()}
        onPreview={setPreviewTask}
        onReuse={reuseTask}
        onHide={hideTask}
        onClear={clearHistory}
      />

      <ModelPickerDialog
        models={studio.models}
        selectedModelName={studio.selectedModelName}
        onSelect={studio.selectModel}
        open={modelDialogOpen}
        onOpenChange={setModelDialogOpen}
      />
      <VideoPreviewDialog
        task={previewTask}
        onOpenChange={(open) => {
          if (!open) setPreviewTask(null)
        }}
        onReuse={reuseTask}
        onHide={hideTask}
      />
    </div>
  )
}
