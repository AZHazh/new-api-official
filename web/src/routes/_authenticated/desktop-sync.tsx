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
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { CheckCircle2, Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { API } from '@/lib/api-client'

export const Route = createFileRoute('/_authenticated/desktop-sync')({
  component: DesktopSyncPage,
  validateSearch: (search: Record<string, unknown>) => ({
    state: (search.state as string) || '',
    redirect_uri: (search.redirect_uri as string) || '',
  }),
})

interface Token {
  id: number
  name: string
  key: string
}

function DesktopSyncPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { state, redirect_uri } = Route.useSearch()
  const [tokens, setTokens] = useState<Token[]>([])
  const [selectedTokenId, setSelectedTokenId] = useState<string>('')
  const [loading, setLoading] = useState(true)
  const [authorizing, setAuthorizing] = useState(false)
  const [authorized, setAuthorized] = useState(false)

  useEffect(() => {
    if (!state || !redirect_uri) {
      toast.error(t('Invalid desktop sync callback, please restart sync from Evancod'))
      return
    }

    // Fetch user tokens
    API.get('/api/user/self')
      .then(async (res) => {
        if (res.data.success) {
          // Fetch tokens list
          const tokenRes = await API.get('/api/token?p=0&size=1000')
          if (tokenRes.data.success) {
            setTokens(tokenRes.data.data || [])
          }
        }
      })
      .catch(() => {
        toast.error(t('Failed to load tokens'))
      })
      .finally(() => {
        setLoading(false)
      })
  }, [state, redirect_uri, t])

  const handleAuthorize = async () => {
    if (!selectedTokenId) {
      toast.error(t('Please select a token'))
      return
    }

    const selectedToken = tokens.find((t) => t.id.toString() === selectedTokenId)
    if (!selectedToken) {
      return
    }

    setAuthorizing(true)

    try {
      await API.post('/api/desktop-sync/sessions', {
        state,
        token_name: selectedToken.name,
        token: selectedToken.key,
      })

      setAuthorized(true)
      toast.success(t('Authorization successful'))

      // Wait 2 seconds then redirect back
      setTimeout(() => {
        window.location.href = redirect_uri
      }, 2000)
    } catch (error) {
      toast.error(t('Authorization failed'))
      setAuthorizing(false)
    }
  }

  if (!state || !redirect_uri) {
    return (
      <div className='container max-w-2xl py-8'>
        <Card>
          <CardContent className='pt-6'>
            <p className='text-center text-muted-foreground'>
              {t('Invalid desktop sync callback, please restart sync from Evancod')}
            </p>
          </CardContent>
        </Card>
      </div>
    )
  }

  if (loading) {
    return (
      <div className='container max-w-2xl py-8'>
        <Card>
          <CardContent className='flex items-center justify-center py-12'>
            <Loader2 className='h-8 w-8 animate-spin text-muted-foreground' />
          </CardContent>
        </Card>
      </div>
    )
  }

  if (authorized) {
    return (
      <div className='container max-w-2xl py-8'>
        <Card>
          <CardContent className='flex flex-col items-center justify-center py-12'>
            <CheckCircle2 className='mb-4 h-16 w-16 text-green-500' />
            <h2 className='mb-2 text-xl font-semibold'>{t('Authorization Successful')}</h2>
            <p className='text-muted-foreground'>{t('Redirecting back to Evancod...')}</p>
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className='container max-w-2xl py-8'>
      <Card>
        <CardHeader>
          <CardTitle>{t('Authorize Evancod')}</CardTitle>
        </CardHeader>
        <CardContent className='space-y-4'>
          <div>
            <p className='mb-4 text-sm text-muted-foreground'>
              {t('Evancod is requesting to read your token list.')}
            </p>
            <p className='mb-4 text-sm text-muted-foreground'>
              {t('Please select a token to authorize:')}
            </p>
          </div>

          {tokens.length === 0 ? (
            <div className='rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground'>
              {t('No tokens available. Please create a token first.')}
            </div>
          ) : (
            <Select value={selectedTokenId} onValueChange={setSelectedTokenId}>
              <SelectTrigger>
                <SelectValue placeholder={t('Select a token')} />
              </SelectTrigger>
              <SelectContent>
                {tokens.map((token) => (
                  <SelectItem key={token.id} value={token.id.toString()}>
                    {token.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}

          <div className='flex gap-2 pt-4'>
            <Button
              onClick={() => navigate({ to: '/dashboard' })}
              variant='outline'
              className='flex-1'
            >
              {t('Cancel')}
            </Button>
            <Button
              onClick={handleAuthorize}
              disabled={!selectedTokenId || authorizing || tokens.length === 0}
              className='flex-1'
            >
              {authorizing ? (
                <>
                  <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                  {t('Authorizing...')}
                </>
              ) : (
                t('Authorize')
              )}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
