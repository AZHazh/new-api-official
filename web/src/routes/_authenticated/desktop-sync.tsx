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
import { useMemo, useState } from 'react'
import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { ShieldCheck, AlertTriangle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { useSystemConfig } from '@/hooks/use-system-config'
import { Button } from '@/components/ui/button'

const searchSchema = z.object({
  redirect_uri: z.string().optional(),
  state: z.string().optional(),
  mode: z.enum(['callback', 'polling']).optional().default('callback'),
})

function isValidRedirectURI(value?: string) {
  if (!value) return false
  try {
    const url = new URL(value)
    return (
      url.protocol === 'http:' &&
      (url.hostname === 'localhost' || url.hostname === '127.0.0.1') &&
      url.port !== '' &&
      url.pathname === '/callback' &&
      url.search === '' &&
      url.hash === '' &&
      url.username === '' &&
      url.password === ''
    )
  } catch {
    return false
  }
}

function DesktopSyncPage() {
  const { t } = useTranslation()
  const { redirect_uri: redirectURI, state, mode } = Route.useSearch()
  const [loading, setLoading] = useState(false)
  const [success, setSuccess] = useState(false)
  const { logo } = useSystemConfig()

  // 轮询模式：不需要 redirect_uri
  const isPollingMode = mode === 'polling'
  const validRedirectURI = useMemo(
    () => isPollingMode || isValidRedirectURI(redirectURI),
    [redirectURI, isPollingMode]
  )

  const handleAuthorize = async () => {
    if (!validRedirectURI) return
    setLoading(true)
    try {
      const res = await api.post('/api/desktop-sync/issue', {
        redirect_uri: redirectURI,
      })
      if (!res.data?.success || !res.data?.data?.code) return

      const code = res.data.data.code

      if (isPollingMode && state) {
        // 轮询模式：存储 code 到服务端
        await api.post('/api/desktop-sync/sessions', {
          state,
          code,
        })
        setSuccess(true)
        setLoading(false)
      } else if (redirectURI) {
        // 传统回调模式
        const callbackURL = new URL(redirectURI)
        callbackURL.searchParams.set('code', code)
        if (state) callbackURL.searchParams.set('state', state)
        window.location.href = callbackURL.toString()
      }
    } catch {
      toast.error(t('Failed to issue desktop sync code'))
      setLoading(false)
    }
  }

  return (
    <div className='bg-muted/40 flex min-h-[calc(100vh-4rem)] items-center justify-center p-6'>
      <div className='bg-card w-full max-w-md rounded-3xl border p-10 text-center shadow-xl'>
        <div className='relative mx-auto mb-7 size-[88px]'>
          <div className='bg-background flex size-[88px] items-center justify-center overflow-hidden rounded-[22px] shadow-md'>
            <img
              src={logo}
              alt={t('Logo')}
              className='size-full rounded-[22px] object-cover'
            />
          </div>
          <span
            className={`border-card absolute -right-1.5 -bottom-1.5 flex size-8 items-center justify-center rounded-full border-[3px] text-white ${
              validRedirectURI ? 'bg-blue-600' : 'bg-destructive'
            }`}
          >
            {validRedirectURI ? (
              <ShieldCheck className='size-4' />
            ) : (
              <AlertTriangle className='size-4' />
            )}
          </span>
        </div>

        <h1 className='text-foreground text-[26px] leading-tight font-bold'>
          {t('Authorize Evancod')}
        </h1>

        {success ? (
          <div className='border-green-600/30 bg-green-600/10 text-green-600 mt-5 rounded-xl border px-3.5 py-3 text-sm'>
            <p className='font-semibold'>
              {t('Authorization successful!')}
            </p>
            <p className='mt-2'>
              {t('Please return to VSCode to continue.')}
            </p>
          </div>
        ) : validRedirectURI ? (
          <p className='text-muted-foreground mt-2.5 text-[15px] leading-relaxed'>
            {t('Evancod is requesting permission to read your token list.')}
          </p>
        ) : (
          <p className='border-destructive/30 bg-destructive/10 text-destructive mt-5 rounded-xl border px-3.5 py-3 text-sm'>
            {t('Invalid desktop sync redirect URI.')}
          </p>
        )}

        {!success && (
          <Button
            type='button'
            size='lg'
            className='mt-7 h-12 w-full rounded-xl text-[15px] font-semibold'
            disabled={!validRedirectURI || loading}
            onClick={handleAuthorize}
          >
            {loading ? t('Authorizing...') : t('Authorize and continue')}
          </Button>
        )}

        {validRedirectURI && (
          <>
            <div className='bg-border my-7 h-px' />
            <p className='text-muted-foreground text-[13.5px] leading-relaxed'>
              {t(
                'A one-time temporary credential will be generated for this sync only. Your login information will not be saved.'
              )}
            </p>
          </>
        )}
      </div>
    </div>
  )
}

export const Route = createFileRoute('/_authenticated/desktop-sync')({
  component: DesktopSyncPage,
  validateSearch: searchSchema,
})
