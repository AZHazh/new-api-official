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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'

import {
  downloadMediaAssets,
  getMediaAssets,
  removeMediaAssets,
  saveImageBlob,
} from '../api'
import type { MediaAsset } from '../types'
import { ProtectedImage } from './protected-image'

export function AssetGallery(props: {
  onReference: (asset: MediaAsset) => void
  onPreview: (asset: MediaAsset) => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [page, setPage] = useState(1)
  const [kind, setKind] = useState('')
  const [selection, setSelected] = useState<string[]>([])
  const [confirm, setConfirm] = useState(false)
  const assets = useQuery({
    queryKey: ['multimedia', 'assets', page, kind],
    queryFn: () => getMediaAssets(page, kind),
  })
  const items = assets.data?.items || []
  const selected = selection.filter((id) =>
    items.some((asset) => asset.id === id)
  )
  const remove = useMutation({
    mutationFn: removeMediaAssets,
    onSuccess: () => {
      setSelected([])
      setConfirm(false)
      if (page > 1 && selected.length === assets.data?.items.length) {
        setPage(page - 1)
      }
      void client.invalidateQueries({ queryKey: ['multimedia'] })
    },
  })
  const download = useMutation({
    mutationFn: downloadMediaAssets,
    onSuccess: (blob) => saveImageBlob(blob, 'images.zip'),
  })
  const busy = download.isPending || remove.isPending
  return (
    <section aria-label={t('My assets')}>
      <div className='mb-6 flex flex-wrap items-center justify-between gap-3'>
        <div>
          <h1 className='text-2xl font-semibold'>{t('My assets')}</h1>
          <p className='text-muted-foreground mt-2 text-sm'>
            {t(
              'Images are retained for {{days}} days. Download them before they expire.',
              { days: assets.data?.retention_days || 30 }
            )}
          </p>
        </div>
        <Button
          variant='outline'
          onClick={() => void assets.refetch()}
          disabled={assets.isFetching}
        >
          {t('Refresh')}
        </Button>
      </div>
      <div className='mb-5 flex flex-wrap items-center gap-3'>
        <NativeSelect
          aria-label={t('Asset type')}
          value={kind}
          onChange={(event) => {
            setKind(event.target.value)
            setPage(1)
            setSelected([])
          }}
        >
          <option value=''>{t('All images')}</option>
          <option value='generated'>{t('Generated images')}</option>
          <option value='reference'>{t('Reference images')}</option>
        </NativeSelect>
        <label className='flex cursor-pointer items-center gap-2 text-sm'>
          <input
            type='checkbox'
            checked={items.length > 0 && selected.length === items.length}
            disabled={!items.length || busy}
            onChange={(event) =>
              setSelected(
                event.target.checked ? items.map((asset) => asset.id) : []
              )
            }
          />
          {t('Select all')}
        </label>
        <span className='text-muted-foreground text-sm'>
          {t('{{count}} selected', { count: selected.length })}
        </span>
        <div className='flex gap-2 sm:ml-auto'>
          <Button
            size='sm'
            variant='outline'
            disabled={!selected.length || busy}
            onClick={() => download.mutate(selected)}
          >
            {download.isPending ? t('Downloading...') : t('Download')}
          </Button>
          <Button
            size='sm'
            variant='outline'
            disabled={!selected.length || busy}
            onClick={() => setConfirm(true)}
          >
            {t('Delete')}
          </Button>
        </div>
      </div>
      {assets.isPending && (
        <div className='grid grid-cols-2 gap-4 sm:grid-cols-3'>
          {[0, 1, 2].map((id) => (
            <Skeleton key={id} className='aspect-square rounded-xl' />
          ))}
        </div>
      )}
      {assets.isError && (
        <p role='alert' className='text-destructive py-12 text-center'>
          {t('Unable to load assets')}{' '}
          <Button variant='link' onClick={() => void assets.refetch()}>
            {t('Retry')}
          </Button>
        </p>
      )}
      {assets.isSuccess && !items.length && (
        <div className='text-muted-foreground rounded-xl border border-dashed px-6 py-20 text-center'>
          {t(
            'No images yet. Generate an image or upload a reference to get started.'
          )}
        </div>
      )}
      <div className='grid grid-cols-2 gap-4 lg:grid-cols-3'>
        {items.map((asset) => (
          <article
            key={asset.id}
            className='bg-card min-w-0 overflow-hidden rounded-xl border'
          >
            <div className='relative'>
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
              <input
                type='checkbox'
                className='accent-primary absolute top-3 left-3 size-4'
                aria-label={t('Select {{name}}', { name: asset.name })}
                checked={selected.includes(asset.id)}
                disabled={busy}
                onChange={(event) =>
                  setSelected((ids) =>
                    event.target.checked
                      ? [...ids, asset.id]
                      : ids.filter((id) => id !== asset.id)
                  )
                }
              />
            </div>
            <div className='space-y-2 p-3'>
              <p className='truncate text-sm font-medium' title={asset.name}>
                {asset.name}
              </p>
              <p className='text-muted-foreground text-xs'>
                {asset.width} × {asset.height} ·{' '}
                {(asset.bytes / 1024 / 1024).toFixed(1)} MB
              </p>
              <p className='text-muted-foreground text-xs'>
                {t('Expires {{date}}', {
                  date: new Date(asset.expires_at * 1000).toLocaleDateString(),
                })}
              </p>
              <Button
                size='sm'
                variant='secondary'
                className='w-full'
                onClick={() => props.onReference(asset)}
              >
                {t('Use as reference')}
              </Button>
            </div>
          </article>
        ))}
      </div>
      <MediaPagination
        page={page}
        total={assets.data?.total || 0}
        pageSize={18}
        disabled={assets.isFetching || busy}
        onChange={(next) => {
          setPage(next)
          setSelected([])
        }}
      />
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={t('Delete selected images?')}
        desc={t(
          'Deleted images cannot be restored. Download any images you want to keep first.'
        )}
        destructive
        confirmText={t('Delete')}
        isLoading={remove.isPending}
        handleConfirm={() => remove.mutate(selected)}
      />
    </section>
  )
}

export function MediaPagination(props: {
  page: number
  total: number
  pageSize: number
  disabled: boolean
  onChange: (page: number) => void
}) {
  const { t } = useTranslation()
  if (props.total <= props.pageSize && props.page === 1) return null
  return (
    <div className='mt-6 flex items-center justify-center gap-4'>
      <Button
        variant='outline'
        size='sm'
        disabled={props.disabled || props.page <= 1}
        onClick={() => props.onChange(props.page - 1)}
      >
        {t('Previous')}
      </Button>
      <span className='text-muted-foreground text-sm'>
        {props.page} / {Math.max(1, Math.ceil(props.total / props.pageSize))}
      </span>
      <Button
        variant='outline'
        size='sm'
        disabled={props.disabled || props.page * props.pageSize >= props.total}
        onClick={() => props.onChange(props.page + 1)}
      >
        {t('Next')}
      </Button>
    </div>
  )
}
