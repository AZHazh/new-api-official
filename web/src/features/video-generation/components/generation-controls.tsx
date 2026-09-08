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
import { Add01Icon, MinusSignIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Slider } from '@/components/ui/slider'
import { Switch } from '@/components/ui/switch'

import { MAX_VIDEO_REQUEST_COUNT } from '../constants'
import type {
  VideoCapabilityField,
  VideoFormValues,
  VideoGenerationModel,
} from '../types'

function fieldLabel(t: TFunction, field: VideoCapabilityField): string {
  switch (field.key) {
    case 'metadata.ratio':
    case 'aspect_ratio':
      return t('Aspect ratio')
    case 'metadata.resolution':
    case 'resolution':
      return t('Resolution')
    case 'seconds':
    case 'duration':
    case 'metadata.duration':
      return t('Video duration')
    case 'metadata.generate_audio':
    case 'generate_audio':
      return t('Generate audio')
    case 'metadata.return_last_frame':
    case 'return_last_frame':
      return t('Return last frame')
    case 'batch_size':
      return t('Batch size')
    case 'metadata.seed':
      return t('Seed')
    case 'metadata.negative_prompt':
      return t('Negative prompt')
    case 'metadata.prompt_extend':
      return t('Prompt enhancement')
    case 'metadata.draft':
      return t('Draft mode')
    case 'metadata.safety_tolerance':
      return t('Safety tolerance')
    case 'metadata.draft_cache':
      return t('Draft cache')
    case 'metadata.script_name':
      return t('Script name')
    case 'metadata.type':
      return t('Generation type')
    case 'metadata.extend_from_task_id':
      return t('Source task ID')
    case 'metadata.sessionId':
      return t('Lip sync session ID')
    case 'metadata.faceId':
      return t('Face ID')
    case 'video_type':
      return t('Video type')
    case 'animate_mode':
      return t('Animation mode')
    case 'motion':
      return t('Motion')
    default:
      return field.label
  }
}

