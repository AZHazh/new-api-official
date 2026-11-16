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
import { useQuery } from '@tanstack/react-query'
import { ShieldCheck, AlertTriangle } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { clearDirectorAuthorization } from '@/features/auth/lib/director-authorization'
import { useSystemConfig } from '@/hooks/use-system-config'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import type { DesktopSyncSearch } from './search'

function isValidRedirectURI(value?: string, webClient = false): boolean {
  if (!value) return false
  try {
    const url = new URL(value)
    if (url.search || url.hash || url.username || url.password) return false
    const loopback = ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
    if (webClient) {
      return (
        url.pathname === '/auth/callback' &&
        (url.protocol === 'https:' || (url.protocol === 'http:' && loopback))
      )
    }
    return (
      url.protocol === 'http:' &&
      ['localhost', '127.0.0.1'].includes(url.hostname) &&
      url.port !== '' &&
      url.pathname === '/callback'
    )
  } catch {
    return false
  }
}

export function DesktopSyncAuthorization(props: { search: DesktopSyncSearch }) {
  const { t } = useTranslation()
  const search = props.search
  const redirectURI = search.redirect_uri
  const clientID = search.client_id
  const webClient = Boolean(clientID)
  const isPollingMode = !webClient && search.mode === 'polling'
  const validWebRequest =
    isValidRedirectURI(redirectURI, true) &&
    /^[a-zA-Z0-9_-]{1,80}$/.test(clientID || '') &&
    /^[a-zA-Z0-9_-]{43}$/.test(search.state || '') &&
    /^[a-zA-Z0-9_-]{43}$/.test(search.code_challenge || '') &&
    search.code_challenge_method === 'S256'
  const user = useAuthStore((s) => s.auth.user)
  const [loading, setLoading] = useState(false)
  const [success, setSuccess] = useState(false)
  const { logo } = useSystemConfig()
  const client = useQuery({
    queryKey: [
      'director-web-client',
      clientID,
      redirectURI,
      search.code_challenge,
      search.code_challenge_method,
    ],
    enabled: webClient && validWebRequest,
    retry: false,
    staleTime: 60000,
    queryFn: async () => {
      const response = await api.get('/api/desktop-sync/web-client', {
        params: {
          client_id: clientID,
          redirect_uri: redirectURI,
          code_challenge: search.code_challenge,
          code_challenge_method: search.code_challenge_method,
        },
      })
      if (
        !response.data?.success ||
        response.data.data?.redirect_uri !== redirectURI ||
        response.data.data?.client_id !== clientID
      ) {
        throw new Error('Invalid Cloud Director authorization request')
      }
      return true
    },
  })
  const validRedirectURI = webClient
    ? validWebRequest && client.isSuccess
    : isPollingMode || isValidRedirectURI(redirectURI)
  const checking = webClient && validWebRequest && client.isPending

  useEffect(() => {
    if (webClient) clearDirectorAuthorization()
  }, [webClient])

  const handleAuthorize = async (): Promise<void> => {
    if (!validRedirectURI) return
    setLoading(true)
    try {
      const res = await api.post('/api/desktop-sync/issue', {
        redirect_uri: redirectURI,
        ...(webClient
          ? {
              client_id: clientID,
              code_challenge: search.code_challenge,
              code_challenge_method: search.code_challenge_method,
            }
          : {}),
      })
      if (!res.data?.success || !res.data?.data?.code) return
      const code = res.data.data.code
      if (isPollingMode && search.state) {
        const stored = await api.post('/api/desktop-sync/sessions', {
          state: search.state,
          code,
        })
        if (stored.data?.success) setSuccess(true)
      } else if (redirectURI) {
        const callbackURL = new URL(redirectURI)
        callbackURL.searchParams.set('code', code)
        if (search.state) callbackURL.searchParams.set('state', search.state)
        window.location.assign(callbackURL.toString())
      }
    } catch {
      toast.error(t('Failed to issue desktop sync code'))
    } finally {
      setLoading(false)
    }
  }

  const handleCancel = (): void => {
    if (!webClient || !validRedirectURI || !redirectURI || !search.state) return
    const callback = new URL(redirectURI)
    callback.searchParams.set('error', 'access_denied')
    callback.searchParams.set('state', search.state)
    window.location.assign(callback.toString())
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
            className={`border-card absolute -right-1.5 -bottom-1.5 flex size-8 items-center justify-center rounded-full border-[3px] text-white ${validRedirectURI ? 'bg-blue-600' : 'bg-destructive'}`}
          >
            {validRedirectURI ? (
              <ShieldCheck className='size-4' />
            ) : (
              <AlertTriangle className='size-4' />
            )}
          </span>
        </div>
        <h1 className='text-foreground text-[26px] leading-tight font-bold'>
          {webClient ? t('Authorize Cloud Director') : t('Authorize Evancod')}
        </h1>
        {checking && (
          <p className='text-muted-foreground mt-5 text-sm' role='status'>
            {t('Checking authorization request...')}
          </p>
        )}
        {success && (
          <div className='mt-5 rounded-xl border border-green-600/30 bg-green-600/10 px-3.5 py-3 text-sm text-green-600'>
            <p className='font-semibold'>{t('Authorization successful!')}</p>
            <p className='mt-2'>{t('Please return to VSCode to continue.')}</p>
          </div>
        )}
        {!success && validRedirectURI && (
          <p className='text-muted-foreground mt-2.5 text-[15px] leading-relaxed'>
            {webClient
              ? t(
                  'Cloud Director is requesting permission to verify your account identity. Your API keys will not be shared.'
                )
              : t('Evancod is requesting permission to read your token list.')}
          </p>
        )}
        {!success && !validRedirectURI && !checking && (
          <p
            className='border-destructive/30 bg-destructive/10 text-destructive mt-5 rounded-xl border px-3.5 py-3 text-sm'
            role='alert'
          >
            {webClient
              ? t(
                  'Invalid Cloud Director authorization request. Please restart login from Cloud Director.'
                )
              : t('Invalid desktop sync redirect URI.')}
          </p>
        )}
        {webClient && validRedirectURI && (
          <div className='text-muted-foreground mt-4 space-y-2 text-sm'>
            <p>
              {t('Signed in as {{username}}', {
                username: user?.display_name || user?.username,
              })}
            </p>
            <p className='break-all'>
              {t('Return to {{origin}}', {
                origin: redirectURI ? new URL(redirectURI).origin : '',
              })}
            </p>
          </div>
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
        {webClient && validRedirectURI && (
          <Button
            type='button'
            variant='outline'
            className='mt-3 h-12 w-full rounded-xl'
            disabled={loading}
            onClick={handleCancel}
          >
            {t('Cancel and return')}
          </Button>
        )}
        {validRedirectURI && (
          <>
            <div className='bg-border my-7 h-px' />
            <p className='text-muted-foreground text-[13.5px] leading-relaxed'>
              {webClient
                ? t(
                    'Only your account ID, username, and display name will be shared. Your password and API keys stay on this site.'
                  )
                : t(
                    'A one-time temporary credential will be generated for this sync only. Your login information will not be saved.'
                  )}
            </p>
          </>
        )}
      </div>
    </div>
  )
}
