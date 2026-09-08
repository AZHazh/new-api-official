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
  Search01Icon,
  Tick02Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

import type { VideoGenerationModel } from '../types'

type ModelCatalogProps = {
  models: VideoGenerationModel[]
  selectedModelName: string
  onSelect: (modelName: string) => void
  className?: string
}

function ModelList(props: {
  models: VideoGenerationModel[]
  selectedModelName: string
  onSelect: (modelName: string) => void
}) {
  const { t } = useTranslation()
  if (props.models.length === 0) {
    return (
      <Empty className='min-h-52 border-0'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <HugeiconsIcon icon={Search01Icon} strokeWidth={2} />
          </EmptyMedia>
          <EmptyTitle>{t('No models match your search')}</EmptyTitle>
          <EmptyDescription>
            {t('Try a different model name or category.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  return (
    <div className='flex flex-col gap-1.5 p-1'>
      {props.models.map((model) => {
        const selected = model.modelName === props.selectedModelName
        return (
          <button
            key={model.id}
            type='button'
            aria-pressed={selected}
            className={cn(
              'hover:bg-muted/70 focus-visible:ring-ring/50 flex min-w-0 items-start gap-3 rounded-lg border border-transparent px-3 py-2.5 text-left outline-none transition-colors focus-visible:ring-3',
              selected && 'border-border bg-muted'
            )}
            onClick={() => props.onSelect(model.modelName)}
          >
            <span className='bg-background flex size-9 shrink-0 items-center justify-center rounded-lg border'>
              <HugeiconsIcon
                icon={AiVideoIcon}
                strokeWidth={1.8}
                className='size-5'
              />
            </span>
            <span className='min-w-0 flex-1'>
              <span className='flex items-center gap-2'>
                <span className='truncate text-sm font-medium'>
                  {model.displayName}
                </span>
                {model.featured && (
                  <Badge variant='secondary'>{t('Featured')}</Badge>
                )}
                {model.isNew && <Badge variant='outline'>{t('New')}</Badge>}
              </span>
              <span className='text-muted-foreground mt-0.5 line-clamp-2 text-xs leading-relaxed'>
                {model.description ||
                  model.family ||
                  t('Video generation model')}
              </span>
            </span>
            {selected && (
              <HugeiconsIcon
                icon={Tick02Icon}
                strokeWidth={2.2}
                className='text-primary mt-1 size-4 shrink-0'
              />
            )}
          </button>
        )
      })}
    </div>
  )
}

export function ModelCatalog(props: ModelCatalogProps) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase()
    if (!query) return props.models
    return props.models.filter((model) =>
      [
        model.modelName,
        model.displayName,
        model.description,
        model.family,
        ...model.tags,
      ]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(query))
    )
  }, [props.models, search])
  const featured = useMemo(() => {
    const explicit = filtered.filter((model) => model.featured)
    const source = explicit.length > 0 ? explicit : filtered
    return [...source]
      .sort((left, right) => right.recentCallCount - left.recentCallCount)
      .slice(0, 12)
  }, [filtered])
  const newest = useMemo(() => {
    const explicitlyNew = filtered.filter((model) => model.isNew)
    if (explicitlyNew.length > 0) return explicitlyNew
    return [...filtered].sort(
      (left, right) => (right.createdAt ?? 0) - (left.createdAt ?? 0)
    )
  }, [filtered])

  return (
    <section
      className={cn('flex min-h-0 flex-col', props.className)}
      aria-label={t('Video models')}
    >
      <div className='border-b p-3'>
        <h2 className='mb-2 text-sm font-semibold'>
          {t('Choose a video model')}
        </h2>
        <InputGroup>
          <InputGroupAddon>
            <HugeiconsIcon icon={Search01Icon} strokeWidth={2} />
          </InputGroupAddon>
          <InputGroupInput
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('Search video models...')}
            aria-label={t('Search video models')}
          />
        </InputGroup>
      </div>
      <Tabs defaultValue='featured' className='min-h-0 flex-1 gap-0'>
        <TabsList variant='line' className='mx-3 mt-2'>
          <TabsTrigger value='featured'>{t('Featured')}</TabsTrigger>
          <TabsTrigger value='all'>{t('All models')}</TabsTrigger>
          <TabsTrigger value='newest'>{t('Newest')}</TabsTrigger>
        </TabsList>
        <TabsContent value='featured' className='min-h-0'>
          <ScrollArea className='h-full'>
            <ModelList
              models={featured}
              selectedModelName={props.selectedModelName}
              onSelect={props.onSelect}
            />
          </ScrollArea>
        </TabsContent>
        <TabsContent value='all' className='min-h-0'>
          <ScrollArea className='h-full'>
            <ModelList
              models={filtered}
              selectedModelName={props.selectedModelName}
              onSelect={props.onSelect}
            />
          </ScrollArea>
        </TabsContent>
        <TabsContent value='newest' className='min-h-0'>
          <ScrollArea className='h-full'>
            <ModelList
              models={newest}
              selectedModelName={props.selectedModelName}
              onSelect={props.onSelect}
            />
          </ScrollArea>
        </TabsContent>
      </Tabs>
    </section>
  )
}

export function ModelPickerDialog(
  props: ModelCatalogProps & {
    open: boolean
    onOpenChange: (open: boolean) => void
  }
) {
  const { t } = useTranslation()
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='grid h-[min(760px,calc(100svh-2rem))] grid-rows-[auto_minmax(0,1fr)] gap-0 p-0 sm:max-w-3xl'>
        <DialogHeader className='border-b p-4'>
          <DialogTitle>{t('Choose a video model')}</DialogTitle>
          <DialogDescription>
            {t('Select from the video models available to your account.')}
          </DialogDescription>
        </DialogHeader>
        <ModelCatalog
          models={props.models}
          selectedModelName={props.selectedModelName}
          onSelect={(modelName) => {
            props.onSelect(modelName)
            props.onOpenChange(false)
          }}
        />
      </DialogContent>
    </Dialog>
  )
}
