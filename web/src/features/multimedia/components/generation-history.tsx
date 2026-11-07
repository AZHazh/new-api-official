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
import { Image01Icon, SparklesIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

import { getImageGenerations } from '../api'
import { generationIsActive } from '../lib/image-form'
import type { ImageGeneration, MediaAsset } from '../types'
import { MediaPagination } from './asset-gallery'
import { ProtectedImage } from './protected-image'

export function GenerationHistory(props: {
  submittedGeneration?: string
  onReuse: (job: ImageGeneration) => void
  onPreview: (asset: MediaAsset) => void
  onReference: (asset: MediaAsset) => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [page, setPage] = useState(1)
  const previousActive = useRef(new Set<string>())
  useEffect(() => {
    if (props.submittedGeneration) setPage(1)
  }, [props.submittedGeneration])
  const jobs = useQuery({
    queryKey: ['multimedia', 'generations', page],
    queryFn: () => getImageGenerations(page),
    refetchInterval: (query) =>
      query.state.data?.items.some((job) => generationIsActive(job.status))
        ? 2000
        : false,
  })
  useEffect(() => {
    const items = jobs.data?.items || []
    if (
      items.some(
        (job) =>
          previousActive.current.has(job.id) && !generationIsActive(job.status)
      )
    ) {
      void client.invalidateQueries({ queryKey: ['multimedia', 'assets'] })
    }
    previousActive.current = new Set(
      items.filter((job) => generationIsActive(job.status)).map((job) => job.id)
    )
  }, [jobs.data, client])
  const statusLabels = {
    queued: t('Queued'),
    running: t('Generating...'),
    succeeded: t('Completed'),
    failed: t('Failed'),
    interrupted: t('Interrupted'),
    partial: t('Partially saved'),
    storage_failed: t('Could not save images'),
  }
  return (
    <section className='mt-10' aria-label={t('My works')}>
      <div className='mb-5 flex items-center justify-between'>
        <h2 className='text-lg font-semibold'>{t('My works')}</h2>
        <Button
          size='sm'
          variant='ghost'
          onClick={() => void jobs.refetch()}
          disabled={jobs.isFetching}
        >
          {t('Refresh')}
        </Button>
      </div>
      {jobs.isPending && (
        <div
          className='grid grid-cols-2 gap-4 lg:grid-cols-3'
          role='status'
          aria-label={t('Loading...')}
        >
          {[0, 1, 2].map((id) => (
            <Skeleton
              key={id}
              className='aspect-square rounded-xl motion-reduce:animate-none'
            />
          ))}
        </div>
      )}
      {jobs.isError && (
        <p className='text-destructive py-12 text-center' role='alert'>
          {t('Unable to load generation history')}{' '}
          <Button variant='link' onClick={() => void jobs.refetch()}>
            {t('Retry')}
          </Button>
        </p>
      )}
      {jobs.isSuccess && !jobs.data.items.length && (
        <div className='text-muted-foreground rounded-xl border border-dashed px-6 py-16 text-center text-sm'>
          {t('Your generated images will appear here.')}
        </div>
      )}
      <div className='grid grid-cols-2 items-start gap-4 lg:grid-cols-3'>
        {jobs.data?.items.flatMap((job) => {
          const active = generationIsActive(job.status)
          const failed = ['failed', 'interrupted', 'storage_failed'].includes(
            job.status
          )
          const images: (MediaAsset | null)[] = job.assets?.length
            ? job.assets
            : [null]
          return images.map((asset) => (
            <article
              key={`${job.id}:${asset?.id || 'placeholder'}`}
              className='bg-card min-w-0 overflow-hidden rounded-xl border'
              aria-label={job.prompt}
              aria-busy={active}
            >
              {asset ? (
                <button
                  type='button'
                  className='block w-full focus-visible:ring-2 focus-visible:ring-inset'
                  aria-label={t('Preview {{name}}', { name: asset.name })}
                  onClick={() => props.onPreview(asset)}
                >
                  <ProtectedImage
                    asset={asset}
                    className='aspect-square w-full object-cover'
                  />
                </button>
              ) : (
                <Skeleton
                  role={active ? 'status' : 'img'}
                  aria-label={
                    job.status === 'succeeded'
                      ? t('Image unavailable')
                      : statusLabels[job.status]
                  }
                  className={cn(
                    'relative flex aspect-square w-full items-center justify-center overflow-hidden rounded-none bg-linear-to-br from-violet-300 via-fuchsia-200 to-sky-200 motion-reduce:animate-none dark:from-violet-950 dark:via-fuchsia-950 dark:to-slate-800',
                    !active && 'animate-none'
                  )}
                >
                  <div
                    aria-hidden='true'
                    className='absolute inset-0 bg-linear-to-tr from-white/45 via-transparent to-white/35 dark:from-white/5 dark:to-white/10'
                  />
                  <div aria-hidden='true' className='relative text-white/90'>
                    <HugeiconsIcon
                      icon={Image01Icon}
                      className='size-10 sm:size-12'
                      strokeWidth={1.8}
                    />
                    <HugeiconsIcon
                      icon={SparklesIcon}
                      className='absolute -top-2 -right-2 size-5 sm:size-6'
                      strokeWidth={1.8}
                    />
                  </div>
                </Skeleton>
              )}
              <div className='space-y-3 p-3'>
                <div className='flex items-center gap-2'>
                  <Badge variant={failed ? 'destructive' : 'secondary'}>
                    {statusLabels[job.status]}
                  </Badge>
                  <span
                    className='text-muted-foreground min-w-0 truncate text-xs'
                    title={job.model}
                  >
                    {job.model}
                  </span>
                </div>
                <p
                  className='line-clamp-2 min-h-10 text-sm break-words whitespace-pre-wrap'
                  title={job.prompt}
                >
                  {job.prompt}
                </p>
                <p className='text-muted-foreground text-xs'>
                  {new Date(job.created_at * 1000).toLocaleString()}
                </p>
                {job.error && (
                  <details className='text-destructive text-xs'>
                    <summary className='focus-visible:ring-ring cursor-pointer rounded-sm focus-visible:ring-2'>
                      {t('View details')}
                    </summary>
                    <p
                      className='mt-2 break-all whitespace-pre-wrap'
                      role='alert'
                    >
                      {job.error}
                    </p>
                  </details>
                )}
                <div className='grid gap-2 sm:grid-cols-2'>
                  <Button
                    size='sm'
                    variant='ghost'
                    className={cn('min-w-0 px-2', !asset && 'sm:col-span-2')}
                    onClick={() => props.onReuse(job)}
                  >
                    {t('Reuse settings')}
                  </Button>
                  {asset && (
                    <Button
                      size='sm'
                      variant='secondary'
                      className='min-w-0 px-2'
                      onClick={() => props.onReference(asset)}
                    >
                      {t('Use as reference')}
                    </Button>
                  )}
                </div>
              </div>
            </article>
          ))
        })}
      </div>
      <MediaPagination
        page={page}
        total={jobs.data?.total || 0}
        pageSize={12}
        disabled={jobs.isFetching}
        onChange={setPage}
      />
    </section>
  )
}
