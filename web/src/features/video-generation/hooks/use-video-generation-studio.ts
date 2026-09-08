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
import { nanoid } from 'nanoid'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  getVideoModels,
  getVideoTasks,
  submitVideoGeneration,
  uploadVideoAsset,
} from '../api'
import {
  MAX_VIDEO_REQUEST_COUNT,
  VIDEO_HIDDEN_TASKS_STORAGE_KEY,
  VIDEO_HISTORY_DAYS,
  VIDEO_MODELS_QUERY_KEY,
  VIDEO_TASKS_QUERY_KEY,
} from '../constants'
import {
  buildGenerationPayload,
  buildInitialValues,
  estimateVideoPrice,
  filterVideoHistoryTasks,
  validateAssetSelection,
  validateGeneration,
} from '../lib/video-logic'
import type {
  GenerationValidationIssue,
  VideoAsset,
  VideoFormValues,
  VideoGenerationModel,
  VideoGenerationPayload,
  VideoMediaRequirement,
  VideoTask,
} from '../types'

const TASK_POLL_INTERVAL_MS = 3_000
const EMPTY_MODELS: VideoGenerationModel[] = []
const EMPTY_TASKS: VideoTask[] = []

type HistoryVisibility = {
  hiddenTaskIds: Set<string>
  clearedBefore: number
}

function storedHistoryVisibility(): HistoryVisibility {
  try {
    const stored = window.localStorage.getItem(VIDEO_HIDDEN_TASKS_STORAGE_KEY)
    if (!stored) return { hiddenTaskIds: new Set(), clearedBefore: 0 }
    const values: unknown = JSON.parse(stored)
    if (Array.isArray(values)) {
      return {
        hiddenTaskIds: new Set(
          values
            .filter((value): value is string => typeof value === 'string')
            .slice(-200)
        ),
        clearedBefore: 0,
      }
    }
    if (!values || typeof values !== 'object') {
      return { hiddenTaskIds: new Set(), clearedBefore: 0 }
    }
    const hidden = 'hidden' in values ? values.hidden : []
    const clearedBeforeValue =
      'clearedBefore' in values ? Number(values.clearedBefore) : 0
    return {
      hiddenTaskIds: new Set(
        (Array.isArray(hidden) ? hidden : [])
          .filter((value): value is string => typeof value === 'string')
          .slice(-200)
      ),
      clearedBefore:
        Number.isFinite(clearedBeforeValue) && clearedBeforeValue > 0
          ? clearedBeforeValue
          : 0,
    }
  } catch {
    return { hiddenTaskIds: new Set(), clearedBefore: 0 }
  }
}

function persistHistoryVisibility(visibility: HistoryVisibility): void {
  try {
    window.localStorage.setItem(
      VIDEO_HIDDEN_TASKS_STORAGE_KEY,
      JSON.stringify({
        hidden: [...visibility.hiddenTaskIds].slice(-200),
        clearedBefore: visibility.clearedBefore,
      })
    )
  } catch {
    // Local history controls remain usable for the current session.
  }
}

type UseVideoGenerationStudioOptions = {
  initialModelName?: string
  onModelChange?: (modelName: string) => void
}

type SubmissionBatch = {
  payload: VideoGenerationPayload
  count: number
}

type SubmissionBatchResult = {
  submitted: number
  failed: number
}

function errorMessage(error: unknown): string | undefined {
  if (error instanceof Error && error.message.trim()) return error.message
  if (!error || typeof error !== 'object') return undefined
  const response = 'response' in error ? error.response : undefined
  if (!response || typeof response !== 'object') return undefined
  const data = 'data' in response ? response.data : undefined
  if (!data || typeof data !== 'object') return undefined
  const message = 'message' in data ? data.message : undefined
  return typeof message === 'string' && message.trim() ? message : undefined
}

