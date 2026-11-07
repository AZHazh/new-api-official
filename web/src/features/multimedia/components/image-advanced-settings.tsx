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
import { useTranslation } from 'react-i18next'

import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'

import type { ImageGenerator } from '../hooks/use-image-generator'

export function ImageAdvancedSettings(props: { generator: ImageGenerator }) {
  const { t } = useTranslation()
  const generator = props.generator
  const form = generator.form
  const values = form.watch()
  const capabilities = generator.model?.capabilities
  if (!capabilities?.qualities.length && !capabilities?.advanced) {
    return null
  }
  const optionLabels: Record<string, string> = {
    auto: t('Auto'),
    low: t('Low'),
    medium: t('Medium'),
    high: t('High'),
    standard: t('Standard'),
    hd: t('HD'),
  }
  return (
    <details className='mt-5 border-t pt-4'>
      <summary className='focus-visible:ring-ring cursor-pointer text-sm font-medium focus-visible:ring-2'>
        {t('More settings')}
      </summary>
      <FieldGroup className='mt-4 grid grid-cols-2 gap-4 sm:grid-cols-3'>
        {!!capabilities?.qualities.length && (
          <Field>
            <FieldLabel htmlFor='image-quality'>{t('Quality')}</FieldLabel>
            <NativeSelect
              id='image-quality'
              className='w-full'
              {...form.register('quality')}
              value={values.quality}
            >
              {capabilities?.qualities.map((quality) => (
                <option key={quality} value={quality}>
                  {optionLabels[quality] || quality}
                </option>
              ))}
            </NativeSelect>
          </Field>
        )}
        {capabilities?.advanced && (
          <>
            <Field>
              <FieldLabel htmlFor='image-format'>
                {t('Output format')}
              </FieldLabel>
              <NativeSelect
                id='image-format'
                className='w-full'
                {...form.register('outputFormat')}
              >
                {['png', 'jpeg', 'webp'].map((format) => (
                  <option key={format} value={format}>
                    {format.toUpperCase()}
                  </option>
                ))}
              </NativeSelect>
            </Field>
            {values.outputFormat !== 'png' && (
              <Field data-invalid={!!form.formState.errors.compression}>
                <FieldLabel htmlFor='image-compression'>
                  {t('Output compression (0–100)')}
                </FieldLabel>
                <Input
                  id='image-compression'
                  type='number'
                  min={0}
                  max={100}
                  {...form.register('compression', { valueAsNumber: true })}
                  aria-invalid={!!form.formState.errors.compression}
                />
              </Field>
            )}
          </>
        )}
      </FieldGroup>
    </details>
  )
}
