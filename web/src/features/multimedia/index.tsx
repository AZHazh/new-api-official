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
import { Folder01Icon, Image01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

import { getImageBlob, saveImageBlob } from './api'
import { AssetGallery } from './components/asset-gallery'
import { GenerationHistory } from './components/generation-history'
import { ImageGeneratorForm } from './components/image-generator-form'
import { ProtectedImage } from './components/protected-image'
import { useImageGenerator } from './hooks/use-image-generator'
import type { MediaAsset } from './types'

export function Multimedia() {
  const { t } = useTranslation()
  const [view, setView] = useState<'generate' | 'assets'>('generate')
  const [preview, setPreview] = useState<MediaAsset | null>(null)
  const generator = useImageGenerator()
  const download = useMutation({
    mutationFn: (asset: MediaAsset) =>
      getImageBlob(asset.id, false).then((blob) =>
        saveImageBlob(blob, asset.name)
      ),
  })
  function handleReference(asset: MediaAsset): void {
    if (generator.addReference(asset)) {
      setView('generate')
      setPreview(null)
      window.scrollTo({ top: 0, behavior: 'smooth' })
    }
  }
  return (
    <PublicLayout showMainContainer={false}>
      <MultimediaNavigation view={view} onChange={setView} />
      <main className='mx-auto min-h-svh max-w-5xl px-4 pt-28 pb-28 sm:px-8 md:pl-28 lg:pl-8'>
        <div hidden={view !== 'generate'}>
          <header className='mb-7 text-center'>
            <div className='bg-primary/10 text-primary mx-auto mb-4 flex size-12 items-center justify-center rounded-2xl'>
              <HugeiconsIcon icon={Image01Icon} size={25} />
            </div>
            <h1 className='text-2xl font-semibold tracking-tight sm:text-3xl'>
              {t('Turn your ideas into images')}
            </h1>
            <p className='text-muted-foreground mt-3 text-sm'>
              {t(
                'Create with a prompt, refine with references, and keep your work together.'
              )}
            </p>
          </header>
          <ImageGeneratorForm generator={generator} />
          <GenerationHistory
            submittedGeneration={generator.submit.data?.id}
            onPreview={setPreview}
            onReference={handleReference}
            onReuse={(job) => {
              generator.reuseGeneration(job)
              window.scrollTo({ top: 0, behavior: 'smooth' })
            }}
          />
        </div>
        {view === 'assets' && (
          <AssetGallery onReference={handleReference} onPreview={setPreview} />
        )}
      </main>
      <Dialog
        open={!!preview}
        onOpenChange={(open) => {
          if (!open) setPreview(null)
        }}
      >
        <DialogContent className='max-h-[90svh] overflow-auto sm:max-w-4xl'>
          <DialogHeader>
            <DialogTitle className='truncate pr-8'>
              {preview?.name || t('Image preview')}
            </DialogTitle>
          </DialogHeader>
          {preview && (
            <>
              <ProtectedImage
                asset={preview}
                original
                className='max-h-[65svh] w-full object-contain'
              />
              <div className='flex flex-wrap items-center gap-3'>
                <span className='text-muted-foreground mr-auto text-sm'>
                  {preview.width} × {preview.height}
                </span>
                <Button
                  variant='outline'
                  onClick={() => handleReference(preview)}
                >
                  {t('Use as reference')}
                </Button>
                <Button
                  disabled={download.isPending}
                  onClick={() => download.mutate(preview)}
                >
                  {download.isPending ? t('Downloading...') : t('Download')}
                </Button>
              </div>
            </>
          )}
        </DialogContent>
      </Dialog>
    </PublicLayout>
  )
}

export function MultimediaNavigation(props: {
  view: 'generate' | 'assets'
  onChange: (view: 'generate' | 'assets') => void
}) {
  const { t } = useTranslation()
  return (
    <nav
      aria-label={t('Multimedia')}
      className='bg-card/95 fixed bottom-5 left-1/2 z-30 flex -translate-x-1/2 gap-2 rounded-2xl border p-2 shadow-lg backdrop-blur md:top-1/2 md:bottom-auto md:left-5 md:-translate-x-0 md:-translate-y-1/2 md:flex-col'
    >
      {(
        [
          { id: 'generate', title: t('Image generation'), icon: Image01Icon },
          { id: 'assets', title: t('My assets'), icon: Folder01Icon },
        ] as const
      ).map((item) => (
        <Button
          key={item.id}
          variant={props.view === item.id ? 'secondary' : 'ghost'}
          className={cn(
            'h-auto min-w-20 flex-col gap-2 px-3 py-3 text-xs',
            props.view === item.id && 'text-primary'
          )}
          aria-current={props.view === item.id ? 'page' : undefined}
          onClick={() => props.onChange(item.id)}
        >
          <HugeiconsIcon icon={item.icon} size={21} />
          {item.title}
        </Button>
      ))}
    </nav>
  )
}
