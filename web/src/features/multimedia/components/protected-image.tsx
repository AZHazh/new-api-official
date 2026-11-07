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
import { Image01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

import { getImageBlob } from '../api'
import type { MediaAsset } from '../types'

export function ProtectedImage(props: {
  asset: MediaAsset
  original?: boolean
  className?: string
}) {
  const { t } = useTranslation()
  const [url, setUrl] = useState('')
  const query = useQuery({
    queryKey: [
      'multimedia',
      'content',
      props.asset.id,
      Boolean(props.original),
    ],
    queryFn: ({ signal }) =>
      getImageBlob(props.asset.id, !props.original, signal),
    staleTime: 60_000,
    gcTime: props.original ? 0 : 300_000,
    retry: false,
  })
  useEffect(() => {
    if (!query.data) {
      setUrl('')
      return
    }
    const objectUrl = URL.createObjectURL(query.data)
    setUrl(objectUrl)
    return () => URL.revokeObjectURL(objectUrl)
  }, [query.data])
  if (query.isError) {
    return (
      <div
        className={cn(
          'bg-muted text-muted-foreground flex items-center justify-center gap-2',
          props.className
        )}
      >
        <HugeiconsIcon icon={Image01Icon} size={20} />
        <span className='text-xs'>{t('Image unavailable')}</span>
      </div>
    )
  }
  if (!url || query.isPending) {
    return <Skeleton className={cn('size-full', props.className)} />
  }
  return (
    <img
      src={url}
      alt={props.asset.name}
      className={props.className}
      loading='lazy'
    />
  )
}
