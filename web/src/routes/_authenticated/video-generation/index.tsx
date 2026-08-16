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
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useCallback } from 'react'
import { z } from 'zod'

import { Main } from '@/components/layout'
import { VideoGeneration } from '@/features/video-generation'
import { isSidebarModuleEnabled } from '@/lib/nav-modules'

const videoGenerationSearchSchema = z.object({
  model: z.string().optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/video-generation/')({
  beforeLoad: () => {
    if (!isSidebarModuleEnabled('chat', 'video_generation')) {
      throw redirect({ to: '/dashboard' })
    }
  },
  validateSearch: videoGenerationSearchSchema,
  component: VideoGenerationPage,
})

function VideoGenerationPage() {
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const handleModelChange = useCallback(
    (model: string) => {
      void navigate({
        replace: true,
        search: (previous) => ({ ...previous, model }),
      })
    },
    [navigate]
  )

  return (
    <Main className='p-0'>
      <VideoGeneration
        initialModelName={search.model}
        onModelChange={handleModelChange}
      />
    </Main>
  )
}
