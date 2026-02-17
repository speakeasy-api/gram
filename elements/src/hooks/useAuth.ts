import { useReplayContext } from '@/contexts/ReplayContext'
import {
  hasExplicitSessionAuth,
  isAnyStaticSession,
  isDangerousApiKeyAuth,
  isStaticSessionAuth,
  isUnifiedFunctionSession,
  isUnifiedStaticSession,
} from '@/lib/auth'
import { isTokenExpired } from '@/lib/token'
import { useCallback, useMemo, useRef } from 'react'
import { ApiConfig, GetSessionFn } from '../types'
import { useSession } from './useSession'
import { useQueryClient } from '@tanstack/react-query'

declare const __GRAM_API_URL__: string | undefined

export type Auth =
  | {
      headers: Record<string, string>
      isLoading: false
      ensureValidHeaders: () => Promise<Record<string, string>>
    }
  | {
      headers?: Record<string, string>
      isLoading: true
      ensureValidHeaders: () => Promise<Record<string, string>>
    }

async function defaultGetSession(init: {
  projectSlug: string
}): Promise<string> {
  const response = await fetch('/chat/session', {
    method: 'POST',
    headers: {
      'Gram-Project': init.projectSlug,
    },
    body: JSON.stringify({
      embedOrigin: window.location.origin,
    }),
  })
  const data = await response.json()
  return data.client_token
}

function createDangerousApiKeySessionFn(
  apiKey: string,
  apiUrl: string
): GetSessionFn {
  return async ({ projectSlug }) => {
    const response = await fetch(`${apiUrl}/rpc/chatSessions.create`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Gram-Key': apiKey,
        'Gram-Project': projectSlug,
      },
      body: JSON.stringify({
        embed_origin: window.location.origin,
        user_identifier: 'elements-dev',
      }),
    })
    const data = await response.json()
    return data.client_token
  }
}

/**
 * Hook to fetch or retrieve the session token for the chat.
 * @returns Auth object with headers and ensureValidHeaders for pre-request token refresh
 */
export const useAuth = ({
  projectSlug,
  auth,
  refreshSession,
}: {
  auth?: ApiConfig
  projectSlug: string
  refreshSession?: boolean
}): Auth => {
  const replayCtx = useReplayContext()
  const isReplay = replayCtx?.isReplay ?? false
  const queryClient = useQueryClient()

  const apiUrl = useMemo(() => {
    const envUrl =
      typeof __GRAM_API_URL__ !== 'undefined' ? __GRAM_API_URL__ : undefined
    const url = auth?.url || envUrl || 'https://app.getgram.ai'
    return url.replace(/\/+$/, '')
  }, [auth?.url])

  const getSession = useMemo(() => {
    // In replay mode, skip session fetching entirely
    if (isReplay) {
      return null
    }
    // dangerousApiKey — exchange key for session via API
    if (isDangerousApiKeyAuth(auth)) {
      return createDangerousApiKeySessionFn(auth.dangerousApiKey, apiUrl)
    }
    // Unified session: static string
    if (isUnifiedStaticSession(auth)) {
      return () => Promise.resolve(auth.session)
    }
    // Unified session: function
    if (isUnifiedFunctionSession(auth)) {
      return auth.session
    }
    // Legacy: static sessionToken (deprecated)
    if (isStaticSessionAuth(auth)) {
      return () => Promise.resolve(auth.sessionToken)
    }
    // Legacy: explicit sessionFn (deprecated)
    if (hasExplicitSessionAuth(auth)) {
      return auth.sessionFn
    }
    return defaultGetSession
  }, [auth, isReplay, apiUrl])

  // The session request is only necessary if we are not using static session auth
  // configuration. If a custom session fetcher is provided, we use it,
  // otherwise we fallback to the default session fetcher
  const session = useSession({
    getSession,
    projectSlug,
  })

  const shouldRefresh =
    (refreshSession ?? true) && !isAnyStaticSession(auth) && !isReplay

  // Ref to deduplicate concurrent refresh calls
  const refreshPromiseRef = useRef<Promise<string> | null>(null)

  const queryKey = useMemo(() => ['chatSession', projectSlug], [projectSlug])

  const ensureValidHeaders = useCallback(async (): Promise<
    Record<string, string>
  > => {
    // Read the current cached token
    const cachedToken = queryClient.getQueryData<string>(queryKey)

    // If refresh is disabled, or no token to check, return current headers
    if (!shouldRefresh || !cachedToken || !getSession) {
      return {
        'Gram-Project': projectSlug,
        ...(cachedToken && { 'Gram-Chat-Session': cachedToken }),
      }
    }

    // Token is still valid — return current headers
    if (!isTokenExpired(cachedToken)) {
      return {
        'Gram-Project': projectSlug,
        'Gram-Chat-Session': cachedToken,
      }
    }

    // Token is expired — refresh, deduplicating concurrent calls
    if (!refreshPromiseRef.current) {
      refreshPromiseRef.current = getSession({ projectSlug }).finally(() => {
        refreshPromiseRef.current = null
      })
    }

    const freshToken = await refreshPromiseRef.current
    // Update the query cache so useSession consumers see the new token
    queryClient.setQueryData(queryKey, freshToken)

    return {
      'Gram-Project': projectSlug,
      'Gram-Chat-Session': freshToken,
    }
  }, [shouldRefresh, getSession, projectSlug, queryClient, queryKey])

  // In replay mode, return immediately without waiting for session
  if (isReplay) {
    return {
      headers: {},
      isLoading: false,
      ensureValidHeaders: async () => ({}),
    }
  }

  return !session
    ? {
        isLoading: true,
        ensureValidHeaders,
      }
    : {
        headers: {
          'Gram-Project': projectSlug,
          'Gram-Chat-Session': session,
        },
        isLoading: false,
        ensureValidHeaders,
      }
}