export function useVideoGenerationStudio(
  options: UseVideoGenerationStudioOptions
) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [requestedGroup, setRequestedGroup] = useState('')
  const [selectedModelName, setSelectedModelName] = useState(
    options.initialModelName ?? ''
  )
  const [values, setValues] = useState<VideoFormValues>({ prompt: '' })
  const [assets, setAssets] = useState<VideoAsset[]>([])
  const [requestCount, setRequestCount] = useState(1)
  const [validationIssue, setValidationIssue] =
    useState<GenerationValidationIssue | null>(null)
  const [historyVisibility, setHistoryVisibility] = useState(
    storedHistoryVisibility
  )
  const [pollAfterSubmitUntil, setPollAfterSubmitUntil] = useState(0)
  const assetsRef = useRef<VideoAsset[]>([])
  const activeModelIdRef = useRef<string | undefined>(undefined)
  const pendingReuseRef = useRef<{
    modelName: string
    values: VideoFormValues
  } | null>(null)
  const uploadControllersRef = useRef(new Map<string, AbortController>())

  const modelsQuery = useQuery({
    queryKey: [...VIDEO_MODELS_QUERY_KEY, requestedGroup || 'self'],
    queryFn: () => getVideoModels(requestedGroup || undefined),
    retry: false,
    staleTime: 60_000,
  })
  const models = modelsQuery.data?.models ?? EMPTY_MODELS
  const selectedModel = useMemo(
    () =>
      models.find((model) => model.modelName === selectedModelName) ??
      models.find((model) => model.modelName === options.initialModelName) ??
      models[0] ??
      null,
    [models, options.initialModelName, selectedModelName]
  )
  const group =
    requestedGroup ||
    modelsQuery.data?.defaultGroup ||
    selectedModel?.defaultGroup ||
    ''

  const updateAssets = useCallback(
    (updater: (current: VideoAsset[]) => VideoAsset[]) => {
      setAssets((current) => {
        const next = updater(current)
        assetsRef.current = next
        return next
      })
    },
    []
  )

  const clearAssets = useCallback(() => {
    for (const controller of uploadControllersRef.current.values()) {
      controller.abort()
    }
    uploadControllersRef.current.clear()
    updateAssets((current) => {
      for (const asset of current) URL.revokeObjectURL(asset.previewUrl)
      return []
    })
  }, [updateAssets])

  useEffect(() => {
    if (!selectedModel || activeModelIdRef.current === selectedModel.id) return
    activeModelIdRef.current = selectedModel.id
    clearAssets()
    const initialValues = buildInitialValues(selectedModel)
    const pendingReuse = pendingReuseRef.current
    if (pendingReuse?.modelName === selectedModel.modelName) {
      setValues({ ...initialValues, ...pendingReuse.values })
      pendingReuseRef.current = null
    } else {
      setValues(initialValues)
    }
    setRequestCount(1)
    setValidationIssue(null)
  }, [clearAssets, selectedModel])

  useEffect(
    () => () => {
      for (const controller of uploadControllersRef.current.values()) {
        controller.abort()
      }
      for (const asset of assetsRef.current) {
        URL.revokeObjectURL(asset.previewUrl)
      }
    },
    []
  )

  const tasksQuery = useQuery({
    queryKey: VIDEO_TASKS_QUERY_KEY,
    queryFn: getVideoTasks,
    retry: false,
    refetchInterval: (query) =>
      query.state.data?.items.some(
        (task) => task.status === 'pending' || task.status === 'running'
      ) || Date.now() < pollAfterSubmitUntil
        ? TASK_POLL_INTERVAL_MS
        : false,
  })
  const allTasks = tasksQuery.data?.items ?? EMPTY_TASKS
  const tasks = useMemo(
    () =>
      filterVideoHistoryTasks(
        allTasks,
        historyVisibility.hiddenTaskIds,
        Date.now(),
        VIDEO_HISTORY_DAYS,
        historyVisibility.clearedBefore
      ),
    [allTasks, historyVisibility]
  )

  const submitMutation = useMutation({
    mutationFn: async (
      batch: SubmissionBatch
    ): Promise<SubmissionBatchResult> => {
      const attempts = Array.from({ length: batch.count }, () =>
        submitVideoGeneration(batch.payload)
      )
      const results = await Promise.allSettled(attempts)
      const submitted = results.filter(
        (result) => result.status === 'fulfilled'
      ).length
      const failed = results.length - submitted
      if (submitted === 0) {
        const firstFailure = results.find(
          (result): result is PromiseRejectedResult =>
            result.status === 'rejected'
        )
        throw (
          firstFailure?.reason ??
          new Error(t('Failed to submit video generation'))
        )
      }
      return { submitted, failed }
    },
    onSuccess: (result) => {
      setPollAfterSubmitUntil(Date.now() + 30_000)
      if (result.failed > 0) {
        toast.warning(
          t('{{count}} video tasks submitted; {{failed}} failed', {
            count: result.submitted,
            failed: result.failed,
          })
        )
        return
      }
      toast.success(
        t('{{count}} video tasks submitted', { count: result.submitted })
      )
    },
    onError: (error: unknown) => {
      toast.error(errorMessage(error) ?? t('Failed to submit video generation'))
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: VIDEO_TASKS_QUERY_KEY })
    },
  })

  const uploadAsset = useCallback(
    async (asset: VideoAsset, uploadGroup: string) => {
      const controller = new AbortController()
      uploadControllersRef.current.set(asset.id, controller)
      updateAssets((current) =>
        current.map((item) =>
          item.id === asset.id
            ? { ...item, status: 'uploading', progress: 0, error: undefined }
            : item
        )
      )
      try {
        const remoteUrl = await uploadVideoAsset(
          asset.file,
          uploadGroup || undefined,
          (progress) => {
            updateAssets((current) =>
              current.map((item) =>
                item.id === asset.id ? { ...item, progress } : item
              )
            )
          },
          controller.signal
        )
        updateAssets((current) =>
          current.map((item) =>
            item.id === asset.id
              ? { ...item, status: 'uploaded', progress: 100, remoteUrl }
              : item
          )
        )
      } catch (error) {
        if (controller.signal.aborted) return
        const message = errorMessage(error) ?? t('Upload failed')
        updateAssets((current) =>
          current.map((item) =>
            item.id === asset.id
              ? { ...item, status: 'failed', progress: 0, error: message }
              : item
          )
        )
        toast.error(t('Upload failed for {{name}}', { name: asset.file.name }))
      } finally {
        uploadControllersRef.current.delete(asset.id)
      }
    },
    [t, updateAssets]
  )

  const addFiles = useCallback(
    (requirement: VideoMediaRequirement, files: File[]) => {
      const currentCount = assetsRef.current.filter(
        (asset) => asset.requirementId === requirement.id
      ).length
      const selection = validateAssetSelection(files, currentCount, requirement)
      for (const failure of selection.rejected) {
        if (failure.code === 'unsupported_type') {
          toast.error(
            t('Unsupported file type: {{name}}', { name: failure.file.name })
          )
        } else if (failure.code === 'file_too_large') {
          toast.error(
            t('File exceeds the upload size limit: {{name}}', {
              name: failure.file.name,
            })
          )
        } else {
          toast.error(
            t('{{field}} accepts up to {{count}} files', {
              field: t(requirement.label),
              count: requirement.max,
            })
          )
        }
      }
      if (selection.accepted.length === 0) return
      const nextAssets = selection.accepted.map<VideoAsset>((file) => ({
        id: nanoid(),
        kind: requirement.kind,
        requirementId: requirement.id,
        file,
        previewUrl: URL.createObjectURL(file),
        status: 'queued',
        progress: 0,
      }))
      updateAssets((current) => [...current, ...nextAssets])
      setValidationIssue(null)
      for (const asset of nextAssets) void uploadAsset(asset, group)
    },
    [group, t, updateAssets, uploadAsset]
  )

  const removeAsset = useCallback(
    (assetId: string) => {
      uploadControllersRef.current.get(assetId)?.abort()
      uploadControllersRef.current.delete(assetId)
      updateAssets((current) => {
        const removed = current.find((asset) => asset.id === assetId)
        if (removed) URL.revokeObjectURL(removed.previewUrl)
        return current.filter((asset) => asset.id !== assetId)
      })
      setValidationIssue(null)
    },
    [updateAssets]
  )

  const retryAsset = useCallback(
    (assetId: string) => {
      const asset = assetsRef.current.find((item) => item.id === assetId)
      if (asset) void uploadAsset(asset, group)
    },
    [group, uploadAsset]
  )

  const selectModel = useCallback(
    (modelName: string) => {
      if (modelName === selectedModel?.modelName) return
      setSelectedModelName(modelName)
      options.onModelChange?.(modelName)
    },
    [options, selectedModel?.modelName]
  )

  const changeGroup = useCallback(
    (nextGroup: string) => {
      if (!nextGroup || nextGroup === group) return
      clearAssets()
      activeModelIdRef.current = undefined
      setRequestedGroup(nextGroup)
      setValidationIssue(null)
    },
    [clearAssets, group]
  )

  const changeField = useCallback(
    (key: string, value: string | number | boolean) => {
      setValues((current) => ({ ...current, [key]: value }))
      setValidationIssue(null)
    },
    []
  )

  const changeRequestCount = useCallback((count: number) => {
    setRequestCount(
      Math.min(MAX_VIDEO_REQUEST_COUNT, Math.max(1, Math.floor(count)))
    )
  }, [])

  const reuseTask = useCallback(
    (task: VideoTask) => {
      const targetModel = models.find(
        (model) => model.modelName === task.modelName
      )
      if (!targetModel) {
        toast.error(t('The model used by this task is no longer available.'))
        return
      }
      const reusedValues: VideoFormValues = {
        ...task.parameters,
        prompt: task.prompt ?? task.parameters.prompt ?? '',
      }
      clearAssets()
      setValidationIssue(null)
      if (selectedModel?.modelName === targetModel.modelName) {
        setValues({ ...buildInitialValues(targetModel), ...reusedValues })
      } else {
        pendingReuseRef.current = {
          modelName: targetModel.modelName,
          values: reusedValues,
        }
        setSelectedModelName(targetModel.modelName)
        options.onModelChange?.(targetModel.modelName)
      }
      toast.success(t('Generation parameters restored'))
    },
    [clearAssets, models, options, selectedModel?.modelName, t]
  )

  const hideTask = useCallback((taskId: string) => {
    setHistoryVisibility((current) => {
      const next = {
        ...current,
        hiddenTaskIds: new Set(current.hiddenTaskIds).add(taskId),
      }
      persistHistoryVisibility(next)
      return next
    })
  }, [])

  const clearHistory = useCallback(() => {
    setHistoryVisibility((current) => {
      const next = { ...current, clearedBefore: Date.now() }
      persistHistoryVisibility(next)
      return next
    })
  }, [])

  const generate = useCallback(() => {
    if (!selectedModel) return
    if (
      estimateVideoPrice(
        selectedModel,
        requestCount,
        Number(values.batch_size ?? 1)
      ) === null
    ) {
      toast.error(
        t('This model cannot be generated until a price is configured.')
      )
      return
    }
    const issue = validateGeneration(selectedModel, values, assetsRef.current)
    setValidationIssue(issue)
    if (issue) return
    const payload = buildGenerationPayload(
      selectedModel,
      values,
      assetsRef.current,
      group || undefined
    )
    submitMutation.mutate({ payload, count: requestCount })
  }, [group, requestCount, selectedModel, submitMutation, t, values])

  const hasActiveUploads = assets.some(
    (asset) => asset.status === 'queued' || asset.status === 'uploading'
  )
  const estimatedPrice = selectedModel
    ? estimateVideoPrice(
        selectedModel,
        requestCount,
        Number(values.batch_size ?? 1)
      )
    : null

  return {
    models,
    modelsError: modelsQuery.error,
    modelsLoading: modelsQuery.isLoading,
    refetchModels: modelsQuery.refetch,
    selectedModel,
    selectedModelName: selectedModel?.modelName ?? '',
    selectModel,
    group,
    groups: modelsQuery.data?.groups ?? [],
    changeGroup,
    estimateIsMaximum: modelsQuery.data?.estimateIsMaximum === true,
    values,
    changeField,
    assets,
    addFiles,
    removeAsset,
    retryAsset,
    requestCount,
    changeRequestCount,
    estimatedPrice,
    validationIssue,
    generate,
    submitting: submitMutation.isPending,
    hasActiveUploads,
    tasks,
    tasksError: tasksQuery.error,
    tasksLoading: tasksQuery.isLoading,
    tasksFetching: tasksQuery.isFetching,
    refetchTasks: tasksQuery.refetch,
    reuseTask,
    hideTask,
    clearHistory,
  }
}

export type VideoGenerationStudioState = ReturnType<
  typeof useVideoGenerationStudio
>
