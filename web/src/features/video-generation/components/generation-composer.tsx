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
  AiMagicIcon,
  AiVideoIcon,
  CameraVideoIcon,
  CoinsDollarIcon,
  Settings01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import type { TFunction } from 'i18next'
import { useCallback, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Popover,
  PopoverContent,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from '@/components/ui/popover'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatBillingCurrencyFromUSD } from '@/lib/currency'

import { optimizeVideoPrompt } from '../api'
import type { VideoGenerationStudioState } from '../hooks/use-video-generation-studio'
import { buildVideoAssetReferences, insertPromptText } from '../lib/video-logic'
import { AssetUploader } from './asset-uploader'
import { GenerationSettings } from './generation-controls'

const DIRECTOR_GENRES = [
  'General',
  'Commercial',
  'Film',
  'Anime',
  'Documentary',
  'Music video',
] as const
const DIRECTOR_STYLES = [
  'Auto',
  'Cinematic',
  'Photorealistic',
  'Animation',
  'Noir',
  'Vintage',
] as const
const DIRECTOR_CAMERAS = [
  'Auto',
  'Dolly in',
  'Orbit',
  'Handheld',
  'Aerial',
  'Static',
] as const

type DirectorGenre = (typeof DIRECTOR_GENRES)[number]
type DirectorStyle = (typeof DIRECTOR_STYLES)[number]
type DirectorCamera = (typeof DIRECTOR_CAMERAS)[number]

function directorOptionLabel(value: string, t: TFunction): string {
  switch (value) {
    case 'General':
      return t('General')
    case 'Commercial':
      return t('Commercial')
    case 'Film':
      return t('Film')
    case 'Anime':
      return t('Anime')
    case 'Documentary':
      return t('Documentary')
    case 'Music video':
      return t('Music video')
    case 'Auto':
      return t('Auto')
    case 'Cinematic':
      return t('Cinematic')
    case 'Photorealistic':
      return t('Photorealistic')
    case 'Animation':
      return t('Animation')
    case 'Noir':
      return t('Noir')
    case 'Vintage':
      return t('Vintage')
    case 'Dolly in':
      return t('Dolly in')
    case 'Orbit':
      return t('Orbit')
    case 'Handheld':
      return t('Handheld')
    case 'Aerial':
      return t('Aerial')
    case 'Static':
      return t('Static')
    default:
      return value
  }
}