function DynamicField(props: {
  field: VideoCapabilityField
  value: string | number | boolean | undefined
  disabled: boolean
  onChange: (value: string | number | boolean) => void
}) {
  const { t } = useTranslation()
  const label = fieldLabel(t, props.field)
  if (props.field.widget === 'boolean') {
    return (
      <Field orientation='horizontal'>
        <div className='min-w-0 flex-1'>
          <FieldLabel htmlFor={`video-field-${props.field.key}`}>
            {label}
          </FieldLabel>
          {props.field.description && (
            <FieldDescription>{props.field.description}</FieldDescription>
          )}
        </div>
        <Switch
          id={`video-field-${props.field.key}`}
          checked={props.value === true}
          disabled={props.disabled}
          onCheckedChange={props.onChange}
        />
      </Field>
    )
  }
  if (props.field.widget === 'select' && props.field.options) {
    const items = props.field.options.map((option) => ({
      label: option.label,
      value: String(option.value),
    }))
    return (
      <Field>
        <FieldLabel htmlFor={`video-field-${props.field.key}`}>
          {label}
        </FieldLabel>
        <Select
          items={items}
          value={String(props.value ?? '')}
          disabled={props.disabled}
          onValueChange={(value) => {
            if (value === null) return
            const selectedOption = props.field.options?.find(
              (option) => String(option.value) === String(value)
            )
            props.onChange(selectedOption?.value ?? String(value))
          }}
        >
          <SelectTrigger
            id={`video-field-${props.field.key}`}
            className='w-full'
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              {items.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        {props.field.description && (
          <FieldDescription>{props.field.description}</FieldDescription>
        )}
      </Field>
    )
  }
  if (props.field.widget === 'slider') {
    const number = Number(props.value ?? props.field.min ?? 0)
    return (
      <Field>
        <div className='flex items-center justify-between gap-3'>
          <FieldLabel htmlFor={`video-field-${props.field.key}`}>
            {label}
          </FieldLabel>
          <span className='text-muted-foreground text-xs tabular-nums'>
            {number}
          </span>
        </div>
        <Slider
          id={`video-field-${props.field.key}`}
          value={number}
          min={props.field.min ?? 0}
          max={props.field.max ?? 100}
          step={props.field.step ?? 1}
          disabled={props.disabled}
          onValueChange={(value) => props.onChange(Number(value))}
          aria-label={label}
        />
        {props.field.description && (
          <FieldDescription>{props.field.description}</FieldDescription>
        )}
      </Field>
    )
  }
  return (
    <Field>
      <FieldLabel htmlFor={`video-field-${props.field.key}`}>
        {label}
      </FieldLabel>
      <Input
        id={`video-field-${props.field.key}`}
        type={props.field.widget === 'number' ? 'number' : 'text'}
        value={String(props.value ?? '')}
        min={props.field.min}
        max={props.field.max}
        step={props.field.step}
        pattern={props.field.pattern}
        disabled={props.disabled}
        required={props.field.required}
        onChange={(event) => {
          if (props.field.widget === 'number') {
            props.onChange(
              event.target.value === '' ? '' : Number(event.target.value)
            )
          } else {
            props.onChange(event.target.value)
          }
        }}
      />
      {props.field.description && (
        <FieldDescription>{props.field.description}</FieldDescription>
      )}
    </Field>
  )
}

export type GenerationSettingsProps = {
  model: VideoGenerationModel
  values: VideoFormValues
  group: string
  groups: string[]
  requestCount: number
  disabled: boolean
  onFieldChange: (key: string, value: string | number | boolean) => void
  onGroupChange: (group: string) => void
  onCountChange: (count: number) => void
}

export function GenerationSettings(props: GenerationSettingsProps) {
  const { t } = useTranslation()
  const groupItems = props.groups.map((group) => ({
    label: group,
    value: group,
  }))
  return (
    <FieldGroup>
      {groupItems.length > 1 && (
        <Field>
          <FieldLabel htmlFor='video-generation-group'>{t('Group')}</FieldLabel>
          <Select
            items={groupItems}
            value={props.group}
            disabled={props.disabled}
            onValueChange={(value) =>
              value !== null && props.onGroupChange(String(value))
            }
          >
            <SelectTrigger id='video-generation-group' className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {groupItems.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
      )}
      {props.model.fields.length > 0 && (
        <FieldSet>
          <FieldLegend variant='label'>{t('Video settings')}</FieldLegend>
          <FieldGroup>
            {props.model.fields.map((field) => (
              <DynamicField
                key={field.key}
                field={field}
                value={props.values[field.key]}
                disabled={props.disabled}
                onChange={(value) => props.onFieldChange(field.key, value)}
              />
            ))}
          </FieldGroup>
        </FieldSet>
      )}
      <Field>
        <FieldLabel>{t('Generation count')}</FieldLabel>
        <div className='flex h-9 items-center justify-between rounded-lg border px-1'>
          <Button
            type='button'
            variant='ghost'
            size='icon-sm'
            disabled={props.disabled || props.requestCount <= 1}
            onClick={() => props.onCountChange(props.requestCount - 1)}
            aria-label={t('Decrease generation count')}
          >
            <HugeiconsIcon icon={MinusSignIcon} strokeWidth={2} />
          </Button>
          <span className='text-sm font-medium tabular-nums'>
            {props.requestCount}
          </span>
          <Button
            type='button'
            variant='ghost'
            size='icon-sm'
            disabled={
              props.disabled || props.requestCount >= MAX_VIDEO_REQUEST_COUNT
            }
            onClick={() => props.onCountChange(props.requestCount + 1)}
            aria-label={t('Increase generation count')}
          >
            <HugeiconsIcon icon={Add01Icon} strokeWidth={2} />
          </Button>
        </div>
      </Field>
    </FieldGroup>
  )
}
