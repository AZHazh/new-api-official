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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { getImageCatalog, submitImageGeneration, uploadImage } from '../api'
import {
  buildImageRequest,
  IMAGE_FORM_DEFAULTS,
  imageFormSchema,
  parseImageParameters,
  type ImageFormValues,
} from '../lib/image-form'
import type { ImageGeneration, MediaAsset, ReferenceImage } from '../types'

export function useImageGenerator() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [references, setReferences] = useState<ReferenceImage[]>([])
  const referenceSlots = useRef(new Set<string>())
  const requestIdentity = useRef({ payload: '', key: '' })
  const form = useForm<ImageFormValues>({
    resolver: zodResolver(imageFormSchema),
    defaultValues: IMAGE_FORM_DEFAULTS,
  })
  const catalog = useQuery({
    queryKey: ['multimedia', 'models'],
    queryFn: getImageCatalog,
  })
  const modelName = form.watch('model')
  const model = catalog.data?.models.find((item) => item.name === modelName)

  function selectModel(name: string): void {
    const next = catalog.data?.models.find((item) => item.name === name)
    form.setValue('model', name)
    form.setValue(
      'ratio',
      next?.capabilities.custom_size
        ? '1:1'
        : next?.capabilities.sizes[0] || 'auto'
    )
    form.setValue('quality', next?.capabilities.qualities[0] || '')
    form.setValue('resolution', next?.capabilities.resolutions?.[0] || '1K')
    form.setValue('n', 1)
    const retained = references.slice(0, next?.capabilities.max_references || 0)
    referenceSlots.current = new Set(retained.map((item) => item.id))
    setReferences(retained)
  }

  useEffect(() => {
    const models = catalog.data?.models
    if (
      !models ||
      models.some((item) => item.name === form.getValues('model'))
    ) {
      return
    }
    const next = models[0]
    form.setValue('model', next?.name || '')
    form.setValue(
      'ratio',
      next?.capabilities.custom_size
        ? '1:1'
        : next?.capabilities.sizes[0] || 'auto'
    )
    form.setValue('quality', next?.capabilities.qualities[0] || '')
    form.setValue('resolution', next?.capabilities.resolutions?.[0] || '1K')
    form.setValue('n', 1)
    setReferences([])
    referenceSlots.current.clear()
  }, [catalog.data, form])

  async function uploadReferences(files: File[]): Promise<void> {
    if (!model) return
    const maximum = model.capabilities.max_references
    const remaining = maximum - referenceSlots.current.size
    if (files.length > remaining) {
      toast.error(t('Up to {{count}} reference images', { count: maximum }))
      return
    }
    if (
      files.some(
        (file) =>
          !['image/png', 'image/jpeg', 'image/webp'].includes(file.type) ||
          file.size > (catalog.data?.max_upload_bytes || 0)
      )
    ) {
      toast.error(t('Use PNG, JPEG or WebP images up to 32 MB'))
      return
    }
    // Reserve synchronously so overlapping drop/select events share the same limit.
    await Promise.allSettled(
      files.map(async (file) => {
        const id = crypto.randomUUID()
        referenceSlots.current.add(id)
        setReferences((items) => [
          ...items,
          { id, name: file.name, status: 'uploading' },
        ])
        try {
          const asset = await uploadImage(file)
          setReferences((items) =>
            items.map((item) =>
              item.id === id ? { ...item, asset, status: 'uploaded' } : item
            )
          )
          void client.invalidateQueries({ queryKey: ['multimedia', 'assets'] })
        } catch {
          setReferences((items) =>
            items.map((item) =>
              item.id === id ? { ...item, status: 'failed' } : item
            )
          )
        }
      })
    )
  }

  const submit = useMutation({
    mutationFn: submitImageGeneration,
    onSuccess: () => {
      requestIdentity.current = { payload: '', key: '' }
      void client.invalidateQueries({ queryKey: ['multimedia', 'generations'] })
      toast.success(t('Image generation queued'))
    },
  })
  const uploading = references.some((item) => item.status === 'uploading')
  const canSubmit =
    !!model &&
    !uploading &&
    !submit.isPending &&
    references.every((item) => item.status === 'uploaded')
  const onSubmit = form.handleSubmit(async (values) => {
    if (!model || !canSubmit) return
    const assets = references.flatMap((item) =>
      item.asset ? [item.asset] : []
    )
    const request = buildImageRequest(values, model, assets, '')
    const payload = JSON.stringify(request)
    if (requestIdentity.current.payload !== payload) {
      requestIdentity.current = { payload, key: crypto.randomUUID() }
    }
    request.request_key = requestIdentity.current.key
    try {
      await submit.mutateAsync(request)
    } catch {
      /* Global API error handling reports the failed submission. */
    }
  })

  function addReference(asset: MediaAsset): boolean {
    if (!model || references.length >= model.capabilities.max_references) {
      toast.error(
        t('Up to {{count}} reference images', {
          count: model?.capabilities.max_references || 0,
        })
      )
      return false
    }
    if (!references.some((item) => item.asset?.id === asset.id)) {
      referenceSlots.current.add(asset.id)
      setReferences((items) => [
        ...items,
        { id: asset.id, name: asset.name, asset, status: 'uploaded' },
      ])
    }
    return true
  }

  function reuseGeneration(job: ImageGeneration): void {
    // References may have expired, so reuse the editable prompt and supported settings.
    const parameters = parseImageParameters(job.parameters)
    const next = catalog.data?.models.find((item) => item.name === job.model)
    form.setValue('prompt', job.prompt)
    if (!next) return
    selectModel(next.name)
    referenceSlots.current.clear()
    setReferences([])
    form.setValue('n', Math.min(parameters.n || 1, next.capabilities.max_count))
    if (
      parameters.quality &&
      next.capabilities.qualities.includes(parameters.quality)
    ) {
      form.setValue('quality', parameters.quality)
    }
    if (
      parameters.resolution &&
      next.capabilities.resolutions?.includes(parameters.resolution)
    ) {
      form.setValue(
        'resolution',
        parameters.resolution as ImageFormValues['resolution']
      )
    }
    if (parameters.size && next.capabilities.sizes.includes(parameters.size)) {
      form.setValue('ratio', parameters.size)
    }
    if (
      parameters.size &&
      next.capabilities.custom_size &&
      parameters.size !== 'auto'
    ) {
      const [width, height] = parameters.size.split('x').map(Number)
      if (width && height) {
        form.setValue('ratio', 'custom')
        form.setValue('width', width)
        form.setValue('height', height)
      }
    }
    if (parameters.output_format) {
      form.setValue(
        'outputFormat',
        parameters.output_format as ImageFormValues['outputFormat']
      )
    }
    form.setValue('compression', parameters.output_compression ?? 100)
  }

  return {
    form,
    catalog,
    model,
    selectModel,
    references,
    uploading,
    canSubmit,
    submit,
    onSubmit,
    uploadReferences,
    addReference,
    reuseGeneration,
    removeReference: (id: string) => {
      referenceSlots.current.delete(id)
      setReferences((items) => items.filter((item) => item.id !== id))
    },
  }
}

export type ImageGenerator = ReturnType<typeof useImageGenerator>