function DirectorToggleGroup<T extends string>(props: {
  label: string
  options: readonly T[]
  value: T
  onChange: (value: T) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-col gap-1.5'>
      <p className='text-muted-foreground text-xs font-medium'>{props.label}</p>
      <ToggleGroup
        value={[props.value]}
        variant='outline'
        size='sm'
        spacing={1}
        className='flex w-full flex-wrap justify-start'
        aria-label={props.label}
        onValueChange={(values) => {
          const value = values[0] as T | undefined
          if (value) props.onChange(value)
        }}
      >
        {props.options.map((option) => (
          <ToggleGroupItem key={option} value={option}>
            {directorOptionLabel(option, t)}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
    </div>
  )
}

export function GenerationComposer(props: {
  studio: VideoGenerationStudioState
  validationMessage?: string
  onOpenModels: () => void
}) {
  const { t } = useTranslation()
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [directorOpen, setDirectorOpen] = useState(false)
  const [directorGenre, setDirectorGenre] = useState<DirectorGenre>('General')
  const [directorStyle, setDirectorStyle] = useState<DirectorStyle>('Auto')
  const [directorCamera, setDirectorCamera] = useState<DirectorCamera>('Auto')
  const [optimizing, setOptimizing] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const studio = props.studio
  const model = studio.selectedModel
  const assetReferences = useMemo(
    () => buildVideoAssetReferences(studio.assets),
    [studio.assets]
  )

  const insertIntoPrompt = useCallback(
    (text: string, append = false) => {
      if (!model?.promptSupported) return
      const prompt = String(studio.values.prompt ?? '')
      const textarea = textareaRef.current
      const start = append
        ? prompt.length
        : (textarea?.selectionStart ?? prompt.length)
      const end = append ? prompt.length : (textarea?.selectionEnd ?? start)
      const result = insertPromptText(prompt, text, start, end)
      studio.changeField('prompt', result.value)
      requestAnimationFrame(() => {
        textarea?.focus()
        textarea?.setSelectionRange(result.cursor, result.cursor)
      })
    },
    [model?.promptSupported, studio]
  )

  const optimizePrompt = useCallback(async () => {
    const prompt = String(studio.values.prompt ?? '').trim()
    if (!prompt) {
      toast.error(t('Enter a prompt to continue.'))
      return
    }
    setOptimizing(true)
    try {
      const optimized = await optimizeVideoPrompt(
        prompt,
        studio.group || undefined
      )
      const preservedReferences = assetReferences
        .map((reference) => reference.token)
        .filter((token) => !optimized.includes(token))
      const restoredPrompt =
        preservedReferences.length === 0
          ? optimized
          : `${optimized}\n\n${preservedReferences.join(' ')}`
      if (
        model.promptMaxLength &&
        restoredPrompt.length > model.promptMaxLength
      ) {
        throw new Error('optimized prompt exceeds model limit')
      }
      studio.changeField('prompt', restoredPrompt)
      requestAnimationFrame(() => {
        textareaRef.current?.focus()
        textareaRef.current?.setSelectionRange(
          restoredPrompt.length,
          restoredPrompt.length
        )
      })
      toast.success(t('Operation successful'))
    } catch {
      toast.error(t('Operation failed'))
    } finally {
      setOptimizing(false)
    }
  }, [assetReferences, model?.promptMaxLength, studio, t])

  if (!model) return null

  const generateDisabled =
    studio.submitting ||
    studio.hasActiveUploads ||
    studio.estimatedPrice === null
  const price =
    studio.estimatedPrice === null
      ? t('Unable to estimate')
      : formatBillingCurrencyFromUSD(studio.estimatedPrice, {
          digitsLarge: 4,
          digitsSmall: 6,
          abbreviate: false,
        })

  return (
    <>
      <section
        aria-label={t('Video creation controls')}
        className='bg-background/95 shrink-0 border-t supports-backdrop-filter:backdrop-blur-md'
      >
        <div className='mx-auto w-full max-w-5xl p-2 sm:p-3'>
          <div className='bg-background grid max-h-[52svh] grid-rows-[minmax(0,1fr)_auto] overflow-hidden rounded-lg border shadow-lg'>
            <div className='min-h-0 overflow-y-auto'>
              {model.media.length > 0 && (
                <div className='border-b p-3'>
                  <AssetUploader
                    requirements={model.media}
                    assets={studio.assets}
                    disabled={studio.submitting}
                    compact
                    onFiles={studio.addFiles}
                    onRemove={studio.removeAsset}
                    onRetry={studio.retryAsset}
                  />
                </div>
              )}

              {model.promptSupported && (
                <div className='flex flex-col gap-2 p-3'>
                  <div className='flex items-center justify-between gap-3'>
                    <label
                      htmlFor='video-generation-prompt'
                      className='text-sm font-medium'
                    >
                      {t('Prompt')}
                      {model.promptRequired && (
                        <span className='text-destructive' aria-hidden='true'>
                          {' '}
                          *
                        </span>
                      )}
                    </label>
                    {model.promptMaxLength && (
                      <span className='text-muted-foreground text-xs tabular-nums'>
                        {String(studio.values.prompt ?? '').length}/
                        {model.promptMaxLength}
                      </span>
                    )}
                  </div>
                  <Textarea
                    ref={textareaRef}
                    id='video-generation-prompt'
                    value={String(studio.values.prompt ?? '')}
                    maxLength={model.promptMaxLength}
                    required={model.promptRequired}
                    disabled={studio.submitting}
                    aria-invalid={
                      studio.validationIssue?.code === 'prompt_required'
                    }
                    placeholder={t(
                      'Describe the scene, motion, camera, and visual style...'
                    )}
                    className='min-h-20 resize-none sm:min-h-24'
                    onChange={(event) =>
                      studio.changeField('prompt', event.target.value)
                    }
                  />

                  <div className='flex min-h-8 flex-wrap items-center gap-1.5'>
                    {assetReferences.map((reference) => (
                      <Button
                        key={reference.assetId}
                        type='button'
                        variant='outline'
                        size='xs'
                        disabled={studio.submitting}
                        onClick={() => insertIntoPrompt(reference.token)}
                        aria-label={t('Insert {{reference}} into prompt', {
                          reference: reference.token,
                        })}
                      >
                        {reference.token}
                      </Button>
                    ))}

                    <Popover open={directorOpen} onOpenChange={setDirectorOpen}>
                      <PopoverTrigger
                        render={
                          <Button
                            type='button'
                            variant='ghost'
                            size='xs'
                            disabled={studio.submitting}
                          />
                        }
                      >
                        <HugeiconsIcon
                          icon={CameraVideoIcon}
                          strokeWidth={2}
                          data-icon='inline-start'
                        />
                        {t('Director controls')}
                      </PopoverTrigger>
                      <PopoverContent side='top' align='start' className='w-80'>
                        <PopoverHeader>
                          <PopoverTitle>{t('Director controls')}</PopoverTitle>
                        </PopoverHeader>
                        <DirectorToggleGroup
                          label={t('Genre')}
                          options={DIRECTOR_GENRES}
                          value={directorGenre}
                          onChange={setDirectorGenre}
                        />
                        <DirectorToggleGroup
                          label={t('Style')}
                          options={DIRECTOR_STYLES}
                          value={directorStyle}
                          onChange={setDirectorStyle}
                        />
                        <DirectorToggleGroup
                          label={t('Camera')}
                          options={DIRECTOR_CAMERAS}
                          value={directorCamera}
                          onChange={setDirectorCamera}
                        />
                        <Button
                          type='button'
                          size='sm'
                          className='w-full'
                          onClick={() => {
                            insertIntoPrompt(
                              `Director controls: Genre=${directorGenre}; Style=${directorStyle}; Camera=${directorCamera}.`,
                              true
                            )
                            setDirectorOpen(false)
                          }}
                        >
                          <HugeiconsIcon
                            icon={CameraVideoIcon}
                            strokeWidth={2}
                            data-icon='inline-start'
                          />
                          {t('Apply director controls')}
                        </Button>
                      </PopoverContent>
                    </Popover>

                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <Button
                            type='button'
                            variant='ghost'
                            size='xs'
                            disabled={studio.submitting || optimizing}
                            onClick={() => void optimizePrompt()}
                          />
                        }
                      >
                        {optimizing ? (
                          <Spinner data-icon='inline-start' />
                        ) : (
                          <HugeiconsIcon
                            icon={AiMagicIcon}
                            strokeWidth={2}
                            data-icon='inline-start'
                          />
                        )}
                        {t('Optimize')}
                      </TooltipTrigger>
                      <TooltipContent>{t('Optimize')}</TooltipContent>
                    </Tooltip>
                  </div>
                </div>
              )}
            </div>

            <div className='bg-muted/20 flex flex-wrap items-center gap-2 border-t p-2 sm:p-3'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                className='max-w-56 min-w-0'
                disabled={studio.modelsLoading || studio.models.length === 0}
                onClick={props.onOpenModels}
              >
                <HugeiconsIcon
                  icon={AiVideoIcon}
                  strokeWidth={2}
                  data-icon='inline-start'
                />
                <span className='truncate'>{model.displayName}</span>
              </Button>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={studio.submitting}
                onClick={() => setSettingsOpen(true)}
              >
                <HugeiconsIcon
                  icon={Settings01Icon}
                  strokeWidth={2}
                  data-icon='inline-start'
                />
                {t('Settings')}
              </Button>

              <div className='ml-auto min-w-24 text-right'>
                <p className='text-muted-foreground text-[11px]'>
                  {studio.estimateIsMaximum
                    ? t('Maximum estimated cost')
                    : t('Estimated cost')}
                </p>
                <p className='flex items-center justify-end gap-1 text-sm font-semibold tabular-nums'>
                  <HugeiconsIcon icon={CoinsDollarIcon} strokeWidth={1.8} />
                  {price}
                </p>
              </div>
              <Button
                type='button'
                size='sm'
                disabled={generateDisabled}
                onClick={studio.generate}
              >
                {studio.submitting ? (
                  <Spinner data-icon='inline-start' />
                ) : (
                  <HugeiconsIcon
                    icon={AiMagicIcon}
                    strokeWidth={2}
                    data-icon='inline-start'
                  />
                )}
                {studio.submitting ? t('Submitting...') : t('Generate video')}
              </Button>
              {props.validationMessage && (
                <p className='text-destructive w-full text-xs' role='alert'>
                  {props.validationMessage}
                </p>
              )}
            </div>
          </div>
        </div>
      </section>

      <Dialog open={settingsOpen} onOpenChange={setSettingsOpen}>
        <DialogContent className='grid max-h-[min(760px,calc(100svh-2rem))] grid-rows-[auto_minmax(0,1fr)] gap-0 p-0 sm:max-w-lg'>
          <DialogHeader className='border-b p-4'>
            <DialogTitle>{t('Generation settings')}</DialogTitle>
            <DialogDescription>
              {t('Settings are provided by the selected model.')}
            </DialogDescription>
          </DialogHeader>
          <div className='min-h-0 overflow-y-auto p-4'>
            <GenerationSettings
              model={model}
              values={studio.values}
              group={studio.group}
              groups={studio.groups}
              requestCount={studio.requestCount}
              disabled={studio.submitting}
              onFieldChange={studio.changeField}
              onGroupChange={studio.changeGroup}
              onCountChange={studio.changeRequestCount}
            />
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
