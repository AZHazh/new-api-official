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
import { Cancel01Icon, Upload01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'

import type { ImageGenerator } from '../hooks/use-image-generator'
import { IMAGE_RATIOS, imageSize } from '../lib/image-form'
import { ImageAdvancedSettings } from './image-advanced-settings'
import { ProtectedImage } from './protected-image'

export function ImageGeneratorForm(props: { generator: ImageGenerator }) {
  const { t } = useTranslation()
  const generator = props.generator
  const form = generator.form
  const values = form.watch()
  const uploadInput = useRef<HTMLInputElement>(null)
  const capabilities = generator.model?.capabilities
  const disabled = generator.uploading || generator.submit.isPending
  return (
    <form
      onSubmit={generator.onSubmit}
      className='bg-card rounded-2xl border p-5 shadow-sm sm:p-7'
      onDragOver={(event) => event.preventDefault()}
      onDrop={(event) => {
        event.preventDefault()
        if (!disabled) {
          void generator.uploadReferences([...event.dataTransfer.files])
        }
      }}
    >
      <div className='mb-5 flex flex-wrap items-end gap-4'>
        <div className='min-w-40 flex-1 space-y-2'>
          <Label htmlFor='image-model'>{t('Model')}</Label>
          <NativeSelect
            id='image-model'
            className='w-full'
            value={values.model}
            disabled={disabled || !generator.catalog.data?.models.length}
            onChange={(event) => generator.selectModel(event.target.value)}
          >
            {!generator.catalog.data?.models.length && (
              <option value=''>{t('No image models available')}</option>
            )}
            {generator.catalog.data?.models.map((model) => (
              <option key={model.name} value={model.name}>
                {model.name}
              </option>
            ))}
          </NativeSelect>
        </div>
      </div>
      <Label htmlFor='image-prompt'>{t('Prompt')}</Label>
      <Textarea
        id='image-prompt'
        {...form.register('prompt')}
        placeholder={t('Describe the image you want to create')}
        className='bg-muted/40 mt-2 min-h-36 resize-y border-0 p-4 text-base shadow-none'
        aria-invalid={!!form.formState.errors.prompt}
        aria-describedby='image-prompt-error'
        maxLength={32000}
      />
      <p
        id='image-prompt-error'
        className='text-destructive mt-1 text-sm'
        role='alert'
      >
        {form.formState.errors.prompt &&
          t('Describe the image you want to create')}
      </p>
      {!!capabilities?.max_references && (
        <div className='mt-4'>
          <div className='mb-2 flex items-center justify-between gap-2 text-sm'>
            <span>{t('Reference images')}</span>
            <span className='text-muted-foreground'>
              {generator.references.length}/{capabilities.max_references}
            </span>
          </div>
          <div className='flex flex-wrap gap-2'>
            {generator.references.map((reference) => (
              <div
                key={reference.id}
                className='bg-muted relative flex size-20 items-center justify-center overflow-hidden rounded-lg border'
              >
                {reference.asset && (
                  <ProtectedImage
                    asset={reference.asset}
                    className='size-full object-cover'
                  />
                )}
                {reference.status === 'uploading' && (
                  <span className='text-xs' role='status'>
                    {t('Uploading...')}
                  </span>
                )}
                {reference.status === 'failed' && (
                  <span className='text-destructive px-1 text-center text-xs'>
                    {t('Upload failed')}
                  </span>
                )}
                <Button
                  type='button'
                  size='icon'
                  variant='secondary'
                  className='absolute top-0 right-0 size-6'
                  aria-label={t('Remove reference image')}
                  disabled={
                    reference.status === 'uploading' ||
                    generator.submit.isPending
                  }
                  onClick={() => generator.removeReference(reference.id)}
                >
                  <HugeiconsIcon icon={Cancel01Icon} size={14} />
                </Button>
              </div>
            ))}
            <Button
              type='button'
              variant='outline'
              className='h-20 border-dashed px-5'
              disabled={
                disabled ||
                generator.references.length >= capabilities.max_references
              }
              onClick={() => uploadInput.current?.click()}
            >
              <HugeiconsIcon icon={Upload01Icon} size={18} />
              {t('Add images')}
            </Button>
          </div>
          <input
            ref={uploadInput}
            type='file'
            accept='image/png,image/jpeg,image/webp'
            multiple
            className='sr-only'
            aria-label={t('Reference images')}
            disabled={disabled}
            onChange={(event) => {
              void generator.uploadReferences([...(event.target.files || [])])
              event.target.value = ''
            }}
          />
          <p className='text-muted-foreground mt-2 text-xs'>
            {t('Drop images here, or select from My assets. Up to 32 MB each.')}
          </p>
        </div>
      )}
      <div className='mt-5 grid grid-cols-2 gap-4 sm:grid-cols-3'>
        <div className='space-y-2'>
          <Label htmlFor='image-ratio'>{t('Aspect ratio')}</Label>
          <NativeSelect
            id='image-ratio'
            className='w-full'
            {...form.register('ratio')}
            value={values.ratio}
            disabled={!capabilities}
          >
            {(capabilities?.custom_size
              ? IMAGE_RATIOS
              : capabilities?.sizes || []
            ).map((ratio) => (
              <option key={ratio} value={ratio}>
                {{ auto: t('Auto'), custom: t('Custom') }[ratio] || ratio}
              </option>
            ))}
          </NativeSelect>
        </div>
        {(capabilities?.custom_size || !!capabilities?.resolutions?.length) && (
          <div className='space-y-2'>
            <Label htmlFor='image-resolution'>{t('Resolution')}</Label>
            <NativeSelect
              id='image-resolution'
              className='w-full'
              {...form.register('resolution')}
              disabled={
                capabilities.custom_size &&
                (values.ratio === 'auto' || values.ratio === 'custom')
              }
            >
              {(capabilities.resolutions?.length
                ? capabilities.resolutions
                : ['1K', '2K', '4K']
              ).map((resolution) => (
                <option key={resolution}>{resolution}</option>
              ))}
            </NativeSelect>
          </div>
        )}
        <div className='space-y-2'>
          <Label htmlFor='image-count'>{t('Image count')}</Label>
          <Input
            id='image-count'
            type='number'
            min={1}
            max={capabilities?.max_count || 1}
            {...form.register('n', { valueAsNumber: true })}
            aria-invalid={!!form.formState.errors.n}
          />
        </div>
        {values.ratio === 'custom' && capabilities?.custom_size && (
          <>
            <div className='space-y-2'>
              <Label htmlFor='image-width'>{t('Width (px)')}</Label>
              <Input
                id='image-width'
                type='number'
                min={256}
                max={3840}
                {...form.register('width', { valueAsNumber: true })}
                aria-invalid={!!form.formState.errors.width}
              />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='image-height'>{t('Height (px)')}</Label>
              <Input
                id='image-height'
                type='number'
                min={256}
                max={3840}
                {...form.register('height', { valueAsNumber: true })}
                aria-invalid={!!form.formState.errors.height}
              />
            </div>
          </>
        )}
      </div>
      {(form.formState.errors.n ||
        form.formState.errors.width ||
        form.formState.errors.height) && (
        <p className='text-destructive mt-2 text-sm' role='alert'>
          {t('Check the image count and dimensions')}
        </p>
      )}
      <ImageAdvancedSettings generator={generator} />
      <div className='mt-6 flex flex-wrap items-center justify-between gap-3'>
        <p className='text-muted-foreground text-xs'>
          {generator.model && imageSize(values, generator.model)} ·{' '}
          {t('Billed at the automatically selected group rate')}
        </p>
        <Button type='submit' size='lg' disabled={!generator.canSubmit}>
          {generator.submit.isPending
            ? t('Submitting...')
            : t('Generate images')}
        </Button>
      </div>
      {generator.catalog.isError && (
        <p className='text-destructive mt-3 text-sm' role='alert'>
          {t('Unable to load image models')}{' '}
          <Button
            type='button'
            variant='link'
            onClick={() => void generator.catalog.refetch()}
          >
            {t('Retry')}
          </Button>
        </p>
      )}
      {generator.catalog.isSuccess && !generator.catalog.data.models.length && (
        <p className='text-muted-foreground mt-3 text-sm'>
          {t('Ask an administrator to enable an image model for your group.')}
        </p>
      )}
    </form>
  )
}
